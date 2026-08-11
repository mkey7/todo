CREATE TABLE IF NOT EXISTS tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    description TEXT   NOT NULL DEFAULT '',
    color      TEXT    NOT NULL DEFAULT '#6b7280',
    include_in_stats INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS todo_tags (
    todo_id INTEGER NOT NULL,
    tag_id  INTEGER NOT NULL,
    tag_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (todo_id, tag_id)
);

CREATE TABLE IF NOT EXISTS todos (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id    INTEGER,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending',
    priority     INTEGER NOT NULL DEFAULT 0,
    due_date     TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

CREATE TABLE IF NOT EXISTS time_entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    todo_id    INTEGER,
    tag_id     INTEGER,
    start_time TEXT    NOT NULL,
    end_time   TEXT,
    note       TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS daily_summaries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    date        TEXT    NOT NULL UNIQUE,
    content     TEXT    NOT NULL DEFAULT '',
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS weekly_summaries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    week        TEXT    NOT NULL UNIQUE,
    content     TEXT    NOT NULL DEFAULT '',
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);


CREATE INDEX IF NOT EXISTS idx_todos_parent       ON todos(parent_id);
CREATE INDEX IF NOT EXISTS idx_todos_status       ON todos(status);
CREATE INDEX IF NOT EXISTS idx_time_entries_start ON time_entries(start_time);
CREATE INDEX IF NOT EXISTS idx_time_entries_todo  ON time_entries(todo_id);
CREATE INDEX IF NOT EXISTS idx_todo_tags_tag ON todo_tags(tag_id);
