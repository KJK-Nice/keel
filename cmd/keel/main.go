package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"keel/internal/app"
	"keel/internal/daemon"
	"keel/internal/paths"
	"keel/internal/tui"
)

//go:embed all:web
var embeddedWeb embed.FS

func main() {
	args := os.Args[1:]
	cmd := "app"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	addr := paths.Addr()
	dataDir := paths.DataDir()
	token := strings.TrimSpace(os.Getenv("KEEL_TOKEN"))
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("web assets: %v", err)
	}

	switch cmd {
	case "app":
		if err := app.RunDesktop(web, addr, dataDir, token, parseName(args)); err != nil {
			log.Fatal(err)
		}
	case "server":
		if err := app.RunServer(web, addr, dataDir, token); err != nil {
			log.Fatal(err)
		}
	case "tui":
		runTUI(addr, dataDir, token, parseName(args))
	case "help", "-h", "--help":
		fmt.Print(helpText)
	default:
		log.Fatalf("unknown command %q (try keel, keel tui, or keel server)", cmd)
	}
}

func parseName(args []string) string {
	name := os.Getenv("KEEL_NAME")
	if name == "" {
		name = os.Getenv("USER")
	}
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

func runTUI(addr, dataDir, token, name string) {
	base, err := daemon.Ensure(addr, dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if err := tui.Run(base, tui.Options{Token: token, Name: name}); err != nil {
		log.Fatal(err)
	}
}

const helpText = `Keel — local SSH workspace

keel opens a desktop window. Closing it kills the runtime and every PTY.

  keel            open the desktop app (default)
  keel app        same
  keel tui        attach the TUI (starts a runtime if needed)
  keel tui --name alice
  keel server     headless runtime (API + web UI, no window)

Environment:
  KEEL_ADDR       listen address (default 127.0.0.1:47241)
  KEEL_DATA       data directory (default: OS config dir / keel)
  KEEL_TOKEN      optional share token (required on API/WS when set)
  KEEL_NAME       display name
  KEEL_URL        attach TUI to a remote runtime (never starts a local daemon)

The desktop window is a native WebKit/WebView2 webview (Wails). Build:

  make build

or:

  go build -tags desktop,production -o keel ./cmd/keel
  go build -tags desktop,production,webkit2_41 -o keel ./cmd/keel   # Ubuntu 24.04+

Linux packages: libgtk-3-dev libwebkit2gtk-4.1-dev
macOS: Xcode Command Line Tools (UniformTypeIdentifiers is linked automatically)


TUI keys:
  enter           attach a session or spawn from a host
  x               kill the selected session
  n               spawn a local shell
  ctrl+b d        detach the TUI (sessions keep running)
  ctrl+b t        take control (owner) or request control
  q               quit the TUI
`
