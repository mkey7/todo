package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"todo/internal/tutil"
)

type SQLiteExecutor struct{ db *sql.DB }

func NewSQLiteExecutor(db *sql.DB) Executor { return &SQLiteExecutor{db} }
func quoted(s string) string                { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// Table and column names are resolved exclusively from the server catalog.
func sqliteField(source, key string) string {
	f, _ := NewCatalog().field(source, key)
	if f.Key == "duration_seconds" || strings.HasSuffix(f.Key, ".duration_seconds") {
		alias := "b"
		if prefix, _, ok := strings.Cut(f.Key, "."); ok {
			alias = "r_" + prefix
		}
		return "ROUND((julianday(COALESCE(" + quoted(alias) + ".end_time, datetime('now'))) - julianday(" + quoted(alias) + ".start_time)) * 86400.0, 3)"
	}
	alias, col := "b", f.Key
	if prefix, suffix, ok := strings.Cut(f.Key, "."); ok {
		alias = "r_" + prefix
		col = suffix
	}
	expr := quoted(alias) + "." + quoted(col)
	if f.Type == "date" {
		return "CAST(" + expr + " AS TEXT)"
	}
	return expr
}
func sqliteJoin(source, relation string) string {
	templates := map[string]map[string]string{
		"todos": {
			"tags":         ` LEFT JOIN todo_tags AS jt_tags ON jt_tags.todo_id = b.id LEFT JOIN tags AS r_tags ON r_tags.id = jt_tags.tag_id`,
			"time_entries": ` LEFT JOIN time_entries AS r_time_entries ON r_time_entries.todo_id = b.id`,
			"parent":       ` LEFT JOIN todos AS r_parent ON r_parent.id = b.parent_id`,
			"children":     ` LEFT JOIN todos AS r_children ON r_children.parent_id = b.id`,
		},
		"tags": {
			"todos":        ` LEFT JOIN todo_tags AS jt_todos ON jt_todos.tag_id = b.id LEFT JOIN todos AS r_todos ON r_todos.id = jt_todos.todo_id`,
			"time_entries": ` LEFT JOIN time_entries AS r_time_entries ON r_time_entries.tag_id = b.id`,
		},
		"time_entries": {
			"todo": ` LEFT JOIN todos AS r_todo ON r_todo.id = b.todo_id`,
			"tag":  ` LEFT JOIN tags AS r_tag ON r_tag.id = b.tag_id`,
		},
	}
	return templates[source][relation]
}
func sqliteSelect(source string, s SelectItem) string {
	expr := "*"
	if s.Field != "" {
		expr = sqliteField(source, s.Field)
	}
	if s.Aggregate == "count_distinct" {
		return "COUNT(DISTINCT " + expr + ")"
	}
	if s.Aggregate != "" {
		return strings.ToUpper(s.Aggregate) + "(" + expr + ")"
	}
	return expr
}
func compileSQLite(input NormalizedQuery) (string, []any, string, []any, error) {
	return compileSQLiteGroup(input, nil)
}
func compileSQLiteGroup(input NormalizedQuery, displayGroup *DisplayGroup) (sqlText string, args []any, countSQL string, countArgs []any, err error) {
	// Revalidate at the executor boundary, including callers that construct a plan directly.
	q, e := ValidateAndNormalize(input.QueryRequest, NewCatalog())
	if e != nil {
		err = e
		return
	}
	fields := []string{}
	aggregate := false
	outputs := map[string]string{}
	for _, s := range q.Select {
		expr := sqliteSelect(q.Source, s)
		key := selectKey(s)
		outputs[key] = expr
		fields = append(fields, expr+" AS "+quoted(key))
		aggregate = aggregate || s.Aggregate != ""
	}
	from := " FROM " + quoted(q.Source) + " AS b"
	for _, j := range q.Joins {
		from += sqliteJoin(q.Source, j.Relation)
	}
	args = []any{}
	var predicate func(*Predicate) string
	predicate = func(p *Predicate) string {
		if p.Not != nil {
			return "NOT (" + predicate(p.Not) + ")"
		}
		if p.And != nil || p.Or != nil {
			items := p.And
			op := " AND "
			if p.Or != nil {
				items = p.Or
				op = " OR "
			}
			parts := []string{}
			for i := range items {
				parts = append(parts, "("+predicate(&items[i])+")")
			}
			return strings.Join(parts, op)
		}
		f := sqliteField(q.Source, p.Field)
		meta, _ := NewCatalog().field(q.Source, p.Field)
		if meta.Type == "date" {
			f = "julianday(" + f + ")"
		}
		placeholder := "?"
		if meta.Type == "date" {
			placeholder = "julianday(?)"
		}
		switch p.Op {
		case "is_empty", "is_not_empty":
			empty := f + " IS NULL"
			if meta.Type == "text" {
				empty = "(" + empty + " OR " + f + " = '')"
			}
			if p.Op == "is_not_empty" {
				return "NOT (" + empty + ")"
			}
			return empty
		case "contains", "starts_with":
			value := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(p.Value.(string))
			if p.Op == "contains" {
				value = "%" + value
			}
			args = append(args, value+"%")
			return f + ` LIKE ? ESCAPE '\'`
		case "in":
			values := p.Value.([]any)
			marks := make([]string, len(values))
			for i, v := range values {
				args = append(args, v)
				marks[i] = "?"
			}
			return f + " IN (" + strings.Join(marks, ",") + ")"
		case "between":
			values := p.Value.([]any)
			args = append(args, values...)
			return f + " BETWEEN " + placeholder + " AND " + placeholder
		default:
			ops := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
			args = append(args, p.Value)
			return f + " " + ops[p.Op] + " " + placeholder
		}
	}
	if q.Where != nil {
		from += " WHERE " + predicate(q.Where)
	}
	groups := []string{}
	for _, g := range q.GroupBy {
		groups = append(groups, sqliteField(q.Source, g))
	}
	if len(groups) > 0 {
		from += " GROUP BY " + strings.Join(groups, ",")
	}
	base := "SELECT " + strings.Join(fields, ",") + from
	countSQL = "SELECT COUNT(*) FROM (" + base + ") AS query_count"
	countArgs = append([]any(nil), args...)
	order := []string{}
	if displayGroup != nil {
		expression, ok := outputs[displayGroup.Field]
		if !ok || (displayGroup.Direction != "asc" && displayGroup.Direction != "desc") {
			err = invalid("invalid_value", "view.options.group", "invalid grouping field or direction")
			return
		}
		typ := ""
		for _, s := range q.Select {
			if selectKey(s) == displayGroup.Field {
				f, _ := NewCatalog().field(q.Source, s.Field)
				typ = f.Type
			}
		}
		empty := expression + " IS NULL"
		if typ == "text" {
			empty = "(" + empty + " OR " + expression + "='')"
		}
		order = append(order, "CASE WHEN "+empty+" THEN 0 ELSE 1 END ASC", "CASE WHEN "+empty+" THEN NULL ELSE "+expression+" END "+strings.ToUpper(displayGroup.Direction))
	}
	for _, o := range q.OrderBy {
		expr, ok := outputs[o.Key]
		if !ok {
			expr = sqliteField(q.Source, o.Key)
		}
		order = append(order, expr+" "+strings.ToUpper(o.Direction))
	}
	if len(groups) > 0 {
		for _, g := range groups {
			order = append(order, g+" ASC")
		}
	} else if !aggregate {
		order = append(order, "b.id ASC")
		for _, j := range q.Joins {
			order = append(order, quoted("r_"+j.Relation)+".id ASC")
		}
	}
	if len(order) > 0 {
		base += " ORDER BY " + strings.Join(order, ",")
	}
	sqlText = base + " LIMIT ? OFFSET ?"
	args = append(args, q.Limit, q.Offset)
	return
}
func (e *SQLiteExecutor) Execute(ctx context.Context, input NormalizedQuery) (result QueryResult, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer func() {
		if err != nil && (ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
			err = invalid("query_timeout", "query", "query timed out or was canceled")
		}
	}()
	q, err := ValidateAndNormalize(input.QueryRequest, NewCatalog())
	if err != nil {
		return result, err
	}
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	result, err = ExecuteTx(ctx, tx, q, time.Now())
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// ExecuteTx permits multiple pages to share a snapshot, deadline and clock.
func ExecuteTx(ctx context.Context, tx *sql.Tx, input NormalizedQuery, asOf time.Time) (result QueryResult, err error) {
	q, err := ValidateAndNormalize(input.QueryRequest, NewCatalog())
	if err != nil {
		return result, err
	}
	sqlText, args, countSQL, countArgs, err := compileSQLite(q)
	if err != nil {
		return result, err
	}
	sqlText, args = bindClock(sqlText, args, asOf)
	countSQL, countArgs = bindClock(countSQL, countArgs, asOf)
	result = QueryResult{Columns: []Column{}, Rows: []map[string]any{}, Aggregates: map[string]any{}}
	if err = tx.QueryRowContext(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, err
	}
	aggregateOnly := len(q.GroupBy) == 0
	for _, s := range q.Select {
		typ := "number"
		if s.Field != "" {
			f, _ := NewCatalog().field(q.Source, s.Field)
			typ = f.Type
		}
		if s.Aggregate == "count" || s.Aggregate == "count_distinct" || s.Aggregate == "sum" || s.Aggregate == "avg" {
			typ = "number"
		}
		key := selectKey(s)
		result.Columns = append(result.Columns, Column{Key: key, Label: key, Type: typ})
		aggregateOnly = aggregateOnly && s.Aggregate != ""
	}
	rows, err := tx.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]any, len(q.Select))
		ptrs := make([]any, len(values))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return result, err
		}
		row := map[string]any{}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if tm, ok := v.(time.Time); ok {
				v = tm.Format("2006-01-02 15:04:05")
			}
			if result.Columns[i].Type == "boolean" && v != nil {
				v = fmt.Sprint(v) != "0"
			}
			row[result.Columns[i].Key] = v
		}
		result.Rows = append(result.Rows, row)
		if aggregateOnly {
			result.Aggregates = row
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	result.RowCount = len(result.Rows)
	next := q.Offset + result.RowCount
	if result.RowCount > 0 && next < result.TotalCount {
		result.NextOffset = &next
	}
	return result, nil
}

