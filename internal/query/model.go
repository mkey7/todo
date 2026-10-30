package query

import "context"

type QueryRequest struct {
	Source  string       `json:"source"`
	Select  []SelectItem `json:"select"`
	Joins   []Join       `json:"joins"`
	Where   *Predicate   `json:"where,omitempty"`
	GroupBy []string     `json:"group_by"`
	OrderBy []OrderItem  `json:"order_by"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
}
type SelectItem struct {
	Field     string `json:"field,omitempty"`
	Aggregate string `json:"aggregate,omitempty"`
	Alias     string `json:"alias,omitempty"`
}
type Join struct {
	Relation string `json:"relation"`
}
type Predicate struct {
	Field string      `json:"field,omitempty"`
	Op    string      `json:"op,omitempty"`
	Value any         `json:"value,omitempty"`
	And   []Predicate `json:"and,omitempty"`
	Or    []Predicate `json:"or,omitempty"`
	Not   *Predicate  `json:"not,omitempty"`
}
type OrderItem struct {
	Key       string `json:"key"`
	Direction string `json:"direction"`
}
type NormalizedQuery struct{ QueryRequest }
type Column struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Type          string   `json:"type"`
	Semantic      string   `json:"semantic,omitempty"`
	Unit          string   `json:"unit,omitempty"`
	Nullable      bool     `json:"nullable,omitempty"`
	SummaryOps    []string `json:"summary_ops,omitempty"`
	Groupable     bool     `json:"groupable,omitempty"`
	Searchable    bool     `json:"searchable,omitempty"`
	NoteFilterOps []string `json:"note_filter_ops,omitempty"`
}
type QueryResult struct {
	Columns    []Column         `json:"columns"`
	Rows       []map[string]any `json:"rows"`
	RowCount   int              `json:"row_count"`
	TotalCount int              `json:"total_count"`
	Aggregates map[string]any   `json:"aggregates"`
	NextOffset *int             `json:"next_offset"`
}
type Executor interface {
	Execute(context.Context, NormalizedQuery) (QueryResult, error)
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path"`
}

func (e *Error) Error() string                 { return e.Path + ": " + e.Message }
func invalid(code, path, message string) error { return &Error{code, message, path} }
func selectKey(s SelectItem) string {
	if s.Alias != "" {
		return s.Alias
	}
	if s.Aggregate != "" {
		if s.Field == "" {
			return s.Aggregate
		}
		return s.Aggregate + "_" + s.Field
	}
	return s.Field
}
