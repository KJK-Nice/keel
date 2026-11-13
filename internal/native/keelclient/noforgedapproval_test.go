package keelclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// docs/ui-redesign.md §6 rule 1: the only grant to write a PTY for the user is the one-time nonce keel serve issues.
// A client flag "approved" in a frame is never a grant (keel serve ignores it: httpapi's TestInjectNonceOneShotRejectsClientApprovedReplay),
// and this client never sends one. This holds for the native client what the webview's test held for pty.js
// ("client must not forge approved:true").
func TestNoFrameOfAnApprovedInjectCarriesAnApprovedFlag(t *testing.T) {
	for _, opts := range [][]InjectOption{nil, {FromNotebook}} {
		c, s := dialInject(t, "ready", "driver", nonceAnswer)
		if err := c.ApprovedInject(context.Background(), "ls\n", nil, opts...); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(s.all()) < 2 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if len(s.all()) != 2 {
			t.Fatalf("frames %v", s.all())
		}
		for _, m := range s.all() {
			for k := range m {
				if strings.EqualFold(k, "approved") {
					t.Errorf("a %v frame carries %q: only keel serve's nonce grants a write", m["type"], k)
				}
			}
		}
	}
}

// No source file of the native client names an "approved" field of a frame.
func TestNoSourceFileNamesAnApprovedFrameField(t *testing.T) {
	for _, dir := range []string{".", ".."} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no files in %s: %v", dir, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), `"approved"`) || strings.Contains(string(b), `approved:`) {
				t.Errorf("%s names an approved frame field: only keel serve's nonce grants a write", f)
			}
		}
	}
}
