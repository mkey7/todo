package handlers

import (
	"net/http"

	"todo/internal/analysis"
	"todo/internal/tutil"
)

// GET /api/analysis/daily?date=YYYY-MM-DD
func (h *Handler) DailyAnalysis(w http.ResponseWriter, r *http.Request) {
	date := strParam(r, "date")
	if date == "" {
		date = tutil.Today()
	}
	res, err := analysis.AnalyzeDaily(h.DB, date)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /api/analysis/weekly?week=YYYY-Www
func (h *Handler) WeeklyAnalysis(w http.ResponseWriter, r *http.Request) {
	week := strParam(r, "week")
	if week == "" {
		week = tutil.CurrentISOWeek()
	}
	res, err := analysis.AnalyzeWeekly(h.DB, week)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
