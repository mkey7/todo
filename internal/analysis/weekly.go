package analysis

import (
	"database/sql"
	"fmt"
	"time"

	"todo/internal/store"
	"todo/internal/tutil"
)

var weekdaysCN = []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// AnalyzeWeekly computes the rule-based analysis for the ISO week given as
// "YYYY-Www" (e.g. "2026-W28"). Empty means the current week.
func AnalyzeWeekly(db *sql.DB, week string) (WeeklyResult, error) {
	start, end, startDay := tutil.WeekRange(week)
	monday := tutil.ParseStorage(startDay + " 00:00:00")
	_, w := monday.ISOWeek()

	entries, err := store.ListEntriesInRange(db, start, end)
	if err != nil {
		return WeeklyResult{}, err
	}
	completed, err := store.CountCompletedInRange(db, start, end)
	if err != nil {
		return WeeklyResult{}, err
	}

	res := WeeklyResult{
		WeekStart:     startDay,
		WeekLabel:     fmt.Sprintf("%d-W%02d", monday.Year(), w),
		EntryCount:    len(entries),
		CompletedTodos: completed,
	}

	// Per-day bucketing (Mon..Sun).
	type bucket struct {
		seconds float64
		count   int
	}
	buckets := make([]bucket, 7)
	for _, e := range entries {
		s := entrySeconds(e)
		res.TotalSeconds += s
		dayIdx := weekdayIndex(tutil.ParseStorage(e.StartTime))
		buckets[dayIdx].seconds += s
		buckets[dayIdx].count++
	}
	res.TotalDuration = tutil.FormatDuration(res.TotalSeconds)

	res.DailyTrend = make([]DayStat, 7)
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		res.DailyTrend[i] = DayStat{
			Date:       d.Format(tutil.DateLayout),
			Weekday:    weekdaysCN[i],
			Seconds:    buckets[i].seconds,
			Duration:   tutil.FormatDuration(buckets[i].seconds),
			EntryCount: buckets[i].count,
		}
	}

	res.GroupBreakdown = buildGroupBreakdown(entries, res.TotalSeconds)

	// Best / slowest day (only counting days with any work).
	for i := range res.DailyTrend {
		d := res.DailyTrend[i]
		if d.Seconds <= 0 {
			continue
		}
		if res.BestDay == nil || d.Seconds > res.BestDay.Seconds {
			b := d
			res.BestDay = &b
		}
		if res.SlowestDay == nil || d.Seconds < res.SlowestDay.Seconds {
			s := d
			res.SlowestDay = &s
		}
	}

	// Vs last week.
	prevWeek := tutil.PrevISOWeek(res.WeekLabel)
	pStart, pEnd, _ := tutil.WeekRange(prevWeek)
	if pEntries, err := store.ListEntriesInRange(db, pStart, pEnd); err == nil {
		var pTotal float64
		for _, e := range pEntries {
			pTotal += entrySeconds(e)
		}
		delta := res.TotalSeconds - pTotal
		res.VsLastWeek = &WeekCompare{
			LastWeekSeconds: pTotal,
			DeltaSeconds:    delta,
			Duration:        signedDuration(delta),
		}
		if pTotal > 0 {
			res.VsLastWeek.DeltaPercent = (delta / pTotal) * 100
		}
	}

	res.Summary = weeklySummaryText(res)
	return res, nil
}

// weekdayIndex returns 0 for Monday .. 6 for Sunday.
func weekdayIndex(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		return 6 // Sunday
	}
	return wd - 1
}

func weeklySummaryText(r WeeklyResult) string {
	if r.TotalSeconds < 1 {
		return "本周还没有记录工时。"
	}
	parts := []string{fmt.Sprintf("本周累计工作 %s，共 %d 条记录。", r.TotalDuration, r.EntryCount)}
	if r.CompletedTodos > 0 {
		parts = append(parts, fmt.Sprintf("完成任务 %d 个。", r.CompletedTodos))
	}
	if r.BestDay != nil {
		parts = append(parts, fmt.Sprintf("投入最多的一天是 %s（%s）。", r.BestDay.Weekday, r.BestDay.Duration))
	}
	if len(r.GroupBreakdown) > 0 {
		g := r.GroupBreakdown[0]
		parts = append(parts, fmt.Sprintf("主要投入在「%s」(%.0f%%)。", g.GroupName, g.Percent))
	}
	if r.VsLastWeek != nil && r.VsLastWeek.LastWeekSeconds > 0 {
		d := r.VsLastWeek
		if d.DeltaSeconds > 0 {
			parts = append(parts, fmt.Sprintf("比上周多 %s（%.0f%%）。", tutil.FormatDuration(d.DeltaSeconds), d.DeltaPercent))
		} else if d.DeltaSeconds < 0 {
			parts = append(parts, fmt.Sprintf("比上周少 %s（%.0f%%）。", tutil.FormatDuration(-d.DeltaSeconds), d.DeltaPercent))
		} else {
			parts = append(parts, "与上周持平。")
		}
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}
