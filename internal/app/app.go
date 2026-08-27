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

	"keel/internal/daemon"
	"keel/internal/desktop"
	"keel/internal/httpapi"
	"keel/internal/runtime"
	"keel/internal/store"
)

func RunServer(web fs.FS, addr, dataDir, token string) error {
	_, srv, err := start(web, addr, dataDir, token)
	if err != nil {
		return err
	}
	log.Printf("Keel runtime listening on http://%s", addr)
	if token != "" {
		log.Printf("Share token is set — clients must pass KEEL_TOKEN or ?token=")
	}
	log.Printf("TUI clients: keel tui   web: %s", daemon.ClientBase(addr))
	err = srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func RunDesktop(web fs.FS, addr, dataDir, token, name string) error {
	rt, srv, err := start(web, addr, dataDir, token)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("Keel could not bind %s (is it already running?): %w", addr, err)
	}
	base := "http://" + ln.Addr().String()
	log.Printf("Keel app on %s — close the window to quit and kill every session", base)

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()

	u, err := url.Parse(base + "/")
	if err != nil {
		_ = srv.Close()
		return err
	}
	q := u.Query()
	if name != "" {
		q.Set("name", name)
	}
	if token != "" {
		q.Set("token", token)
	}
	u.RawQuery = q.Encode()

	profile := filepath.Join(dataDir, "window-profile")
	winErr := desktop.Run(u.String(), profile)

	rt.KillAll()
	_ = srv.Close()
	return winErr
}

func start(web fs.FS, addr, dataDir, token string) (*runtime.Runtime, *http.Server, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, err
	}
	st, err := store.New(filepath.Join(dataDir, "keel.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("store: %w", err)
	}
	rt := runtime.New(st, nil)
	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(st, rt, web, token).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return rt, srv, nil
}
