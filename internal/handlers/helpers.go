package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler holds shared dependencies for all API handler groups.
type Handler struct {
	DB *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{DB: db}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func intParam(r *http.Request, key string) (int64, bool) {
	q := r.URL.Query().Get(key)
	if q == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(q, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func strParam(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

func ptrInt64Param(r *http.Request, key string) *int64 {
	q := r.URL.Query().Get(key)
	if q == "" {
		return nil
	}
	n, err := strconv.ParseInt(q, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
