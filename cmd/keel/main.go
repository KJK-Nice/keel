package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	args := os.Args[1:]
	cmd := "tui"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	addr := env("KEEL_ADDR", "0.0.0.0:47241")
	dataDir := env("KEEL_DATA", "data")
	token := strings.TrimSpace(os.Getenv("KEEL_TOKEN"))

	switch cmd {
	case "server":
		runServer(addr, dataDir, token)
	case "tui":
		runTUI(addr, dataDir, token, parseName(args))
	case "help", "-h", "--help":
		fmt.Print(helpText)
	default:
		log.Fatalf("unknown command %q (try keel, keel tui, or keel server)", cmd)
	}
}

func parseName(args []string) string {
	name := env("KEEL_NAME", os.Getenv("USER"))
	for i := 0; i < len(args); i++ {
		switch {
		case (args[i] == "--name" || args[i] == "-n") && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(args[i], "--name="):
			return strings.TrimPrefix(args[i], "--name=")
		}
	}
	if name == "" {
		return "guest"
	}
	return name
}

func runServer(addr, dataDir, token string) {
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
		Handler:           httpapi.New(st, rt, web, token).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("Keel runtime listening on http://%s", addr)
	if token != "" {
		log.Printf("Share token is set — clients must pass KEEL_TOKEN or ?token=")
	}
	log.Printf("TUI clients: keel tui   web: %s", daemon.ClientBase(addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func runTUI(addr, dataDir, token, name string) {
	base, err := daemon.Ensure(addr, dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if err := tui.Run(base, tui.Options{Token: token, Name: name}); err != nil {
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
Detach does not kill sessions. Multiplayer is driver/follow:
one person types, others watch, control can be passed.

  keel            attach the TUI (starts the runtime if needed)
  keel tui        same
  keel tui --name alice
  keel server     run the runtime in the foreground (API + web UI)

Environment:
  KEEL_ADDR       listen address (default 0.0.0.0:47241)
  KEEL_DATA       data directory (default ./data)
  KEEL_TOKEN      optional share token (required on API/WS when set)
  KEEL_NAME       display name for this TUI client
  KEEL_URL        attach to a remote runtime (never starts a local daemon)

TUI keys:
  enter           attach a session or spawn from a host
  x               kill the selected session
  n               spawn a local shell
  ctrl+b d        detach the TUI (sessions keep running)
  ctrl+b t        take control (owner) or request control
  q               quit the TUI
`
