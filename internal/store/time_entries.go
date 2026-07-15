package store

import (
	"database/sql"
	"fmt"

	"todo/internal/models"
	"todo/internal/tutil"
)

func scanEntry(scanner interface{ Scan(...any) error }) (models.TimeEntry, error) {
	var e models.TimeEntry
	var todoID, groupID sql.NullInt64
	var endTime, note, createdAt sql.NullString
	var groupName, groupColor, todoTitle sql.NullString
	err := scanner.Scan(&e.ID, &todoID, &groupID, &e.StartTime, &endTime,
		&note, &createdAt, &groupName, &groupColor, &todoTitle)
	if err != nil {
		return e, err
	}
	e.TodoID = models.NullInt(todoID)
	e.GroupID = models.NullInt(groupID)
	e.EndTime = models.NullStr(endTime)
	e.Note = note.String
	e.CreatedAt = createdAt.String
	e.GroupName = groupName.String
	e.GroupColor = groupColor.String
	e.TodoTitle = todoTitle.String
	return e, nil
}

// entryColumns includes joined group/todo display fields.
const entryColumns = `te.id, te.todo_id, te.group_id, te.start_time, te.end_time, te.note, te.created_at,
	g.name AS group_name, g.color AS group_color, t.title AS todo_title`

func entryQuery(where string, args ...any) string {
	return fmt.Sprintf(`SELECT %s FROM time_entries te
		LEFT JOIN groups g ON g.id = te.group_id
		LEFT JOIN todos t ON t.id = te.todo_id
		WHERE %s ORDER BY te.start_time ASC`, entryColumns, where)
}

// ListEntriesForDay returns entries overlapping the given date.
func ListEntriesForDay(db *sql.DB, date string) ([]models.TimeEntry, error) {
	start, end := tutil.DayRange(date)
	rows, err := db.Query(entryQuery(`te.start_time < ? AND (te.end_time IS NULL OR te.end_time > ?)`, end, start),
		end, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var es []models.TimeEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		es = append(es, e)
	}
	return es, rows.Err()
}

// ListEntriesInRange returns entries overlapping [start, end) storage strings.
func ListEntriesInRange(db *sql.DB, start, end string) ([]models.TimeEntry, error) {
	rows, err := db.Query(entryQuery(`te.start_time < ? AND (te.end_time IS NULL OR te.end_time > ?)`, end, start),
		end, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var es []models.TimeEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		es = append(es, e)
	}
	return es, rows.Err()
}

// GetEntry fetches a single entry by id.
func GetEntry(db *sql.DB, id int64) (models.TimeEntry, error) {
	row := db.QueryRow(entryQuery(`te.id = ?`, id), id)
	return scanEntry(row)
}

// StartEntry creates a new in-progress entry, first stopping any active entry.
func StartEntry(db *sql.DB, todoID *int64, groupID *int64, note string) (models.TimeEntry, error) {
	// Inherit group from the todo if not provided.
	if groupID == nil && todoID != nil {
		var g sql.NullInt64
		if err := db.QueryRow(`SELECT group_id FROM todos WHERE id = ?`, *todoID).Scan(&g); err == nil && g.Valid {
			v := g.Int64
			groupID = &v
		}
	}
	now := tutil.Now()
	tx, err := db.Begin()
	if err != nil {
		return models.TimeEntry{}, err
	}
	// Stop any currently active entry.
	if _, err := tx.Exec(`UPDATE time_entries SET end_time = ? WHERE end_time IS NULL`, now); err != nil {
		tx.Rollback()
		return models.TimeEntry{}, err
	}
	res, err := tx.Exec(`INSERT INTO time_entries (todo_id, group_id, start_time, note) VALUES (?, ?, ?, ?)`,
		todoID, groupID, now, note)
	if err != nil {
		tx.Rollback()
		return models.TimeEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.TimeEntry{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.TimeEntry{}, err
	}
	return GetEntry(db, id)
}

// StopEntry closes the currently active entry (if any), setting end_time=now.
// It returns the stopped entry, or a zero-value entry with id 0 if none active.
func StopEntry(db *sql.DB) (models.TimeEntry, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM time_entries WHERE end_time IS NULL ORDER BY start_time DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return models.TimeEntry{}, nil
	}
	if err != nil {
		return models.TimeEntry{}, err
	}
	if _, err := db.Exec(`UPDATE time_entries SET end_time = ? WHERE id = ?`, tutil.Now(), id); err != nil {
		return models.TimeEntry{}, err
	}
	return GetEntry(db, id)
}

