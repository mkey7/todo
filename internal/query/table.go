package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

type DisplayGroup struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}
type Metric struct {
	ID    string `json:"id"`
	Field string `json:"field,omitempty"`
	Op    string `json:"op"`
}
type MetricValue struct {
	ID    string `json:"id"`
	Field string `json:"field,omitempty"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}
type Summaries struct {
	Scope           string        `json:"scope"`
	MatchedRowCount int           `json:"matched_row_count"`
	Items           []MetricValue `json:"items"`
	Complete        bool          `json:"complete"`
}
type GroupKey struct {
	Type  string `json:"type"`
	Value any    `json:"value,omitempty"`
}
type GroupResult struct {
	Key       GroupKey      `json:"key"`
	Label     string        `json:"label"`
	Indexes   []int         `json:"page_row_indexes"`
	RowCount  int           `json:"row_count"`
	Summaries []MetricValue `json:"summaries"`
	Previous  bool          `json:"has_previous_rows"`
	Next      bool          `json:"has_next_rows"`
}

// ReadPlan only contains SQL produced by trusted server compilers.
type ReadPlan struct {
	SQL     string
	Args    []any
	AllSQL  string
	AllArgs []any
	Columns []Column
}

func SummaryOps(typ string, aggregates []string) []string {
	ops := []string{"count_all", "count_values", "count_not_empty", "count_empty", "count_distinct", "percent_empty", "percent_not_empty"}
	for _, op := range []string{"sum", "avg", "min", "max"} {
		if (typ == "number" || ((typ == "text" || typ == "date") && (op == "min" || op == "max"))) && (aggregates == nil || slices.Contains(aggregates, op)) {
			ops = append(ops, op)
		}
	}
	return ops
}
func ValidateMetrics(metrics []Metric) error {
	if len(metrics) > 10 {
		return invalid("query_limit_exceeded", "view.options.summaries", "maximum 10 metrics")
	}
	ids := map[string]bool{}
	for i, m := range metrics {
		path := fmt.Sprintf("view.options.summaries[%d]", i)
		if m.ID == "" || utf8.RuneCountInString(m.ID) > 64 || ids[m.ID] {
			return invalid("invalid_value", path+".id", "unique nonempty metric ID required (maximum 64 characters)")
		}
		ids[m.ID] = true
		if !slices.Contains(SummaryOps("number", nil), m.Op) {
			return invalid("invalid_value", path+".op", "unknown summary operation")
		}
		if m.Op != "count_all" && m.Field == "" {
			return invalid("invalid_value", path+".field", "summary field required")
		}
	}
	return nil
}
func ValidateTableOptions(columns []Column, group *DisplayGroup, metrics []Metric) error {
	if err := ValidateMetrics(metrics); err != nil {
		return err
	}
	fields := map[string]Column{}
	for _, c := range columns {
		fields[c.Key] = c
	}
	if group != nil {
		c, ok := fields[group.Field]
		if !ok || !slices.Contains([]string{"number", "text", "date", "boolean"}, c.Type) {
			return invalid("unknown_field", "view.options.group.field", "select a scalar output field")
		}
		if group.Direction != "asc" && group.Direction != "desc" {
			return invalid("invalid_value", "view.options.group.direction", "expected asc or desc")
		}
	}
	for i, m := range metrics {
		if m.Op == "count_all" && m.Field == "" {
			continue
		}
		c, ok := fields[m.Field]
		ops := c.SummaryOps
		if ops == nil {
			ops = SummaryOps(c.Type, nil)
		}
		if !ok || !slices.Contains(ops, m.Op) {
			return invalid("invalid_value", fmt.Sprintf("view.options.summaries[%d]", i), "operation is not supported by this output field")
		}
	}
	return nil
}
func metricExpressions(columns []Column, metrics []Metric) []string {
	fields := map[string]Column{}
	for _, c := range columns {
		fields[c.Key] = c
	}
	out := []string{}
	for _, m := range metrics {
		f := quoted(m.Field)
		empty := f + " IS NULL"
		if fields[m.Field].Type == "text" {
			empty = "(" + empty + " OR " + f + "='')"
		}
		value := "CASE WHEN " + empty + " THEN NULL ELSE " + f + " END"
		var expr string
		switch m.Op {
		case "count_all":
			expr = "COUNT(*)"
		case "count_values", "count_not_empty":
			expr = "COUNT(" + value + ")"
		case "count_empty":
			expr = "COUNT(CASE WHEN " + empty + " THEN 1 END)"
		case "count_distinct":
			expr = "COUNT(DISTINCT " + value + ")"
		case "percent_empty":
			expr = "100.0*COUNT(CASE WHEN " + empty + " THEN 1 END)/NULLIF(COUNT(*),0)"
		case "percent_not_empty":
			expr = "100.0*COUNT(" + value + ")/NULLIF(COUNT(*),0)"
		default:
			expr = strings.ToUpper(m.Op) + "(" + value + ")"
		}
		out = append(out, expr)
	}
	return out
}
func statisticsTx(ctx context.Context, tx *sql.Tx, plan ReadPlan, metrics []Metric, where string, args []any) (int, []MetricValue, error) {
	expressions := append([]string{"COUNT(*)"}, metricExpressions(plan.Columns, metrics)...)
	values := make([]any, len(expressions))
	ptrs := make([]any, len(values))
	for i := range values {
		ptrs[i] = &values[i]
	}
	bindings := append(append([]any{}, plan.AllArgs...), args...)
	if err := tx.QueryRowContext(ctx, "SELECT "+strings.Join(expressions, ",")+" FROM ("+plan.AllSQL+") AS filtered"+where, bindings...).Scan(ptrs...); err != nil {
		return 0, nil, err
	}
	count := int(values[0].(int64))
	out := []MetricValue{}
	for i, m := range metrics {
		v := values[i+1]
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		if n, ok := v.(float64); ok && (math.IsInf(n, 0) || math.IsNaN(n)) {
			return 0, nil, invalid("invalid_value", "summaries", "non-finite statistic")
		}
		out = append(out, MetricValue{m.ID, m.Field, m.Op, v})
	}
	return count, out, nil
}
func ReadRowsTx(ctx context.Context, tx *sql.Tx, plan ReadPlan) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(plan.Columns))
		ptrs := make([]any, len(values))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, c := range plan.Columns {
			v := values[i]
			if bytes, ok := v.([]byte); ok {
				v = string(bytes)
			}
			if c.Type == "boolean" && v != nil {
				v = fmt.Sprint(v) != "0"
			}
			row[c.Key] = v
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
func ReadTableTx(ctx context.Context, tx *sql.Tx, plan ReadPlan, offset int, group *DisplayGroup, metrics []Metric) (result QueryResult, summaries *Summaries, groups []GroupResult, err error) {
	if err = ValidateTableOptions(plan.Columns, group, metrics); err != nil {
		return
	}
	result = QueryResult{Columns: plan.Columns, Rows: []map[string]any{}, Aggregates: map[string]any{}}
	if len(metrics) > 0 {
		var items []MetricValue
		result.TotalCount, items, err = statisticsTx(ctx, tx, plan, metrics, "", nil)
		if err != nil {
			return
		}
		summaries = &Summaries{"filtered_result", result.TotalCount, items, true}
	} else {
		err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+plan.AllSQL+")", plan.AllArgs...).Scan(&result.TotalCount)
		if err != nil {
			return
		}
	}
	result.Rows, err = ReadRowsTx(ctx, tx, plan)
	if err != nil {
		return
	}
	result.RowCount = len(result.Rows)
	next := offset + result.RowCount
	if result.RowCount > 0 && next < result.TotalCount {
		result.NextOffset = &next
	}
	if group == nil {
		return
	}
	var column Column
	for _, c := range plan.Columns {
		if c.Key == group.Field {
			column = c
		}
	}
	indices := map[string]int{}
	for i, row := range result.Rows {
		value := row[group.Field]
		empty := value == nil || (column.Type == "text" && value == "")
		key := GroupKey{Type: column.Type, Value: value}
		if empty {
			key = GroupKey{Type: "empty"}
		}
		raw, _ := json.Marshal(key)
		index, ok := indices[string(raw)]
		if !ok {
			index = len(groups)
			indices[string(raw)] = index
			label := fmt.Sprint(value)
			if empty {
				label = "未填写"
			}
			groups = append(groups, GroupResult{Key: key, Label: label, Indexes: []int{}, Summaries: []MetricValue{}})
		}
		groups[index].Indexes = append(groups[index].Indexes, i)
	}
	for i := range groups {
		g := &groups[i]
		field := quoted(group.Field)
		where := " WHERE " + field + "=?"
		bindings := []any{g.Key.Value}
		if g.Key.Type == "empty" {
			where = " WHERE " + field + " IS NULL"
			bindings = nil
			if column.Type == "text" {
				where = " WHERE (" + field + " IS NULL OR " + field + "='')"
			}
		}
		g.RowCount, g.Summaries, err = statisticsTx(ctx, tx, plan, metrics, where, bindings)
		if err != nil {
			return
		}
		// Groups are contiguous. A page starting mid-group has matching rows before it.
		before := 0
		if offset > 0 && g.Indexes[0] == 0 {
			for _, prior := range groups[:i] {
				before += prior.RowCount
			}
			var countBefore int
			// Count preceding groups using the same grouping expression and direction.
			expr := field
			if column.Type == "text" {
				expr = "NULLIF(" + field + ",'')"
			}
			op := "<"
			if group.Direction == "desc" {
				op = ">"
			}
			if g.Key.Type != "empty" {
				countWhere := " WHERE " + expr + " IS NULL OR " + expr + op + "?"
				bindings := append(append([]any{}, plan.AllArgs...), g.Key.Value)
				err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+plan.AllSQL+")"+countWhere, bindings...).Scan(&countBefore)
				if err != nil {
					return
				}
			}
			before = max(0, offset-countBefore)
		}
		g.Previous = before > 0
		g.Next = before+len(g.Indexes) < g.RowCount
	}
	return
}
