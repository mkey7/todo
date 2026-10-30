package notefunc

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"
	"todo/internal/db"
	"todo/internal/notes"
	"todo/internal/query"
)

func fixture(t *testing.T) (*sql.DB, Executor) {
	t.Helper()
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.Local)
	return d, Executor{DB: d, Now: func() time.Time { return now }}
}
func req(name string) Request {
	return Request{Definition: notes.Definition{Kind: "function", Name: name, Version: 1, Params: notes.Params{Range: notes.Range{Mode: "inherit"}, TagIDs: notes.Tags{Mode: "inherit"}}}, Context: notes.Context{Range: notes.Range{Mode: "fixed", Start: "2026-10-26", End: "2026-11-02"}}, View: notes.View{Type: "table"}}
}
func exec(t *testing.T, e Executor, r Request) Response {
	t.Helper()
	out, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestFunctionSemantics(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`INSERT INTO tags(id,name) VALUES(1,'进行中'),(2,'已完成'),(3,'项目');INSERT INTO todos(id,title,created_at) VALUES(1,'双状态','2026-10-27 09:00:00'),(2,'待办','2026-10-27 09:00:00');INSERT INTO todo_tags(todo_id,tag_id) VALUES(1,1),(1,2),(1,3);INSERT INTO time_entries(todo_id,tag_id,start_time,end_time) VALUES(1,3,'2026-10-25 23:00:00','2026-10-26 01:00:00'),(1,3,'2026-10-26 23:30:00','2026-10-27 00:30:00'),(NULL,NULL,'2026-10-30 11:00:00',NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	r := req("todos")
	r.Definition.Params.Status = "done"
	out := exec(t, e, r)
	if out.Result.TotalCount != 1 || out.Result.Rows[0]["status"] != "done" {
		t.Fatal(out)
	}
	r = req("time_stats")
	out = exec(t, e, r)
	if math.Abs(out.Result.Rows[0]["duration_seconds"].(float64)-7200) > .01 {
		t.Fatal(out)
	}
	r.Definition.Params.GroupBy = "day"
	out = exec(t, e, r)
	if out.Result.TotalCount != 2 || out.Result.Rows[0]["duration_seconds"] != float64(5400) {
		t.Fatal(out)
	}
	r.Definition.Params.IncludeRunning = true
	r.Definition.Params.GroupBy = "total"
	out = exec(t, e, r)
	if out.Result.Rows[0]["duration_seconds"] != float64(10800) {
		t.Fatal(out)
	}
	r = req("time_entries")
	out = exec(t, e, r)
	if out.Result.TotalCount != 2 {
		t.Fatal(out)
	}
	r.Definition.Params.TagIDs = notes.Tags{Mode: "explicit", Values: []int64{999}}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("missing tag ignored")
	}
	r = req("time_stats")
	r.Definition.Params.GroupBy = "bad"
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("invalid group")
	}
}
func TestEventCompletenessAndCustom(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<501) INSERT INTO time_entries(start_time,end_time) SELECT '2026-10-27 09:00:00','2026-10-27 10:00:00' FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	r := req("time_entries")
	r.View = notes.View{Type: "timeline", Mapping: Catalog()[1].Mapping}
	r.Viewport = &Window{"2026-10-26", "2026-11-02"}
	out := exec(t, e, r)
	if out.Result.RowCount != 501 || !out.Meta.Complete {
		t.Fatal(out.Result.RowCount)
	}
	custom := r
	custom.Definition = notes.Definition{Kind: "custom", Query: &query.QueryRequest{Source: "time_entries", Select: []query.SelectItem{{Field: "id"}, {Field: "note", Alias: "title"}, {Field: "start_time"}, {Field: "end_time"}}}}
	out = exec(t, e, custom)
	if out.Result.RowCount != 501 {
		t.Fatal(out.Result.RowCount)
	}
	_, err = d.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<4500) INSERT INTO time_entries(start_time,end_time) SELECT '2026-10-27 09:00:00','2026-10-27 10:00:00' FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []Request{r, custom} {
		if _, err = e.Execute(context.Background(), test); err == nil {
			t.Fatal("truncated calendar accepted")
		}
	}
	r.View.Type = "table"
	out = exec(t, e, r)
	if out.Result.RowCount != 50 || out.Result.TotalCount != 5001 || *out.Result.NextOffset != 50 {
		t.Fatal(out.Result)
	}
	r = req("todos")
	r.View = notes.View{Type: "timeline", Mapping: notes.Mapping{Title: "title", Start: "due_date", End: "completed_at"}}
	r.Viewport = &Window{"2026-10-26", "2026-11-02"}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("date-only timeline")
	}
}
func TestClockBoundariesAndCancellation(t *testing.T) {
	d, e := fixture(t)
	r := req("time_entries")
	r.Definition.Params.Range = notes.Range{Mode: "relative", Preset: "this_week"}
	out := exec(t, e, r)
	if out.Meta.EffectiveRange.Start != "2026-10-26" {
		t.Fatal(out.Meta)
	}
	r.Definition.Params.Range = notes.Range{Mode: "fixed", Start: "2026-02-30", End: "2026-03-03"}
	if _, err := e.Execute(context.Background(), r); err == nil {
		t.Fatal("invalid date")
	}
	_, err := d.Exec(`INSERT INTO time_entries(start_time,end_time) VALUES('2026-10-26 00:00:00','2026-10-27 00:00:00'),('2026-10-25 23:00:00','2026-10-26 00:00:00'),('2026-10-26 13:00:00','2026-10-26 12:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	r = req("time_entries")
	r.Definition.Params.Range = notes.Range{Mode: "fixed", Start: "2026-10-26", End: "2026-10-27"}
	out = exec(t, e, r)
	if out.Result.TotalCount != 1 || len(out.Meta.Warnings) != 1 {
		t.Fatal(out)
	}
	conn, err := d.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = e.Execute(ctx, r); err == nil {
		t.Fatal("cancellation ignored")
	}
	conn.Close()
	exec(t, e, r)
}

func TestMissingDatesAndDisjointViewport(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`INSERT INTO todos(title,created_at) VALUES('no due','2026-10-27 09:00:00');INSERT INTO time_entries(start_time,end_time) VALUES('2026-01-01 00:00:00','2027-01-01 00:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	r := req("todos")
	r.View = notes.View{Type: "calendar", Mapping: notes.Mapping{Title: "title", Date: "due_date"}}
	r.Viewport = &Window{"2026-10-26", "2026-11-02"}
	out := exec(t, e, r)
	if out.Result.RowCount != 0 || len(out.Meta.Warnings) != 1 || out.Meta.Warnings[0].Count != 1 {
		t.Fatal(out)
	}
	r = req("time_entries")
	r.View = notes.View{Type: "timeline", Mapping: Catalog()[1].Mapping}
	r.Viewport = &Window{"2026-11-09", "2026-11-16"}
	out = exec(t, e, r)
	if out.Result.RowCount != 0 {
		t.Fatal("disjoint ranges returned long event", out)
	}
	r = req("time_stats")
	r.Definition.Params.GroupBy = "day"
	_, err = d.Exec(`INSERT INTO time_entries(start_time,end_time) VALUES('2026-10-27 10:00:00','2026-10-27 09:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	out = exec(t, e, r)
	if len(out.Meta.Warnings) != 1 {
		t.Fatal(out)
	}
	custom := req("todos")
	custom.Definition = notes.Definition{Kind: "custom", Query: &query.QueryRequest{Source: "todos"}, Bindings: []notes.Binding{{Type: "range", Field: "not_a_field", Source: "context"}}}
	custom.Context.Range = notes.Range{Mode: "none"}
	if _, err = e.Execute(context.Background(), custom); err == nil {
		t.Fatal("invalid binding hidden by empty context")
	}
}
