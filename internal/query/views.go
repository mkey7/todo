package query

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

type SavedView struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	Query     QueryRequest `json:"query"`
	CreatedAt string       `json:"created_at"`
	UpdatedAt string       `json:"updated_at"`
}

func viewData(name string, req QueryRequest) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", "", invalid("invalid_value", "name", "name must contain 1–100 characters")
	}
	if _, e := ValidateAndNormalize(req, NewCatalog()); e != nil {
		return "", "", e
	}
	b, e := json.Marshal(req)
	if e != nil {
		return "", "", e
	}
	if len(b) > 64*1024 {
		return "", "", invalid("query_limit_exceeded", "query", "query exceeds 64 KiB")
	}
	return name, string(b), nil
}
func ListViews(db *sql.DB) ([]SavedView, error) {
	rows, e := db.Query(`SELECT id,name,query_json,CAST(created_at AS TEXT),CAST(updated_at AS TEXT) FROM query_views ORDER BY updated_at DESC,id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	views := []SavedView{}
	for rows.Next() {
		var v SavedView
		var raw string
		if e = rows.Scan(&v.ID, &v.Name, &raw, &v.CreatedAt, &v.UpdatedAt); e != nil {
			return nil, e
		}
		if len(raw) > 64*1024 {
			return nil, invalid("query_limit_exceeded", "query", "stored query exceeds 64 KiB")
		}
		dec := json.NewDecoder(bytes.NewBufferString(raw))
		dec.DisallowUnknownFields()
		if e = dec.Decode(&v.Query); e != nil {
			return nil, invalid("invalid_query", "query", "invalid stored query")
		}
		if e = dec.Decode(new(any)); e != io.EOF {
			return nil, invalid("invalid_query", "query", "invalid stored query suffix")
		}
		q, e := ValidateAndNormalize(v.Query, NewCatalog())
		if e != nil {
			return nil, e
		}
		v.Query = q.QueryRequest
		views = append(views, v)
	}
	return views, rows.Err()
}
func CreateView(db *sql.DB, name string, req QueryRequest) (SavedView, error) {
	name, raw, e := viewData(name, req)
	if e != nil {
		return SavedView{}, e
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r, e := db.Exec(`INSERT INTO query_views(name,query_json,created_at,updated_at) VALUES(?,?,?,?)`, name, raw, now, now)
	if e != nil {
		return SavedView{}, e
	}
	id, e := r.LastInsertId()
	if e != nil {
		return SavedView{}, e
	}
	q, _ := ValidateAndNormalize(req, NewCatalog())
	return SavedView{id, name, q.QueryRequest, now, now}, nil
}
func UpdateView(db *sql.DB, id int64, name string, req QueryRequest) (SavedView, error) {
	name, raw, e := viewData(name, req)
	if e != nil {
		return SavedView{}, e
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r, e := db.Exec(`UPDATE query_views SET name=?,query_json=?,updated_at=? WHERE id=?`, name, raw, now, id)
	if e != nil {
		return SavedView{}, e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return SavedView{}, e
	}
	if n == 0 {
		return SavedView{}, sql.ErrNoRows
	}
	var created string
	if e = db.QueryRow(`SELECT CAST(created_at AS TEXT) FROM query_views WHERE id=?`, id).Scan(&created); e != nil {
		return SavedView{}, e
	}
	q, _ := ValidateAndNormalize(req, NewCatalog())
	return SavedView{id, name, q.QueryRequest, created, now}, nil
}
func DeleteView(db *sql.DB, id int64) error {
	r, e := db.Exec(`DELETE FROM query_views WHERE id=?`, id)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n == 0 {
		return sql.ErrNoRows
	}
	return e
}
