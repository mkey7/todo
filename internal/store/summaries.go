package store

import (
	"database/sql"

	"todo/internal/models"
	"todo/internal/tutil"
)

// GetDailySummary returns the summary for a date, or an empty one if absent.
func GetDailySummary(db *sql.DB, date string) (models.DailySummary, error) {
	var s models.DailySummary
	err := db.QueryRow(`SELECT id, date, improvement, notes, updated_at
		FROM daily_summaries WHERE date = ?`, date).
		Scan(&s.ID, &s.Date, &s.Improvement, &s.Notes, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return models.DailySummary{Date: date}, nil
	}
	return s, err
}

// UpsertDailySummary inserts or updates the user's reflection for a date.
func UpsertDailySummary(db *sql.DB, date, improvement, notes string) (models.DailySummary, error) {
	if _, err := db.Exec(`INSERT INTO daily_summaries (date, improvement, notes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET improvement = excluded.improvement,
			notes = excluded.notes, updated_at = excluded.updated_at`,
		date, improvement, notes, tutil.Now()); err != nil {
		return models.DailySummary{}, err
	}
	return GetDailySummary(db, date)
}
