package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"keel/internal/daemon"
	"keel/internal/httpapi"
	"keel/internal/runtime"
	"keel/internal/store"
	"keel/internal/tui"
)

//go:embed all:web
var embeddedWeb embed.FS

func main() {
	cmd := "tui"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	addr := env("KEEL_ADDR", "0.0.0.0:47241")
	dataDir := env("KEEL_DATA", "data")

	switch cmd {
	case "server":
		runServer(addr, dataDir)
	case "tui":
		runTUI(addr, dataDir)
	case "help", "-h", "--help":
		fmt.Print(helpText)
	default:
		log.Fatalf("unknown command %q (try keel, keel tui, or keel server)", cmd)
	}
}

func runServer(addr, dataDir string) {
	st, err := store.New(filepath.Join(dataDir, "keel.json"))
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("web assets: %v", err)
	}
	rt := runtime.New(st, nil)
	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(st, rt, web).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("Keel runtime listening on http://%s", addr)
	log.Printf("TUI clients: keel tui   web: %s", daemon.ClientBase(addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func runTUI(addr, dataDir string) {
	base, err := daemon.Ensure(addr, dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if err := tui.Run(base); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const helpText = `Keel — SSH workspace runtime

The runtime owns PTYs. The TUI and web UI are clients of it.
Detach does not kill sessions.

  keel            attach the TUI (starts the runtime if needed)
  keel tui        same
  keel server     run the runtime in the foreground (API + web UI)

Environment:
  KEEL_ADDR       listen address (default 0.0.0.0:47241)
  KEEL_DATA       data directory (default ./data)

TUI keys:
  enter           attach a session or spawn from a host
  x               kill the selected session
  n               spawn a local shell
  ctrl+b d        detach the TUI (sessions keep running)
  q               quit the TUI
`
