package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"todo/internal/db"
)

func TestQueryAPI(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	h := New(d, nil).routes()
	call := func(method, path, body string, status int) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var v map[string]any
		if status != 204 {
			if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
				t.Fatal(e)
			}
		}
		return v
	}
	call("GET", "/api/query/catalog", "", 200)
	call("POST", "/api/query/execute", `{"source":"todos"}`, 200)
	for _, raw := range []string{`"SELECT * FROM todos"`, `{"source":"todos","sql":"DROP TABLE todos"}`, `{"source":"todos","limit":501}`, `{"source":"todos","select":[{"field":"title; DROP TABLE todos"}]}`, `{"source":"todos","joins":[{"relation":"evil"}]}`, `{"source":"todos"} {}`, `{"source":"todos","where":{"not":{"not":{"not":{"not":{"not":{"field":"id","op":"eq","value":1}}}}}}}`} {
		v := call("POST", "/api/query/execute", raw, 400)
		if v["code"] == nil || v["path"] == nil {
			t.Fatal(v)
		}
	}
	saved := call("POST", "/api/query/views", `{"name":"report","query":{"source":"todos"}}`, 201)
	if saved["id"] != float64(1) {
		t.Fatal(saved)
	}
	call("PUT", "/api/query/views/1", `{"name":"rename","query":{"source":"tags"}}`, 200)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/query/views", nil))
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("rename")) {
		t.Fatal(w.Body.String())
	}
	call("DELETE", "/api/query/views/1", "", 204)
	call("DELETE", "/api/query/views/1", "", 404)
	call("POST", "/api/query/execute", `{"source":"todos","where":{"field":"title","op":"eq","value":"; DROP TABLE todos"}}`, 200)
	call("POST", "/api/query/execute", `{"source":"todos","where":{"field":"title","op":"eq","value":"`+string(bytes.Repeat([]byte("a"), 65536))+`"}}`, 400)
	var n int
	if e = d.QueryRow(`SELECT count(*) FROM todos`).Scan(&n); e != nil {
		t.Fatal(e)
	}
}

func TestQueryMonthlyAcceptance(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	h := New(d, nil).routes()
	call := func(method, path string, body any, status int) []byte {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(raw)))
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	call("POST", "/api/tags", map[string]any{"name": "开发"}, 201)
	call("POST", "/api/todos", map[string]any{"title": "写文档", "tag_ids": []int{1}}, 201)
	for _, entry := range []map[string]any{
		{"todo_id": 1, "tag_id": 1, "start_time": "2026-10-01 09:00:00", "end_time": "2026-10-01 10:00:00"},
		{"todo_id": 1, "tag_id": 1, "start_time": "2026-10-02 09:00:00", "end_time": "2026-10-02 09:30:00"},
		{"todo_id": 1, "tag_id": 1, "start_time": "2026-09-30 09:00:00", "end_time": "2026-09-30 10:00:00"},
	} {
		call("POST", "/api/time-entries", entry, 201)
	}
	request := map[string]any{"source": "time_entries", "joins": []map[string]any{{"relation": "todo"}}, "select": []map[string]any{{"field": "todo.title", "alias": "任务"}, {"field": "duration_seconds", "aggregate": "sum", "alias": "总秒数"}}, "where": map[string]any{"and": []map[string]any{{"field": "start_time", "op": "gte", "value": "2026-10-01 00:00:00"}, {"field": "start_time", "op": "lt", "value": "2026-11-01 00:00:00"}}}, "group_by": []string{"todo.title"}, "order_by": []map[string]any{{"key": "总秒数", "direction": "desc"}}}
	first := call("POST", "/api/query/execute", request, 200)
	var result struct {
		Rows       []map[string]any `json:"rows"`
		TotalCount int              `json:"total_count"`
	}
	if e = json.Unmarshal(first, &result); e != nil {
		t.Fatal(e)
	}
	if result.TotalCount != 1 || len(result.Rows) != 1 || result.Rows[0]["任务"] != "写文档" {
		t.Fatalf("%s", first)
	}
	seconds := result.Rows[0]["总秒数"].(float64)
	if seconds < 5399.99 || seconds > 5400.01 {
		t.Fatalf("%s", first)
	}
	call("POST", "/api/query/views", map[string]any{"name": "十月工时", "query": request}, 201)
	raw := call("GET", "/api/query/views", nil, 200)
	var views []struct {
		Query map[string]any `json:"query"`
	}
	if e = json.Unmarshal(raw, &views); e != nil || len(views) != 1 {
		t.Fatalf("%s %v", raw, e)
	}
	second := call("POST", "/api/query/execute", views[0].Query, 200)
	if !bytes.Equal(first, second) {
		t.Fatalf("reopened result differs:\n%s\n%s", first, second)
	}
}

func TestQueryExecutionErrors(t *testing.T) {
	d, e := db.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	h := New(d, nil).routes()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/query/execute", bytes.NewBufferString(`{"source":"todos"}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 504 || !bytes.Contains(w.Body.Bytes(), []byte(`"query_timeout"`)) {
		t.Fatalf("timeout: %d %s", w.Code, w.Body.String())
	}
	d.Close()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/query/execute", bytes.NewBufferString(`{"source":"todos"}`)))
	if w.Code != 500 || !bytes.Contains(w.Body.Bytes(), []byte(`"query_failed"`)) {
		t.Fatalf("execution error: %d %s", w.Code, w.Body.String())
	}
}
