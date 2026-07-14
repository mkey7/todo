package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

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
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return db, nil
}
