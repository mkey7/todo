package store

import (
	"database/sql"
	"errors"
	"fmt"

	"todo/internal/models"
	"todo/internal/tutil"
)

// endOfTime is a far-future sentinel used as the end of an in-progress entry
// when checking for overlaps, so an open entry conflicts with everything that
// starts after it.
const endOfTime = "9999-12-31 23:59:59"

// OverlapError reports that a write would overlap existing time entries.
// Callers may retry with WriteOptions{AllowOverlap: true} to accept it.
type OverlapError struct {
	Conflicts []models.TimeEntry
}

func (e *OverlapError) Error() string {
	return fmt.Sprintf("该时间段与 %d 条已有记录重叠", len(e.Conflicts))
}

// ActiveEntryError reports that a write would create a second in-progress
// entry. Unlike OverlapError this is never waivable: the app assumes at most
// one running entry.
type ActiveEntryError struct{}

func (ActiveEntryError) Error() string { return "已有进行中的记录，请先停止" }

func scanEntry(scanner interface{ Scan(...any) error }) (models.TimeEntry, error) {
	var e models.TimeEntry
	var todoID, tagID sql.NullInt64
	var endTime, note, createdAt sql.NullString
	var tagName, tagColor, todoTitle sql.NullString
	err := scanner.Scan(&e.ID, &todoID, &tagID, &e.StartTime, &endTime,
		&note, &createdAt, &tagName, &tagColor, &todoTitle)
	if err != nil {
		return e, err
	}
	e.TodoID = models.NullInt(todoID)
	e.TagID = models.NullInt(tagID)
	e.EndTime = models.NullStr(endTime)
	e.Note = note.String
	e.CreatedAt = createdAt.String
	e.TagName = tagName.String
	e.TagColor = tagColor.String
	e.TodoTitle = todoTitle.String
	return e, nil
}

// entryColumns includes joined tag/todo display fields.
const entryColumns = `te.id, te.todo_id, te.tag_id, te.start_time, te.end_time, te.note, te.created_at,
	g.name AS tag_name, g.color AS tag_color, t.title AS todo_title`

func entryQueryOrder(where, order string) string {
	return fmt.Sprintf(`SELECT %s FROM time_entries te
		LEFT JOIN tags g ON g.id = te.tag_id
		LEFT JOIN todos t ON t.id = te.todo_id
		WHERE %s ORDER BY te.start_time %s`, entryColumns, where, order)
}

func entryQuery(where string) string {
	return entryQueryOrder(where, "ASC")
}

