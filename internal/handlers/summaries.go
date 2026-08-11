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

// PUT /api/summaries/daily?date=YYYY-MM-DD  {content}
func (h *Handler) PutDailySummary(w http.ResponseWriter, r *http.Request) {
	date := strParam(r, "date")
	if date == "" {
		date = tutil.Today()
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	s, err := store.UpsertDailySummary(h.DB, date, body.Content)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// GET /api/summaries/weekly?week=YYYY-Www
func (h *Handler) GetWeeklySummary(w http.ResponseWriter, r *http.Request) {
	week := strParam(r, "week")
	if week == "" {
		week = tutil.CurrentISOWeek()
	}
	s, err := store.GetWeeklySummary(h.DB, week)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// PUT /api/summaries/weekly?week=YYYY-Www  {content}
func (h *Handler) PutWeeklySummary(w http.ResponseWriter, r *http.Request) {
	week := strParam(r, "week")
	if week == "" {
		week = tutil.CurrentISOWeek()
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	s, err := store.UpsertWeeklySummary(h.DB, week, body.Content)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}
