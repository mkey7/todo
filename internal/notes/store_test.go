package notes

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"todo/internal/db"
	"todo/internal/query"
)

func document() Document {
	return Document{SchemaVersion: 1, Context: Context{Range: Range{Mode: "none"}, TagIDs: []int64{}}, Blocks: []Block{{ID: ID(), Type: "paragraph", Text: "<script>纯文本</script>"}}}
}
func TestNoteLifecycleAndImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	n, err := Create(ctx, d, document())
	if err != nil {
		t.Fatal(err)
	}
	doc := *n.Document
	doc.Title = "100%_总结"
	n, err = Update(ctx, d, n.ID, n.Revision, doc)
	if err != nil || n.Revision != 2 {
		t.Fatal(n, err)
	}
	if _, err = Update(ctx, d, n.ID, 1, doc); err == nil {
		t.Fatal("stale write accepted")
	}
	list, err := List(ctx, d, 50, 0, "%_", false)
	if err != nil || len(list.Items) != 1 || list.Items[0].Document != nil {
		t.Fatal(list, err)
	}
	n, err = SetDeleted(ctx, d, n.ID, n.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Get(ctx, d, n.ID); err == nil {
		t.Fatal("deleted note visible")
	}
	n, err = SetDeleted(ctx, d, n.ID, n.Revision, false)
	if err != nil || n.Revision != 4 {
		t.Fatal(n, err)
	}
	_, err = d.Exec(`INSERT INTO daily_summaries(date,content) VALUES('2026-10-30',?)`, strings.Repeat("笔", 20001))
	if err != nil {
		t.Fatal(err)
	}
	imported, created, err := Import(ctx, d, "daily", "2026-10-30")
	if err != nil || !created || len(imported.Document.Blocks) != 2 {
		t.Fatal(imported, created, err)
	}
	again, created, err := Import(ctx, d, "daily", "2026-10-30")
	if err != nil || created || again.ID != imported.ID {
		t.Fatal(again, created, err)
	}
	if _, _, err = Import(ctx, d, "weekly", "2026-W99"); err == nil {
		t.Fatal("invalid week accepted")
	}
	d.Close()
	d, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	again, err = Get(ctx, d, imported.ID)
	if err != nil || again.Document.Blocks[0].Text != imported.Document.Blocks[0].Text {
		t.Fatal(err)
	}
	var original string
	if err = d.QueryRow(`SELECT content FROM daily_summaries WHERE date='2026-10-30'`).Scan(&original); err != nil || len([]rune(original)) != 20001 {
		t.Fatal("legacy content changed", err)
	}
}
func TestDocumentValidationAndDraft(t *testing.T) {
	doc := document()
	doc.Blocks = append(doc.Blocks, Block{ID: ID(), Type: "query", Definition: &Definition{Kind: "function", Name: "future", Version: 9}})
	if err := Validate(doc); err != nil {
		t.Fatal("draft rejected", err)
	}
	doc.Blocks[1].ID = doc.Blocks[0].ID
	if err := Validate(doc); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	raw, _ := json.Marshal(document())
	var out Document
	if err := Strict(append(raw, []byte(` {}`)...), &out); err == nil {
		t.Fatal("trailing JSON")
	}
	if err := Strict([]byte(`{"schema_version":1,"html":"bad"}`), &out); err == nil {
		t.Fatal("unknown key")
	}
}

func TestConcurrentCASHasOneWinner(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	n, err := Create(ctx, d, document())
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		n   Note
		err error
	}
	results := make(chan outcome, 2)
	for _, title := range []string{"first tab", "second tab"} {
		go func(title string) {
			doc := document()
			doc.Title = title
			got, e := Update(ctx, d, n.ID, n.Revision, doc)
			results <- outcome{got, e}
		}(title)
	}
	successes := 0
	var winner Note
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err == nil {
			successes++
			winner = r.n
		} else if e, ok := r.err.(*Error); !ok || e.Code != "revision_conflict" {
			t.Fatal(r.err)
		}
	}
	saved, err := Get(ctx, d, n.ID)
	if err != nil || successes != 1 || saved.Revision != 2 || saved.Document.Title != winner.Document.Title {
		t.Fatal(saved, winner, successes, err)
	}
}

