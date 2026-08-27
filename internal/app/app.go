package app

import (
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"keel/internal/desktop"
	"keel/internal/httpapi"
	"keel/internal/runtime"
	"keel/internal/share"
	"keel/internal/store"
)

func RunDesktop(web fs.FS, addr, dataDir, name string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	st, err := store.New(filepath.Join(dataDir, "keel.json"))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	rt := runtime.New(st, nil)
	hub := share.FromEnv(rt)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(st, rt, web, hub).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("Keel could not bind %s (is it already running?): %w", addr, err)
	}
	base := "http://" + ln.Addr().String()
	log.Printf("Keel app on %s — close the window to quit and kill every session", base)
	if hub.Enabled() {
		log.Printf("Internet share via %s", os.Getenv("KEEL_SIGNAL"))
	}

	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()

	u, err := url.Parse(base + "/")
	if err != nil {
		_ = httpSrv.Close()
		return err
	}
	q := u.Query()
	if name != "" {
		q.Set("name", name)
	}
	u.RawQuery = q.Encode()

	profile := filepath.Join(dataDir, "window-profile")
	winErr := desktop.Run(u.String(), profile)

	rt.KillAll()
	_ = httpSrv.Close()
	return winErr
}
