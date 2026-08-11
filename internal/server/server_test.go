package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"todo/internal/db"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	srv := New(d, nil) // nil webFS: we only exercise the API in these tests
	return httptest.NewServer(srv.routes())
}

func do(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var r bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = *bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, &r)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func TestAPICreateGroupAndTodo(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	// create a group
	resp := do(t, "POST", s.URL+"/api/groups", map[string]any{"name": "开发", "color": "#6366f1"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create group status = %d", resp.StatusCode)
	}
	var g map[string]any
	json.NewDecoder(resp.Body).Decode(&g)
	resp.Body.Close()
	if g["name"] != "开发" {
		t.Errorf("group name = %v", g["name"])
	}
	gid := int64(g["id"].(float64))

	// create a todo in that group
	resp = do(t, "POST", s.URL+"/api/todos", map[string]any{"title": "写文档", "tag_ids": []int64{gid}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create todo status = %d", resp.StatusCode)
	}
	var todo map[string]any
	json.NewDecoder(resp.Body).Decode(&todo)
	resp.Body.Close()
	if todo["title"] != "写文档" {
		t.Errorf("todo title = %v", todo["title"])
	}
	tid := int64(todo["id"].(float64))

	// list todos
	resp = do(t, "GET", s.URL+"/api/todos", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list todos status = %d", resp.StatusCode)
	}
	var list []map[string]any
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(list))
	}

	// patch status to done
	resp = do(t, "PATCH", s.URL+"/api/todos/"+itoa(int(tid))+"/status", map[string]any{"status": "done"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// create a manual time entry
	resp = do(t, "POST", s.URL+"/api/time-entries", map[string]any{
		"tag_id": gid, "start_time": "2026-07-14 09:00:00", "end_time": "2026-07-14 10:30:00",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create entry status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// daily analysis
	resp = do(t, "GET", s.URL+"/api/analysis/daily?date=2026-07-14", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("daily analysis status = %d", resp.StatusCode)
	}
	var analysis map[string]any
	body := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, _ := resp.Body.Read(buf)
		if n == 0 {
			break
		}
		body = append(body, buf[:n]...)
	}
	resp.Body.Close()
	if err := json.Unmarshal(body, &analysis); err != nil {
		t.Fatalf("decode analysis: %v\nbody: %s", err, string(body))
	}
	// 1.5h = 5400s
	if v, ok := analysis["total_seconds"].(float64); !ok || v < 5399 || v > 5401 {
		t.Errorf("total_seconds = %v, want ~5400", analysis["total_seconds"])
	}
	if s := analysis["summary"].(string); !strings.Contains(s, "累计工作") {
		t.Errorf("summary = %q", s)
	}

	// put daily summary
	resp = do(t, "PUT", s.URL+"/api/summaries/daily?date=2026-07-14", map[string]any{"content": "学会了时间轴"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put summary status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// get it back
	resp = do(t, "GET", s.URL+"/api/summaries/daily?date=2026-07-14", nil)
	var summ map[string]any
	json.NewDecoder(resp.Body).Decode(&summ)
	resp.Body.Close()
	if summ["content"] != "学会了时间轴" {
		t.Errorf("summary content = %v", summ["content"])
	}

	resp = do(t, "PUT", s.URL+"/api/summaries/weekly?week=2026-W29", map[string]any{"content": "本周完成时间轴"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put weekly summary status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = do(t, "GET", s.URL+"/api/summaries/weekly?week=2026-W29", nil)
	var weeklySumm map[string]any
	json.NewDecoder(resp.Body).Decode(&weeklySumm)
	resp.Body.Close()
	if weeklySumm["content"] != "本周完成时间轴" {
		t.Errorf("weekly summary content = %v", weeklySumm["content"])
	}
}

func TestAPIStartStopTimer(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	// start
	resp := do(t, "POST", s.URL+"/api/time-entries/start", map[string]any{"tag_id": nil, "note": "测试"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// active
	resp = do(t, "GET", s.URL+"/api/time-entries/active", nil)
	var active map[string]any
	json.NewDecoder(resp.Body).Decode(&active)
	resp.Body.Close()
	if active == nil {
		t.Fatal("expected active entry")
	}

	// stop
	resp = do(t, "POST", s.URL+"/api/time-entries/stop", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// active again -> null
	resp = do(t, "GET", s.URL+"/api/time-entries/active", nil)
	var active2 any
	json.NewDecoder(resp.Body).Decode(&active2)
	resp.Body.Close()
	if active2 != nil {
		t.Errorf("expected null active, got %v", active2)
	}
}

// ensure the embedded DB import is referenced for the test binary.
var _ = sql.ErrNoRows

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
