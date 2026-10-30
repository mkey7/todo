package query

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	c := NewCatalog()
	q, err := ValidateAndNormalize(QueryRequest{Source: "todos"}, c)
	if err != nil || q.Limit != 50 || len(q.Select) == 0 {
		t.Fatalf("defaults: %+v %v", q, err)
	}
	nested := &Predicate{And: []Predicate{{Field: "priority", Op: "gte", Value: float64(1)}, {Or: []Predicate{{Field: "title", Op: "contains", Value: "x"}, {Not: &Predicate{Field: "due_date", Op: "is_empty"}}}}}}
	if _, err := ValidateAndNormalize(QueryRequest{Source: "todos", Where: nested}, c); err != nil {
		t.Fatal(err)
	}
	for name, req := range map[string]QueryRequest{
		"unknown":   {Source: "todos", Select: []SelectItem{{Field: "nope"}}},
		"sql":       {Source: "todos; DROP TABLE todos"},
		"type":      {Source: "todos", Where: &Predicate{Field: "priority", Op: "eq", Value: "1"}},
		"alias":     {Source: "todos", Select: []SelectItem{{Field: "title", Alias: "a"}, {Field: "id", Alias: "a"}}},
		"limit":     {Source: "todos", Limit: 501},
		"offset":    {Source: "todos", Offset: 100001},
		"relation":  {Source: "time_entries", Select: []SelectItem{{Field: "todo.title"}}},
		"aggregate": {Source: "todos", Select: []SelectItem{{Field: "title"}, {Field: "id", Aggregate: "sum"}}},
		"bad-date":  {Source: "todos", Where: &Predicate{Field: "due_date", Op: "eq", Value: "tomorrow"}},
		"alias-sql": {Source: "todos", Select: []SelectItem{{Field: "title", Alias: "a;drop"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := ValidateAndNormalize(req, c); e == nil {
				t.Fatal("accepted invalid query")
			}
		})
	}
	p := &Predicate{Field: "title", Op: "eq", Value: "x"}
	for i := 0; i < 6; i++ {
		p = &Predicate{Not: p}
	}
	_, err = ValidateAndNormalize(QueryRequest{Source: "todos", Where: p}, c)
	if err == nil || !strings.Contains(err.Error(), "where") {
		t.Fatalf("depth: %v", err)
	}
}

func TestNormalizationAndLimits(t *testing.T) {
	q, e := ValidateAndNormalize(QueryRequest{Source: "todos"}, NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	if q.Joins == nil || q.GroupBy == nil || q.OrderBy == nil {
		t.Fatalf("normalized arrays: %+v", q)
	}
	for name, req := range map[string]QueryRequest{
		"many-select":         {Source: "todos", Select: make([]SelectItem, 21)},
		"many-order":          {Source: "todos", OrderBy: make([]OrderItem, 21)},
		"many-group":          {Source: "todos", GroupBy: make([]string, 21)},
		"many-joins":          {Source: "todos", Joins: make([]Join, 4)},
		"duplicate-join":      {Source: "todos", Joins: []Join{{"tags"}, {"tags"}}},
		"bad-aggregate":       {Source: "todos", Select: []SelectItem{{Field: "title", Aggregate: "sum"}}},
		"bad-direction":       {Source: "todos", OrderBy: []OrderItem{{"title", "DESC; DROP"}}},
		"ambiguous-predicate": {Source: "todos", Where: &Predicate{Field: "title", Op: "eq", Value: "x", Not: &Predicate{Field: "id", Op: "eq", Value: float64(1)}}},
		"empty-in":            {Source: "todos", Where: &Predicate{Field: "id", Op: "in", Value: []any{}}},
		"too-many-in":         {Source: "todos", Where: &Predicate{Field: "id", Op: "in", Value: make([]any, 101)}},
		"between-size":        {Source: "todos", Where: &Predicate{Field: "due_date", Op: "between", Value: []any{"2026-10-01"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := ValidateAndNormalize(req, NewCatalog()); e == nil {
				t.Fatal("accepted invalid query")
			}
		})
	}
	leaves := []Predicate{}
	for i := 0; i < 20; i++ {
		leaves = append(leaves, Predicate{Field: "id", Op: "eq", Value: float64(1)})
	}
	_, e = ValidateAndNormalize(QueryRequest{Source: "todos", Where: &Predicate{And: []Predicate{{And: leaves}, {Field: "id", Op: "eq", Value: float64(2)}}}}, NewCatalog())
	if e == nil {
		t.Fatal("accepted 21 leaves")
	}
}
