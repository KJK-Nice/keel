// Package buildid names a Keel build, so the desktop app can tell when the
// keel serve it connects to came from a different build.
//
// keel serve outlives the window (sessions keep running), and it, not the
// app, serves the UI. After an upgrade the old daemon keeps serving the old
// UI until it is stopped, which looks exactly like a fix that didn't land.
package buildid

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"runtime/debug"
	"sort"
)

// Of returns an id for this binary: the VCS revision it was built from
// (with "+dirty" for a modified tree) and a hash of the embedded web UI.
// The hash catches UI changes that a dirty tree's revision doesn't, and it
// survives the ad-hoc code signing that changes the binary's own bytes.
func Of(web fs.FS) string {
	rev := "dev"
	if info, ok := debug.ReadBuildInfo(); ok {
		var modified bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 12 {
					rev = s.Value[:12]
				} else if s.Value != "" {
					rev = s.Value
				}
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if modified {
			rev += "+dirty"
		}
	}
	return rev + "-" + webHash(web)
}

// webHash hashes every file's path and contents, in path order.
func webHash(web fs.FS) string {
	if web == nil {
		return "noweb"
	}
	var paths []string
	_ = fs.WalkDir(web, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		f, err := web.Open(p)
		if err != nil {
			continue
		}
		_, _ = io.WriteString(h, p+"\x00")
		_, _ = io.Copy(h, f)
		_, _ = io.WriteString(h, "\x00")
		_ = f.Close()
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
