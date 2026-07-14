package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"todo/internal/db"
)

// TestStaticServing verifies the frontend is served and the SPA fallback
// returns index.html for unknown paths.
func TestStaticServing(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	webFS := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><html><body>app</body></html>")},
		"app.js":     {Data: []byte("console.log('hi');")},
		"style.css":  {Data: []byte("body{}")},
	}
	srv := New(d, webFS)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	// index.html at root
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("root status = %d", resp.StatusCode)
	}
	body := readBody(resp)
	if !strings.Contains(body, "app") {
		t.Errorf("root body = %q", body)
	}

	// app.js
	resp, err = http.Get(ts.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("app.js status = %d", resp.StatusCode)
	}

	// unknown path -> SPA fallback to index.html
	resp, err = http.Get(ts.URL + "/some-spa-route")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("spa fallback status = %d", resp.StatusCode)
	}
	body = readBody(resp)
	if !strings.Contains(body, "app") {
		t.Errorf("fallback body = %q", body)
	}
}

func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(b)
}
