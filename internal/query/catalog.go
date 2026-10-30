package query

import "strings"

type Field struct {
	SummaryOps    []string `json:"summary_ops,omitempty"`
	Groupable     bool     `json:"groupable,omitempty"`
	Searchable    bool     `json:"searchable,omitempty"`
	NoteFilterOps []string `json:"note_filter_ops,omitempty"`
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Type          string   `json:"type"`
	Nullable      bool     `json:"nullable"`
	Operators     []string `json:"operators"`
	Aggregates    []string `json:"aggregates"`
}
type Relation struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Source string `json:"source"`
}
type Source struct {
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	Fields    []Field    `json:"fields"`
	Relations []Relation `json:"relations"`
	Defaults  []string   `json:"defaults"`
}
type Catalog struct {
	Sources []Source `json:"sources"`
}

func (c Catalog) source(key string) (Source, bool) {
	for _, s := range c.Sources {
		if s.Key == key {
			return s, true
		}
	}
	return Source{}, false
}
func (c Catalog) Fields(key string) []Field       { s, _ := c.source(key); return s.Fields }
func (c Catalog) Relations(key string) []Relation { s, _ := c.source(key); return s.Relations }
func (c Catalog) field(source, key string) (Field, bool) {
	for _, f := range c.Fields(source) {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}
func NewCatalog() Catalog {
	c := Catalog{Sources: []Source{
		{Key: "todos", Label: "任务", Defaults: []string{"id", "title", "priority"}, Relations: []Relation{{"tags", "标签", "tags"}, {"time_entries", "时间记录", "time_entries"}, {"parent", "父任务", "todos"}, {"children", "子任务", "todos"}}},
		{Key: "tags", Label: "标签", Defaults: []string{"id", "name", "include_in_stats"}, Relations: []Relation{{"todos", "任务", "todos"}, {"time_entries", "时间记录", "time_entries"}}},
		{Key: "time_entries", Label: "时间记录", Defaults: []string{"id", "start_time", "end_time", "duration_seconds"}, Relations: []Relation{{"todo", "任务", "todos"}, {"tag", "标签", "tags"}}},
		{Key: "daily_summaries", Label: "每日总结", Defaults: []string{"id", "date", "content"}, Relations: []Relation{}},
		{Key: "weekly_summaries", Label: "每周总结", Defaults: []string{"id", "week", "content"}, Relations: []Relation{}},
	}}
	definitions := map[string][]string{
		"todos":            {"id:number", "title:text", "description:text", "priority:number", "due_date:date?", "created_at:date", "completed_at:date?", "parent_id:number?"},
		"tags":             {"id:number", "name:text", "description:text", "color:text", "include_in_stats:boolean", "created_at:date"},
		"time_entries":     {"id:number", "todo_id:number?", "tag_id:number?", "start_time:date", "end_time:date?", "note:text", "created_at:date", "duration_seconds:number"},
		"daily_summaries":  {"id:number", "date:date", "content:text", "updated_at:date"},
		"weekly_summaries": {"id:number", "week:text", "content:text", "updated_at:date"},
	}
	labels := map[string]string{"id": "ID", "title": "标题", "description": "描述", "priority": "优先级", "due_date": "截止日期", "created_at": "创建时间", "completed_at": "完成时间", "parent_id": "父任务 ID", "name": "名称", "color": "颜色", "include_in_stats": "计入统计", "todo_id": "任务 ID", "tag_id": "标签 ID", "start_time": "开始时间", "end_time": "结束时间", "note": "备注", "duration_seconds": "时长（秒）", "date": "日期", "week": "周", "content": "内容", "updated_at": "更新时间"}
	ops := map[string][]string{"text": {"eq", "neq", "contains", "starts_with", "is_empty", "is_not_empty"}, "number": {"eq", "neq", "gt", "gte", "lt", "lte", "in"}, "date": {"eq", "neq", "gt", "gte", "lt", "lte", "between", "is_empty", "is_not_empty"}, "boolean": {"eq"}}
	for i := range c.Sources {
		s := &c.Sources[i]
		for _, d := range definitions[s.Key] {
			p := strings.Split(d, ":")
			typ := strings.TrimSuffix(p[1], "?")
			a := []string{"count", "count_distinct", "min", "max"}
			if typ == "number" {
				a = append(a, "sum", "avg")
			}
			s.Fields = append(s.Fields, Field{Key: p[0], Label: labels[p[0]], Type: typ, Nullable: strings.HasSuffix(p[1], "?"), Operators: ops[typ], Aggregates: a, SummaryOps: SummaryOps(typ, a), Groupable: true, Searchable: typ == "text", NoteFilterOps: func() []string {
				if typ == "date" {
					return []string{"in_relative_range"}
				}
				return nil
			}()})
		}
	}
	// Relation fields are one hop only; callers must explicitly enable each relation.
	for i := range c.Sources {
		s := &c.Sources[i]
		for _, r := range s.Relations {
			for _, f := range c.Fields(r.Source) {
				if strings.Contains(f.Key, ".") {
					continue
				}
				if f.Key == "duration_seconds" {
					f.Aggregates = []string{"count", "count_distinct", "min", "max"}
					f.SummaryOps = SummaryOps(f.Type, f.Aggregates)
				}
				f.Key = r.Key + "." + f.Key
				f.Label = r.Label + " · " + f.Label
				f.Nullable = true
				s.Fields = append(s.Fields, f)
			}
		}
	}
	return c
}
