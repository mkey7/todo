package analysis

import "todo/internal/models"

// DailyResult is the rule-based analysis output for a single day.
type DailyResult struct {
	Date             string             `json:"date"`
	TotalSeconds     float64            `json:"total_seconds"`
	TotalDuration    string             `json:"total_duration"`
	EntryCount       int                `json:"entry_count"`
	CompletedTodos   int                `json:"completed_todos"`
	ActiveTodoCount  int                `json:"active_todo_count"`
	GroupBreakdown   []GroupStat        `json:"group_breakdown"`
	PartOfDay        []PartOfDayStat    `json:"part_of_day"`
	LongestFocus     *FocusBlock        `json:"longest_focus"`
	HourlyHistogram  []HourBin          `json:"hourly_histogram"`
	Improvement      string             `json:"improvement,omitempty"`
	Notes            string             `json:"notes,omitempty"`
	VsYesterday      *DayComparison     `json:"vs_yesterday,omitempty"`
	Summary          string             `json:"summary"` // human-readable one-liner
}

// GroupStat is the time spent in one group.
type GroupStat struct {
	GroupID      int64   `json:"group_id"`
	GroupName    string  `json:"group_name"`
	GroupColor   string  `json:"group_color"`
	Seconds      float64 `json:"seconds"`
	Duration     string  `json:"duration"`
	Percent      float64 `json:"percent"`
}

// PartOfDayStat is the time spent in a named time band.
type PartOfDayStat struct {
	Part     string  `json:"part"`
	Seconds  float64 `json:"seconds"`
	Duration string  `json:"duration"`
	Percent  float64 `json:"percent"`
}

// FocusBlock is a contiguous run of work (gap < threshold) and its span.
type FocusBlock struct {
	Start        string  `json:"start"`
	End          string  `json:"end"`
	Seconds      float64 `json:"seconds"`
	Duration     string  `json:"duration"`
	EntryCount   int     `json:"entry_count"`
}

// HourBin is the worked seconds attributed to one clock hour (0-23).
type HourBin struct {
	Hour    int     `json:"hour"`
	Seconds float64 `json:"seconds"`
}

// DayComparison compares today against the previous day.
type DayComparison struct {
	YesterdaySeconds float64 `json:"yesterday_seconds"`
	DeltaSeconds     float64 `json:"delta_seconds"`
	DeltaPercent     float64 `json:"delta_percent"`
	Duration         string  `json:"duration"` // signed duration string
}

// WeeklyResult is the rule-based analysis output for a week.
type WeeklyResult struct {
	WeekStart        string       `json:"week_start"`
	WeekLabel        string       `json:"week_label"`
	TotalSeconds     float64      `json:"total_seconds"`
	TotalDuration    string       `json:"total_duration"`
	EntryCount       int          `json:"entry_count"`
	CompletedTodos   int          `json:"completed_todos"`
	GroupBreakdown   []GroupStat  `json:"group_breakdown"`
	DailyTrend       []DayStat    `json:"daily_trend"`
	// WeeklyEntries is the flat list of the week's time entries, used by the
	// frontend to render the 7-day parallel timeline. Each per-day timeline
	// clips these entries to its own day, so cross-midnight entries render
	// correctly on both days.
	WeeklyEntries    []models.TimeEntry `json:"weekly_entries,omitempty"`
	BestDay          *DayStat     `json:"best_day"`
	SlowestDay       *DayStat     `json:"slowest_day"`
	VsLastWeek       *WeekCompare `json:"vs_last_week,omitempty"`
	Summary          string       `json:"summary"`
}

// DayStat is a single day's totals within a week.
type DayStat struct {
	Date        string  `json:"date"`
	Weekday     string  `json:"weekday"`
	Seconds     float64 `json:"seconds"`
	Duration    string  `json:"duration"`
	EntryCount  int     `json:"entry_count"`
}

// WeekCompare compares this week against the previous week.
type WeekCompare struct {
	LastWeekSeconds float64 `json:"last_week_seconds"`
	DeltaSeconds    float64 `json:"delta_seconds"`
	DeltaPercent    float64 `json:"delta_percent"`
	Duration        string  `json:"duration"`
}

// dedupeGroups collapses entries with the same group into one stat, preserving
// the group that carries the display name/color.
func collectGroups(entries []models.TimeEntry) []GroupStat {
	byID := map[int64]*GroupStat{}
	var order []int64
	for _, e := range entries {
		var gid int64
		name := "未分组"
		color := "#9ca3af"
		if e.GroupID != nil {
			gid = *e.GroupID
		}
		if e.GroupName != "" {
			name = e.GroupName
		}
		if e.GroupColor != "" {
			color = e.GroupColor
		}
		sec := entrySeconds(e)
		if st, ok := byID[gid]; ok {
			st.Seconds += sec
		} else {
			byID[gid] = &GroupStat{GroupID: gid, GroupName: name, GroupColor: color, Seconds: sec}
			order = append(order, gid)
		}
	}
	out := make([]GroupStat, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

// entrySeconds returns the duration of an entry in seconds. An open entry is
// treated as ending now.
func entrySeconds(e models.TimeEntry) float64 {
	start := parseOrZero(e.StartTime)
	var end float64
	if e.EndTime != nil {
		end = parseOrZero(*e.EndTime)
	} else {
		end = nowSeconds()
	}
	if end <= start {
		return 0
	}
	return end - start
}
