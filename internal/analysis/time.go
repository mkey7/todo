package analysis

import (
	"time"

	"todo/internal/tutil"
)

// parseOrZero converts a storage datetime string to epoch seconds.
func parseOrZero(s string) float64 {
	t := tutil.ParseStorage(s)
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}

// nowSeconds returns the current epoch seconds.
func nowSeconds() float64 {
	t := time.Now()
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}

// nowTime returns the current local time.
func nowTime() time.Time {
	return time.Now()
}
