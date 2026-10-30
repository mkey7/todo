package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"todo/internal/db"
	"todo/internal/notes"
)

func TestNotesAPI(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	h := New(d, nil).routes()
	call := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		var raw []byte
		switch v := body.(type) {
		case string:
			raw = []byte(v)
		default:
			raw, _ = json.Marshal(v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(raw)))
		if w.Code != status {
			t.Fatalf("%s %s: expected %d got %d %s", method, path, status, w.Code, w.Body.String())
		}
		out := map[string]any{}
		if status != 204 {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	doc := notes.Document{SchemaVersion: 1, Title: "测试", Context: notes.Context{Range: notes.Range{Mode: "none"}}, Blocks: []notes.Block{{ID: notes.ID(), Type: "paragraph", Text: "<b>text</b>"}}}
	n := call("POST", "/api/notes", map[string]any{"document": doc}, 201)
	id := int(n["id"].(float64))
	path := fmt.Sprintf("/api/notes/%d", id)
	call("GET", path, nil, 200)
	call("PUT", path, map[string]any{"revision": 1, "document": doc}, 200)
	call("PUT", path, map[string]any{"revision": 1, "document": doc}, 409)
	list := call("GET", "/api/notes?search=测试", nil, 200)
	if list["total_count"] != float64(1) {
		t.Fatal(list)
	}
	item := list["items"].([]any)[0].(map[string]any)
	if item["document"] != nil {
		t.Fatal("list leaked entire document")
	}
	call("DELETE", path, `{"revision":2}`, 204)
	call("GET", path, nil, 404)
	call("POST", path+"/restore", `{"revision":3}`, 200)
	for _, bad := range []string{`{} {}`, `{"document":{},"sql":"DROP TABLE notes"}`, `{"document":{"schema_version":99}}`} {
		call("POST", "/api/notes", bad, 400)
	}
	call("GET", "/api/notes?limit=101", nil, 400)
	call("GET", "/api/notes?deleted=bad", nil, 400)
	call("POST", "/api/notes", strings.Repeat(" ", notes.MaxDocument+1), 413)
	call("POST", "/api/notes/import-summary", `{"kind":"daily","key":"2026-02-30"}`, 400)
	call("POST", "/api/notes/import-summary", `{"kind":"daily","key":"2026-10-30"}`, 404)
	if _, err = d.Exec(`INSERT INTO daily_summaries(date,content) VALUES('2026-10-30','原文')`); err != nil {
		t.Fatal(err)
	}
	imported := call("POST", "/api/notes/import-summary", `{"kind":"daily","key":"2026-10-30"}`, 201)
	again := call("POST", "/api/notes/import-summary", `{"kind":"daily","key":"2026-10-30"}`, 200)
	if imported["id"] != again["id"] {
		t.Fatal("duplicate import")
	}
	call("GET", "/api/note-functions", nil, 200)
	query := `{"definition":{"kind":"function","name":"todos","version":1,"params":{"range":{"mode":"inherit"},"tag_ids":{"mode":"inherit"}}},"context":{"range":{"mode":"none"},"tag_ids":[]},"view":{"type":"table","mapping":{},"options":{}}}`
	call("POST", "/api/note-query/execute", query, 200)
	call("POST", "/api/note-query/execute", strings.Replace(query, `"version":1`, `"version":99`, 1), 422)
	call("POST", "/api/note-query/execute", strings.Replace(query, `"name":"todos"`, `"name":"todos;DROP TABLE notes"`, 1), 422)
	call("POST", "/api/note-query/execute", strings.Replace(query, `"mapping":{}`, `"mapping":{"html":"x"}`, 1), 400)
	call("POST", "/api/note-query/execute", query+` {}`, 400)
	call("POST", "/api/note-query/execute", strings.Repeat(" ", 128*1024+1), 413)
	// Existing APIs remain present and their old payloads still work.
	call("POST", "/api/query/execute", `{"source":"todos"}`, 200)
	call("GET", "/api/summaries/daily?date=2026-10-30", nil, 200)
}

func TestV2NotesQueryHTTPContract(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	h := New(d, nil).routes()
	call := func(body string, status int) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/note-query/execute", strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("expected %d got %d: %s", status, w.Code, w.Body.String())
		}
		var result map[string]any
		if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	body := `{"query_mode":"detail","definition":{"kind":"custom","query":{"source":"todos","select":[{"field":"title"},{"field":"priority"}],"order_by":[{"key":"priority","direction":"asc"}]},"search":{"text":"","fields":["title"]}},"context":{"range":{"mode":"none"},"tag_ids":[]},"view":{"type":"table","mapping":{},"options":{"group":{"field":"title","direction":"asc"},"summaries":[{"id":"n","op":"count_all"},{"id":"sum","field":"priority","op":"sum"}]}},"page":{"limit":50,"offset":0}}`
	out := call(body, 200)
	if out["summaries"].(map[string]any)["matched_row_count"] != float64(0) {
		t.Fatal(out)
	}
	call(strings.Replace(body, `"field":"priority","op":"sum"`, `"field":"title","op":"sum"`, 1), 400)
	call(strings.Replace(body, `"id":"sum"`, `"id":"n"`, 1), 400)
	call(strings.Replace(body, `"query_mode":"detail"`, `"query_mode":"aggregate"`, 1), 400)
	call(strings.Replace(body, `"query_mode":"detail"`, `"query_mode":"detail","sort":[{"key":"priority","direction":"desc"}]`, 1), 400)
	call(strings.Replace(body, `"source":"todos"`, `"source":"todos","sql":"DROP TABLE notes"`, 1), 400)
}
