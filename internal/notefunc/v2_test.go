package notefunc

import (
	"context"
	"testing"
	"time"
	"todo/internal/notes"
	"todo/internal/query"
)

func customRequest(source string, selects []query.SelectItem) Request {
	return Request{QueryMode: "detail", Definition: notes.Definition{Kind: "custom", Query: &query.QueryRequest{Source: source, Select: selects}}, Context: notes.Context{Range: notes.Range{Mode: "none"}}, View: notes.View{Type: "table"}, Page: Page{Limit: 50}}
}
func metricValue(t *testing.T, out Response, id string) any {
	t.Helper()
	if out.Summaries == nil {
		t.Fatal("missing summaries")
	}
	for _, m := range out.Summaries.Items {
		if m.ID == id {
			return m.Value
		}
	}
	t.Fatalf("missing metric %s", id)
	return nil
}
func TestV2FullSetSummariesAndGroupPaging(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<501) INSERT INTO todos(title,priority,description,created_at) SELECT CASE WHEN x<=300 THEN 'A' ELSE 'B' END,x,'开发100%_x','2026-10-30 09:00:00' FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	r := customRequest("todos", []query.SelectItem{{Field: "title", Alias: "group"}, {Field: "priority"}, {Field: "description"}})
	r.Definition.Query.OrderBy = []query.OrderItem{{Key: "priority", Direction: "asc"}}
	r.View.Options.Group = &query.DisplayGroup{Field: "group", Direction: "asc"}
	r.View.Options.Summaries = []query.Metric{{ID: "n", Op: "count_all"}, {ID: "sum", Field: "priority", Op: "sum"}, {ID: "avg", Field: "priority", Op: "avg"}}
	out := exec(t, e, r)
	if out.Result.RowCount != 50 || out.Result.TotalCount != 501 || metricValue(t, out, "n") != int64(501) || metricValue(t, out, "sum") != int64(125751) {
		t.Fatal(out)
	}
	if len(out.Groups) != 1 || out.Groups[0].RowCount != 300 || out.Groups[0].Previous || !out.Groups[0].Next {
		t.Fatal(out.Groups)
	}
	r.Page.Offset = 290
	out = exec(t, e, r)
	if len(out.Groups) != 2 || !out.Groups[0].Previous || out.Groups[0].Next || out.Groups[1].Previous || !out.Groups[1].Next || len(out.Groups[0].Indexes) != 10 {
		t.Fatal(out.Groups)
	}
	if metricValue(t, out, "sum") != int64(125751) {
		t.Fatal("page affected sum")
	}
	// All 5001 matched rows still participate in table statistics, despite event limits.
	_, err = d.Exec(`WITH RECURSIVE n(x) AS (SELECT 502 UNION ALL SELECT x+1 FROM n WHERE x<5001) INSERT INTO todos(title,priority,description,created_at) SELECT 'C',x,'开发100%_x','2026-10-30 09:00:00' FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	r.Page.Offset = 0
	out = exec(t, e, r)
	if out.Result.TotalCount != 5001 || metricValue(t, out, "sum") != int64(12507501) {
		t.Fatal(out)
	}
	r.Definition.Search = &notes.Search{Text: "100%_", Fields: []string{"description"}}
	out = exec(t, e, r)
	if out.Result.TotalCount != 5001 {
		t.Fatal("literal search lost rows", out)
	}
	r.Definition.Search.Text = "absent"
	out = exec(t, e, r)
	if out.Result.TotalCount != 0 || metricValue(t, out, "n") != int64(0) || metricValue(t, out, "sum") != nil || metricValue(t, out, "avg") != nil {
		t.Fatal(out)
	}
}
func TestV2EmptyZeroFalseAndTypedGroups(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`INSERT INTO time_entries(todo_id,start_time,end_time,note) VALUES(NULL,'2026-10-30 09:00:00','2026-10-30 10:00:00',''),(0,'2026-10-30 09:00:00','2026-10-30 10:00:00','0'),(2,'2026-10-30 09:00:00','2026-10-30 10:00:00','未填写'),(2,'2026-10-30 09:00:00','2026-10-30 10:00:00',' ');INSERT INTO tags(name,include_in_stats) VALUES('',0),('false',0),('true',1)`)
	if err != nil {
		t.Fatal(err)
	}
	r := customRequest("time_entries", []query.SelectItem{{Field: "todo_id"}, {Field: "note"}})
	r.View.Options.Group = &query.DisplayGroup{Field: "note", Direction: "asc"}
	r.View.Options.Summaries = []query.Metric{{ID: "empty", Field: "todo_id", Op: "count_empty"}, {ID: "values", Field: "todo_id", Op: "count_values"}, {ID: "distinct", Field: "todo_id", Op: "count_distinct"}, {ID: "p", Field: "todo_id", Op: "percent_empty"}, {ID: "text_empty", Field: "note", Op: "count_empty"}}
	out := exec(t, e, r)
	if metricValue(t, out, "empty") != int64(1) || metricValue(t, out, "values") != int64(3) || metricValue(t, out, "distinct") != int64(2) || metricValue(t, out, "p") != float64(25) || metricValue(t, out, "text_empty") != int64(1) {
		t.Fatal(out)
	}
	if out.Groups[0].Key.Type != "empty" {
		t.Fatal(out.Groups)
	}
	for _, g := range out.Groups {
		if g.Label == "未填写" && g.Key.Type == "text" && g.RowCount != 1 {
			t.Fatal(g)
		}
	}
	r = customRequest("tags", []query.SelectItem{{Field: "include_in_stats"}})
	r.View.Options.Group = &query.DisplayGroup{Field: "include_in_stats", Direction: "desc"}
	r.View.Options.Summaries = []query.Metric{{ID: "empty", Field: "include_in_stats", Op: "count_empty"}}
	out = exec(t, e, r)
	if metricValue(t, out, "empty") != int64(0) || len(out.Groups) != 2 {
		t.Fatal(out)
	}
	r.View.Options.Summaries = []query.Metric{{ID: "bad", Field: "include_in_stats", Op: "sum"}}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("boolean sum allowed")
	}
}
func TestV2RelativeDatesModesAndSortConflicts(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`INSERT INTO todos(title,priority,created_at) VALUES('old',0,'2026-10-23 00:00:00'),('week',1,'2026-10-26 00:00:00'),('today',2,'2026-10-30 12:00:00'),('next',3,'2026-11-02 00:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	r := customRequest("todos", []query.SelectItem{{Field: "title"}})
	r.Definition.Query.Where = &query.Predicate{Field: "created_at", Op: "in_relative_range", Value: "this_week"}
	r.Definition.Query.OrderBy = []query.OrderItem{{Key: "priority", Direction: "desc"}}
	out := exec(t, e, r)
	if out.Result.TotalCount != 2 || out.Result.Rows[0]["title"] != "today" {
		t.Fatal(out)
	}
	if r.Definition.Query.Where.Op != "in_relative_range" {
		t.Fatal("mutated authoring definition")
	}
	r.Sort = []query.OrderItem{{Key: "priority", Direction: "asc"}}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("sort conflict accepted")
	}
	r.QueryMode = ""
	out = exec(t, e, r)
	if out.Result.Rows[0]["title"] != "week" {
		t.Fatal("legacy sort changed", out)
	}
	r.QueryMode = "detail"
	r.Sort = nil
	r.Definition.Query.Select = []query.SelectItem{{Field: "priority", Aggregate: "sum"}}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("detail silently aggregated")
	}
	r.QueryMode = "aggregate"
	r.Definition.Query.OrderBy = nil
	out = exec(t, e, r)
	if out.Result.RowCount != 1 || out.Result.Rows[0]["sum_priority"] != int64(3) {
		t.Fatal(out)
	}
	for _, tc := range []struct {
		now                time.Time
		preset, start, end string
	}{{time.Date(2027, 1, 1, 12, 0, 0, 0, time.Local), "this_week", "2026-12-28", "2027-01-04"}, {time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local), "past_7_days", "2026-02-23", "2026-03-02"}} {
		b, err := resolve(notes.Range{Mode: "relative", Preset: tc.preset}, notes.Context{}, tc.now)
		if err != nil || window(b).Start != tc.start || window(b).End != tc.end {
			t.Fatal(b, err)
		}
	}
}
func TestV2FunctionStatisticsAndValidation(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`INSERT INTO todos(title,created_at) VALUES('开发','2026-10-30 09:00:00'),('测试','2026-10-30 10:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	r := req("todos")
	r.QueryMode = "detail"
	r.View.Options.Group = &query.DisplayGroup{Field: "title", Direction: "asc"}
	r.View.Options.Summaries = []query.Metric{{ID: "n", Op: "count_all"}}
	r.Definition.Search = &notes.Search{Text: "开发", Fields: []string{"title"}}
	out := exec(t, e, r)
	if metricValue(t, out, "n") != int64(1) || out.Result.RowCount != 1 {
		t.Fatal(out)
	}
	r.View.Options.Summaries = []query.Metric{{ID: "x", Field: "title", Op: "sum"}}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("text sum")
	}
	r.View.Options.Summaries = []query.Metric{{ID: "x", Op: "count_all"}, {ID: "x", Op: "count_all"}}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("duplicate metric")
	}
	r.View.Options.Summaries = nil
	r.Definition.Search = &notes.Search{Text: "x", Fields: []string{"title; DROP TABLE todos"}}
	if _, err = e.Execute(context.Background(), r); err == nil {
		t.Fatal("search identifier injection")
	}
}

func TestV2StatisticsCancellationAndDescendingGroups(t *testing.T) {
	d, e := fixture(t)
	_, err := d.Exec(`INSERT INTO todos(title,priority) VALUES('A',1),('A',2),('B',3),('B',4),('',5)`)
	if err != nil {
		t.Fatal(err)
	}
	r := customRequest("todos", []query.SelectItem{{Field: "title"}, {Field: "priority"}})
	r.Page = Page{Limit: 2, Offset: 2}
	r.View.Options.Group = &query.DisplayGroup{Field: "title", Direction: "desc"}
	r.View.Options.Summaries = []query.Metric{{ID: "sum", Field: "priority", Op: "sum"}}
	out := exec(t, e, r)
	if len(out.Groups) != 2 || out.Groups[0].Key.Value != "B" || !out.Groups[0].Previous || out.Groups[0].Next || out.Groups[1].Key.Value != "A" || out.Groups[1].Previous || !out.Groups[1].Next {
		t.Fatal(out.Groups)
	}
	conn, err := d.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = e.Execute(ctx, r)
	if err == nil {
		t.Fatal("cancellation ignored")
	}
	conn.Close()
	exec(t, e, r)
}

func TestV2UnboundCustomIgnoresUnusedContext(t *testing.T) {
	_, e := fixture(t)
	r := customRequest("todos", nil)
	r.Context.Range = notes.Range{Mode: "fixed", Start: "incomplete", End: ""}
	r.Context.TagIDs = []int64{-1}
	exec(t, e, r)
	r.Definition.Bindings = []notes.Binding{{Type: "range", Field: "created_at", Source: "context"}}
	if _, err := e.Execute(context.Background(), r); err == nil {
		t.Fatal("bound invalid context accepted")
	}
}
