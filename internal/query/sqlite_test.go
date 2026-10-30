package query

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
	"todo/internal/db"
)

func TestSQLite(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	_, e = d.Exec(`INSERT INTO tags(id,name) VALUES(1,'开发'); INSERT INTO todos(id,title) VALUES(1,'文档'),(2,'100%_x'); INSERT INTO todo_tags VALUES(1,1,0); INSERT INTO time_entries(todo_id,tag_id,start_time,end_time) VALUES(1,1,'2026-10-01 09:00:00','2026-10-01 10:00:00'),(1,1,'2026-10-02 09:00:00','2026-10-02 09:30:00')`)
	if e != nil {
		t.Fatal(e)
	}
	run := func(req QueryRequest) QueryResult {
		t.Helper()
		q, e := ValidateAndNormalize(req, NewCatalog())
		if e != nil {
			t.Fatal(e)
		}
		r, e := NewSQLiteExecutor(d).Execute(context.Background(), q)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := run(QueryRequest{Source: "time_entries", Joins: []Join{{"todo"}}, Select: []SelectItem{{Field: "todo.title"}, {Field: "duration_seconds", Aggregate: "sum", Alias: "seconds"}}, GroupBy: []string{"todo.title"}, OrderBy: []OrderItem{{"seconds", "desc"}}})
	if r.TotalCount != 1 || math.Abs(r.Rows[0]["seconds"].(float64)-5400) > 0.01 {
		t.Fatalf("group result: %+v", r)
	}
	for _, agg := range []string{"sum", "avg", "min", "max", "count", "count_distinct"} {
		r := run(QueryRequest{Source: "time_entries", Select: []SelectItem{{Field: "duration_seconds", Aggregate: agg}}})
		if r.RowCount != 1 || len(r.Aggregates) != 1 {
			t.Fatalf("%s: %+v", agg, r)
		}
	}
	r = run(QueryRequest{Source: "todos", Where: &Predicate{Field: "title", Op: "contains", Value: "%_"}, Limit: 1})
	if r.RowCount != 1 || r.Rows[0]["id"] != int64(2) {
		t.Fatalf("LIKE escape: %+v", r)
	}
	r = run(QueryRequest{Source: "todos", Limit: 1})
	if r.TotalCount != 2 || r.NextOffset == nil || *r.NextOffset != 1 {
		t.Fatalf("page %+v", r)
	}
	r = run(QueryRequest{Source: "todos", Limit: 1, Offset: 2})
	if r.RowCount != 0 || r.TotalCount != 2 || r.NextOffset != nil {
		t.Fatalf("empty page %+v", r)
	}
	r = run(QueryRequest{Source: "todos", Joins: []Join{{"tags"}}, Select: []SelectItem{{Field: "tags.name"}}})
	if r.Rows[0]["tags.name"] != "开发" || r.Rows[1]["tags.name"] != nil {
		t.Fatalf("left join %+v", r)
	}
	q, _ := ValidateAndNormalize(QueryRequest{Source: "todos", Where: &Predicate{Field: "title", Op: "eq", Value: "'; DROP TABLE todos;--"}}, NewCatalog())
	sql, args, _, _, e := compileSQLite(q)
	if e != nil || strings.Contains(sql, "DROP") || len(args) != 3 {
		t.Fatalf("binding: %s %v %v", sql, args, e)
	}
	_, e = d.Exec(`INSERT INTO time_entries(start_time) VALUES(datetime('now','localtime','-60 seconds'))`)
	if e != nil {
		t.Fatal(e)
	}
	r = run(QueryRequest{Source: "time_entries", Select: []SelectItem{{Field: "duration_seconds"}}, Where: &Predicate{Field: "end_time", Op: "is_empty"}})
	if math.Abs(r.Rows[0]["duration_seconds"].(float64)-60) > 3 {
		t.Fatalf("active %+v", r)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, e = NewSQLiteExecutor(d).Execute(ctx, q)
	if qe, ok := e.(*Error); !ok || qe.Code != "query_timeout" {
		t.Fatalf("cancellation: %v", e)
	}
}

func TestSQLiteOperatorsAndRelations(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	_, e = d.Exec(`INSERT INTO todos(id,title,description,priority,parent_id,due_date) VALUES(1,'alpha','',1,NULL,'2026-10-01'),(2,'beta','note',2,1,NULL),(3,'alpine','',3,NULL,'2026-11-01'); INSERT INTO tags(id,name,include_in_stats) VALUES(1,'tag',0); INSERT INTO todo_tags VALUES(1,1,0); INSERT INTO time_entries(todo_id,tag_id,start_time,end_time) VALUES(1,1,'2026-10-01 09:00:00','2026-10-01 10:00:00')`)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		field, op string
		value     any
		count     int
	}{
		{"title", "eq", "alpha", 1}, {"title", "neq", "alpha", 2}, {"title", "starts_with", "al", 2}, {"title", "contains", "ph", 1}, {"description", "is_empty", nil, 2}, {"description", "is_not_empty", nil, 1},
		{"priority", "gt", float64(1), 2}, {"priority", "gte", float64(2), 2}, {"priority", "lt", float64(2), 1}, {"priority", "lte", float64(2), 2}, {"priority", "neq", float64(2), 2}, {"priority", "in", []any{float64(1), float64(3)}, 2},
		{"due_date", "between", []any{"2026-10-01", "2026-10-31"}, 1}, {"due_date", "is_empty", nil, 1}, {"due_date", "is_not_empty", nil, 2}, {"due_date", "gte", "2026-10-01T00:00:00Z", 2},
	} {
		t.Run(tc.field+"/"+tc.op, func(t *testing.T) {
			q, e := ValidateAndNormalize(QueryRequest{Source: "todos", Where: &Predicate{Field: tc.field, Op: tc.op, Value: tc.value}}, NewCatalog())
			if e != nil {
				t.Fatal(e)
			}
			r, e := NewSQLiteExecutor(d).Execute(context.Background(), q)
			if e != nil || r.TotalCount != tc.count {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
	for _, source := range NewCatalog().Sources {
		for _, relation := range source.Relations {
			t.Run(source.Key+"/"+relation.Key, func(t *testing.T) {
				q, e := ValidateAndNormalize(QueryRequest{Source: source.Key, Joins: []Join{{relation.Key}}, Select: []SelectItem{{Field: "id"}, {Field: relation.Key + ".id"}}}, NewCatalog())
				if e != nil {
					t.Fatal(e)
				}
				if _, e = NewSQLiteExecutor(d).Execute(context.Background(), q); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
	q, e := ValidateAndNormalize(QueryRequest{Source: "tags", Where: &Predicate{Field: "include_in_stats", Op: "eq", Value: false}}, NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewSQLiteExecutor(d).Execute(context.Background(), q)
	if e != nil || r.TotalCount != 1 || r.Rows[0]["include_in_stats"] != false {
		t.Fatalf("boolean: %+v %v", r, e)
	}
	q, e = ValidateAndNormalize(QueryRequest{Source: "todos", Where: &Predicate{Not: &Predicate{Or: []Predicate{{Field: "priority", Op: "eq", Value: float64(1)}, {Field: "priority", Op: "eq", Value: float64(3)}}}}}, NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	r, e = NewSQLiteExecutor(d).Execute(context.Background(), q)
	if e != nil || r.TotalCount != 1 || r.Rows[0]["id"] != int64(2) {
		t.Fatalf("nested: %+v %v", r, e)
	}
	if _, e = NewSQLiteExecutor(d).Execute(context.Background(), NormalizedQuery{QueryRequest{Source: "todos", Limit: 501}}); e == nil {
		t.Fatal("executor accepted unvalidated limit")
	}
}

func TestSQLiteRelatedDuration(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if _, e = d.Exec(`INSERT INTO todos(id,title) VALUES(1,'test'); INSERT INTO time_entries(todo_id,start_time,end_time) VALUES(1,'2026-10-01 09:00:00','2026-10-01 10:00:00')`); e != nil {
		t.Fatal(e)
	}
	req := QueryRequest{Source: "todos", Joins: []Join{{"time_entries"}}, Select: []SelectItem{{Field: "time_entries.duration_seconds"}}}
	q, e := ValidateAndNormalize(req, NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewSQLiteExecutor(d).Execute(context.Background(), q)
	if e != nil || math.Abs(r.Rows[0]["time_entries.duration_seconds"].(float64)-3600) > 0.01 {
		t.Fatalf("related duration %+v %v", r, e)
	}
	for _, agg := range []string{"sum", "avg"} {
		req.Select[0].Aggregate = agg
		if _, e = ValidateAndNormalize(req, NewCatalog()); e == nil {
			t.Fatalf("%s accepted outside time_entries source", agg)
		}
	}
}

func TestSQLiteDefaultTimeout(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// Occupy the only connection so Execute must obey its own deadline while waiting.
	conn, err := d.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q, err := ValidateAndNormalize(QueryRequest{Source: "todos"}, NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = NewSQLiteExecutor(d).Execute(context.Background(), q)
	qe, ok := err.(*Error)
	if !ok || qe.Code != "query_timeout" {
		t.Fatalf("expected query_timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 4*time.Second || elapsed > 10*time.Second {
		t.Fatalf("expected 5-second timeout, elapsed %v", elapsed)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = NewSQLiteExecutor(d).Execute(context.Background(), q); err != nil {
		t.Fatalf("connection unusable after timeout: %v", err)
	}
}
