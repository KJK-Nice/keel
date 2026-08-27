package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"keel/internal/app"
	"keel/internal/paths"
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
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("web assets: %v", err)
	}

	switch cmd {
	case "app":
		if err := app.RunDesktop(web, addr, dataDir, parseName(args)); err != nil {
			log.Fatal(err)
		}
	case "help", "-h", "--help":
		fmt.Print(helpText)
	default:
		log.Fatalf("unknown command %q (try keel or keel app — TUI/server are gone; internet join uses keel-signal)", cmd)
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

keel opens a desktop window. Closing it kills the runtime and every PTY.

  keel            open the desktop app (default)
  keel app        same

Internet follow (no LAN) needs a public signaling server:

  keel-signal                 # listen :47242
  KEEL_SIGNAL=https://… keel  # host copies a join link from Share

Environment:
  KEEL_ADDR       local webview bind (default 127.0.0.1:47241)
  KEEL_DATA       data directory (default: OS config dir / keel)
  KEEL_NAME       display name
  KEEL_SIGNAL     public URL of keel-signal
  KEEL_VAULT_PASSPHRASE  unlock/create the vault when the OS keychain is unavailable

The desktop window is a native WebKit/WebView2 webview (Wails). Build:

  make build

Linux packages: libgtk-3-dev libwebkit2gtk-4.1-dev
macOS: Xcode Command Line Tools (use make / scripts/go.sh so Wails CGO flags are set)
`
