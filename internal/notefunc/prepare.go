package notefunc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"todo/internal/notes"
	"todo/internal/query"
	"todo/internal/tutil"
	"unicode/utf8"
)

func prepareRequest(r Request, now time.Time) (Request, error) {
	if r.QueryMode != "" {
		if r.QueryMode != "detail" && r.QueryMode != "aggregate" {
			return r, notes.Invalid("invalid_value", "query_mode", "expected detail or aggregate")
		}
		if r.Definition.Kind == "custom" || r.Definition.Name == "todos" || r.Definition.Name == "time_entries" || r.Definition.Name == "time_stats" {
			if r.QueryMode != notes.Mode(&r.Definition) {
				return r, notes.Invalid("invalid_value", "query_mode", "query definition does not match the selected mode")
			}
		}
	}
	if notes.Mode(&r.Definition) == "aggregate" {
		if r.View.Type != "table" {
			return r, notes.Invalid("unsupported_view", "view.type", "aggregate reports support table only")
		}
		if r.View.Options.Group != nil || len(r.View.Options.Summaries) > 0 {
			return r, notes.Invalid("invalid_value", "view.options", "aggregate reports cannot use detail groups or footer statistics")
		}
	}
	order := r.Definition.OrderBy
	if r.Definition.Kind == "custom" && r.Definition.Query != nil {
		order = r.Definition.Query.OrderBy
		if len(r.Definition.OrderBy) > 0 {
			return r, notes.Invalid("invalid_value", "definition.order_by", "custom query stores sorting in query.order_by")
		}
	}
	if r.QueryMode != "" && len(r.Sort) > 0 && !sameOrder(r.Sort, order) {
		return r, notes.Invalid("invalid_value", "sort", "sort conflicts with the persisted block order")
	}
	if len(r.Sort) == 0 {
		r.Sort = append([]query.OrderItem{}, order...)
	}
	if r.Definition.Kind == "custom" && r.Definition.Query != nil {
		raw, err := json.Marshal(r.Definition.Query)
		if err != nil {
			return r, notes.Invalid("invalid_value", "definition.query", "invalid query values")
		}
		var q query.QueryRequest
		if err = json.Unmarshal(raw, &q); err != nil {
			return r, err
		}
		fields := map[string]query.Field{}
		for _, f := range query.NewCatalog().Fields(q.Source) {
			fields[f.Key] = f
		}
		q.Where, err = relativeFilter(q.Where, fields, now, 1)
		if err != nil {
			return r, err
		}
		search, err := searchPredicate(r.Definition.Search, fields, q.Joins)
		if err != nil {
			return r, err
		}
		if search != nil {
			and(&q, *search)
		}
		r.Definition.Query = &q
	}
	return r, nil
}
func sameOrder(a, b []query.OrderItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		direction := func(s string) string {
			if s == "" {
				return "asc"
			}
			return s
		}
		if a[i].Key != b[i].Key || direction(a[i].Direction) != direction(b[i].Direction) {
			return false
		}
	}
	return true
}
func relativeFilter(p *query.Predicate, fields map[string]query.Field, now time.Time, depth int) (*query.Predicate, error) {
	if p == nil {
		return nil, nil
	}
	if depth > 5 {
		return nil, notes.Invalid("query_limit_exceeded", "where", "maximum condition depth is 5")
	}
	if p.Op == "in_relative_range" {
		if p.And != nil || p.Or != nil || p.Not != nil || fields[p.Field].Type != "date" {
			return nil, notes.Invalid("invalid_value", "where", "relative range requires a date field")
		}
		preset, ok := p.Value.(string)
		if !ok {
			return nil, notes.Invalid("invalid_value", "where.value", "relative preset required")
		}
		b, err := resolve(notes.Range{Mode: "relative", Preset: preset}, notes.Context{}, now)
		if err != nil {
			return nil, err
		}
		return &query.Predicate{And: []query.Predicate{{Field: p.Field, Op: "gte", Value: b.start.Format(tutil.Layout)}, {Field: p.Field, Op: "lt", Value: b.end.Format(tutil.Layout)}}}, nil
	}
	var err error
	copy := *p
	if p.Not != nil {
		copy.Not, err = relativeFilter(p.Not, fields, now, depth+1)
		if err != nil {
			return nil, err
		}
	}
	if p.And != nil {
		copy.And = make([]query.Predicate, len(p.And))
		for i := range p.And {
			var next *query.Predicate
			next, err = relativeFilter(&p.And[i], fields, now, depth+1)
			if err != nil {
				return nil, err
			}
			copy.And[i] = *next
		}
	}
	if p.Or != nil {
		copy.Or = make([]query.Predicate, len(p.Or))
		for i := range p.Or {
			var next *query.Predicate
			next, err = relativeFilter(&p.Or[i], fields, now, depth+1)
			if err != nil {
				return nil, err
			}
			copy.Or[i] = *next
		}
	}
	return &copy, nil
}
func searchPredicate(s *notes.Search, fields map[string]query.Field, joins []query.Join) (*query.Predicate, error) {
	if s == nil {
		return nil, nil
	}
	if utf8.RuneCountInString(s.Text) > 200 || len(s.Fields) > 20 {
		return nil, notes.Invalid("query_limit_exceeded", "definition.search", "search exceeds limits")
	}
	enabled := map[string]bool{}
	for _, j := range joins {
		enabled[j.Relation] = true
	}
	names := append([]string{}, s.Fields...)
	if len(names) == 0 {
		for key, f := range fields {
			if f.Type == "text" && (!strings.Contains(key, ".") || enabled[strings.Split(key, ".")[0]]) {
				names = append(names, key)
			}
		}
	}
	conditions := []query.Predicate{}
	seen := map[string]bool{}
	for i, key := range names {
		f, ok := fields[key]
		if !ok || f.Type != "text" || seen[key] || (strings.Contains(key, ".") && !enabled[strings.Split(key, ".")[0]]) {
			return nil, notes.Invalid("unknown_field", fmt.Sprintf("definition.search.fields[%d]", i), "search requires an enabled text field")
		}
		seen[key] = true
		conditions = append(conditions, query.Predicate{Field: key, Op: "contains", Value: s.Text})
	}
	if s.Text == "" {
		return nil, nil
	}
	if len(conditions) == 0 {
		return nil, notes.Invalid("invalid_value", "definition.search", "no searchable text fields")
	}
	return &query.Predicate{Or: conditions}, nil
}