func TestWeeklyImportDeletedSourceAndDocumentLimits(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	if _, err = d.Exec(`INSERT INTO weekly_summaries(week,content) VALUES('2020-W53','跨年周报')`); err != nil {
		t.Fatal(err)
	}
	n, created, err := Import(ctx, d, "weekly", "2020-W53")
	if err != nil || !created || n.Document.Context.Range.Start != "2020-12-28" || n.Document.Context.Range.End != "2021-01-04" {
		t.Fatal(n, err)
	}
	if _, err = SetDeleted(ctx, d, n.ID, n.Revision, true); err != nil {
		t.Fatal(err)
	}
	again, created, err := Import(ctx, d, "weekly", "2020-W53")
	if err != nil || created || again.ID != n.ID || again.DeletedAt == nil {
		t.Fatal(again, err)
	}
	cases := []Document{}
	doc := document()
	doc.Title = strings.Repeat("笔", 201)
	cases = append(cases, doc)
	doc = document()
	doc.Blocks[0].Text = strings.Repeat("笔", 20001)
	cases = append(cases, doc)
	doc = document()
	for i := 0; i < 31; i++ {
		doc.Blocks = append(doc.Blocks, Block{ID: ID(), Type: "query"})
	}
	cases = append(cases, doc)
	doc = document()
	for i := 0; i < 200; i++ {
		doc.Blocks = append(doc.Blocks, Block{ID: ID(), Type: "paragraph"})
	}
	cases = append(cases, doc)
	doc = document()
	doc.Context.Range = Range{Mode: "fixed", Start: "2026-02-30", End: "2026-03-02"}
	cases = append(cases, doc)
	for i, doc := range cases {
		if err = Validate(doc); err == nil {
			t.Fatalf("case %d limit accepted", i)
		}
	}
}

func TestV2DocumentRoundTripAndV1Upgrade(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	old := document()
	old.Blocks = append(old.Blocks, Block{ID: ID(), Type: "query", Definition: &Definition{Kind: "custom", Query: &query.QueryRequest{Source: "todos", Select: []query.SelectItem{{Aggregate: "count"}}}}, View: &View{Type: "table"}})
	n, err := Create(ctx, d, old)
	if err != nil {
		t.Fatal(err)
	}
	if n.Document.SchemaVersion != 1 || n.Document.Blocks[1].QueryMode != "aggregate" {
		t.Fatal(n)
	}
	var stored string
	if err = d.QueryRow(`SELECT document_json FROM notes WHERE id=?`, n.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "query_mode") {
		t.Fatal("reading v1 rewrote stored document")
	}
	next := *n.Document
	next.SchemaVersion = 2
	next.Blocks[1] = Block{ID: old.Blocks[1].ID, Type: "query", QueryMode: "detail", Definition: &Definition{Kind: "custom", Search: &Search{Text: "开发", Fields: []string{"title"}}, Query: &query.QueryRequest{Source: "todos"}}, View: &View{Type: "table", Options: Options{Group: &query.DisplayGroup{Field: "title", Direction: "asc"}, Summaries: []query.Metric{{ID: "n", Op: "count_all"}}}}}
	saved, err := Update(ctx, d, n.ID, n.Revision, next)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Document.SchemaVersion != 2 || saved.Document.Blocks[1].View.Options.Group.Field != "title" || saved.Document.Blocks[1].Definition.Search.Text != "开发" {
		t.Fatal(saved)
	}
	next.Blocks[1].View.Options.Summaries = append(next.Blocks[1].View.Options.Summaries, query.Metric{ID: "n", Op: "sum", Field: "priority"})
	if err = Validate(next); err == nil {
		t.Fatal("duplicate metric saved")
	}
}
