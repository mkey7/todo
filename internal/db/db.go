package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Open creates/opens the SQLite database at path, ensures the parent directory
// exists, applies the schema, and sets pragmatic defaults for a single-process
// embedded database.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// A single writer connection serializes writes safely; allow multiple
	// readers. For this app's load, one connection is simplest and correct.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("set WAL: %w", err)
	}
	// Relations are maintained by application logic. Disabling SQLite foreign
	// keys also keeps databases created by earlier schemas usable after the
	// groups-to-tags migration.
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return nil, fmt.Errorf("disable foreign keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrateTimeEntryTagColumn(db); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_time_entries_tag ON time_entries(tag_id)`); err != nil {
		return nil, fmt.Errorf("index time entry tag: %w", err)
	}
	// schema.sql is also applied to databases created by earlier app versions.
	// SQLite's CREATE TABLE IF NOT EXISTS does not add new columns, so migrate
	// the tag description explicitly and preserve every legacy group binding.
	// Copy legacy groups into the canonical tags table without changing IDs.
	if _, err := db.Exec(`INSERT OR IGNORE INTO tags (id, name, description, color, created_at)
		SELECT id, name, COALESCE(description, ''), color, created_at FROM groups`); err != nil && !strings.Contains(err.Error(), "no such table") {
		return nil, fmt.Errorf("migrate legacy groups: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE tags ADD COLUMN include_in_stats INTEGER NOT NULL DEFAULT 1`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return nil, fmt.Errorf("migrate tag statistics flag: %w", err)
	}
	if _, err := db.Exec(`UPDATE tags SET include_in_stats = 0 WHERE name IN ('进行中', '已完成')`); err != nil {
		return nil, fmt.Errorf("exclude status tags from statistics: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE todo_tags ADD COLUMN tag_order INTEGER NOT NULL DEFAULT 0`); err == nil {
		// Preserve the old global-tag order once when upgrading existing data.
		if _, err := db.Exec(`UPDATE todo_tags SET tag_order = COALESCE(
			(SELECT sort_order FROM groups WHERE groups.id = todo_tags.tag_id), 0)`); err != nil {
			return nil, fmt.Errorf("migrate todo tag order: %w", err)
		}
	} else if !strings.Contains(err.Error(), "duplicate column name") {
		return nil, fmt.Errorf("migrate todo tag order: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_todo_tags_order ON todo_tags(todo_id, tag_order)`); err != nil {
		return nil, fmt.Errorf("index todo tag order: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE daily_summaries ADD COLUMN content TEXT NOT NULL DEFAULT ''`); err == nil {
		if _, err := db.Exec(`UPDATE daily_summaries SET content = TRIM(improvement || CASE WHEN notes <> '' THEN '\n' || notes ELSE '' END)`); err != nil {
			return nil, fmt.Errorf("migrate daily summary content: %w", err)
		}
	} else if !strings.Contains(err.Error(), "duplicate column name") {
		return nil, fmt.Errorf("migrate daily summary content: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO todo_tags (todo_id, tag_id, tag_order)
		SELECT id, group_id, 0 FROM todos WHERE group_id IS NOT NULL`); err != nil && !strings.Contains(err.Error(), "no such column") {
		return nil, fmt.Errorf("migrate todo tags: %w", err)
	}
	return db, nil
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// migrateTimeEntryTagColumn rebuilds the legacy table so group_id and any old
// foreign-key definitions are physically removed, preserving every record.
func migrateTimeEntryTagColumn(db *sql.DB) error {
	hasTag, err := hasColumn(db, "time_entries", "tag_id")
	if err != nil {
		return fmt.Errorf("inspect time_entries: %w", err)
	}
	if hasTag {
		return nil
	}
	hasGroup, err := hasColumn(db, "time_entries", "group_id")
	if err != nil {
		return fmt.Errorf("inspect legacy time_entries: %w", err)
	}
	if !hasGroup {
		return nil
	}
	if _, err := db.Exec(`CREATE TABLE time_entries_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT, todo_id INTEGER, tag_id INTEGER,
		start_time TEXT NOT NULL, end_time TEXT, note TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("create time entry migration table: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO time_entries_new (id, todo_id, tag_id, start_time, end_time, note, created_at)
		SELECT id, todo_id, group_id, start_time, end_time, note, created_at FROM time_entries`); err != nil {
		return fmt.Errorf("copy time entries: %w", err)
	}
	if _, err := db.Exec(`DROP TABLE time_entries`); err != nil {
		return fmt.Errorf("remove legacy time_entries: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE time_entries_new RENAME TO time_entries`); err != nil {
		return fmt.Errorf("rename migrated time_entries: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_time_entries_start ON time_entries(start_time)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_time_entries_todo ON time_entries(todo_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_time_entries_tag ON time_entries(tag_id)`); err != nil {
		return err
	}
	return nil
}
