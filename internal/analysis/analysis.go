package analysis

import (
	"database/sql"
	"strings"
	"todo/internal/models"
)

// DailyResult is the rule-based analysis output for a single day.
type DailyResult struct {
	Date            string          `json:"date"`
	TotalSeconds    float64         `json:"total_seconds"`
	TotalDuration   string          `json:"total_duration"`
	EntryCount      int             `json:"entry_count"`
	CompletedTodos  int             `json:"completed_todos"`
	ActiveTodoCount int             `json:"active_todo_count"`
	GroupBreakdown  []GroupStat     `json:"group_breakdown"`
	TodoBreakdown   []TodoStat      `json:"todo_breakdown"`
	PartOfDay       []PartOfDayStat `json:"part_of_day"`
	LongestFocus    *FocusBlock     `json:"longest_focus"`
	HourlyHistogram []HourBin       `json:"hourly_histogram"`
	Content         string          `json:"content,omitempty"`
	VsYesterday     *DayComparison  `json:"vs_yesterday,omitempty"`
	Summary         string          `json:"summary"` // human-readable one-liner
}

// GroupStat is the time spent in one group.
type GroupStat struct {
	GroupID    int64   `json:"group_id"`
	GroupName  string  `json:"group_name"`
	GroupColor string  `json:"group_color"`
	Seconds    float64 `json:"seconds"`
	Duration   string  `json:"duration"`
	Percent    float64 `json:"percent"`
}

// TodoStat is the time attributed to one task. Unlinked time is grouped as
// “未关联任务”, so the breakdown still adds up to the total duration.
type TodoStat struct {
	TodoID    int64   `json:"todo_id"`
	TodoName  string  `json:"todo_name"`
	TodoColor string  `json:"todo_color"`
	Seconds   float64 `json:"seconds"`
	Duration  string  `json:"duration"`
	Percent   float64 `json:"percent"`
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
	Start      string  `json:"start"`
	End        string  `json:"end"`
	Seconds    float64 `json:"seconds"`
	Duration   string  `json:"duration"`
	EntryCount int     `json:"entry_count"`
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
	WeekStart      string      `json:"week_start"`
	WeekLabel      string      `json:"week_label"`
	TotalSeconds   float64     `json:"total_seconds"`
	TotalDuration  string      `json:"total_duration"`
	EntryCount     int         `json:"entry_count"`
	CompletedTodos int         `json:"completed_todos"`
	GroupBreakdown []GroupStat `json:"group_breakdown"`
	TodoBreakdown  []TodoStat  `json:"todo_breakdown"`
	DailyTrend     []DayStat   `json:"daily_trend"`
	// WeeklyEntries is the flat list of the week's time entries, used by the
	// frontend to render the 7-day parallel timeline. Each per-day timeline
	// clips these entries to its own day, so cross-midnight entries render
	// correctly on both days.
	WeeklyEntries []models.TimeEntry `json:"weekly_entries,omitempty"`
	BestDay       *DayStat           `json:"best_day"`
	SlowestDay    *DayStat           `json:"slowest_day"`
	VsLastWeek    *WeekCompare       `json:"vs_last_week,omitempty"`
	Summary       string             `json:"summary"`
}

// DayStat is a single day's totals within a week.
type DayStat struct {
	Date       string  `json:"date"`
	Weekday    string  `json:"weekday"`
	Seconds    float64 `json:"seconds"`
	Duration   string  `json:"duration"`
	EntryCount int     `json:"entry_count"`
}

// WeekCompare compares this week against the previous week.
type WeekCompare struct {
	LastWeekSeconds float64 `json:"last_week_seconds"`
	DeltaSeconds    float64 `json:"delta_seconds"`
	DeltaPercent    float64 `json:"delta_percent"`
	Duration        string  `json:"duration"`
}

// collectGroups attributes each entry equally across all tags on its todo.
// A manually-recorded entry uses its selected legacy group as a single tag.
// Splitting avoids multi-tag entries inflating total percentages above 100%.
func collectGroups(db *sql.DB, entries []models.TimeEntry) ([]GroupStat, error) {
	byID := map[int64]*GroupStat{}
	var order []int64
	for _, e := range entries {
		tags, err := entryTags(db, e)
		if err != nil {
			return nil, err
		}
		if len(tags) == 0 {
			tags = []models.Group{{Name: "未标记", Color: "#9ca3af"}}
		}
		sec := entrySeconds(e) / float64(len(tags))
		for _, tag := range tags {
			if st, ok := byID[tag.ID]; ok {
				st.Seconds += sec
			} else {
				byID[tag.ID] = &GroupStat{GroupID: tag.ID, GroupName: tag.Name, GroupColor: tag.Color, Seconds: sec}
				order = append(order, tag.ID)
			}
		}
	}
	out := make([]GroupStat, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func entryTags(db *sql.DB, e models.TimeEntry) ([]models.Group, error) {
	if e.TodoID == nil {
		if e.TagID == nil {
			return nil, nil
		}
		var g models.Group
		err := db.QueryRow(`SELECT id, name, color, include_in_stats, COALESCE(created_at, '') FROM tags WHERE id = ? AND include_in_stats = 1`, *e.TagID).
			Scan(&g.ID, &g.Name, &g.Color, new(bool), &g.CreatedAt)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []models.Group{g}, nil
	}
	rows, err := db.Query(`SELECT g.id, g.name, g.color, g.include_in_stats, COALESCE(g.created_at, '')
		FROM todo_tags tt JOIN tags g ON g.id = tt.tag_id WHERE tt.todo_id = ? AND g.include_in_stats = 1 ORDER BY tt.tag_order, tt.tag_id`, *e.TodoID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return legacyEntryTag(e), nil
		}
		return nil, err
	}
	defer rows.Close()
	var tags []models.Group
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Color, new(bool), &g.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, g)
	}
	return tags, rows.Err()
}

func legacyEntryTag(e models.TimeEntry) []models.Group {
	if e.TagID == nil {
		return nil
	}
	return []models.Group{{ID: *e.TagID, Name: e.TagName, Color: e.TagColor}}
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
