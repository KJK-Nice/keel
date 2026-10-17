package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// staticHandler serves the web UI's files with a content-hash ETag and
// "Cache-Control: no-cache", gzipped once when that pays.
//
// The files used to go out "no-store": every window open re-downloaded and
// re-parsed ~1.5 MB of uncompressed JS and CSS (xterm, the WebGL addon, the
// notebook editor, term.js and its modules), and WebKit could not keep its
// bytecode cache. no-cache still revalidates on every load, so a changed
// file is never served stale (the reason for no-store); an unchanged one
// costs a 304.
//
// Entries are keyed by path, size and modification time. The embedded FS
// has fixed times, so each file is read and hashed once; an os.DirFS in
// development picks up edits on the next request.
type staticHandler struct {
	fsys     fs.FS
	fallback http.Handler

	mu    sync.Mutex
	files map[string]*staticFile
}

type staticFile struct {
	size    int64
	modTime time.Time
	etag    string
	ctype   string
	raw     []byte
	gz      []byte // nil when compression does not pay
}

// staticMaxBytes keeps unusually large files on http.FileServer (ranges,
// streaming) instead of in memory.
const staticMaxBytes = 4 << 20

func newStaticHandler(fsys fs.FS) *staticHandler {
	return &staticHandler{
		fsys:     fsys,
		fallback: http.FileServer(http.FS(fsys)),
		files:    map[string]*staticFile{},
	}
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.fallback.ServeHTTP(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	f := h.file(name)
	if f == nil {
		h.fallback.ServeHTTP(w, r)
		return
	}
	hdr := w.Header()
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("ETag", f.etag)
	if f.ctype != "" {
		hdr.Set("Content-Type", f.ctype)
	}
	if f.gz != nil {
		hdr.Add("Vary", "Accept-Encoding")
	}
	if etagMatch(r.Header.Get("If-None-Match"), f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := f.raw
	if f.gz != nil && acceptsGzip(r) {
		hdr.Set("Content-Encoding", "gzip")
		body = f.gz
	}
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// file returns the cached entry for name, (re)loading it when the file's
// size or time changed. It returns nil for directories, missing files and
// anything http.FileServer should handle itself.
func (h *staticHandler) file(name string) *staticFile {
	if name == "" || strings.HasSuffix(name, "/") {
		return nil
	}
	st, err := fs.Stat(h.fsys, name)
	if err != nil || st.IsDir() || st.Size() > staticMaxBytes {
		return nil
	}
	h.mu.Lock()
	f := h.files[name]
	h.mu.Unlock()
	if f != nil && f.size == st.Size() && f.modTime.Equal(st.ModTime()) {
		return f
	}
	raw, err := fs.ReadFile(h.fsys, name)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(raw)
	f = &staticFile{
		size:    st.Size(),
		modTime: st.ModTime(),
		etag:    `"` + hex.EncodeToString(sum[:12]) + `"`,
		ctype:   mime.TypeByExtension(path.Ext(name)),
		raw:     raw,
	}
	if f.ctype == "" {
		f.ctype = http.DetectContentType(raw)
	}
	if compressible(f.ctype) && len(raw) >= 1024 {
		if gz := gzipBytes(raw); len(gz) < len(raw)*9/10 {
			f.gz = gz
		}
	}
	h.mu.Lock()
	h.files[name] = f
	h.mu.Unlock()
	return f
}

func compressible(ctype string) bool {
	ct, _, _ := strings.Cut(ctype, ";")
	ct = strings.TrimSpace(ct)
	switch {
	case strings.HasPrefix(ct, "text/"):
		return true
	case ct == "application/javascript", ct == "application/json",
		ct == "image/svg+xml", ct == "application/wasm":
		return true
	}
	return false
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(enc), "gzip") {
			continue
		}
		q := strings.TrimSpace(params)
		if v, ok := strings.CutPrefix(q, "q="); ok {
			f, err := strconv.ParseFloat(v, 64)
			return err == nil && f > 0
		}
		return true
	}
	return false
}

// etagMatch reports whether an If-None-Match header names etag (or "*").
func etagMatch(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "W/")
		if t == "*" || t == etag {
			return true
		}
	}
	return false
}
