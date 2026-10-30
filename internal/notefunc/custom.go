package notefunc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"todo/internal/notes"
	"todo/internal/query"
	"todo/internal/tutil"
)

func and(q *query.QueryRequest, p query.Predicate) {
	if q.Where == nil {
		q.Where = &p
	} else {
		q.Where = &query.Predicate{And: []query.Predicate{*q.Where, p}}
	}
}
func predicateRange(q *query.QueryRequest, field string, b *bounds) {
	if b != nil {
		and(q, query.Predicate{Field: field, Op: "gte", Value: b.start.Format(tutil.DateLayout)})
		and(q, query.Predicate{Field: field, Op: "lt", Value: b.end.Format(tutil.DateLayout)})
	}
}
func executeCustom(ctx context.Context, tx *sql.Tx, r Request, now time.Time, out *Response) (result query.QueryResult, err error) {
	meta := &out.Meta
	if r.Definition.Query == nil {
		return result, notes.Invalid("invalid_value", "definition.query", "configure a query")
	}
	raw := *r.Definition.Query
	raw.Select = append([]query.SelectItem{}, raw.Select...)
	raw.OrderBy = append([]query.OrderItem{}, raw.OrderBy...)
	if len(r.Definition.Bindings) > 20 {
		return result, notes.Invalid("query_limit_exceeded", "definition.bindings", "maximum 20 bindings")
	}
	for i, b := range r.Definition.Bindings {
		path := fmt.Sprintf("definition.bindings[%d]", i)
		if b.Source != "context" {
			return result, notes.Invalid("invalid_value", path+".source", "only context binding supported")
		}
		var field *query.Field
		for _, f := range query.NewCatalog().Fields(raw.Source) {
			if f.Key == b.Field {
				copy := f
				field = &copy
			}
		}
		if field == nil || (b.Type == "range" && field.Type != "date") || (b.Type == "tag_ids" && field.Type != "number") {
			return result, notes.Invalid("unknown_field", path+".field", "binding requires a registered field of the matching type")
		}
		if strings.Contains(b.Field, ".") {
			relation := strings.Split(b.Field, ".")[0]
			found := false
			for _, j := range raw.Joins {
				found = found || j.Relation == relation
			}
			if !found {
				return result, notes.Invalid("unknown_field", path+".field", "declare the relation in joins")
			}
		}
		switch b.Type {
		case "range":
			effective, e := resolve(r.Context.Range, r.Context, now)
			if e != nil {
				return result, e
			}
			predicateRange(&raw, b.Field, effective)
			meta.EffectiveRange = window(effective)
		case "tag_ids":
			if err = checkIDs(ctx, tx, "tags", r.Context.TagIDs, "context.tag_ids"); err != nil {
				return result, err
			}
			if len(r.Context.TagIDs) > 0 {
				vals := []any{}
				for _, id := range r.Context.TagIDs {
					vals = append(vals, id)
				}
				and(&raw, query.Predicate{Field: b.Field, Op: "in", Value: vals})
			}
		default:
			return result, notes.Invalid("invalid_value", path+".type", "unknown binding")
		}
	}
	if len(r.Sort) > 0 {
		raw.OrderBy = r.Sort
	}
	raw.Limit = r.Page.Limit
	raw.Offset = r.Page.Offset
	q, err := query.ValidateAndNormalize(raw, query.NewCatalog())
	if err != nil {
		return result, err
	}
	fields := map[string]query.Field{}
	for _, f := range query.NewCatalog().Fields(q.Source) {
		fields[f.Key] = f
	}
	columns := []query.Column{}
	outputFields := map[string]string{}
	aggregate := false
	for _, s := range q.Select {
		key := s.Alias
		if key == "" {
			key = s.Field
			if s.Aggregate != "" {
				key = s.Aggregate
				if s.Field != "" {
					key += "_" + s.Field
				}
			}
		}
		f := fields[s.Field]
		typ := f.Type
		if s.Aggregate == "count" || s.Aggregate == "count_distinct" || s.Aggregate == "sum" || s.Aggregate == "avg" {
			typ = "number"
		}
		sem := ""
		if typ == "date" {
			sem = "datetime"
			if s.Field == "due_date" || s.Field == "date" {
				sem = "date"
			}
		}
		columns = append(columns, col(key, key, typ, sem))
		if s.Aggregate == "" {
			outputFields[key] = s.Field
		} else {
			aggregate = true
		}
	}
	if err = validateMapping(r.View, columns); err != nil {
		return result, err
	}
	if r.View.Type != "table" {
		if aggregate || len(q.GroupBy) > 0 || len(q.Joins) > 0 {
			return result, notes.Invalid("unsupported_view", "definition.query", "event views require ungrouped rows without joins")
		}
		s, e := date(r.Viewport.Start, "viewport.start")
		if e != nil {
			return result, e
		}
		end, e := date(r.Viewport.End, "viewport.end_exclusive")
		if e != nil {
			return result, e
		}
		viewport := &bounds{s, end}
		m := r.View.Mapping
		if m.Start != "" && m.End != "" {
			if q.Source != "time_entries" || outputFields[m.Start] != "start_time" || outputFields[m.End] != "end_time" {
				return result, notes.Invalid("unsupported_view", "view.mapping", "interval mapping must use time entry start/end fields")
			}
			and(&raw, query.Predicate{Field: "start_time", Op: "lt", Value: end.Format(tutil.Layout)})
			and(&raw, query.Predicate{Or: []query.Predicate{{Field: "end_time", Op: "gt", Value: s.Format(tutil.Layout)}, {And: []query.Predicate{{Field: "end_time", Op: "is_empty"}, {Field: "start_time", Op: "lt", Value: now.Format(tutil.Layout)}}}}})
		} else {
			key := m.Date
			if key == "" {
				key = m.Start
			}
			field := outputFields[key]
			if field == "" || strings.Contains(field, ".") {
				return result, notes.Invalid("unsupported_view", "view.mapping.date", "a direct date field is required")
			}
			predicateRange(&raw, field, viewport)
		}
		raw.Limit = 500
		raw.Offset = 0
	}
	q, err = query.ValidateAndNormalize(raw, query.NewCatalog())
	if err != nil {
		return result, err
	}
	if r.View.Type == "table" && notes.Mode(&r.Definition) == "detail" {
		plan, e := query.CompileTableRead(q, now, r.View.Options.Group)
		if e != nil {
			return result, e
		}
		result, out.Summaries, out.Groups, err = query.ReadTableTx(ctx, tx, plan, r.Page.Offset, r.View.Options.Group, r.View.Options.Summaries)
		return result, err
	}
	result, err = query.ExecuteTx(ctx, tx, q, now)
	if err != nil {
		return result, err
	}
	result.Columns = columns
	if r.View.Type != "table" {
		if result.TotalCount > 5000 {
			return result, notes.Invalid("visualization_limit_exceeded", "viewport", "more than 5000 events; narrow range or use table")
		}
		for result.NextOffset != nil {
			q.Offset = *result.NextOffset
			page, e := query.ExecuteTx(ctx, tx, q, now)
			if e != nil {
				return result, e
			}
			result.Rows = append(result.Rows, page.Rows...)
			result.NextOffset = page.NextOffset
		}
		result.RowCount = len(result.Rows)
	}
	return result, nil
}
