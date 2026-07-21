package analysis

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupDB creates an in-memory SQLite database with the minimal tables the
// analysis package reads, and returns it.
func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	schema := `
	CREATE TABLE groups (id INTEGER PRIMARY KEY, name TEXT, color TEXT, sort_order INTEGER, created_at TEXT);
	CREATE TABLE todos (id INTEGER PRIMARY KEY, group_id INTEGER, parent_id INTEGER, title TEXT, description TEXT,
		status TEXT, priority INTEGER, due_date TEXT, created_at TEXT, completed_at TEXT);
	CREATE TABLE time_entries (id INTEGER PRIMARY KEY, todo_id INTEGER, group_id INTEGER,
		start_time TEXT, end_time TEXT, note TEXT, created_at TEXT);
	CREATE TABLE daily_summaries (id INTEGER PRIMARY KEY, date TEXT UNIQUE, improvement TEXT, notes TEXT, updated_at TEXT);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// fixedTime returns a time.Time for the given clock hour on today's date,
// in local time, to keep tests deterministic relative to "today".
func fixedTime(hour, min int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, time.Local)
}

// ts converts a local time to the storage string format.
func ts(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func insertEntry(t *testing.T, db *sql.DB, groupID int, start, end string) {
	t.Helper()
	var endArg any
	if end != "" {
		endArg = end
	}
	if _, err := db.Exec(`INSERT INTO time_entries (group_id, start_time, end_time) VALUES (?, ?, ?)`,
		groupID, start, endArg); err != nil {
		t.Fatalf("insert entry: %v", err)
	}
}

func TestDailyAnalysisTotalsAndGroups(t *testing.T) {
	db := setupDB(t)
	// two groups
	db.Exec(`INSERT INTO groups (id, name, color) VALUES (1, '开发', '#6366f1'), (2, '学习', '#22c55e')`)

	today := time.Now().Format("2006-01-02")
	// 09:00-10:00 dev (3600s), 14:00-15:30 study (5400s)
	insertEntry(t, db, 1, today+" 09:00:00", today+" 10:00:00")
	insertEntry(t, db, 2, today+" 14:00:00", today+" 15:30:00")

	res, err := AnalyzeDaily(db, today)
	if err != nil {
		t.Fatalf("AnalyzeDaily: %v", err)
	}
	wantTotal := 3600.0 + 5400.0
	if abs(res.TotalSeconds-wantTotal) > 1 {
		t.Errorf("total seconds = %v, want %v", res.TotalSeconds, wantTotal)
	}
	if len(res.GroupBreakdown) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(res.GroupBreakdown))
	}
	// study (5400) should be the largest group
	if res.GroupBreakdown[0].GroupName != "学习" {
		t.Errorf("expected top group 学习, got %s", res.GroupBreakdown[0].GroupName)
	}
	if abs(res.GroupBreakdown[0].Seconds-5400) > 1 {
		t.Errorf("study seconds = %v, want 5400", res.GroupBreakdown[0].Seconds)
	}
	if res.EntryCount != 2 {
		t.Errorf("entry count = %v, want 2", res.EntryCount)
	}
	// longest focus: the 1.5h study block (single entry) vs 1h dev block.
	// Since the two entries are >15min apart, they are separate blocks.
	if res.LongestFocus == nil {
		t.Fatal("expected longest focus block")
	}
	if abs(res.LongestFocus.Seconds-5400) > 1 {
		t.Errorf("longest focus = %v, want 5400", res.LongestFocus.Seconds)
	}
}

func TestDailyAnalysisPartOfDay(t *testing.T) {
	db := setupDB(t)
	db.Exec(`INSERT INTO groups (id, name, color) VALUES (1, '开发', '#6366f1')`)
	today := time.Now().Format("2006-01-02")
	// 10:00-13:00 spans 上午 (10-12) and 下午 (12-13)
	insertEntry(t, db, 1, today+" 10:00:00", today+" 13:00:00")

	res, err := AnalyzeDaily(db, today)
	if err != nil {
		t.Fatalf("AnalyzeDaily: %v", err)
	}
	partSec := map[string]float64{}
	for _, p := range res.PartOfDay {
		partSec[p.Part] = p.Seconds
	}
	// 上午 should get 2h (10-12), 下午 should get 1h (12-13)
	if abs(partSec["上午"]-7200) > 1 {
		t.Errorf("上午 = %v, want 7200", partSec["上午"])
	}
	if abs(partSec["下午"]-3600) > 1 {
		t.Errorf("下午 = %v, want 3600", partSec["下午"])
	}
}

func TestDailyAnalysisActiveEntryCountsAsNow(t *testing.T) {
	db := setupDB(t)
	db.Exec(`INSERT INTO groups (id, name, color) VALUES (1, '开发', '#6366f1')`)
	today := time.Now().Format("2006-01-02")
	// An entry started 1 hour ago with no end_time should count ~3600s.
	start := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	insertEntry(t, db, 1, start, "")

	res, err := AnalyzeDaily(db, today)
	if err != nil {
		t.Fatalf("AnalyzeDaily: %v", err)
	}
	if res.TotalSeconds < 3500 || res.TotalSeconds > 3700 {
		t.Errorf("active entry total = %v, want ~3600", res.TotalSeconds)
	}
}

func TestWeeklyAnalysisTrend(t *testing.T) {
	db := setupDB(t)
	db.Exec(`INSERT INTO groups (id, name, color) VALUES (1, '开发', '#6366f1')`)

	// Build entries for the current ISO week: 1h on Monday, 2h on Wednesday.
	year, week := time.Now().ISOWeek()
	monday := mondayOfISOWeek(year, week)
	db.Exec(`INSERT INTO time_entries (group_id, start_time, end_time) VALUES (1, ?, ?)`,
		ts(monday.Add(9*time.Hour)), ts(monday.Add(10*time.Hour)))
	wed := monday.AddDate(0, 0, 2)
	db.Exec(`INSERT INTO time_entries (group_id, start_time, end_time) VALUES (1, ?, ?)`,
		ts(wed.Add(9*time.Hour)), ts(wed.Add(11*time.Hour)))

	weekLabel := formatISOWeek(monday)
	res, err := AnalyzeWeekly(db, weekLabel)
	if err != nil {
		t.Fatalf("AnalyzeWeekly: %v", err)
	}
	if abs(res.TotalSeconds-10800) > 1 {
		t.Errorf("weekly total = %v, want 10800", res.TotalSeconds)
	}
	if len(res.DailyTrend) != 7 {
		t.Fatalf("expected 7 trend days, got %d", len(res.DailyTrend))
	}
	if res.BestDay == nil || res.BestDay.Weekday != "周三" {
		t.Errorf("best day = %+v, want 周三", res.BestDay)
	}
}

func TestWeeklyAnalysisUsesISOYearAtCalendarBoundary(t *testing.T) {
	db := setupDB(t)

	res, err := AnalyzeWeekly(db, "2025-W01")
	if err != nil {
		t.Fatalf("AnalyzeWeekly: %v", err)
	}
	if res.WeekStart != "2024-12-30" {
		t.Errorf("week start = %q, want 2024-12-30", res.WeekStart)
	}
	if res.WeekLabel != "2025-W01" {
		t.Errorf("week label = %q, want 2025-W01", res.WeekLabel)
	}
}

// helpers
func mondayOfISOWeek(year, week int) time.Time {
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.Local)
	wd := int(jan4.Weekday())
	if wd == 0 {
		wd = 7
	}
	mondayWeek1 := jan4.AddDate(0, 0, -(wd-1))
	return mondayWeek1.AddDate(0, 0, (week-1)*7)
}
func formatISOWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return formatWeek(y, w)
}
func formatWeek(y, w int) string {
	return pad(y) + "-W" + pad2(w)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func pad(y int) string {
	return fmt.Sprintf("%04d", y)
}

func pad2(w int) string {
	return fmt.Sprintf("%02d", w)
}
