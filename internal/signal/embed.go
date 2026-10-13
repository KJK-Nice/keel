package signal

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:web
var webFS embed.FS

func fsSub() (fs.FS, error) {
	return fs.Sub(webFS, "web")
}

func (s *Server) joinPage(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(webFS, "web/join.html")
	if err != nil {
		http.Error(w, "join page missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

// script serves one of the join page's ES modules from web/.
func (s *Server) script(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(webFS, "web/"+name)
		if err != nil {
			http.Error(w, "script missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(b)
	}
}
