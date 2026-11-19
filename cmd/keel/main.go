package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"keel/internal/app"
	"keel/internal/env"
	"keel/internal/paths"
)

func init() {
	// AppKit must run on thread 0. Lock before any other package
	// hops the main goroutine off the main OS thread.
	runtime.LockOSThread()
}

// version is the release this binary was built as, set by the release
// build: make build SLIME_VERSION=vX.Y.Z (-X main.version).
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

	switch cmd {
	case "app":
		// The window's display name for sharing (Settings · Sharing's own name comes first).
		if err := app.RunNative(addr, dataDir, parseName(args)); err != nil {
			log.Fatal(err)
		}
	case "serve":
		if len(args) > 0 && args[0] == "stop" {
			if err := app.StopServe(addr, dataDir); err != nil {
				log.Fatal(err)
			}
			return
		}
		if err := app.RunServe(addr, dataDir); err != nil {
			log.Fatal(err)
		}
	case "mcp":
		// stdout carries MCP messages only: logs go to stderr.
		log.SetOutput(os.Stderr)
		if err := app.RunMCP(addr, dataDir); err != nil {
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
	name := env.Get("NAME")
	if name == "" {
		name = paths.LocalUser()
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

const helpText = `Slime — local SSH workspace

keel opens a desktop window (native, MyGo + libghostty). Closing the window
detaches; PTYs keep running in a background keel serve process. Closing a
session tab kills that PTY.

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
  Settings · Sharing · Signal server: its URL (saved; host copies a join link from Share)
  SLIME_SIGNAL=https://… keel  # or for one run (wins over the saved server)
  make build SLIME_SIGNAL=https://…   or bake a default into the build

  Host: Share. Other Slime: Join session, paste the link.
  Two apps on one Mac: SLIME_ADDR=127.0.0.1:47243 SLIME_DATA=/tmp/keel-join ./bin/keel

Environment:
  SLIME_ADDR       local bind of keel serve (default 127.0.0.1:47241)
  SLIME_DATA       data directory (default: OS config dir / keel)
  SLIME_NAME       display name (keel app --name sets it for the run)
  SLIME_SIGNAL     public URL of keel-signal for this run (overrides Settings · Sharing)
  SLIME_VAULT_PASSPHRASE  unlock/create the vault when the OS keychain is unavailable
  (each also works under its old name, KEEL_*)

The window is native (MyGo, pure Go; libghostty-vt terminals, fetched
and SHA-256 checked on first run, or found beside the binary). Build:

  make build
  make build SLIME_SIGNAL=https://your-signal.example

make build also packages the app with mygo build (build/darwin-*/Slime.app on macOS) — open that, or run ./bin/keel.
`
