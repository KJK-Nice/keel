package native

import (
	"context"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// ⌘-click (Control-click elsewhere) on a file path printed in a pane opens it, as Rex does. MyGo finds the path
// under the pointer (terminal.Options.OpenLink) and opens web links itself. A local pane opens the file where it
// is. A remote pane copies it from the host through slime serve (GET /api/sessions/{id}/file, over the session's
// own SSH connection) into Downloads › Slime › <host>, opens the copy and says so in a toast with Show in Finder.
// Nothing here writes to a PTY.

const fileCopyTimeout = 2 * time.Minute

// Replaced by tests: the desktop's own open and reveal.
var (
	openPath   = mygo.Shell.OpenPath
	revealPath = mygo.Shell.ShowItemInFolder
	downloads  = func() (string, error) {
		home, err := os.UserHomeDir()
		return filepath.Join(home, "Downloads"), err
	}
)

// openLink is terminal.Options.OpenLink for p: true when Slime handled link, false to leave it to MyGo (a web
// link it opens with the browser, or nothing for a path).
func (w *workspace) openLink(p *pane, link string) bool {
	name, ok := linkPath(link)
	if !ok || p.remote || p.nb != nil { // a joined share's files are on another machine Slime cannot reach
		return false
	}
	dir := ""
	if p.dir != nil {
		dir = p.dir()
	}
	if p.host.IsLocal() {
		w.openLocal(name, dir)
		return true
	}
	sid := p.sessionID()
	if sid == "" {
		return false
	}
	host := p.host.Name
	w.work(func() {
		c, cancel := context.WithTimeout(context.Background(), fileCopyTimeout)
		defer cancel()
		data, from, err := w.be.ReadSessionFile(c, sid, name)
		var saved string
		if err == nil {
			saved, err = saveCopy(host, from, data)
		}
		w.post(func() {
			if w.closed {
				return
			}
			if err != nil {
				w.warn(errText("Could not copy "+name, err))
				return
			}
			if err := openPath(saved); err != nil {
				w.warn(errText("Could not open "+filepath.Base(saved), err))
				return
			}
			w.sayDetail("Opened a copy of “"+filepath.Base(saved)+"”", "In Downloads › Slime › "+filepath.Base(filepath.Dir(saved)),
				revealLabel(), func() { revealPath(saved) }, toastTimeout)
		})
	})
	return true
}

// linkPath is the file a link names: a path as printed, or a file:// URL's path. ok is false for a web link.
func linkPath(link string) (string, bool) {
	if strings.HasPrefix(link, "file://") {
		u, err := url.Parse(link)
		if err != nil || u.Path == "" {
			return "", false
		}
		return u.Path, true
	}
	if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
		return "", false
	}
	return link, link != ""
}

func (w *workspace) openLocal(name, dir string) {
	if rest, ok := strings.CutPrefix(name, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			name = filepath.Join(home, rest)
		}
	}
	if !filepath.IsAbs(name) {
		if dir == "" {
			w.warn("The shell does not report its folder (OSC 7), so " + name + " cannot be found")
			return
		}
		name = filepath.Join(dir, name)
	}
	if _, err := os.Stat(name); err != nil {
		w.warn("No file at " + name)
		return
	}
	if err := openPath(name); err != nil {
		w.warn(errText("Could not open "+filepath.Base(name), err))
	}
}

// saveCopy writes data, the file at remote on host, to Downloads/Slime/<host>/<its name>, replacing an earlier
// copy of the same name, and returns where.
func saveCopy(host, remote string, data []byte) (string, error) {
	root, err := downloads()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "Slime", safeName(host, "host"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, safeName(path.Base(remote), "file"))
	return dst, os.WriteFile(dst, data, 0o644)
}

// safeName is s as one path element: no separators, not hidden and not . or .., or fallback when nothing is left.
func safeName(s, fallback string) string {
	s = strings.TrimLeft(strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 || r == ':' {
			return '_'
		}
		return r
	}, s), ".")
	if s == "" {
		return fallback
	}
	return s
}

func revealLabel() string {
	if isMac() {
		return "Show in Finder"
	}
	return "Show in Folder"
}
