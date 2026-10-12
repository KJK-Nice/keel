//go:build desktop

package app

import (
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"keel/internal/authtoken"
	"keel/internal/desktop"
)

func RunDesktop(_ fs.FS, addr, dataDir, name string) error {
	// Re-exec into a signed .app before opening WebKit. WKWebView on
	// Apple Silicon SIGTRAPs a naked Go binary (no bundle, no JIT entitlements).
	if err := desktop.EnsureMacApp(); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	token, err := authtoken.LoadOrCreate(dataDir)
	if err != nil {
		return err
	}
	if err := EnsureServe(addr, dataDir); err != nil {
		return err
	}
	if err := VerifyServe(addr, token); err != nil {
		return err
	}
	base := "http://" + addr
	log.Printf("Keel app on %s — close the window to detach; sessions stay in keel serve", base)

	u, err := url.Parse(base + "/")
	if err != nil {
		return err
	}
	q := u.Query()
	if name != "" {
		q.Set("name", name)
	}
	// Traded for an HttpOnly cookie on first load; the server redirects to
	// the same URL without it.
	q.Set("keel_token", token)
	u.RawQuery = q.Encode()

	profile := filepath.Join(dataDir, "window-profile")
	return desktop.Run(u.String(), profile)
}
