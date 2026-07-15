package server

import (
	"database/sql"
	"io/fs"
	"net/http"
	"strings"

	"todo/internal/handlers"
)

// Server is the HTTP server serving both the API and the embedded frontend.
type Server struct {
	db     *sql.DB
	webFS  fs.FS
}

// New creates a Server. webFS should be the (sub)filesystem containing the
// embedded frontend assets (index.html, app.js, style.css).
func New(db *sql.DB, webFS fs.FS) *Server {
	return &Server{db: db, webFS: webFS}
}

func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.routes())
}

func (s *Server) routes() http.Handler {
	h := handlers.New(s.db)
	mux := http.NewServeMux()

	// --- API ---
	mux.HandleFunc("GET /api/groups", h.ListGroups)
	mux.HandleFunc("POST /api/groups", h.CreateGroup)
	mux.HandleFunc("PUT /api/groups/{id}", h.UpdateGroup)
	mux.HandleFunc("DELETE /api/groups/{id}", h.DeleteGroup)

	mux.HandleFunc("GET /api/todos", h.ListTodos)
	mux.HandleFunc("POST /api/todos", h.CreateTodo)
	mux.HandleFunc("PUT /api/todos/{id}", h.UpdateTodo)
	mux.HandleFunc("PATCH /api/todos/{id}/status", h.PatchTodoStatus)
	mux.HandleFunc("DELETE /api/todos/{id}", h.DeleteTodo)
	mux.HandleFunc("GET /api/todos/{id}/time-entries", h.TodoTimeEntries)
	mux.HandleFunc("GET /api/todos/{id}/time-entries/monthly", h.TodoTimeEntriesMonthly)

	mux.HandleFunc("GET /api/time-entries", h.ListTimeEntries)
	mux.HandleFunc("GET /api/time-entries/active", h.ActiveTimeEntry)
	mux.HandleFunc("POST /api/time-entries/start", h.StartTimeEntry)
	mux.HandleFunc("POST /api/time-entries/stop", h.StopTimeEntry)
	mux.HandleFunc("POST /api/time-entries", h.CreateTimeEntry)
	mux.HandleFunc("PUT /api/time-entries/{id}", h.UpdateTimeEntry)
	mux.HandleFunc("DELETE /api/time-entries/{id}", h.DeleteTimeEntry)

	mux.HandleFunc("GET /api/summaries/daily", h.GetDailySummary)
	mux.HandleFunc("PUT /api/summaries/daily", h.PutDailySummary)

	mux.HandleFunc("GET /api/analysis/daily", h.DailyAnalysis)
	mux.HandleFunc("GET /api/analysis/weekly", h.WeeklyAnalysis)

	// --- Frontend (embedded) ---
	fileServer := http.FileServer(http.FS(s.webFS))
	mux.Handle("/", fileHandler{fs: s.webFS, h: fileServer})

	return loggingMiddleware(corsMiddleware(mux))
}

// fileHandler serves embedded files and falls back to index.html for
// client-side routes (single-page app).
type fileHandler struct {
	fs fs.FS
	h  http.Handler
}

func (f fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		f.h.ServeHTTP(w, r)
		return
	}
	// If the requested file does not exist, fall back to index.html (SPA).
	if _, err := fs.Stat(f.fs, p); err != nil {
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		f.h.ServeHTTP(w, r2)
		return
	}
	f.h.ServeHTTP(w, r)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
