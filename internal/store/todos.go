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
	var title, description sql.NullString
	var dueDate, completedAt, createdAt sql.NullString
	err := scanner.Scan(&t.ID, &parentID, &title, &description,
		&t.Priority, &dueDate, &createdAt, &completedAt)
	if err != nil {
		return t, err
	}
	t.Title = title.String
	t.Description = description.String
	t.CreatedAt = createdAt.String
	t.ParentID = models.NullInt(parentID)
	t.DueDate = models.NullStr(dueDate)
	t.CompletedAt = models.NullStr(completedAt)
	return t, nil
}

const todoColumns = `id, parent_id, title, description, priority, due_date, created_at, completed_at`

// ListTodos returns top-level todos optionally filtered by tag name.
// Only top-level todos (parent_id IS NULL) are returned by default; children
// are fetched and assembled via WithChildren.
func ListTodos(db *sql.DB, tagID *int64, statusTag string) ([]models.Todo, error) {
	q := `SELECT ` + todoColumns + ` FROM todos WHERE parent_id IS NULL`
	args := []any{}
	if tagID != nil {
		q += ` AND EXISTS (SELECT 1 FROM todo_tags tt WHERE tt.todo_id = todos.id AND tt.tag_id = ?)`
		args = append(args, *tagID)
	}
	if statusTag != "" {
		q += ` AND EXISTS (SELECT 1 FROM todo_tags tt JOIN tags tg ON tg.id = tt.tag_id WHERE tt.todo_id = todos.id AND tg.name = ?)`
		args = append(args, statusTag)
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
		ts[i].Children = children
	}
	return ts, nil
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
	t.Tags = []models.Tag{}
	t.TagIDs = []int64{}
	for rows.Next() {
		var g models.Tag
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

// copyParentTags copies the parent's current tags when a subtask is created.
// The copied IDs are stored on the child and are independent afterwards.
func copyParentTags(db *sql.DB, t *models.Todo) error {
	if t.ParentID == nil {
		return nil
	}
	parent, err := GetTodo(db, *t.ParentID)
	if err != nil {
		return fmt.Errorf("get parent tags: %w", err)
	}
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

func hasTag(db *sql.DB, tagIDs []int64, name string) bool {
	id, err := tagIDByName(db, name)
	if err != nil {
		return false
	}
	for _, tagID := range tagIDs {
		if tagID == id {
			return true
		}
	}
	return false
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
	created, err := CreateTag(db, models.Tag{
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

// normalizeStatusTags enforces mutual exclusion for the two system tags.
// state is empty for ordinary tag edits, or one of "pending", "in_progress",
// and "done" when called by the compatibility status endpoint.
func normalizeStatusTags(db *sql.DB, tagIDs []int64, state string) ([]int64, error) {
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

	if (state == "done" && progressErr == nil) || (completedErr == nil && hasCompleted && hasProgress) {
		filtered := clean[:0]
		for _, id := range clean {
			if progressErr == nil && id == progressID {
				continue
			}
			filtered = append(filtered, id)
		}
		clean = filtered
	}
	if (state == "pending" || state == "in_progress") && completedErr == nil {
		filtered := clean[:0]
		for _, id := range clean {
			if id != completedID {
				filtered = append(filtered, id)
			}
		}
		clean = filtered
	}
	if state == "pending" && progressErr == nil {
		filtered := clean[:0]
		for _, id := range clean {
			if id != progressID {
				filtered = append(filtered, id)
			}
		}
		clean = filtered
	}
	if state == "in_progress" && progressErr == nil {
		clean = append(clean, progressID)
	}
	if state == "done" {
		id, err := completedTagID(db)
		if err != nil {
			return nil, err
		}
		clean = append(clean, id)
	}
	return clean, nil
}

// clearDescendantProgressTags removes the "进行中" tag from every descendant of
// todoID. Completing a task also takes its subtasks off the today board, so a
// finished parent never leaves stale in-progress children behind.
func clearDescendantProgressTags(db *sql.DB, todoID int64) error {
	progressID, err := tagIDByName(db, progressTagName)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	ids, err := CollectDescendantIDs(db, todoID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := db.Exec(`DELETE FROM todo_tags WHERE todo_id = ? AND tag_id = ?`, id, progressID); err != nil {
			return err
		}
	}
	return nil
}

// CollectDescendantIDs returns all descendant todo IDs for the given todo,
// recursively collecting children, grandchildren, etc. The child rows are read
// to completion before recursing: the pool holds a single connection, so a
// nested query while the cursor is open would block forever.
func CollectDescendantIDs(db *sql.DB, id int64) ([]int64, error) {
	rows, err := db.Query(`SELECT id FROM todos WHERE parent_id = ?`, id)
	if err != nil {
		return nil, err
	}
	var children []int64
	for rows.Next() {
		var childID int64
		if err := rows.Scan(&childID); err != nil {
			rows.Close()
			return nil, err
		}
		children = append(children, childID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	ids := make([]int64, 0, len(children))
	for _, childID := range children {
		ids = append(ids, childID)
		grandIDs, err := CollectDescendantIDs(db, childID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, grandIDs...)
	}
	return ids, nil
}

// CreateTodo inserts a todo. If ParentID is set, it becomes a subtask.
func CreateTodo(db *sql.DB, t models.Todo) (models.Todo, error) {
	if t.Title == "" {
		return models.Todo{}, fmt.Errorf("title is required")
	}
	if err := copyParentTags(db, &t); err != nil {
		return models.Todo{}, err
	}
	normalizedTags, err := normalizeStatusTags(db, t.TagIDs, "")
	if err != nil {
		return models.Todo{}, err
	}
	t.TagIDs = normalizedTags
	var completedAt any
	if hasTag(db, t.TagIDs, completedTagName) {
		c := tutil.Now()
		completedAt = c
		t.CompletedAt = &c
	}
	res, err := db.Exec(`INSERT INTO todos (parent_id, title, description, priority, due_date, completed_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.ParentID, t.Title, t.Description, t.Priority, t.DueDate, completedAt)
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

// UpdateTodo replaces editable fields and derives completion from the tags.
func UpdateTodo(db *sql.DB, id int64, t models.Todo) (models.Todo, error) {
	existing, err := GetTodo(db, id)
	if err != nil {
		return models.Todo{}, err
	}
	if t.TagIDs == nil {
		t.TagIDs = existing.TagIDs
	}
	normalizedTags, err := normalizeStatusTags(db, t.TagIDs, "")
	if err != nil {
		return models.Todo{}, err
	}
	t.TagIDs = normalizedTags
	var completedAt any
	if hasTag(db, t.TagIDs, completedTagName) {
		if existing.CompletedAt != nil {
			completedAt = *existing.CompletedAt
		} else {
			c := tutil.Now()
			completedAt = c
		}
	}
	if _, err := db.Exec(`UPDATE todos SET parent_id = ?, title = ?, description = ?,
		priority = ?, due_date = ?, completed_at = ? WHERE id = ?`,
		t.ParentID, t.Title, t.Description, t.Priority, t.DueDate, completedAt, id); err != nil {
		return models.Todo{}, err
	}
	if t.TagIDs != nil {
		if err := setTodoTags(db, id, t.TagIDs); err != nil {
			return models.Todo{}, err
		}
	}
	if hasTag(db, t.TagIDs, completedTagName) && !hasTag(db, existing.TagIDs, completedTagName) {
		if err := clearDescendantProgressTags(db, id); err != nil {
			return models.Todo{}, err
		}
	}
	return GetTodo(db, id)
}

// SetTodoStatus is retained as an API compatibility operation; it changes the
// corresponding status tag instead of writing a status column.
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
	tagIDs, err := normalizeStatusTags(db, existing.TagIDs, status)
	if err != nil {
		return existing, err
	}
	if err := setTodoTags(db, id, tagIDs); err != nil {
		return existing, err
	}
	if _, err := db.Exec(`UPDATE todos SET completed_at = ? WHERE id = ?`, completedAt, id); err != nil {
		return existing, err
	}
	if status == "done" {
		if err := clearDescendantProgressTags(db, id); err != nil {
			return existing, err
		}
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
	err := db.QueryRow(`SELECT COUNT(*) FROM todos t JOIN todo_tags tt ON tt.todo_id = t.id JOIN tags g ON g.id = tt.tag_id
		WHERE g.name = ? AND t.completed_at >= ? AND t.completed_at < ?`, completedTagName, start, end).Scan(&n)
	return n, err
}

// CountCompletedInRange returns how many todos were completed within [start, end).
func CountCompletedInRange(db *sql.DB, start, end string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM todos t JOIN todo_tags tt ON tt.todo_id = t.id JOIN tags g ON g.id = tt.tag_id
		WHERE g.name = ? AND t.completed_at >= ? AND t.completed_at < ?`, completedTagName, start, end).Scan(&n)
	return n, err
}
