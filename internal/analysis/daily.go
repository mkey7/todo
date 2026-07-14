package analysis

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"todo/internal/models"
	"todo/internal/store"
	"todo/internal/tutil"
)

// AnalyzeDaily computes the rule-based analysis for the given date.
func AnalyzeDaily(db *sql.DB, date string) (DailyResult, error) {
	if date == "" {
		date = tutil.Today()
	}
	entries, err := store.ListEntriesForDay(db, date)
	if err != nil {
		return DailyResult{}, err
	}
	completed, err := store.CountCompletedOnDate(db, date)
	if err != nil {
		return DailyResult{}, err
	}
	active, err := activeTodoCount(db, date)
	if err != nil {
		return DailyResult{}, err
	}
	summary, _ := store.GetDailySummary(db, date)

	res := DailyResult{
		Date:            date,
		EntryCount:      len(entries),
		CompletedTodos:  completed,
		ActiveTodoCount: active,
		Improvement:     summary.Improvement,
		Notes:           summary.Notes,
	}

	for _, e := range entries {
		res.TotalSeconds += entrySeconds(e)
	}
	res.TotalDuration = tutil.FormatDuration(res.TotalSeconds)

	res.GroupBreakdown = buildGroupBreakdown(entries, res.TotalSeconds)
	res.PartOfDay = buildPartOfDay(entries, res.TotalSeconds)
	res.HourlyHistogram = buildHourHistogram(entries)
	res.LongestFocus = findLongestFocus(entries)

	// Comparison vs yesterday.
	yest := yesterday(date)
	yEntries, err := store.ListEntriesForDay(db, yest)
	if err == nil {
		var yTotal float64
		for _, e := range yEntries {
			yTotal += entrySeconds(e)
		}
		delta := res.TotalSeconds - yTotal
		res.VsYesterday = &DayComparison{
			YesterdaySeconds: yTotal,
			DeltaSeconds:     delta,
			Duration:         signedDuration(delta),
		}
		if yTotal > 0 {
			res.VsYesterday.DeltaPercent = (delta / yTotal) * 100
		}
	}

	res.Summary = dailySummaryText(res)
	return res, nil
}

func buildGroupBreakdown(entries []models.TimeEntry, total float64) []GroupStat {
	groups := collectGroups(entries)
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Seconds > groups[j].Seconds })
	for i := range groups {
		groups[i].Duration = tutil.FormatDuration(groups[i].Seconds)
		if total > 0 {
			groups[i].Percent = (groups[i].Seconds / total) * 100
		}
	}
	return groups
}

func buildPartOfDay(entries []models.TimeEntry, total float64) []PartOfDayStat {
	parts := []string{"上午", "下午", "晚上", "深夜"}
	sec := map[string]float64{}
	for _, e := range entries {
		// Attribute worked seconds to each hour the entry covers.
		distributeEntryByHour(e, func(hour int, s float64) {
			sec[tutil.PartOfDay(hour)] += s
		})
	}
	out := make([]PartOfDayStat, 0, len(parts))
	for _, p := range parts {
		s := sec[p]
		st := PartOfDayStat{Part: p, Seconds: s, Duration: tutil.FormatDuration(s)}
		if total > 0 {
			st.Percent = (s / total) * 100
		}
		out = append(out, st)
	}
	return out
}

func buildHourHistogram(entries []models.TimeEntry) []HourBin {
	sec := map[int]float64{}
	for _, e := range entries {
		distributeEntryByHour(e, func(hour int, s float64) { sec[hour] += s })
	}
	out := make([]HourBin, 0, 24)
	for h := 0; h < 24; h++ {
		out = append(out, HourBin{Hour: h, Seconds: sec[h]})
	}
	return out
}