// ActiveEntry returns the currently running entry, or nil if none.
func ActiveEntry(db *sql.DB) (*models.TimeEntry, error) {
	row := db.QueryRow(entryQuery(`te.end_time IS NULL`, ""))
	e, err := scanEntry(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// CreateEntry inserts a fully-specified entry (for manual back-filling).
func CreateEntry(db *sql.DB, e models.TimeEntry) (models.TimeEntry, error) {
	if e.StartTime == "" {
		e.StartTime = tutil.Now()
	}
	if e.EndTime != nil && *e.EndTime != "" && *e.EndTime <= e.StartTime {
		return models.TimeEntry{}, fmt.Errorf("结束时间必须晚于开始时间")
	}
	if e.GroupID == nil && e.TodoID != nil {
		var g sql.NullInt64
		if err := db.QueryRow(`SELECT group_id FROM todos WHERE id = ?`, *e.TodoID).Scan(&g); err == nil && g.Valid {
			v := g.Int64
			e.GroupID = &v
		}
	}
	res, err := db.Exec(`INSERT INTO time_entries (todo_id, group_id, start_time, end_time, note)
		VALUES (?, ?, ?, ?, ?)`, e.TodoID, e.GroupID, e.StartTime, e.EndTime, e.Note)
	if err != nil {
		return models.TimeEntry{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.TimeEntry{}, err
	}
	return GetEntry(db, id)
}

// UpdateEntry replaces editable fields of an entry.
func UpdateEntry(db *sql.DB, id int64, e models.TimeEntry) (models.TimeEntry, error) {
	if e.EndTime != nil && *e.EndTime != "" && *e.EndTime <= e.StartTime {
		return models.TimeEntry{}, fmt.Errorf("结束时间必须晚于开始时间")
	}
	if e.GroupID == nil && e.TodoID != nil {
		var g sql.NullInt64
		if err := db.QueryRow(`SELECT group_id FROM todos WHERE id = ?`, *e.TodoID).Scan(&g); err == nil && g.Valid {
			v := g.Int64
			e.GroupID = &v
		}
	}
	if _, err := db.Exec(`UPDATE time_entries SET todo_id = ?, group_id = ?, start_time = ?, end_time = ?, note = ? WHERE id = ?`,
		e.TodoID, e.GroupID, e.StartTime, e.EndTime, e.Note, id); err != nil {
		return models.TimeEntry{}, err
	}
	return GetEntry(db, id)
}

// DeleteEntry removes an entry.
func DeleteEntry(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM time_entries WHERE id = ?`, id)
	return err
}

// ListEntriesForTodos returns all time entries for the given todo IDs (or
// their direct child todos), ordered by start_time.
func ListEntriesForTodos(db *sql.DB, todoIDs []int64) ([]models.TimeEntry, error) {
	if len(todoIDs) == 0 {
		return nil, nil
	}
	placeholders, args := buildPlaceholders(todoIDs)
	rows, err := db.Query(entryQuery(`te.todo_id IN (`+placeholders+`)`, args...),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var es []models.TimeEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		es = append(es, e)
	}
	return es, rows.Err()
}

// ListEntriesForTodosInRange returns time entries for the given todo IDs
// that overlap [start, end) storage strings.
func ListEntriesForTodosInRange(db *sql.DB, todoIDs []int64, start, end string) ([]models.TimeEntry, error) {
	if len(todoIDs) == 0 {
		return nil, nil
	}
	placeholders, todoArgs := buildPlaceholders(todoIDs)
	where := fmt.Sprintf(`te.todo_id IN (%s) AND te.start_time < ? AND (te.end_time IS NULL OR te.end_time > ?)`, placeholders)
	args := append(todoArgs, end, start)
	rows, err := db.Query(entryQuery(where, args...), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var es []models.TimeEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		es = append(es, e)
	}
	return es, rows.Err()
}

func buildPlaceholders(ids []int64) (string, []any) {
	var ph string
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			ph += ", "
		}
		ph += "?"
		args = append(args, id)
	}
	return ph, args
}
