package handlers

import (
	"net/http"
	"time"
	"todo/internal/notefunc"
)

func (h *Handler) NoteFunctions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"functions": notefunc.Catalog(), "timezone": time.Local.String(), "as_of": time.Now().Format(time.RFC3339)})
}
func (h *Handler) ExecuteNoteQuery(w http.ResponseWriter, r *http.Request) {
	var body notefunc.Request
	if !decodeNote(w, r, &body, 128*1024) {
		return
	}
	out, err := (notefunc.Executor{DB: h.DB}).Execute(r.Context(), body)
	if err != nil {
		noteError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