// distributeEntryByHour splits an entry's duration across the clock hours it
// spans, calling fn for each (hour, seconds-in-that-hour).
func distributeEntryByHour(e models.TimeEntry, fn func(hour int, seconds float64)) {
	start := tutil.ParseStorage(e.StartTime)
	var endT time.Time
	if e.EndTime != nil {
		endT = tutil.ParseStorage(*e.EndTime)
	} else {
		endT = nowTime()
	}
	if !endT.After(start) {
		return
	}
	cur := start
	for cur.Before(endT) {
		// End of this clock hour.
		hourEnd := cur.Truncate(time.Hour).Add(time.Hour)
		if hourEnd.After(endT) {
			hourEnd = endT
		}
		fn(cur.Hour(), hourEnd.Sub(cur).Seconds())
		cur = hourEnd
	}
}

// findLongestFocus groups temporally-adjacent entries (gap <= 15min) and
// returns the longest contiguous block.
func findLongestFocus(entries []models.TimeEntry) *FocusBlock {
	if len(entries) == 0 {
		return nil
	}
	sorted := make([]models.TimeEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartTime < sorted[j].StartTime })

	const gap = 15 * 60 // seconds
	var best *FocusBlock
	cur := &FocusBlock{}
	for _, e := range sorted {
		eStart := parseOrZero(e.StartTime)
		if cur.EntryCount == 0 {
			cur.Start = e.StartTime
			cur.End = formatEnd(e)
			cur.Seconds = entrySeconds(e)
			cur.EntryCount = 1
			continue
		}
		curEnd := parseOrZero(cur.End)
		if eStart-curEnd <= gap {
			cur.End = formatEnd(e)
			cur.Seconds += entrySeconds(e)
			cur.EntryCount++
		} else {
			if best == nil || cur.Seconds > best.Seconds {
				b := *cur
				best = &b
			}
			cur = &FocusBlock{Start: e.StartTime, End: formatEnd(e), Seconds: entrySeconds(e), EntryCount: 1}
		}
	}
	if cur.EntryCount > 0 {
		if best == nil || cur.Seconds > best.Seconds {
			b := *cur
			best = &b
		}
	}
	if best != nil {
		best.Duration = tutil.FormatDuration(best.Seconds)
	}
	return best
}

func dailySummaryText(r DailyResult) string {
	if r.TotalSeconds < 1 {
		return "今天还没有记录工时，开始记录吧。"
	}
	parts := []string{fmt.Sprintf("今日累计工作 %s，共 %d 条记录。", r.TotalDuration, r.EntryCount)}
	if r.CompletedTodos > 0 {
		parts = append(parts, fmt.Sprintf("完成任务 %d 个。", r.CompletedTodos))
	}
	if len(r.GroupBreakdown) > 0 {
		g := r.GroupBreakdown[0]
		parts = append(parts, fmt.Sprintf("主要投入在「%s」(%.0f%%)。", g.GroupName, g.Percent))
	}
	if r.LongestFocus != nil && r.LongestFocus.Seconds >= 60 {
		parts = append(parts, fmt.Sprintf("最长连续专注 %s。", r.LongestFocus.Duration))
	}
	if r.VsYesterday != nil {
		d := r.VsYesterday
		if d.YesterdaySeconds > 0 {
			if d.DeltaSeconds > 0 {
				parts = append(parts, fmt.Sprintf("比昨日多 %s。", tutil.FormatDuration(d.DeltaSeconds)))
			} else if d.DeltaSeconds < 0 {
				parts = append(parts, fmt.Sprintf("比昨日少 %s。", tutil.FormatDuration(-d.DeltaSeconds)))
			} else {
				parts = append(parts, "与昨日持平。")
			}
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

// --- helpers ---

func activeTodoCount(db *sql.DB, date string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM todos WHERE status != 'done' AND parent_id IS NULL`).Scan(&n)
	return n, err
}

func yesterday(date string) string {
	d := tutil.ParseStorage(date + " 00:00:00")
	y := d.AddDate(0, 0, -1)
	return y.Format(tutil.DateLayout)
}

func signedDuration(sec float64) string {
	if sec < 0 {
		return "-" + tutil.FormatDuration(-sec)
	}
	return tutil.FormatDuration(sec)
}

func formatEnd(e models.TimeEntry) string {
	if e.EndTime != nil {
		return *e.EndTime
	}
	return tutil.Now()
}
