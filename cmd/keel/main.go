package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"keel/internal/httpapi"
	"keel/internal/store"
)

//go:embed all:web
var embeddedWeb embed.FS

func main() {
	addr := env("KEEL_ADDR", "0.0.0.0:47241")
	dataDir := env("KEEL_DATA", "data")

	st, err := store.New(filepath.Join(dataDir, "keel.json"))
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("web assets: %v", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(st, web).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("Keel SSH workspace listening on http://%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
