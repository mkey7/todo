package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"todo/internal/query"
)

func queryError(w http.ResponseWriter, err error) {
	var qe *query.Error
	if errors.As(err, &qe) {
		status := http.StatusBadRequest
		if qe.Code == "query_timeout" {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, qe)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 404, &query.Error{Code: "not_found", Message: "view not found", Path: "id"})
		return
	}
	writeJSON(w, 500, &query.Error{Code: "query_failed", Message: "database query failed", Path: "query"})
}
func decodeQuery(w http.ResponseWriter, r *http.Request, v any) bool {
	// Read the entire bounded body so trailing JSON and oversized suffixes cannot be ignored.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err == nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		err = decodeJSON(r, v)
	}
	if err == nil {
		// decodeJSON checks unknown keys; a separate decoder checks that exactly one value was supplied.
		err = ensureSingleJSON(body)
	}
	if err != nil {
		queryError(w, &query.Error{Code: "invalid_query", Message: "expected one JSON object (maximum 64 KiB): " + err.Error(), Path: "query"})
		return false
	}
	return true
}
func (h *Handler) QueryCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, query.NewCatalog())
}
func (h *Handler) ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	var req query.QueryRequest
	if !decodeQuery(w, r, &req) {
		return
	}
	q, err := query.ValidateAndNormalize(req, query.NewCatalog())
	if err != nil {
		queryError(w, err)
		return
	}
	result, err := query.NewSQLiteExecutor(h.DB).Execute(r.Context(), q)
	if err != nil {
		queryError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) ListQueryViews(w http.ResponseWriter, r *http.Request) {
	views, err := query.ListViews(h.DB)
	if err != nil {
		queryError(w, err)
		return
	}
	writeJSON(w, 200, views)
}
func queryViewID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		queryError(w, &query.Error{Code: "invalid_value", Message: "invalid view ID", Path: "id"})
		return 0, false
	}
	return id, true
}
func (h *Handler) writeQueryView(w http.ResponseWriter, r *http.Request, update bool) {
	var body struct {
		Name  string             `json:"name"`
		Query query.QueryRequest `json:"query"`
	}
	if !decodeQuery(w, r, &body) {
		return
	}
	var view query.SavedView
	var err error
	status := 201
	if update {
		id, ok := queryViewID(w, r)
		if !ok {
			return
		}
		view, err = query.UpdateView(h.DB, id, body.Name, body.Query)
		status = 200
	} else {
		view, err = query.CreateView(h.DB, body.Name, body.Query)
	}
	if err != nil {
		queryError(w, err)
		return
	}
	writeJSON(w, status, view)
}
func (h *Handler) CreateQueryView(w http.ResponseWriter, r *http.Request) {
	h.writeQueryView(w, r, false)
}
func (h *Handler) UpdateQueryView(w http.ResponseWriter, r *http.Request) {
	h.writeQueryView(w, r, true)
}
func (h *Handler) DeleteQueryView(w http.ResponseWriter, r *http.Request) {
	id, ok := queryViewID(w, r)
	if !ok {
		return
	}
	if err := query.DeleteView(h.DB, id); err != nil {
		queryError(w, err)
		return
	}
	w.WriteHeader(204)
}

func ensureSingleJSON(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	var v any
	if err := dec.Decode(&v); err != nil {
		return err
	}
	if _, ok := v.(map[string]any); !ok {
		return fmt.Errorf("object required")
	}
	if err := dec.Decode(&v); err != io.EOF {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}
