package models

import "database/sql"

// Group is a todo/time-entry grouping (e.g. "开发", "学习").
type Group struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
}

// Todo is a work item. Subtasks reference a parent via ParentID.
type Todo struct {
	ID          int64   `json:"id"`
	GroupID     *int64  `json:"group_id"`
	ParentID    *int64  `json:"parent_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"` // pending | in_progress | done
	Priority    int     `json:"priority"`
	DueDate     *string `json:"due_date"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`

	// joined/display fields
	GroupName  string `json:"group_name,omitempty"`
	GroupColor string `json:"group_color,omitempty"`
	Children   []Todo `json:"children,omitempty"`
}

// TimeEntry records a span of work. EndTime == nil means in progress.
type TimeEntry struct {
	ID        int64   `json:"id"`
	TodoID    *int64  `json:"todo_id"`
	GroupID   *int64  `json:"group_id"`
	StartTime string  `json:"start_time"`
	EndTime   *string `json:"end_time"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"created_at"`

	// joined/display fields
	GroupName  string `json:"group_name,omitempty"`
	GroupColor string `json:"group_color,omitempty"`
	TodoTitle  string `json:"todo_title,omitempty"`
}

// DailySummary holds a user's free-text reflection for a given day.
type DailySummary struct {
	ID          int64  `json:"id"`
	Date        string `json:"date"`
	Improvement string `json:"improvement"`
	Notes       string `json:"notes"`
	UpdatedAt   string `json:"updated_at"`
}

// NullInt converts a sql.NullInt64 into a pointer (nil if not valid).
func NullInt(n sql.NullInt64) *int64 {
	if n.Valid {
		v := n.Int64
		return &v
	}
	return nil
}

// NullStr converts a sql.NullString into a pointer (nil if not valid).
func NullStr(n sql.NullString) *string {
	if n.Valid {
		v := n.String
		return &v
	}
	return nil
}