// collectEntries drains a rows cursor into a slice of entries.
func collectEntries(rows *sql.Rows) ([]models.TimeEntry, error) {
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

// ListEntriesForDay returns entries overlapping the given date.
func ListEntriesForDay(db *sql.DB, date string) ([]models.TimeEntry, error) {
	start, end := tutil.DayRange(date)
	return listEntriesOverlapping(db, start, end)
}

// ListEntriesInRange returns entries overlapping [start, end) storage strings.
func ListEntriesInRange(db *sql.DB, start, end string) ([]models.TimeEntry, error) {
	return listEntriesOverlapping(db, start, end)
}

// overlapWhere matches entries overlapping [start, end): an entry whose end is
// open (in progress) always ends after start. Touching intervals do not count.
const overlapWhere = `te.start_time < ? AND (te.end_time IS NULL OR te.end_time > ?)`

func listEntriesOverlapping(db *sql.DB, start, end string) ([]models.TimeEntry, error) {
	rows, err := db.Query(entryQuery(overlapWhere), end, start)
	if err != nil {
		return nil, err
	}
	return collectEntries(rows)
}

// findOverlaps returns entries overlapping [start, end), excluding excludeID
// when it is > 0 (so an entry being updated does not conflict with itself).
func findOverlaps(db *sql.DB, excludeID int64, start, end string) ([]models.TimeEntry, error) {
	where, args := overlapWhere, []any{end, start}
	if excludeID > 0 {
		where += ` AND te.id <> ?`
		args = append(args, excludeID)
	}
	rows, err := db.Query(entryQuery(where), args...)
	if err != nil {
		return nil, err
	}
	return collectEntries(rows)
}

// GetEntry fetches a single entry by id.
func GetEntry(db *sql.DB, id int64) (models.TimeEntry, error) {
	row := db.QueryRow(entryQuery(`te.id = ?`), id)
	return scanEntry(row)
}

// StartEntry creates a new in-progress entry, first stopping any active entry.
func StartEntry(db *sql.DB, todoID *int64, tagID *int64, note string) (models.TimeEntry, error) {
	// Inherit the primary tag from the todo if not provided.
	if tagID == nil && todoID != nil {
		if primary, err := primaryTagID(db, *todoID); err == nil {
			tagID = primary
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
	res, err := tx.Exec(`INSERT INTO time_entries (todo_id, tag_id, start_time, note) VALUES (?, ?, ?, ?)`,
		todoID, tagID, now, note)
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

// ActiveEntry returns the currently running entry, or nil if none. It picks the
// most recently started one so it always agrees with StopEntry.
func ActiveEntry(db *sql.DB) (*models.TimeEntry, error) {
	row := db.QueryRow(entryQueryOrder(`te.end_time IS NULL`, "DESC"))
	e, err := scanEntry(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// WriteOptions tunes how a manual entry is validated.
type WriteOptions struct {
	// AllowOverlap accepts a span that overlaps existing entries. It never
	// waives the single-in-progress-entry rule.
	AllowOverlap bool
}

// validateEntry normalizes and validates a manual entry before it is written.
// excludeID skips an existing row (used when updating) both for the overlap
// check and the in-progress check.
func validateEntry(db *sql.DB, excludeID int64, e *models.TimeEntry) error {
	if e.StartTime == "" {
		e.StartTime = tutil.Now()
	}
	start, err := tutil.ParseStrict(e.StartTime)
	if err != nil {
		return fmt.Errorf("开始时间格式无效")
	}
	if e.EndTime != nil && *e.EndTime == "" {
		e.EndTime = nil
	}
	var end string
	if e.EndTime != nil {
		et, err := tutil.ParseStrict(*e.EndTime)
		if err != nil {
			return fmt.Errorf("结束时间格式无效")
		}
		if !et.After(start) {
			return fmt.Errorf("结束时间必须晚于开始时间")
		}
		end = *e.EndTime
	} else {
		// Only one entry may be in progress at a time.
		query := `SELECT id FROM time_entries WHERE end_time IS NULL`
		args := []any{}
		if excludeID > 0 {
			query += ` AND id <> ?`
			args = append(args, excludeID)
		}
		query += ` LIMIT 1`
		var id int64
		switch err := db.QueryRow(query, args...).Scan(&id); {
		case err == nil:
			return ActiveEntryError{}
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		end = endOfTime
	}
	conflicts, err := findOverlaps(db, excludeID, e.StartTime, end)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &OverlapError{Conflicts: conflicts}
	}
	return nil
}

// CreateEntry inserts a fully-specified entry (for manual back-filling).
func CreateEntry(db *sql.DB, e models.TimeEntry, opts WriteOptions) (models.TimeEntry, error) {
	if err := validateEntry(db, 0, &e); err != nil && !(opts.AllowOverlap && isOverlap(err)) {
		return models.TimeEntry{}, err
	}
	if e.TagID == nil && e.TodoID != nil {
		if primary, err := primaryTagID(db, *e.TodoID); err == nil {
			e.TagID = primary
		}
	}
	res, err := db.Exec(`INSERT INTO time_entries (todo_id, tag_id, start_time, end_time, note)
		VALUES (?, ?, ?, ?, ?)`, e.TodoID, e.TagID, e.StartTime, e.EndTime, e.Note)
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
func UpdateEntry(db *sql.DB, id int64, e models.TimeEntry, opts WriteOptions) (models.TimeEntry, error) {
	if err := validateEntry(db, id, &e); err != nil && !(opts.AllowOverlap && isOverlap(err)) {
		return models.TimeEntry{}, err
	}
	if e.TagID == nil && e.TodoID != nil {
		if primary, err := primaryTagID(db, *e.TodoID); err == nil {
			e.TagID = primary
		}
	}
	res, err := db.Exec(`UPDATE time_entries SET todo_id = ?, tag_id = ?, start_time = ?, end_time = ?, note = ? WHERE id = ?`,
		e.TodoID, e.TagID, e.StartTime, e.EndTime, e.Note, id)
	if err != nil {
		return models.TimeEntry{}, err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return models.TimeEntry{}, sql.ErrNoRows
	}
	return GetEntry(db, id)
}

// isOverlap reports whether err is an OverlapError (the only waivable failure).
func isOverlap(err error) bool {
	var oe *OverlapError
	return errors.As(err, &oe)
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
	rows, err := db.Query(entryQuery(`te.todo_id IN (`+placeholders+`)`),
		args...)
	if err != nil {
		return nil, err
	}
	return collectEntries(rows)
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
	rows, err := db.Query(entryQuery(where), args...)
	if err != nil {
		return nil, err
	}
	return collectEntries(rows)
}

func primaryTagID(db *sql.DB, todoID int64) (*int64, error) {
	var tagID sql.NullInt64
	if err := db.QueryRow(`SELECT tag_id FROM todo_tags WHERE todo_id = ? ORDER BY tag_order LIMIT 1`, todoID).Scan(&tagID); err != nil {
		return nil, err
	}
	if !tagID.Valid {
		return nil, nil
	}
	v := tagID.Int64
	return &v, nil
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
