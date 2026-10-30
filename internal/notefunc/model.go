// Package notefunc implements trusted, versioned read-only note functions.
package notefunc

import (
	"fmt"
	"slices"
	"time"
	"todo/internal/notes"
	"todo/internal/query"
)

type Parameter struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Options []string `json:"options,omitempty"`
}
type Function struct {
	Name        string         `json:"name"`
	Version     int            `json:"version"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Parameters  []Parameter    `json:"parameters"`
	Columns     []query.Column `json:"columns"`
	Views       []string       `json:"views"`
	Mapping     notes.Mapping  `json:"mapping"`
}

func col(key, label, typ, semantic string) query.Column {
	return query.Column{Key: key, Label: label, Type: typ, Semantic: semantic, Nullable: true, SummaryOps: query.SummaryOps(typ, nil), Groupable: true, Searchable: typ == "text"}
}
func Catalog() []Function {
	common := []Parameter{{Key: "range", Label: "日期范围", Type: "range"}, {Key: "tag_ids", Label: "记录标签（与任务标签不同）", Type: "tags"}}
	ids := Parameter{Key: "todo_ids", Label: "任务 ID（逗号分隔）", Type: "ids"}
	running := Parameter{Key: "include_running", Label: "包含运行中记录", Type: "boolean"}
	todos := []query.Column{col("id", "ID", "number", "entity_id"), col("title", "任务", "text", ""), col("priority", "优先级", "number", ""), col("due_date", "截止日期", "date", "date"), col("created_at", "创建时间", "date", "datetime"), col("completed_at", "完成时间", "date", "datetime"), col("status", "状态", "text", "")}
	entries := []query.Column{col("id", "ID", "number", "entity_id"), col("todo_id", "任务 ID", "number", "entity_id"), col("title", "任务", "text", ""), col("tag_id", "记录标签 ID", "number", "entity_id"), col("start_time", "开始时间", "date", "datetime"), col("end_time", "结束时间", "date", "datetime"), col("duration_seconds", "时长（秒）", "number", "duration"), col("note", "备注", "text", "")}
	entries[6].Unit = "seconds"
	stats := []query.Column{col("group_key", "分组键", "text", ""), col("group_label", "分组", "text", ""), col("duration_seconds", "时长（秒）", "number", "duration"), col("entry_count", "记录数", "number", "")}
	stats[2].Unit = "seconds"
	return []Function{
		{Name: "todos", Version: 1, Label: "任务查询", Description: "任务标签筛选；状态以已完成／进行中标签判定，一任务一行。", Parameters: []Parameter{common[0], {Key: "tag_ids", Label: "任务标签", Type: "tags"}, {Key: "date_field", Label: "按哪个日期筛选", Type: "enum", Options: []string{"created_at", "due_date", "completed_at"}}, {Key: "tag_rule", Label: "标签匹配", Type: "enum", Options: []string{"any", "all"}}, {Key: "status", Label: "状态", Type: "enum", Options: []string{"all", "pending", "in_progress", "done"}}, {Key: "priority", Label: "优先级（留空为全部）", Type: "number"}}, Columns: todos, Views: []string{"table", "calendar"}, Mapping: notes.Mapping{ID: "id", Title: "title", Date: "created_at"}},
		{Name: "time_entries", Version: 1, Label: "时间记录查询", Description: "按区间重叠查询；记录标签筛选，包含所有统计标记的记录标签。", Parameters: append(append([]Parameter{}, common...), ids, running), Columns: entries, Views: []string{"table", "calendar", "timeline"}, Mapping: notes.Mapping{ID: "id", Title: "title", Start: "start_time", End: "end_time"}},
		{Name: "time_stats", Version: 1, Label: "工时统计", Description: "按日期交集计算，跨日拆分；不因任务多标签重复统计。包含所有记录标签。", Parameters: append(append([]Parameter{}, common...), ids, running, Parameter{Key: "group_by", Label: "分组", Type: "enum", Options: []string{"total", "todo", "tag", "day"}}), Columns: stats, Views: []string{"table"}, Mapping: notes.Mapping{}},
	}
}

type Page struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}
type Window struct {
	Start string `json:"start"`
	End   string `json:"end_exclusive"`
}
type Request struct {
	QueryMode  string            `json:"query_mode,omitempty"`
	Definition notes.Definition  `json:"definition"`
	Context    notes.Context     `json:"context"`
	View       notes.View        `json:"view"`
	Viewport   *Window           `json:"viewport,omitempty"`
	Page       Page              `json:"page,omitempty"`
	Sort       []query.OrderItem `json:"sort,omitempty"`
}
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}
type Meta struct {
	AsOf           string    `json:"as_of"`
	Timezone       string    `json:"timezone"`
	EffectiveRange *Window   `json:"effective_range"`
	Viewport       *Window   `json:"viewport"`
	Warnings       []Warning `json:"warnings"`
	Complete       bool      `json:"complete"`
}
type Response struct {
	Summaries *query.Summaries    `json:"summaries,omitempty"`
	Groups    []query.GroupResult `json:"groups,omitempty"`
	Result    query.QueryResult   `json:"result"`
	Meta      Meta                `json:"meta"`
}
type bounds struct{ start, end time.Time }

func date(s, path string) (time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil || d.Format("2006-01-02") != s {
		return d, notes.Invalid("invalid_value", path, "expected a valid YYYY-MM-DD date")
	}
	return d, nil
}
func resolve(r notes.Range, ctx notes.Context, now time.Time) (*bounds, error) {
	if r.Mode == "" || r.Mode == "inherit" {
		r = ctx.Range
	}
	switch r.Mode {
	case "none":
		return nil, nil
	case "fixed":
		s, e := date(r.Start, "range.start")
		if e != nil {
			return nil, e
		}
		end, e := date(r.End, "range.end_exclusive")
		if e != nil {
			return nil, e
		}
		if !end.After(s) {
			return nil, notes.Invalid("invalid_value", "range", "end must follow start")
		}
		return &bounds{s, end}, nil
	case "relative":
		s := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		end := s.AddDate(0, 0, 1)
		switch r.Preset {
		case "today":
		case "past_7_days":
			s = s.AddDate(0, 0, -6)
			end = s.AddDate(0, 0, 7)
		case "this_week":
			s = s.AddDate(0, 0, -(int(s.Weekday())+6)%7)
			end = s.AddDate(0, 0, 7)
		case "this_month":
			s = time.Date(s.Year(), s.Month(), 1, 0, 0, 0, 0, time.Local)
			end = s.AddDate(0, 1, 0)
		default:
			return nil, notes.Invalid("invalid_value", "range.preset", "unknown relative range")
		}
		return &bounds{s, end}, nil
	default:
		return nil, notes.Invalid("invalid_value", "range.mode", "invalid range mode")
	}
}
func window(b *bounds) *Window {
	if b == nil {
		return nil
	}
	return &Window{b.start.Format("2006-01-02"), b.end.Format("2006-01-02")}
}
func intersect(a, b *bounds) *bounds {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &bounds{later(a.start, b.start), earlier(a.end, b.end)}
}
func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func validateMapping(v notes.View, columns []query.Column) error {
	if err := notes.ValidateView(&v, "view"); err != nil {
		return err
	}
	fields := map[string]query.Column{}
	for _, c := range columns {
		fields[c.Key] = c
	}
	for _, key := range v.Options.Columns {
		if _, ok := fields[key]; !ok {
			return notes.Invalid("unknown_field", "view.options.columns", "column not in result")
		}
	}
	if v.Type == "table" {
		return nil
	}
	m := v.Mapping
	for k, value := range map[string]string{"id": m.ID, "title": m.Title, "date": m.Date, "start": m.Start, "end": m.End} {
		if value != "" {
			if _, ok := fields[value]; !ok {
				return notes.Invalid("unsupported_view", "view.mapping."+k, "column not in result")
			}
		}
	}
	if m.Title == "" {
		return notes.Invalid("unsupported_view", "view.mapping.title", "title required")
	}
	key := m.Start
	if v.Type == "calendar" && m.Date != "" {
		key = m.Date
	}
	c, ok := fields[key]
	if !ok || c.Type != "date" {
		return notes.Invalid("unsupported_view", "view.mapping.start", "date or datetime field required")
	}
	if v.Type == "timeline" {
		end, ok := fields[m.End]
		if c.Semantic == "date" || !ok || end.Type != "date" {
			return notes.Invalid("unsupported_view", "view.mapping.end", "datetime start and end required")
		}
	}
	return nil
}
func enum(value string, choices []string, path string) error {
	if !slices.Contains(choices, value) {
		return notes.Invalid("invalid_value", path, fmt.Sprintf("expected one of %v", choices))
	}
	return nil
}
