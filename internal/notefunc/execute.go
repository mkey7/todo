package notefunc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"todo/internal/notes"
	"todo/internal/query"
	"todo/internal/tutil"
)

type Executor struct {
	DB  *sql.DB
	Now func() time.Time
}

func (e Executor) Execute(ctx context.Context, r Request) (out Response, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = notes.Invalid("query_timeout", "query", "query timed out or was canceled")
		}
	}()
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	now = now.In(time.Local)
	r, err = prepareRequest(r, now)
	if err != nil {
		return
	}
	out.Meta = Meta{AsOf: now.Format(time.RFC3339), Timezone: time.Local.String(), Warnings: []Warning{}, Viewport: r.Viewport}
	if err = notes.ValidateView(&r.View, "view"); err != nil {
		return
	}
	if r.View.Type == "" {
		err = notes.Invalid("invalid_value", "view.type", "view required")
		return
	}
	if r.Page.Limit == 0 {
		r.Page.Limit = 50
	}
	if r.Page.Limit < 1 || r.Page.Limit > 500 || r.Page.Offset < 0 || r.Page.Offset > 100000 || len(r.Sort) > 20 {
		err = notes.Invalid("query_limit_exceeded", "page", "invalid pagination or sort")
		return
	}
	raw, _ := json.Marshal(r.Definition)
	if len(raw) > 65536 {
		err = notes.Invalid("query_limit_exceeded", "definition", "maximum 64 KiB")
		return
	}
	var effective *bounds
	// A custom query only validates/consumes context through explicit bindings.
	if r.Definition.Kind != "custom" {
		effective, err = resolve(r.Definition.Params.Range, r.Context, now)
		if err != nil {
			return
		}
	}
	out.Meta.EffectiveRange = window(effective)
	businessRange := effective
	if r.View.Type != "table" {
		if r.Page.Offset != 0 {
			err = notes.Invalid("invalid_value", "page.offset", "event views do not use pagination")
			return
		}
		if r.Viewport == nil {
			err = notes.Invalid("invalid_value", "viewport", "visible date range required")
			return
		}
		var start, end time.Time
		start, err = date(r.Viewport.Start, "viewport.start")
		if err != nil {
			return
		}
		end, err = date(r.Viewport.End, "viewport.end_exclusive")
		if err != nil {
			return
		}
		maxDays := 42
		if r.View.Type == "timeline" {
			maxDays = 7
		}
		if !end.After(start) || end.After(start.AddDate(0, 0, maxDays)) {
			err = notes.Invalid("query_limit_exceeded", "viewport", "visible range exceeds view limit")
			return
		}
		effective = intersect(effective, &bounds{start, end})
	}
	tx, err := e.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return
	}
	defer tx.Rollback()
	if r.Definition.Kind == "custom" {
		out.Result, err = executeCustom(ctx, tx, r, now, &out)
		if err == nil {
			err = tx.Commit()
		}
		out.Meta.Complete = err == nil
		return
	}
	if r.Definition.Kind != "function" {
		err = notes.Invalid("invalid_value", "definition.kind", "select a query function")
		return
	}
	var function *Function
	for _, f := range Catalog() {
		if f.Name == r.Definition.Name {
			copy := f
			function = &copy
		}
	}
	if function == nil || r.Definition.Version != 1 {
		err = notes.Invalid("unsupported_version", "definition", "unknown function or version")
		return
	}
	p := r.Definition.Params
	if p.DateField == "" && r.Definition.Name == "todos" {
		p.DateField = "created_at"
	}
	if len(r.Sort) == 0 && r.Definition.Name == "todos" {
		r.Sort = []query.OrderItem{{Key: p.DateField, Direction: "asc"}}
	}
	if err = validateParams(r.Definition.Name, p); err != nil {
		return
	}
	tagIDs := p.TagIDs.Values
	switch p.TagIDs.Mode {
	case "", "inherit":
		tagIDs = r.Context.TagIDs
	case "explicit":
	default:
		err = notes.Invalid("invalid_value", "params.tag_ids.mode", "expected inherit or explicit")
		return
	}
	if err = checkIDs(ctx, tx, "tags", tagIDs, "params.tag_ids"); err != nil {
		return
	}
	if err = checkIDs(ctx, tx, "todos", p.TodoIDs, "params.todo_ids"); err != nil {
		return
	}
	if err = validateMapping(r.View, function.Columns); err != nil {
		return
	}
	if r.Definition.Name == "time_stats" && r.View.Type != "table" {
		err = notes.Invalid("unsupported_view", "view.type", "aggregated statistics support table only")
		return
	}
	if r.Definition.Name == "todos" && r.View.Type == "timeline" {
		err = notes.Invalid("unsupported_view", "view.type", "tasks have no time interval")
		return
	}
	var base string
	var args []any
	if r.Definition.Name == "todos" {
		base, args = todoSQL(p, tagIDs, businessRange, effective, r.View)
		if r.View.Type == "calendar" {
			original, values := todoSQL(p, tagIDs, businessRange, nil, notes.View{Type: "table"})
			field := r.View.Mapping.Date
			if field == "" {
				field = r.View.Mapping.Start
			}
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM (`+original+`) WHERE "`+field+`" IS NULL OR "`+field+`"=''`, values...).Scan(&count); err != nil {
				return
			}
			if count > 0 {
				out.Meta.Warnings = append(out.Meta.Warnings, Warning{"missing_date", "记录无日期，未放入日历", count})
			}
		}
	} else {
		base, args = entrySQL(p, tagIDs, effective, now)
	}
	if r.Definition.Search != nil {
		sourceColumns := function.Columns
		if r.Definition.Name == "time_stats" {
			sourceColumns = Catalog()[1].Columns
		}
		fields := map[string]query.Field{}
		for _, c := range sourceColumns {
			fields[c.Key] = query.Field{Key: c.Key, Type: c.Type}
		}
		search, e := searchPredicate(r.Definition.Search, fields, nil)
		if e != nil {
			err = e
			return
		}
		if search != nil {
			conditions := []string{}
			for _, p := range search.Or {
				conditions = append(conditions, `"`+p.Field+`" LIKE ? ESCAPE '\'`)
				value := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(p.Value.(string))
				args = append(args, "%"+value+"%")
			}
			base = `SELECT * FROM (` + base + `) WHERE (` + strings.Join(conditions, " OR ") + `)`
		}
	}
	if r.Definition.Name == "time_stats" {
		out.Result, err = statistics(ctx, tx, base, args, p, effective, now, r, &out.Meta)
	} else {
		out.Result, err = selectRows(ctx, tx, base, args, function.Columns, r, &out)
	}
	if err != nil {
		return
	}
	out.Meta.Complete = true
	err = tx.Commit()
	return
}
func validateParams(name string, p notes.Params) error {
	if len(p.TodoIDs) > 100 || len(p.TagIDs.Values) > 100 {
		return notes.Invalid("query_limit_exceeded", "params", "maximum 100 IDs")
	}
	if name == "todos" {
		if p.DateField != "" {
			if e := enum(p.DateField, []string{"created_at", "due_date", "completed_at"}, "params.date_field"); e != nil {
				return e
			}
		}
		if p.Status != "" {
			if e := enum(p.Status, []string{"all", "pending", "in_progress", "done"}, "params.status"); e != nil {
				return e
			}
		}
		if p.TagRule != "" {
			if e := enum(p.TagRule, []string{"any", "all"}, "params.tag_rule"); e != nil {
				return e
			}
		}
		if len(p.TodoIDs) > 0 || p.GroupBy != "" || p.IncludeRunning {
			return notes.Invalid("invalid_value", "params", "unsupported task parameter")
		}
	} else {
		if p.Status != "" || p.DateField != "" || p.Priority != nil || p.TagRule != "" {
			return notes.Invalid("invalid_value", "params", "unsupported time parameter")
		}
		if name == "time_entries" && p.GroupBy != "" {
			return notes.Invalid("invalid_value", "params.group_by", "grouping only supported by statistics")
		}
		if p.GroupBy != "" {
			return enum(p.GroupBy, []string{"total", "todo", "tag", "day"}, "params.group_by")
		}
	}
	return nil
}
func checkIDs(ctx context.Context, tx *sql.Tx, table string, ids []int64, path string) error {
	if len(ids) > 100 {
		return notes.Invalid("query_limit_exceeded", path, "maximum 100 IDs")
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id < 1 || seen[id] {
			return notes.Invalid("invalid_value", path, "IDs must be positive and unique")
		}
		seen[id] = true
	}
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id IN (`+marks(len(ids))+`)`, args...).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return notes.Invalid("invalid_value", path, "referenced record no longer exists")
	}
	return nil
}
func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func addIDs(where *[]string, args *[]any, field string, ids []int64) {
	if len(ids) == 0 {
		return
	}
	*where = append(*where, field+" IN ("+marks(len(ids))+")")
	for _, id := range ids {
		*args = append(*args, id)
	}
}
func todoSQL(p notes.Params, tags []int64, b, visible *bounds, v notes.View) (string, []any) {
	status := `CASE WHEN EXISTS(SELECT 1 FROM todo_tags st JOIN tags g ON g.id=st.tag_id WHERE st.todo_id=t.id AND g.name='已完成') THEN 'done' WHEN EXISTS(SELECT 1 FROM todo_tags st JOIN tags g ON g.id=st.tag_id WHERE st.todo_id=t.id AND g.name='进行中') THEN 'in_progress' ELSE 'pending' END`
	base := `SELECT t.id,t.title,t.priority,CAST(t.due_date AS TEXT) due_date,CAST(t.created_at AS TEXT) created_at,CAST(t.completed_at AS TEXT) completed_at,` + status + ` status FROM todos t`
	where := []string{"1=1"}
	args := []any{}
	field := p.DateField
	if field == "" {
		field = "created_at"
	}
	if b != nil {
		where = append(where, `CAST(t.`+field+` AS TEXT)>=? AND CAST(t.`+field+` AS TEXT)<?`)
		args = append(args, b.start.Format(tutil.DateLayout), b.end.Format(tutil.DateLayout))
	}
	if len(tags) > 0 {
		rule := p.TagRule
		if rule == "all" {
			for _, id := range tags {
				where = append(where, `EXISTS(SELECT 1 FROM todo_tags tt WHERE tt.todo_id=t.id AND tt.tag_id=?)`)
				args = append(args, id)
			}
		} else {
			where = append(where, `EXISTS(SELECT 1 FROM todo_tags tt WHERE tt.todo_id=t.id AND tt.tag_id IN (`+marks(len(tags))+`))`)
			for _, id := range tags {
				args = append(args, id)
			}
		}
	}
	if p.Priority != nil {
		where = append(where, `t.priority=?`)
		args = append(args, *p.Priority)
	}
	if p.Status != "" && p.Status != "all" {
		where = append(where, "("+status+")=?")
		args = append(args, p.Status)
	}
	// Event mapping can differ from the business date field; intersect both.
	if v.Type == "calendar" && visible != nil {
		f := v.Mapping.Date
		if f == "" {
			f = v.Mapping.Start
		}
		if f == "due_date" || f == "created_at" || f == "completed_at" {
			where = append(where, `CAST(t.`+f+` AS TEXT)>=? AND CAST(t.`+f+` AS TEXT)<?`)
			args = append(args, visible.start.Format(tutil.DateLayout), visible.end.Format(tutil.DateLayout))
		}
	}
	return base + " WHERE " + strings.Join(where, " AND "), args
}
func entrySQL(p notes.Params, tags []int64, b *bounds, now time.Time) (string, []any) {
	inner := `SELECT te.*,COALESCE(te.end_time,?) effective_end,COALESCE(t.title,'未关联任务') task_title,COALESCE(g.name,'未标记') tag_label FROM time_entries te LEFT JOIN todos t ON t.id=te.todo_id LEFT JOIN tags g ON g.id=te.tag_id`
	base := `SELECT e.id,e.todo_id,e.task_title title,e.tag_id,CAST(e.start_time AS TEXT) start_time,CAST(e.end_time AS TEXT) end_time,ROUND((julianday(e.effective_end)-julianday(e.start_time))*86400,3) duration_seconds,e.note,e.tag_label FROM (` + inner + `) e`
	args := []any{now.Format(tutil.Layout)}
	where := []string{"1=1"}
	if b != nil && !b.end.After(b.start) {
		where = append(where, "0=1")
	}
	if !p.IncludeRunning {
		where = append(where, `e.end_time IS NOT NULL`)
	}
	if b != nil {
		where = append(where, `e.start_time<? AND e.effective_end>?`)
		args = append(args, b.end.Format(tutil.Layout), b.start.Format(tutil.Layout))
	}
	addIDs(&where, &args, "e.tag_id", tags)
	addIDs(&where, &args, "e.todo_id", p.TodoIDs)
	return base + " WHERE " + strings.Join(where, " AND "), args
}
func selectRows(ctx context.Context, tx *sql.Tx, base string, args []any, cols []query.Column, r Request, out *Response) (result query.QueryResult, err error) {
	meta := &out.Meta
	result = query.QueryResult{Columns: cols, Rows: []map[string]any{}, Aggregates: map[string]any{}}
	if r.Definition.Name == "time_entries" {
		var bad int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM (`+base+`) WHERE duration_seconds IS NULL OR duration_seconds<0`, args...).Scan(&bad); err != nil {
			return
		}
		if bad > 0 {
			meta.Warnings = append(meta.Warnings, Warning{"invalid_interval", "异常时间记录已跳过", bad})
		}
		base = `SELECT * FROM (` + base + `) WHERE duration_seconds>=0`
	}
	if r.View.Type == "table" {
		if err = query.ValidateTableOptions(cols, r.View.Options.Group, r.View.Options.Summaries); err != nil {
			return
		}
		order, e := orderSQL(r.Sort, cols, r.Definition.Name)
		if e != nil {
			return result, e
		}
		if group := r.View.Options.Group; group != nil {
			typ := ""
			for _, c := range cols {
				if c.Key == group.Field {
					typ = c.Type
				}
			}
			f := `"` + group.Field + `"`
			empty := f + " IS NULL"
			if typ == "text" {
				empty = "(" + empty + " OR " + f + "='')"
			}
			order = "CASE WHEN " + empty + " THEN 0 ELSE 1 END ASC,CASE WHEN " + empty + " THEN NULL ELSE " + f + " END " + group.Direction + "," + order
		}
		keys := []string{}
		for _, c := range cols {
			keys = append(keys, `"`+c.Key+`"`)
		}
		bindings := append(append([]any{}, args...), r.Page.Limit, r.Page.Offset)
		plan := query.ReadPlan{SQL: "SELECT " + strings.Join(keys, ",") + " FROM (" + base + ") ORDER BY " + order + " LIMIT ? OFFSET ?", Args: bindings, AllSQL: base, AllArgs: args, Columns: cols}
		result, out.Summaries, out.Groups, err = query.ReadTableTx(ctx, tx, plan, r.Page.Offset, r.View.Options.Group, r.View.Options.Summaries)
		return
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM (`+base+`)`, args...).Scan(&result.TotalCount); err != nil {
		return
	}
	limit, offset := r.Page.Limit, r.Page.Offset
	if r.View.Type != "table" {
		if result.TotalCount > 5000 {
			return result, notes.Invalid("visualization_limit_exceeded", "viewport", "more than 5000 events; narrow the range or use table")
		}
		limit = 5000
		offset = 0
	}
	order, err := orderSQL(r.Sort, cols, r.Definition.Name)
	if err != nil {
		return result, err
	}
	keys := make([]string, len(cols))
	for i, c := range cols {
		keys[i] = `"` + c.Key + `"`
	}
	// Bounded streaming pages share the transaction and fixed clock.
	for fetched := 0; fetched < limit; {
		size := min(500, limit-fetched)
		pageArgs := append(append([]any{}, args...), size, offset+fetched)
		rows, e := tx.QueryContext(ctx, `SELECT `+strings.Join(keys, ",")+` FROM (`+base+`) ORDER BY `+order+` LIMIT ? OFFSET ?`, pageArgs...)
		if e != nil {
			return result, e
		}
		n := 0
		for rows.Next() {
			values := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range values {
				ptr[i] = &values[i]
			}
			if e = rows.Scan(ptr...); e != nil {
				rows.Close()
				return result, e
			}
			row := map[string]any{}
			for i, c := range cols {
				row[c.Key] = values[i]
			}
			result.Rows = append(result.Rows, row)
			n++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		fetched += n
		if n < size {
			break
		}
	}
	result.RowCount = len(result.Rows)
	next := offset + result.RowCount
	if r.View.Type == "table" && next < result.TotalCount {
		result.NextOffset = &next
	}
	return result, nil
}
func orderSQL(order []query.OrderItem, cols []query.Column, name string) (string, error) {
	allowed := map[string]bool{}
	for _, c := range cols {
		allowed[c.Key] = true
	}
	parts := []string{}
	for i, o := range order {
		if !allowed[o.Key] || (o.Direction != "asc" && o.Direction != "desc") {
			return "", notes.Invalid("invalid_value", fmt.Sprintf("sort[%d]", i), "unknown column or direction")
		}
		parts = append(parts, `"`+o.Key+`" `+o.Direction)
	}
	if name == "time_entries" {
		parts = append(parts, `start_time ASC,id ASC`)
	} else if name == "todos" {
		parts = append(parts, `created_at ASC,id ASC`)
	} else {
		parts = append(parts, `group_key ASC`)
	}
	return strings.Join(parts, ","), nil
}
func statistics(ctx context.Context, tx *sql.Tx, base string, args []any, p notes.Params, b *bounds, now time.Time, r Request, meta *Meta) (query.QueryResult, error) {
	cols := Catalog()[2].Columns
	result := query.QueryResult{Columns: cols, Rows: []map[string]any{}, Aggregates: map[string]any{}}
	rows, err := tx.QueryContext(ctx, `SELECT id,todo_id,title,tag_id,start_time,end_time,tag_label FROM (`+base+`)`, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	bad := 0
	groups := map[string]map[string]any{}
	if p.GroupBy == "" || p.GroupBy == "total" {
		groups["total"] = map[string]any{"group_key": "total", "group_label": "总计", "duration_seconds": float64(0), "entry_count": int64(0)}
	}
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		var id int64
		var todo, tag sql.NullInt64
		var title, start, label string
		var end sql.NullString
		if err = rows.Scan(&id, &todo, &title, &tag, &start, &end, &label); err != nil {
			return result, err
		}
		s, e := time.ParseInLocation(tutil.Layout, start, time.Local)
		if e != nil {
			bad++
			continue
		}
		finish := now
		if end.Valid {
			finish, e = time.ParseInLocation(tutil.Layout, end.String, time.Local)
			if e != nil {
				bad++
				continue
			}
		}
		if finish.Before(s) {
			bad++
			continue
		}
		if b != nil {
			s = later(s, b.start)
			finish = earlier(finish, b.end)
		}
		if !finish.After(s) {
			continue
		}
		add := func(key, label string, seconds float64) {
			g := groups[key]
			if g == nil {
				g = map[string]any{"group_key": key, "group_label": label, "duration_seconds": float64(0), "entry_count": int64(0)}
				groups[key] = g
			}
			g["duration_seconds"] = g["duration_seconds"].(float64) + seconds
			g["entry_count"] = g["entry_count"].(int64) + 1
		}
		switch p.GroupBy {
		case "day":
			for day := s; day.Before(finish); {
				if err = ctx.Err(); err != nil {
					return result, err
				}
				next := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, 1)
				stop := earlier(next, finish)
				key := day.Format(tutil.DateLayout)
				add(key, key, stop.Sub(day).Seconds())
				day = stop
			}
		case "todo":
			key := "none"
			if todo.Valid {
				key = strconv.FormatInt(todo.Int64, 10)
			}
			add(key, title, finish.Sub(s).Seconds())
		case "tag":
			key := "none"
			if tag.Valid {
				key = strconv.FormatInt(tag.Int64, 10)
			}
			add(key, label, finish.Sub(s).Seconds())
		default:
			add("total", "总计", finish.Sub(s).Seconds())
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	if bad > 0 {
		meta.Warnings = append(meta.Warnings, Warning{"invalid_interval", "异常时间记录已跳过", bad})
	}
	for _, g := range groups {
		result.Rows = append(result.Rows, g)
	}
	if _, err = orderSQL(r.Sort, cols, "time_stats"); err != nil {
		return result, err
	}
	sort.SliceStable(result.Rows, func(i, j int) bool {
		a, b := result.Rows[i], result.Rows[j]
		for _, o := range append(r.Sort, query.OrderItem{Key: "group_key", Direction: "asc"}) {
			cmp := compare(a[o.Key], b[o.Key])
			if cmp != 0 {
				if o.Direction == "desc" {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return false
	})
	result.TotalCount = len(result.Rows)
	start := min(r.Page.Offset, len(result.Rows))
	end := min(start+r.Page.Limit, len(result.Rows))
	result.Rows = result.Rows[start:end]
	result.RowCount = len(result.Rows)
	if end < result.TotalCount {
		result.NextOffset = &end
	}
	if p.GroupBy == "" || p.GroupBy == "total" {
		if len(result.Rows) > 0 {
			result.Aggregates = result.Rows[0]
		}
	}
	return result, nil
}
func compare(a, b any) int {
	number := func(v any) (float64, bool) {
		switch n := v.(type) {
		case int64:
			return float64(n), true
		case float64:
			return n, true
		}
		return 0, false
	}
	if x, ok := number(a); ok {
		y, _ := number(b)
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}
