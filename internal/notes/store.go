package notes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func title(d Document) string {
	if strings.TrimSpace(d.Title) == "" {
		return "未命名笔记"
	}
	return d.Title
}

const selectNote = `SELECT id,title,document_json,revision,created_at,updated_at,deleted_at FROM notes`

type scanner interface{ Scan(...any) error }

func scan(s scanner, full bool) (n Note, err error) {
	var raw string
	err = s.Scan(&n.ID, &n.Title, &raw, &n.Revision, &n.CreatedAt, &n.UpdatedAt, &n.DeletedAt)
	if err == sql.ErrNoRows {
		return n, Invalid("not_found", "id", "note not found")
	}
	if err != nil {
		return n, err
	}
	if full {
		var doc Document
		if err = Strict([]byte(raw), &doc); err != nil {
			return n, err
		}
		if err = Validate(doc); err != nil {
			return n, err
		}
		Upgrade(&doc)
		n.Document = &doc
	}
	return
}
func Get(ctx context.Context, db *sql.DB, id int64) (Note, error) {
	return scan(db.QueryRowContext(ctx, selectNote+` WHERE id=? AND deleted_at IS NULL`, id), true)
}
func Create(ctx context.Context, db *sql.DB, d Document) (Note, error) {
	if err := Validate(d); err != nil {
		return Note{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback()
	raw, _ := json.Marshal(d)
	tm := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO notes(title,document_json,created_at,updated_at) VALUES(?,?,?,?)`, title(d), string(raw), tm, tm)
	if err != nil {
		return Note{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Note{}, err
	}
	n, err := scan(tx.QueryRowContext(ctx, selectNote+` WHERE id=?`, id), true)
	if err != nil {
		return Note{}, err
	}
	return n, tx.Commit()
}
func Update(ctx context.Context, db *sql.DB, id, revision int64, d Document) (Note, error) {
	if err := Validate(d); err != nil {
		return Note{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback()
	raw, _ := json.Marshal(d)
	res, err := tx.ExecContext(ctx, `UPDATE notes SET title=?,document_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND deleted_at IS NULL`, title(d), string(raw), now(), id, revision)
	if err != nil {
		return Note{}, err
	}
	if err = changed(ctx, tx, id, res); err != nil {
		return Note{}, err
	}
	n, err := scan(tx.QueryRowContext(ctx, selectNote+` WHERE id=?`, id), true)
	if err != nil {
		return Note{}, err
	}
	return n, tx.Commit()
}

type queryRow interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func changed(ctx context.Context, db queryRow, id int64, r sql.Result) error {
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return Invalid("not_found", "id", "note not found")
	}
	return Invalid("revision_conflict", "revision", "note changed; keep your draft and reload or save a copy")
}
func SetDeleted(ctx context.Context, db *sql.DB, id, revision int64, deleted bool) (Note, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback()
	state := "deleted_at IS NOT NULL"
	var value any
	if deleted {
		state = "deleted_at IS NULL"
		value = now()
	}
	res, err := tx.ExecContext(ctx, `UPDATE notes SET deleted_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND `+state, value, now(), id, revision)
	if err != nil {
		return Note{}, err
	}
	if err = changed(ctx, tx, id, res); err != nil {
		return Note{}, err
	}
	n, err := scan(tx.QueryRowContext(ctx, selectNote+` WHERE id=?`, id), true)
	if err != nil {
		return Note{}, err
	}
	return n, tx.Commit()
}

type ListResult struct {
	Items      []Note `json:"items"`
	TotalCount int    `json:"total_count"`
	NextOffset *int   `json:"next_offset"`
}

func List(ctx context.Context, db *sql.DB, limit, offset int, search string, deleted bool) (out ListResult, err error) {
	out.Items = []Note{}
	where := ` WHERE deleted_at IS NULL`
	if deleted {
		where = ` WHERE deleted_at IS NOT NULL`
	}
	where += ` AND instr(lower(title),lower(?))>0`
	// Single read transaction keeps list metadata and its count consistent.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM notes`+where, search).Scan(&out.TotalCount); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,title,'',revision,created_at,updated_at,deleted_at FROM notes`+where+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, search, limit, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		n, e := scan(rows, false)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, n)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	next := offset + len(out.Items)
	if next < out.TotalCount {
		out.NextOffset = &next
	}
	return out, tx.Commit()
}
func Import(ctx context.Context, db *sql.DB, kind, key string) (note Note, created bool, err error) {
	var table, field string
	var start time.Time
	switch kind {
	case "daily":
		table = "daily_summaries"
		field = "date"
		start, err = time.ParseInLocation("2006-01-02", key, time.Local)
	case "weekly":
		table = "weekly_summaries"
		field = "week"
		var y, w int
		if _, err = fmt.Sscanf(key, "%d-W%d", &y, &w); err == nil {
			jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, time.Local)
			wd := (int(jan4.Weekday()) + 6) % 7
			start = jan4.AddDate(0, 0, -wd+(w-1)*7)
			yy, ww := start.ISOWeek()
			if fmt.Sprintf("%04d-W%02d", yy, ww) != key {
				err = fmt.Errorf("invalid ISO week")
			}
		}
	default:
		err = fmt.Errorf("unknown summary kind")
	}
	if err != nil || start.IsZero() {
		return note, false, Invalid("invalid_value", "key", "invalid summary kind or date")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	origin := kind + ":" + key
	note, err = scan(tx.QueryRowContext(ctx, selectNote+` WHERE import_key=?`, origin), true)
	if err == nil {
		return note, false, tx.Commit()
	}
	if e, ok := err.(*Error); !ok || e.Code != "not_found" {
		return note, false, err
	}
	var content string
	err = tx.QueryRowContext(ctx, `SELECT content FROM `+table+` WHERE `+field+`=?`, key).Scan(&content)
	if err == sql.ErrNoRows {
		return note, false, Invalid("not_found", "key", "summary not found")
	}
	if err != nil {
		return
	}
	days := 1
	if kind == "weekly" {
		days = 7
	}
	d := Document{SchemaVersion: 1, Title: key + " 总结", Context: Context{Range: Range{Mode: "fixed", Start: start.Format("2006-01-02"), End: start.AddDate(0, 0, days).Format("2006-01-02")}, TagIDs: []int64{}}, Blocks: []Block{}}
	// Preserve long legacy text without violating each block's size budget.
	runes := []rune(content)
	for len(runes) > 0 {
		n := min(len(runes), 20000)
		d.Blocks = append(d.Blocks, Block{ID: ID(), Type: "paragraph", Text: string(runes[:n])})
		runes = runes[n:]
	}
	if len(d.Blocks) == 0 {
		d.Blocks = append(d.Blocks, Block{ID: ID(), Type: "paragraph"})
	}
	if err = Validate(d); err != nil {
		return
	}
	raw, _ := json.Marshal(d)
	tm := now()
	r, err := tx.ExecContext(ctx, `INSERT INTO notes(title,document_json,created_at,updated_at,import_key) VALUES(?,?,?,?,?)`, title(d), string(raw), tm, tm, origin)
	if err != nil {
		return note, false, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return note, false, err
	}
	note, err = scan(tx.QueryRowContext(ctx, selectNote+` WHERE id=?`, id), true)
	if err != nil {
		return note, false, err
	}
	return note, true, tx.Commit()
}
