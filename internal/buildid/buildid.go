// Package buildid names a Keel build, so the window can tell when the keel
// serve it connects to came from a different build.
//
// keel serve outlives the window (sessions keep running). After an upgrade
// the old daemon keeps running the old code until it is stopped, which looks
// exactly like a fix that didn't land.
package buildid

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime/debug"
	"sync"
)

// Of returns an id for this binary: the VCS revision it was built from. A
// build from a modified tree, or one with no revision (go run, go test, a
// source archive), adds a hash of the running binary, so two different dev
// builds still differ. keel app and the keel serve it starts run the same
// executable and agree; a keel serve left over from an older binary differs.
func Of() string {
	rev, modified := vcs()
	return compose(rev, modified, binaryHash)
}

// vcs reads the revision (12 characters, "" when the build has none) and whether the tree was modified.
func vcs() (rev string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return rev, modified
}

// compose is Of with its inputs given: a clean revision names the build alone; otherwise the binary's hash joins it.
func compose(rev string, modified bool, binHash func() string) string {
	switch {
	case rev == "":
		return "dev-" + binHash()
	case modified:
		return rev + "+dirty-" + binHash()
	}
	return rev
}

var (
	binOnce sync.Once
	binSum  string
)

// binaryHash is the first 12 hex digits of the SHA-256 of the running executable ("unknown" when it cannot be
// read, which makes every such build differ from every other only by revision). It is computed once.
func binaryHash() string {
	binOnce.Do(func() {
		binSum = "unknown"
		if exe, err := os.Executable(); err == nil {
			if sum, err := hashFile(exe); err == nil {
				binSum = sum
			}
		}
	})
	return binSum
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}
