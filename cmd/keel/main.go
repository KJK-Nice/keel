package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"runtime"
	"strings"

	"keel/internal/app"
	"keel/internal/paths"
)

func init() {
	// AppKit/WKWebView must run on thread 0. Lock before any other package
	// hops the main goroutine off the main OS thread.
	runtime.LockOSThread()
}

//go:embed all:web
var embeddedWeb embed.FS

// version is the release this binary was built as, set by the release
// build: make build VERSION=v0.1.0 (-X main.version).
var version = "dev"

func main() {
	args := os.Args[1:]
	cmd := "app"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	addr := paths.Addr()
	dataDir := paths.DataDir()
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("web assets: %v", err)
	}

	switch cmd {
	case "app":
		if err := app.RunDesktop(web, addr, dataDir, parseName(args)); err != nil {
			log.Fatal(err)
		}
	case "serve":
		if len(args) > 0 && args[0] == "stop" {
			if err := app.StopServe(addr, dataDir); err != nil {
				log.Fatal(err)
			}
			return
		}
		if err := app.RunServe(web, addr, dataDir); err != nil {
			log.Fatal(err)
		}
	case "mcp":
		// stdout carries MCP messages only: logs go to stderr.
		log.SetOutput(os.Stderr)
		if err := app.RunMCP(web, addr, dataDir); err != nil {
			log.Fatal(err)
		}
	case "version", "--version":
		fmt.Println("keel", version)
	case "help", "-h", "--help":
		fmt.Print(helpText)
	default:
		log.Fatalf("unknown command %q (try keel, keel app, keel serve, or keel mcp)", cmd)
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

const helpText = `Keel — local SSH workspace

keel opens a desktop window. Closing the window detaches; PTYs keep running
in a background keel serve process. Closing a session tab kills that PTY.

  keel            open the desktop app (starts serve if needed)
  keel app        same
  keel serve      run the runtime daemon in the foreground
  keel serve stop stop the daemon and kill leftover sessions
  keel version    print the version
  keel mcp        MCP server on stdio for Claude Code: claude mcp add keel -- keel mcp
                  (lists and reads panes the agent may read; proposes commands
                  that wait in the agent panel; never types)

Internet follow (no LAN) needs a public signaling server:

  keel-signal                 # listen :47242
  KEEL_SIGNAL=https://… keel  # host copies a join link from Share
  make build KEEL_SIGNAL=https://…   bake URL into Keel.app (Finder has no shell env)

  Host: Share. Other Keel: Join session, paste the link.
  Two apps on one Mac: KEEL_ADDR=127.0.0.1:47243 KEEL_DATA=/tmp/keel-join ./bin/keel

Environment:
  KEEL_ADDR       local webview bind (default 127.0.0.1:47241)
  KEEL_DATA       data directory (default: OS config dir / keel)
  KEEL_NAME       display name
  KEEL_SIGNAL     public URL of keel-signal (runtime override of the baked default)
  KEEL_VAULT_PASSPHRASE  unlock/create the vault when the OS keychain is unavailable

The desktop window is a native WebKit/WebView2 webview (Wails). Build:

  make build
  make build KEEL_SIGNAL=https://your-signal.example

Linux packages: libgtk-4-dev libwebkitgtk-6.0-dev
macOS: Xcode Command Line Tools (use make / scripts/go.sh so Wails CGO flags are set).
A naked ./bin/keel relaunches as ~/Library/Caches/keel/Keel.app so WebKit JIT is allowed.
make build also writes bin/Keel.app — open that, or run ./bin/keel.
`
