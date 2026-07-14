package handlers

import (
	"net/http"

	"todo/internal/store"
	"todo/internal/tutil"
)

// GET /api/summaries/daily?date=YYYY-MM-DD
func (h *Handler) GetDailySummary(w http.ResponseWriter, r *http.Request) {
	date := strParam(r, "date")
	if date == "" {
		date = tutil.Today()
	}
	s, err := store.GetDailySummary(h.DB, date)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// PUT /api/summaries/daily?date=YYYY-MM-DD  {improvement, notes}
func (h *Handler) PutDailySummary(w http.ResponseWriter, r *http.Request) {
	date := strParam(r, "date")
	if date == "" {
		date = tutil.Today()
	}
	var body struct {
		Improvement string `json:"improvement"`
		Notes       string `json:"notes"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	s, err := store.UpsertDailySummary(h.DB, date, body.Improvement, body.Notes)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}
