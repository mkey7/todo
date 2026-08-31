package store

import (
	"database/sql"
	"fmt"

	"todo/internal/models"
	"todo/internal/tutil"
)

func scanTodo(scanner interface{ Scan(...any) error }) (models.Todo, error) {
	var t models.Todo
	var parentID sql.NullInt64
	var title, description, status sql.NullString
	var dueDate, completedAt, createdAt sql.NullString
	err := scanner.Scan(&t.ID, &parentID, &title, &description,
		&status, &t.Priority, &dueDate, &createdAt, &completedAt)
	if err != nil {
		return t, err
	}
	t.Title = title.String
	t.Description = description.String
	t.Status = status.String
	t.CreatedAt = createdAt.String
	t.ParentID = models.NullInt(parentID)
	t.DueDate = models.NullStr(dueDate)
	t.CompletedAt = models.NullStr(completedAt)
	return t, nil
}

const todoColumns = `id, parent_id, title, description, status, priority, due_date, created_at, completed_at`

// ListTodos returns todos optionally filtered by group_id and/or status.
// Only top-level todos (parent_id IS NULL) are returned by default; children
// are fetched and assembled via WithChildren.
func ListTodos(db *sql.DB, groupID *int64, status string) ([]models.Todo, error) {
	q := `SELECT ` + todoColumns + ` FROM todos WHERE parent_id IS NULL`
	args := []any{}
	if groupID != nil {
		q += ` AND EXISTS (SELECT 1 FROM todo_tags tt WHERE tt.todo_id = todos.id AND tt.tag_id = ?)`
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
	var ts []models.Todo
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		ts = append(ts, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // the DB intentionally has one connection; release it before tag queries.
	for i := range ts {
		if err := loadTodoTags(db, &ts[i]); err != nil {
			return nil, err
		}
	}
	return ts, nil
}

// listChildren returns direct children of a todo.
func listChildren(db *sql.DB, parentID int64) ([]models.Todo, error) {
	rows, err := db.Query(`SELECT `+todoColumns+` FROM todos WHERE parent_id = ? ORDER BY priority DESC, created_at ASC`, parentID)
	if err != nil {
		return nil, err
	}
	var ts []models.Todo
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		ts = append(ts, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range ts {
		if err := loadTodoTags(db, &ts[i]); err != nil {
			return nil, err
		}
	}
	return ts, nil
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
		applyInheritedTags(children, ts[i].Tags)
		ts[i].Children = children
	}
	return ts, nil
}

// applyInheritedTags exposes the parent's tags on every child, including
// subtasks created before tag inheritance was introduced. Child-specific tags
// remain intact and are appended after inherited tags.
func applyInheritedTags(todos []models.Todo, inherited []models.Group) {
	for i := range todos {
		seen := map[int64]bool{}
		tags := make([]models.Group, 0, len(inherited)+len(todos[i].Tags))
		for _, tag := range inherited {
			if !seen[tag.ID] {
				seen[tag.ID] = true
				tags = append(tags, tag)
			}
		}
		for _, tag := range todos[i].Tags {
			if !seen[tag.ID] {
				seen[tag.ID] = true
				tags = append(tags, tag)
			}
		}
		todos[i].InheritedTagIDs = make([]int64, 0, len(inherited))
		todos[i].TagIDs = make([]int64, 0, len(tags))
		for _, tag := range inherited {
			todos[i].InheritedTagIDs = append(todos[i].InheritedTagIDs, tag.ID)
		}
		for _, tag := range tags {
			todos[i].TagIDs = append(todos[i].TagIDs, tag.ID)
		}
		todos[i].Tags = tags
		applyInheritedTags(todos[i].Children, tags)
	}
}

// GetTodo fetches a single todo by id.
func GetTodo(db *sql.DB, id int64) (models.Todo, error) {
	row := db.QueryRow(`SELECT `+todoColumns+` FROM todos WHERE id = ?`, id)
	t, err := scanTodo(row)
	if err != nil {
		return t, err
	}
	return t, loadTodoTags(db, &t)
}

func loadTodoTags(db *sql.DB, t *models.Todo) error {
	rows, err := db.Query(`SELECT g.id, g.name, g.description, g.color, g.created_at
		FROM todo_tags tt JOIN tags g ON g.id = tt.tag_id WHERE tt.todo_id = ? ORDER BY tt.tag_order, tt.tag_id`, t.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	t.Tags = []models.Group{}
	t.TagIDs = []int64{}
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Color, &g.CreatedAt); err != nil {
			return err
		}
		t.Tags = append(t.Tags, g)
		t.TagIDs = append(t.TagIDs, g.ID)
	}
	return rows.Err()
}

func mergeTagIDs(base, extra []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(base)+len(extra))
	for _, id := range append(base, extra...) {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// inheritParentTags ensures a subtask always keeps its parent's effective tags.
func inheritParentTags(db *sql.DB, t *models.Todo) error {
	if t.ParentID == nil {
		return nil
	}
	parent, err := GetTodo(db, *t.ParentID)
	if err != nil {
		return fmt.Errorf("get parent tags: %w", err)
	}
	t.InheritedTagIDs = parent.TagIDs
	t.TagIDs = mergeTagIDs(parent.TagIDs, t.TagIDs)
	return nil
}

func setTodoTags(db *sql.DB, todoID int64, tagIDs []int64) error {
	if _, err := db.Exec(`DELETE FROM todo_tags WHERE todo_id = ?`, todoID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for index, tagID := range tagIDs {
		if tagID <= 0 || seen[tagID] {
			continue
		}
		seen[tagID] = true
		if _, err := db.Exec(`INSERT INTO todo_tags (todo_id, tag_id, tag_order) VALUES (?, ?, ?)`, todoID, tagID, index); err != nil {
			return err
		}
	}
	return nil
}

const completedTagName = "已完成"
const progressTagName = "进行中"

func tagIDByName(db *sql.DB, name string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM tags WHERE name = ? ORDER BY id LIMIT 1`, name).Scan(&id)
	return id, err
}

// completedTagID returns the shared system tag used to mark completed todos,
// creating it only when the first todo is completed.
func completedTagID(db *sql.DB) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM tags WHERE name = ? ORDER BY id LIMIT 1`, completedTagName).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	created, err := CreateGroup(db, models.Group{
		Name:           completedTagName,
		Description:    "系统自动添加：任务完成时标记",
		Color:          "#22c55e",
		IncludeInStats: func() *bool { v := false; return &v }(),
	})
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

// syncCompletedTag keeps the automatic completed tag at the end of a task's
// tag order, so it never replaces the task's primary timeline color.
func syncCompletedTag(db *sql.DB, todoID int64, tagIDs []int64, completed bool) error {
	var completedID int64
	if completed {
		var err error
		completedID, err = completedTagID(db)
		if err != nil {
			return err
		}
	} else if err := db.QueryRow(`SELECT id FROM tags WHERE name = ? ORDER BY id LIMIT 1`, completedTagName).Scan(&completedID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	updated := make([]int64, 0, len(tagIDs)+1)
	for _, tagID := range tagIDs {
		if tagID != completedID {
			updated = append(updated, tagID)
		}
	}
	if completed {
		updated = append(updated, completedID)
	}
	return setTodoTags(db, todoID, updated)
}

// normalizeStatusTags enforces that the two system status tags are mutually
// exclusive. A completed status always wins; otherwise an explicitly selected
// completed tag wins over an in-progress tag. The completed tag is removed
// when a task is reopened, matching the status transition semantics.
func normalizeStatusTags(db *sql.DB, tagIDs []int64, status string, removeCompleted bool) ([]int64, error) {
	completedID, completedErr := tagIDByName(db, completedTagName)
	if completedErr != nil && completedErr != sql.ErrNoRows {
		return nil, completedErr
	}
	progressID, progressErr := tagIDByName(db, progressTagName)
	if progressErr != nil && progressErr != sql.ErrNoRows {
		return nil, progressErr
	}

	seen := map[int64]bool{}
	clean := make([]int64, 0, len(tagIDs)+1)
	hasCompleted, hasProgress := false, false
	for _, id := range tagIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if completedErr == nil && id == completedID {
			hasCompleted = true
		}
		if progressErr == nil && id == progressID {
			hasProgress = true
		}
		clean = append(clean, id)
	}

	if (status == "done" && progressErr == nil) || (completedErr == nil && hasCompleted && hasProgress) {
		filtered := clean[:0]
		for _, id := range clean {
			if progressErr == nil && id == progressID {
				continue
			}
			filtered = append(filtered, id)
		}
		clean = filtered
	}
	if removeCompleted && completedErr == nil {
		filtered := clean[:0]
		for _, id := range clean {
			if id != completedID {
				filtered = append(filtered, id)
			}
		}
		clean = filtered
	}
	if status == "done" {
		id, err := completedTagID(db)
		if err != nil {
			return nil, err
		}
		clean = append(clean, id)
	}
	return clean, nil
}

// CollectDescendantIDs returns all descendant todo IDs for the given todo,
// recursively collecting children, grandchildren, etc.
func CollectDescendantIDs(db *sql.DB, id int64) ([]int64, error) {
	rows, err := db.Query(`SELECT id FROM todos WHERE parent_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var childID int64
		if err := rows.Scan(&childID); err != nil {
			return nil, err
		}
		ids = append(ids, childID)
		// Recurse
		grandIDs, err := CollectDescendantIDs(db, childID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, grandIDs...)
	}
	return ids, rows.Err()
}

// CreateTodo inserts a todo. If ParentID is set, it becomes a subtask.
func CreateTodo(db *sql.DB, t models.Todo) (models.Todo, error) {
	if t.Title == "" {
		return models.Todo{}, fmt.Errorf("title is required")
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	if err := inheritParentTags(db, &t); err != nil {
		return models.Todo{}, err
	}
	normalizedTags, err := normalizeStatusTags(db, t.TagIDs, t.Status, false)
	if err != nil {
		return models.Todo{}, err
	}
	t.TagIDs = normalizedTags
	var completedAt any
	if t.Status == "done" {
		c := tutil.Now()
		completedAt = c
		t.CompletedAt = &c
	}
	res, err := db.Exec(`INSERT INTO todos (parent_id, title, description, status, priority, due_date, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ParentID, t.Title, t.Description, t.Status, t.Priority, t.DueDate, completedAt)
	if err != nil {
		return models.Todo{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Todo{}, err
	}
	if err := setTodoTags(db, id, t.TagIDs); err != nil {
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
	if err := inheritParentTags(db, &t); err != nil {
		return models.Todo{}, err
	}
	if t.TagIDs == nil {
		t.TagIDs = existing.TagIDs
	}
	normalizedTags, err := normalizeStatusTags(db, t.TagIDs, t.Status, existing.Status == "done" && t.Status != "done")
	if err != nil {
		return models.Todo{}, err
	}
	t.TagIDs = normalizedTags
	var completedAt any
	if t.Status == "done" {
		if existing.Status == "done" && existing.CompletedAt != nil {
			completedAt = *existing.CompletedAt
		} else {
			c := tutil.Now()
			completedAt = c
		}
	}
	if _, err := db.Exec(`UPDATE todos SET parent_id = ?, title = ?, description = ?,
		status = ?, priority = ?, due_date = ?, completed_at = ? WHERE id = ?`,
		t.ParentID, t.Title, t.Description, t.Status, t.Priority, t.DueDate, completedAt, id); err != nil {
		return models.Todo{}, err
	}
	if t.TagIDs != nil {
		if err := setTodoTags(db, id, t.TagIDs); err != nil {
			return models.Todo{}, err
		}
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
	tagIDs, err := normalizeStatusTags(db, existing.TagIDs, status, status != "done")
	if err != nil {
		return existing, err
	}
	if err := setTodoTags(db, id, tagIDs); err != nil {
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
