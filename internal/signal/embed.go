package signal

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var webFS embed.FS

func (s *Server) joinPage(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(webFS, "web/join.html")
	if err != nil {
		http.Error(w, "join page missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) joinJS(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(webFS, "web/join.js")
	if err != nil {
		http.Error(w, "join script missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write(b)
}
