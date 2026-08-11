package handlers

import (
	"net/http"
	"strconv"

	"todo/internal/models"
	"todo/internal/store"
	"todo/internal/tutil"
)

// GET /api/time-entries?date=YYYY-MM-DD
func (h *Handler) ListTimeEntries(w http.ResponseWriter, r *http.Request) {
	date := strParam(r, "date")
	if date == "" {
		date = tutil.Today()
	}
	es, err := store.ListEntriesForDay(h.DB, date)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if es == nil {
		es = []models.TimeEntry{}
	}
	writeJSON(w, http.StatusOK, es)
}

// GET /api/time-entries/active
func (h *Handler) ActiveTimeEntry(w http.ResponseWriter, r *http.Request) {
	e, err := store.ActiveEntry(h.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// POST /api/time-entries/start  {todo_id?, tag_id?, note?}
func (h *Handler) StartTimeEntry(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TodoID *int64 `json:"todo_id"`
		TagID  *int64 `json:"tag_id"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	e, err := store.StartEntry(h.DB, body.TodoID, body.TagID, body.Note)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// POST /api/time-entries/stop
func (h *Handler) StopTimeEntry(w http.ResponseWriter, r *http.Request) {
	e, err := store.StopEntry(h.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e.ID == 0 {
		writeErr(w, http.StatusBadRequest, "no active entry")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// POST /api/time-entries  (manual entry)
func (h *Handler) CreateTimeEntry(w http.ResponseWriter, r *http.Request) {
	var e models.TimeEntry
	if err := decodeJSON(r, &e); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	created, err := store.CreateEntry(h.DB, e)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// PUT /api/time-entries/{id}
func (h *Handler) UpdateTimeEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var e models.TimeEntry
	if err := decodeJSON(r, &e); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	updated, err := store.UpdateEntry(h.DB, id, e)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DELETE /api/time-entries/{id}
func (h *Handler) DeleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := store.DeleteEntry(h.DB, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
