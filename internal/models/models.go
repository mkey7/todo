package models

import "database/sql"

// Tag classifies todos and time entries.
type Tag struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	// Nil means enabled by default; false excludes this tag from analysis charts.
	IncludeInStats *bool  `json:"include_in_stats,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// Todo is a work item. Subtasks reference a parent via ParentID.
type Todo struct {
	ID          int64   `json:"id"`
	ParentID    *int64  `json:"parent_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    int     `json:"priority"`
	DueDate     *string `json:"due_date"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`

	// joined/display fields
	Tags     []Tag   `json:"tags,omitempty"`
	TagIDs   []int64 `json:"tag_ids,omitempty"`
	Children []Todo  `json:"children,omitempty"`
}

// TimeEntry records a span of work. EndTime == nil means in progress.
type TimeEntry struct {
	ID        int64   `json:"id"`
	TodoID    *int64  `json:"todo_id"`
	TagID     *int64  `json:"tag_id"`
	StartTime string  `json:"start_time"`
	EndTime   *string `json:"end_time"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"created_at"`

	// joined/display fields
	TagName   string `json:"tag_name,omitempty"`
	TagColor  string `json:"tag_color,omitempty"`
	TodoTitle string `json:"todo_title,omitempty"`
	// TodoPrimaryColor is the color of the todo's first tag, used by timelines.
	TodoPrimaryColor string  `json:"todo_primary_color,omitempty"`
	Tags             []Tag   `json:"tags,omitempty"`
	TagIDs           []int64 `json:"tag_ids,omitempty"`
}

// DailySummary holds a user's free-text reflection for a given day.
type DailySummary struct {
	ID        int64  `json:"id"`
	Date      string `json:"date"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

// WeeklySummary holds a user's free-text reflection for an ISO week.
type WeeklySummary struct {
	ID        int64  `json:"id"`
	Week      string `json:"week"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
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
