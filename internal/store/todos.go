package store

import (
	"database/sql"
	"fmt"

	"todo/internal/models"
	"todo/internal/tutil"
)

func scanTodo(scanner interface{ Scan(...any) error }) (models.Todo, error) {
	var t models.Todo
	var groupID, parentID sql.NullInt64
	var title, description, status sql.NullString
	var dueDate, completedAt, createdAt sql.NullString
	err := scanner.Scan(&t.ID, &groupID, &parentID, &title, &description,
		&status, &t.Priority, &dueDate, &createdAt, &completedAt)
	if err != nil {
		return t, err
	}
	t.Title = title.String
	t.Description = description.String
	t.Status = status.String
	t.CreatedAt = createdAt.String
	t.GroupID = models.NullInt(groupID)
	t.ParentID = models.NullInt(parentID)
	t.DueDate = models.NullStr(dueDate)
	t.CompletedAt = models.NullStr(completedAt)
	return t, nil
}

const todoColumns = `id, group_id, parent_id, title, description, status, priority, due_date, created_at, completed_at`

// ListTodos returns todos optionally filtered by group_id and/or status.
// Only top-level todos (parent_id IS NULL) are returned by default; children
// are fetched and assembled via WithChildren.
func ListTodos(db *sql.DB, groupID *int64, status string) ([]models.Todo, error) {
	q := `SELECT ` + todoColumns + ` FROM todos WHERE parent_id IS NULL`
	args := []any{}
	if groupID != nil {
		q += ` AND group_id = ?`
		args = append(args, *groupID)
	}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY priority DESC, created_at ASC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ts []models.Todo
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		ts = append(ts, t)
	}
	return ts, rows.Err()
}

// listChildren returns direct children of a todo.
func listChildren(db *sql.DB, parentID int64) ([]models.Todo, error) {
	rows, err := db.Query(`SELECT `+todoColumns+` FROM todos WHERE parent_id = ? ORDER BY priority DESC, created_at ASC`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ts []models.Todo
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		ts = append(ts, t)
	}
	return ts, rows.Err()
}

// WithChildren recursively populates Children on each todo.
func WithChildren(db *sql.DB, ts []models.Todo) ([]models.Todo, error) {
	for i := range ts {
		children, err := listChildren(db, ts[i].ID)
		if err != nil {
			return nil, err
		}
		children, err = WithChildren(db, children)
		if err != nil {
			return nil, err
		}
		ts[i].Children = children
	}
	return ts, nil
}

// GetTodo fetches a single todo by id.
func GetTodo(db *sql.DB, id int64) (models.Todo, error) {
	row := db.QueryRow(`SELECT `+todoColumns+` FROM todos WHERE id = ?`, id)
	return scanTodo(row)
}

// CreateTodo inserts a todo. If ParentID is set, it becomes a subtask.
func CreateTodo(db *sql.DB, t models.Todo) (models.Todo, error) {
	if t.Title == "" {
		return models.Todo{}, fmt.Errorf("title is required")
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	var completedAt any
	if t.Status == "done" {
		c := tutil.Now()
		completedAt = c
		t.CompletedAt = &c
	}
	res, err := db.Exec(`INSERT INTO todos (group_id, parent_id, title, description, status, priority, due_date, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.GroupID, t.ParentID, t.Title, t.Description, t.Status, t.Priority, t.DueDate, completedAt)
	if err != nil {
		return models.Todo{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Todo{}, err
	}
	return GetTodo(db, id)
}

// UpdateTodo replaces editable fields and recomputes completed_at from status.
func UpdateTodo(db *sql.DB, id int64, t models.Todo) (models.Todo, error) {
	existing, err := GetTodo(db, id)
	if err != nil {
		return models.Todo{}, err
	}
	var completedAt any
	if t.Status == "done" {
		if existing.Status == "done" && existing.CompletedAt != nil {
			completedAt = *existing.CompletedAt
		} else {
			c := tutil.Now()
			completedAt = c
		}
	}
	if _, err := db.Exec(`UPDATE todos SET group_id = ?, parent_id = ?, title = ?, description = ?,
		status = ?, priority = ?, due_date = ?, completed_at = ? WHERE id = ?`,
		t.GroupID, t.ParentID, t.Title, t.Description, t.Status, t.Priority, t.DueDate, completedAt, id); err != nil {
		return models.Todo{}, err
	}
	return GetTodo(db, id)
}

// SetTodoStatus updates only the status and manages completed_at accordingly.
func SetTodoStatus(db *sql.DB, id int64, status string) (models.Todo, error) {
	existing, err := GetTodo(db, id)
	if err != nil {
		return existing, err
	}
	var completedAt any
	switch status {
	case "done":
		if existing.CompletedAt != nil {
			completedAt = *existing.CompletedAt
		} else {
			c := tutil.Now()
			completedAt = c
		}
	case "pending", "in_progress":
		completedAt = nil
	default:
		return existing, fmt.Errorf("invalid status: %s", status)
	}
	if _, err := db.Exec(`UPDATE todos SET status = ?, completed_at = ? WHERE id = ?`,
		status, completedAt, id); err != nil {
		return existing, err
	}
	return GetTodo(db, id)
}

// DeleteTodo removes a todo (children cascade via ON DELETE CASCADE).
func DeleteTodo(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM todos WHERE id = ?`, id)
	return err
}

// CountCompletedOnDate returns how many todos were completed on the given date.
func CountCompletedOnDate(db *sql.DB, date string) (int, error) {
	start, end := tutil.DayRange(date)
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM todos WHERE status = 'done' AND completed_at >= ? AND completed_at < ?`,
		start, end).Scan(&n)
	return n, err
}

// CountCompletedInRange returns how many todos were completed within [start, end).
func CountCompletedInRange(db *sql.DB, start, end string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM todos WHERE status = 'done' AND completed_at >= ? AND completed_at < ?`,
		start, end).Scan(&n)
	return n, err
}
