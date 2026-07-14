package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	// Embed the timezone database so the binary is self-contained and respects
	// the TZ environment variable without external zoneinfo files.
	_ "time/tzdata"

	"todo/internal/db"
	"todo/internal/server"
)

//go:embed all:web
var webFS embed.FS

func main() {
	dbPath := getenv("DB_PATH", "./data/todo.db")
	port := getenv("PORT", "8080")

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("failed to open database at %s: %v", dbPath, err)
	}
	defer database.Close()

	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embed web assets: %v", err)
	}

	srv := server.New(database, webSub)
	log.Printf("todo server listening on :%s (db=%s)", port, dbPath)
	if err := srv.ListenAndServe(":" + port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
