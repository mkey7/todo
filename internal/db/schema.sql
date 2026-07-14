CREATE TABLE IF NOT EXISTS groups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    color      TEXT    NOT NULL DEFAULT '#6b7280',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS todos (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id     INTEGER,
    parent_id    INTEGER,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending',
    priority     INTEGER NOT NULL DEFAULT 0,
    due_date     TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    FOREIGN KEY (group_id)  REFERENCES groups(id) ON DELETE SET NULL,
    FOREIGN KEY (parent_id) REFERENCES todos(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS time_entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    todo_id    INTEGER,
    group_id   INTEGER,
    start_time TEXT    NOT NULL,
    end_time   TEXT,
    note       TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (todo_id)  REFERENCES todos(id) ON DELETE SET NULL,
    FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS daily_summaries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    date        TEXT    NOT NULL UNIQUE,
    improvement TEXT    NOT NULL DEFAULT '',
    notes       TEXT    NOT NULL DEFAULT '',
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_todos_group        ON todos(group_id);
CREATE INDEX IF NOT EXISTS idx_todos_parent       ON todos(parent_id);
CREATE INDEX IF NOT EXISTS idx_todos_status       ON todos(status);
CREATE INDEX IF NOT EXISTS idx_time_entries_start ON time_entries(start_time);
CREATE INDEX IF NOT EXISTS idx_time_entries_todo  ON time_entries(todo_id);
CREATE INDEX IF NOT EXISTS idx_time_entries_group ON time_entries(group_id);
