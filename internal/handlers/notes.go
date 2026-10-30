package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"todo/internal/notes"
	"todo/internal/query"
)

func noteError(w http.ResponseWriter, err error) {
	var ne *notes.Error
	var qe *query.Error
	if errors.As(err, &qe) {
		ne = &notes.Error{Code: qe.Code, Message: qe.Message, Path: qe.Path}
	}
	if ne == nil && !errors.As(err, &ne) {
		ne = &notes.Error{Code: "internal_error", Message: "operation failed", Path: ""}
	}
	status := 400
	switch ne.Code {
	case "internal_error":
		status = 500
	case "not_found":
		status = 404
	case "revision_conflict":
		status = 409
	case "request_too_large":
		status = 413
	case "unsupported_view", "unsupported_version", "visualization_limit_exceeded":
		status = 422
	case "query_timeout":
		status = 504
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = 504
		ne = &notes.Error{Code: "query_timeout", Message: "query timed out or was canceled", Path: "query"}
	}
	writeJSON(w, status, ne)
}
func decodeNote(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		noteError(w, notes.Invalid("request_too_large", "request", "request exceeds size limit"))
		return false
	}
	if err == nil {
		err = notes.Strict(raw, v)
	}
	if err != nil {
		noteError(w, notes.Invalid("invalid_value", "request", "expected one valid JSON object: "+err.Error()))
		return false
	}
	return true
}
func noteID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		noteError(w, notes.Invalid("invalid_value", "id", "invalid note ID"))
		return 0, false
	}
	return id, true
}
func (h *Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	limit, offset := 50, 0
	var err error
	for k, vs := range r.URL.Query() {
		if len(vs) != 1 || (k != "limit" && k != "offset" && k != "search" && k != "deleted") {
			noteError(w, notes.Invalid("invalid_value", k, "unknown or repeated list parameter"))
			return
		}
	}
	if s := r.URL.Query().Get("limit"); s != "" {
		limit, err = strconv.Atoi(s)
	}
	if err == nil {
		if s := r.URL.Query().Get("offset"); s != "" {
			offset, err = strconv.Atoi(s)
		}
	}
	deleted := r.URL.Query().Get("deleted")
	if err != nil || limit < 1 || limit > 100 || offset < 0 || offset > 100000 || (deleted != "" && deleted != "true" && deleted != "false") {
		noteError(w, notes.Invalid("invalid_value", "page", "invalid list parameters"))
		return
	}
	out, err := notes.List(r.Context(), h.DB, limit, offset, r.URL.Query().Get("search"), deleted == "true")
	if err != nil {
		noteError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *Handler) CreateNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Document notes.Document `json:"document"`
	}
	if !decodeNote(w, r, &body, notes.MaxDocument) {
		return
	}
	n, err := notes.Create(r.Context(), h.DB, body.Document)
	if err != nil {
		noteError(w, err)
		return
	}
	writeJSON(w, 201, n)
}
func (h *Handler) GetNote(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	n, err := notes.Get(r.Context(), h.DB, id)
	if err != nil {
		noteError(w, err)
		return
	}
	writeJSON(w, 200, n)
}
func (h *Handler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	var body struct {
		Revision int64          `json:"revision"`
		Document notes.Document `json:"document"`
	}
	if !decodeNote(w, r, &body, notes.MaxDocument) {
		return
	}
	if body.Revision < 1 {
		noteError(w, notes.Invalid("invalid_value", "revision", "positive revision required"))
		return
	}
	n, err := notes.Update(r.Context(), h.DB, id, body.Revision, body.Document)
	if err != nil {
		noteError(w, err)
		return
	}
	writeJSON(w, 200, n)
}
func (h *Handler) DeleteNote(w http.ResponseWriter, r *http.Request)  { h.setNoteDeleted(w, r, true) }
func (h *Handler) RestoreNote(w http.ResponseWriter, r *http.Request) { h.setNoteDeleted(w, r, false) }
func (h *Handler) setNoteDeleted(w http.ResponseWriter, r *http.Request, deleted bool) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	var body struct {
		Revision int64 `json:"revision"`
	}
	if !decodeNote(w, r, &body, 1024) {
		return
	}
	if body.Revision < 1 {
		noteError(w, notes.Invalid("invalid_value", "revision", "positive revision required"))
		return
	}
	n, err := notes.SetDeleted(r.Context(), h.DB, id, body.Revision, deleted)
	if err != nil {
		noteError(w, err)
		return
	}
	if deleted {
		w.WriteHeader(204)
	} else {
		writeJSON(w, 200, n)
	}
}
func (h *Handler) ImportSummary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		Key  string `json:"key"`
	}
	if !decodeNote(w, r, &body, 1024) {
		return
	}
	n, created, err := notes.Import(r.Context(), h.DB, body.Kind, body.Key)
	if err != nil {
		noteError(w, err)
		return
	}
	status := 200
	if created {
		status = 201
	}
	writeJSON(w, status, n)
}