// Number existing positional bindings before adding a repeated clock binding.
func bindClock(text string, args []any, asOf time.Time) (string, []any) {
	if !strings.Contains(text, "datetime('now')") {
		return text, args
	}
	index := 0
	var out strings.Builder
	for _, c := range text {
		if c == '?' {
			index++
			fmt.Fprintf(&out, "?%d", index)
		} else {
			out.WriteRune(c)
		}
	}
	text = strings.ReplaceAll(out.String(), "datetime('now')", fmt.Sprintf("?%d", len(args)+1))
	return text, append(args, asOf.In(time.Local).Format(tutil.Layout))
}

// CompileTableRead exposes a trusted projection for full-set statistics. Its
// unpaged form is the exact inner SELECT used by the existing count query.
func CompileTableRead(input NormalizedQuery, asOf time.Time, group *DisplayGroup) (ReadPlan, error) {
	q, err := ValidateAndNormalize(input.QueryRequest, NewCatalog())
	if err != nil {
		return ReadPlan{}, err
	}
	for _, s := range q.Select {
		if s.Aggregate != "" {
			return ReadPlan{}, invalid("invalid_query", "query_mode", "detail mode cannot contain aggregates")
		}
	}
	if len(q.GroupBy) > 0 {
		return ReadPlan{}, invalid("invalid_query", "query_mode", "detail mode cannot contain SQL GROUP BY")
	}
	text, args, count, countArgs, err := compileSQLiteGroup(q, group)
	if err != nil {
		return ReadPlan{}, err
	}
	all := strings.TrimSuffix(strings.TrimPrefix(count, "SELECT COUNT(*) FROM ("), ") AS query_count")
	text, args = bindClock(text, args, asOf)
	all, countArgs = bindClock(all, countArgs, asOf)
	cols := []Column{}
	for _, s := range q.Select {
		f, _ := NewCatalog().field(q.Source, s.Field)
		key := selectKey(s)
		semantic := ""
		if f.Type == "date" {
			semantic = "datetime"
			if s.Field == "date" || s.Field == "due_date" {
				semantic = "date"
			}
		}
		unit := ""
		if s.Field == "duration_seconds" {
			unit = "seconds"
			semantic = "duration"
		}
		cols = append(cols, Column{Key: key, Label: key, Type: f.Type, Semantic: semantic, Unit: unit, Nullable: f.Nullable, SummaryOps: SummaryOps(f.Type, f.Aggregates), Groupable: true, Searchable: f.Type == "text"})
	}
	return ReadPlan{SQL: text, Args: args, AllSQL: all, AllArgs: countArgs, Columns: cols}, nil
}
