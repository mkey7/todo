package store

import (
	"database/sql"

	"todo/internal/models"
	"todo/internal/tutil"
)

// GetDailySummary returns the summary for a date, or an empty one if absent.
func GetDailySummary(db *sql.DB, date string) (models.DailySummary, error) {
	var s models.DailySummary
	err := db.QueryRow(`SELECT id, date, content, updated_at
		FROM daily_summaries WHERE date = ?`, date).
		Scan(&s.ID, &s.Date, &s.Content, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return models.DailySummary{Date: date}, nil
	}
	return s, err
}

// UpsertDailySummary inserts or updates the user's reflection for a date.
func UpsertDailySummary(db *sql.DB, date, content string) (models.DailySummary, error) {
	if _, err := db.Exec(`INSERT INTO daily_summaries (date, content, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		date, content, tutil.Now()); err != nil {
		return models.DailySummary{}, err
	}
	return GetDailySummary(db, date)
}

// GetWeeklySummary returns the summary for an ISO week, or an empty one if absent.
func GetWeeklySummary(db *sql.DB, week string) (models.WeeklySummary, error) {
	var s models.WeeklySummary
	err := db.QueryRow(`SELECT id, week, content, updated_at
		FROM weekly_summaries WHERE week = ?`, week).
		Scan(&s.ID, &s.Week, &s.Content, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return models.WeeklySummary{Week: week}, nil
	}
	return s, err
}

// UpsertWeeklySummary inserts or updates the user's reflection for an ISO week.
func UpsertWeeklySummary(db *sql.DB, week, content string) (models.WeeklySummary, error) {
	if _, err := db.Exec(`INSERT INTO weekly_summaries (week, content, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(week) DO UPDATE SET content = excluded.content,
			updated_at = excluded.updated_at`, week, content, tutil.Now()); err != nil {
		return models.WeeklySummary{}, err
	}
	return GetWeeklySummary(db, week)
}
