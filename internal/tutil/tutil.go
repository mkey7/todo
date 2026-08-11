package tutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Layout is the naive local datetime format used for SQLite storage.
// Storing without timezone offset keeps lexicographic string comparisons
// correct for date-range queries, as long as the process timezone is stable.
const Layout = "2006-01-02 15:04:05"

const DateLayout = "2006-01-02"

// Now returns the current local time as a storage string.
func Now() string {
	return time.Now().Format(Layout)
}

// Today returns today's date in the process local timezone.
func Today() string {
	return time.Now().Format(DateLayout)
}

// DateOf parses a storage datetime string and returns just the date part.
func DateOf(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// DayRange returns the [start, end) storage strings for the given date
// (YYYY-MM-DD). If date is empty or invalid, today is used.
func DayRange(date string) (start, end string) {
	d := parseDateOrDefault(date, time.Now())
	start = d.Format(Layout)
	end = d.Add(24 * time.Hour).Format(Layout)
	return start, end
}

// DayBounds returns the start and end times (end exclusive) as time.Time.
func DayBounds(date string) (start, end time.Time) {
	d := parseDateOrDefault(date, time.Now())
	start = d
	end = d.Add(24 * time.Hour)
	return start, end
}

func parseDateOrDefault(date string, fallback time.Time) time.Time {
	if date == "" {
		return time.Date(fallback.Year(), fallback.Month(), fallback.Day(), 0, 0, 0, 0, time.Local)
	}
	d, err := time.ParseInLocation(DateLayout, date, time.Local)
	if err != nil {
		return time.Date(fallback.Year(), fallback.Month(), fallback.Day(), 0, 0, 0, 0, time.Local)
	}
	return d
}

// WeekRange returns the Monday-based [start, end) storage strings for the ISO
// week given as "YYYY-Www" (e.g. "2026-W28"). If empty/invalid, current week.
func WeekRange(week string) (start, end string, startDay string) {
	year, w := parseISOWeek(week, time.Now())
	monday := mondayOfISOWeek(year, w)
	start = monday.Format(Layout)
	end = monday.Add(7 * 24 * time.Hour).Format(Layout)
	startDay = monday.Format(DateLayout)
	return start, end, startDay
}

// PrevISOWeek returns the ISO week string for the week before the given one.
func PrevISOWeek(week string) string {
	year, w := parseISOWeek(week, time.Now())
	monday := mondayOfISOWeek(year, w)
	prev := monday.AddDate(0, 0, -7)
	py, pw := prev.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", py, pw)
}

func parseISOWeek(week string, fallback time.Time) (year, w int) {
	if week == "" {
		y, wn := fallback.ISOWeek()
		return y, wn
	}
	// expect "YYYY-Www" or "YYYY-Www"
	week = strings.TrimSpace(week)
	parts := strings.SplitN(week, "-W", 2)
	if len(parts) != 2 {
		y, wn := fallback.ISOWeek()
		return y, wn
	}
	y, err1 := strconv.Atoi(parts[0])
	wn, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || wn < 1 || wn > 53 {
		y2, wn2 := fallback.ISOWeek()
		return y2, wn2
	}
	return y, wn
}

// mondayOfISOWeek returns the Monday (00:00 local) of the given ISO year/week.
func mondayOfISOWeek(year, week int) time.Time {
	// Jan 4 is always in ISO week 1.
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.Local)
	wd := int(jan4.Weekday())
	if wd == 0 {
		wd = 7 // Sunday -> 7 (ISO Monday=1)
	}
	mondayWeek1 := jan4.AddDate(0, 0, -(wd - 1))
	return mondayWeek1.AddDate(0, 0, (week-1)*7)
}

// CurrentISOWeek returns the current ISO week string.
func CurrentISOWeek() string {
	y, w := time.Now().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

// FormatDuration renders seconds as "1h 23m" / "23m" / "45s".
func FormatDuration(seconds float64) string {
	if seconds < 1 {
		return "0s"
	}
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	s := int(d.Seconds()) - h*3600 - m*60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// PartOfDay classifies an hour into a named time band.
func PartOfDay(hour int) string {
	switch {
	case hour >= 5 && hour < 12:
		return "上午"
	case hour >= 12 && hour < 18:
		return "下午"
	case hour >= 18 && hour < 23:
		return "晚上"
	default:
		return "深夜"
	}
}

// ParseStorage parses a storage datetime string into local time.
func ParseStorage(s string) time.Time {
	t, err := time.ParseInLocation(Layout, s, time.Local)
	if err != nil {
		// try date only
		t, err = time.ParseInLocation(DateLayout, s, time.Local)
		if err != nil {
			return time.Now()
		}
	}
	return t
}
