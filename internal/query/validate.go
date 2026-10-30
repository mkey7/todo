package query

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
)

var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9_\x{4e00}-\x{9fff}-]{1,64}$`)

func ValidateAndNormalize(req QueryRequest, c Catalog) (NormalizedQuery, error) {
	fail := func(code, path, msg string) (NormalizedQuery, error) {
		return NormalizedQuery{}, invalid(code, path, msg)
	}
	s, ok := c.source(req.Source)
	if !ok {
		return fail("invalid_query", "source", "unknown source")
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.Limit < 1 || req.Limit > 500 || req.Offset < 0 || req.Offset > 100000 || len(req.Joins) > 3 || len(req.Select) > 20 || len(req.OrderBy) > 20 || len(req.GroupBy) > 20 {
		return fail("query_limit_exceeded", "query", "query exceeds resource limits")
	}
	joins := map[string]bool{}
	for i, j := range req.Joins {
		found := false
		for _, r := range s.Relations {
			found = found || r.Key == j.Relation
		}
		if !found || joins[j.Relation] {
			return fail("invalid_query", fmt.Sprintf("joins[%d]", i), "unknown or duplicate relation")
		}
		joins[j.Relation] = true
	}
	field := func(key, path string) (Field, error) {
		f, ok := c.field(req.Source, key)
		if !ok {
			return f, invalid("unknown_field", path, "unknown field")
		}
		if p, _, has := strings.Cut(key, "."); has && !joins[p] {
			return f, invalid("unknown_field", path, "relation must be declared in joins")
		}
		return f, nil
	}
	if len(req.Select) == 0 {
		for _, key := range s.Defaults {
			req.Select = append(req.Select, SelectItem{Field: key})
		}
	}
	keys := map[string]bool{}
	aggregated := false
	for i, item := range req.Select {
		path := fmt.Sprintf("select[%d]", i)
		var f Field
		if item.Field != "" {
			var e error
			f, e = field(item.Field, path+".field")
			if e != nil {
				return NormalizedQuery{}, e
			}
		} else if item.Aggregate != "count" {
			return fail("unknown_field", path+".field", "field required")
		}
		if item.Aggregate != "" {
			aggregated = true
			if item.Aggregate != "count" && !slices.Contains(f.Aggregates, item.Aggregate) {
				return fail("invalid_query", path+".aggregate", "unsupported aggregate")
			}
		}
		if item.Alias != "" && !aliasPattern.MatchString(item.Alias) {
			return fail("invalid_query", path+".alias", "invalid alias")
		}
		key := selectKey(item)
		if keys[key] {
			return fail("invalid_query", path+".alias", "duplicate output key")
		}
		keys[key] = true
	}
	groups := map[string]bool{}
	for i, key := range req.GroupBy {
		if _, e := field(key, fmt.Sprintf("group_by[%d]", i)); e != nil {
			return NormalizedQuery{}, e
		}
		if groups[key] {
			return fail("invalid_query", "group_by", "duplicate group key")
		}
		groups[key] = true
	}
	if aggregated || len(groups) > 0 {
		for i, item := range req.Select {
			if item.Aggregate == "" && !groups[item.Field] {
				return fail("invalid_query", fmt.Sprintf("select[%d]", i), "non-aggregate field must be grouped")
			}
		}
	}
	req.OrderBy = append([]OrderItem(nil), req.OrderBy...)
	for i := range req.OrderBy {
		o := &req.OrderBy[i]
		path := fmt.Sprintf("order_by[%d]", i)
		if o.Direction == "" {
			o.Direction = "asc"
		}
		if o.Direction != "asc" && o.Direction != "desc" {
			return fail("invalid_query", path, "invalid direction")
		}
		if !keys[o.Key] {
			if _, e := field(o.Key, path+".key"); e != nil {
				return NormalizedQuery{}, e
			}
			if (aggregated || len(groups) > 0) && !groups[o.Key] {
				return fail("invalid_query", path, "order field must be grouped or selected aggregate")
			}
		}
	}
	if req.Joins == nil {
		req.Joins = []Join{}
	}
	if req.GroupBy == nil {
		req.GroupBy = []string{}
	}
	if req.OrderBy == nil {
		req.OrderBy = []OrderItem{}
	}
	count := 0
	var check func(*Predicate, int, string) error
	check = func(p *Predicate, depth int, path string) error {
		if depth > 5 {
			return invalid("query_limit_exceeded", path, "predicate depth exceeds 5")
		}
		forms := 0
		if p.And != nil {
			forms++
		}
		if p.Or != nil {
			forms++
		}
		if p.Not != nil {
			forms++
		}
		if p.Field != "" || p.Op != "" || p.Value != nil {
			forms++
		}
		if forms != 1 {
			return invalid("invalid_query", path, "predicate needs exactly one leaf or logical group")
		}
		if p.Not != nil {
			return check(p.Not, depth+1, path+".not")
		}
		if p.And != nil || p.Or != nil {
			items := p.And
			key := "and"
			if p.Or != nil {
				items = p.Or
				key = "or"
			}
			if len(items) == 0 || len(items) > 20 {
				return invalid("query_limit_exceeded", path, "group must contain 1–20 predicates")
			}
			for i := range items {
				if e := check(&items[i], depth+1, fmt.Sprintf("%s.%s[%d]", path, key, i)); e != nil {
					return e
				}
			}
			return nil
		}
		count++
		if count > 20 {
			return invalid("query_limit_exceeded", path, "more than 20 predicates")
		}
		f, e := field(p.Field, path+".field")
		if e != nil {
			return e
		}
		if !slices.Contains(f.Operators, p.Op) {
			return invalid("invalid_query", path+".op", "unsupported operator")
		}
		if p.Op == "is_empty" || p.Op == "is_not_empty" {
			if p.Value != nil {
				return invalid("invalid_value", path+".value", "empty test takes no value")
			}
			return nil
		}
		values := []any{p.Value}
		if p.Op == "in" || p.Op == "between" {
			v, ok := p.Value.([]any)
			if !ok || len(v) == 0 || len(v) > 100 || (p.Op == "between" && len(v) != 2) {
				return invalid("invalid_value", path+".value", "invalid array length")
			}
			values = v
		}
		for _, v := range values {
			if !validValue(f.Type, v) {
				return invalid("invalid_value", path+".value", "value does not match "+f.Type)
			}
		}
		return nil
	}
	if req.Where != nil {
		if e := check(req.Where, 1, "where"); e != nil {
			return NormalizedQuery{}, e
		}
	}
	return NormalizedQuery{req}, nil
}
func validValue(typ string, v any) bool {
	switch typ {
	case "text":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		switch n := v.(type) {
		case float64:
			return !math.IsNaN(n) && !math.IsInf(n, 0)
		case int, int64:
			return true
		case json.Number:
			_, err := n.Float64()
			return err == nil
		}
		return false
	case "date":
		s, ok := v.(string)
		if !ok {
			return false
		}
		for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339Nano} {
			if _, e := time.Parse(layout, s); e == nil {
				return true
			}
		}
		return false
	}
	return false
}
