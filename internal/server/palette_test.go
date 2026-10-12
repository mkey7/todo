package server

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"todo/internal/tutil"
)

// TestTodoColorPaletteMatchesFrontend guards the deliberate duplication between
// tutil.TodoColor and TODO_COLORS in web/app.js: a task must render in the same
// color in the timelines (JS) and the analysis charts (Go). Drift here would be
// silent, so pin the two lists to each other.
func TestTodoColorPaletteMatchesFrontend(t *testing.T) {
	src, err := os.ReadFile("../../web/app.js")
	if err != nil {
		t.Fatalf("read web/app.js: %v", err)
	}
	const marker = "const TODO_COLORS = ["
	start := strings.Index(string(src), marker)
	if start < 0 {
		t.Fatalf("%q not found in web/app.js", marker)
	}
	rest := string(src)[start+len(marker):]
	end := strings.Index(rest, "];")
	if end < 0 {
		t.Fatalf("TODO_COLORS array is not terminated with ];")
	}
	quoted := regexp.MustCompile(`'#[0-9a-fA-F]{6}'`).FindAllString(rest[:end], -1)
	jsPalette := make([]string, 0, len(quoted))
	for _, q := range quoted {
		jsPalette = append(jsPalette, strings.Trim(q, "'"))
	}
	if len(jsPalette) == 0 {
		t.Fatal("TODO_COLORS is empty")
	}

	// Both sides index the palette by id % len, so walking ids 1..len visits
	// every slot once. A length mismatch on either side breaks the alignment
	// at some id, since the colors within each list are distinct.
	for id := 1; id <= len(jsPalette); id++ {
		if got, want := tutil.TodoColor(int64(id)), jsPalette[id%len(jsPalette)]; got != want {
			t.Errorf("TodoColor(%d) = %s, but web/app.js TODO_COLORS[%d] = %s", id, got, id%len(jsPalette), want)
		}
	}
}
