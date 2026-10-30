package query

import (
	"strings"
	"testing"
	"todo/internal/db"
)

func TestViews(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	req := QueryRequest{Source: "todos"}
	for _, name := range []string{"", strings.Repeat("中", 101)} {
		if _, e := CreateView(d, name, req); e == nil {
			t.Fatal("invalid name accepted")
		}
	}
	if _, e := CreateView(d, "bad", QueryRequest{Source: "bad"}); e == nil {
		t.Fatal("invalid query accepted")
	}
	if _, e := CreateView(d, "large", QueryRequest{Source: "todos", Where: &Predicate{Field: "title", Op: "eq", Value: strings.Repeat("x", 65536)}}); e == nil {
		t.Fatal("large query accepted")
	}
	v, e := CreateView(d, "月报", req)
	if e != nil {
		t.Fatal(e)
	}
	views, e := ListViews(d)
	if e != nil || len(views) != 1 || views[0].Query.Limit != 50 {
		t.Fatalf("list %+v %v", views, e)
	}
	v, e = UpdateView(d, v.ID, "改名", QueryRequest{Source: "tags"})
	if e != nil || v.Name != "改名" {
		t.Fatalf("update %+v %v", v, e)
	}
	if e = DeleteView(d, v.ID); e != nil {
		t.Fatal(e)
	}
	if e = DeleteView(d, v.ID); e == nil {
		t.Fatal("missing deletion accepted")
	}
	v, e = CreateView(d, "tampered", req)
	if e != nil {
		t.Fatal(e)
	}
	_, e = d.Exec(`UPDATE query_views SET query_json = '{"source":"evil"}' WHERE id = ?`, v.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ListViews(d); e == nil {
		t.Fatal("invalid stored query trusted")
	}
}

func TestViewMigrationPreservesData(t *testing.T) {
	path := t.TempDir() + "/legacy.db"
	d, e := db.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(`INSERT INTO todos(title) VALUES('keep'); DROP TABLE query_views`); e != nil {
		t.Fatal(e)
	}
	d.Close()
	d, e = db.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	var title string
	if e = d.QueryRow(`SELECT title FROM todos`).Scan(&title); e != nil || title != "keep" {
		t.Fatalf("legacy data %q %v", title, e)
	}
	if _, e = ListViews(d); e != nil {
		t.Fatal(e)
	}
}
