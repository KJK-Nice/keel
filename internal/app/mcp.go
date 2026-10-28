package app

import (
	"context"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	"keel/internal/buildid"
	"keel/internal/mcp"
)

// RunMCP runs keel mcp: an MCP server on stdin and stdout for Claude Code
// (claude mcp add keel -- keel mcp). It talks to the keel serve on addr and
// never starts one: the vault must be unlocked in the app first. It ends
// when stdin closes.
func RunMCP(web fs.FS, addr, dataDir string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := mcp.New(mcp.NewClient(addr, dataDir), buildid.Of(web))
	return srv.Serve(ctx, os.Stdin, os.Stdout)
}
