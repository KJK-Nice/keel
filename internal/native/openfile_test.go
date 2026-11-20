package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"slime/internal/native/slimeclient"
)

func (f *fakeBackend) ReadSessionFile(_ context.Context, sid, name string) ([]byte, string, error) {
	f.rec("read file " + sid + " " + name)
	if name == "missing.txt" {
		return nil, "", errors.New("missing.txt: No such file or directory")
	}
	return []byte("PNG"), "/srv/app/" + name, nil
}

func fakeDesktop(t *testing.T) (dl string, opened, revealed *[]string) {
	dl = t.TempDir()
	opened, revealed = new([]string), new([]string)
	oo, or, od := openPath, revealPath, downloads
	openPath = func(p string) error { *opened = append(*opened, p); return nil }
	revealPath = func(p string) { *revealed = append(*revealed, p) }
	downloads = func() (string, error) { return dl, nil }
	t.Cleanup(func() { openPath, revealPath, downloads = oo, or, od })
	return
}

func TestOpenLinkCopiesARemoteFile(t *testing.T) {
	dl, opened, revealed := fakeDesktop(t)
	fb := &fakeBackend{}
	w := newWorkspace(fb)
	w.work, w.post = func(fn func()) { fn() }, func(fn func()) { fn() }
	p := &pane{host: slimeclient.Host{Name: "Mac mini M1", AuthType: "password"}, conn: &fakeConn{id: "s1"}}

	if !w.openLink(p, "1.png") {
		t.Fatal("a path in a remote pane was left to MyGo")
	}
	want := filepath.Join(dl, "Slime", "Mac mini M1", "1.png")
	if b, err := os.ReadFile(want); err != nil || string(b) != "PNG" || len(*opened) != 1 || (*opened)[0] != want {
		t.Fatalf("copy %q %v, opened %v", b, err, *opened)
	}
	if len(w.queued) != 1 || w.queued[0].Title != "Opened a copy of “1.png”" || w.queued[0].Description != "In Downloads › Slime › Mac mini M1" {
		t.Fatalf("toast %+v", w.queued)
	}
	w.queued[0].OnAction()
	if len(*revealed) != 1 || (*revealed)[0] != want {
		t.Fatalf("revealed %v", *revealed)
	}

	w.queued = nil
	w.openLink(p, "missing.txt")
	if len(w.queued) != 1 || w.queued[0].Type != toastError || len(*opened) != 1 {
		t.Fatalf("a failed copy: %+v, opened %v", w.queued, *opened)
	}
}

func TestOpenLinkLeavesWebLinksAndShares(t *testing.T) {
	fakeDesktop(t)
	fb := &fakeBackend{}
	w := newWorkspace(fb)
	w.work, w.post = func(fn func()) { fn() }, func(fn func()) { fn() }
	remote := &pane{host: slimeclient.Host{Name: "web", AuthType: "password"}, conn: &fakeConn{id: "s1"}}
	for _, link := range []string{"https://example.com/a.png", "mailto:a@b.c"} {
		if w.openLink(remote, link) {
			t.Errorf("%s was taken from MyGo", link)
		}
	}
	shared := &pane{host: remote.host, conn: &fakeConn{id: "s2"}, remote: true}
	if w.openLink(shared, "/etc/hosts") || len(fb.calls) != 0 {
		t.Fatalf("a joined share's path was copied: %v", fb.calls)
	}
}

func TestOpenLinkOpensALocalFileInPlace(t *testing.T) {
	_, opened, _ := fakeDesktop(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBackend{}
	w := newWorkspace(fb)
	p := &pane{host: slimeclient.Host{AuthType: slimeclient.AuthLocal}, conn: &fakeConn{id: "s1"}, dir: func() string { return dir }}
	if !w.openLink(p, "a.txt") || len(*opened) != 1 || (*opened)[0] != filepath.Join(dir, "a.txt") || len(fb.calls) != 0 {
		t.Fatalf("opened %v, calls %v", *opened, fb.calls)
	}
	p.dir = func() string { return "" }
	w.openLink(p, "a.txt")
	if len(w.queued) != 1 || w.queued[0].Type != toastError {
		t.Fatalf("a relative path without a folder: %+v", w.queued)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"../../etc": "_.._etc", "..": "file", ".env": "env", "a/b": "a_b", "": "file"} {
		if got := safeName(in, "file"); got != want {
			t.Errorf("safeName(%q) = %q; want %q", in, got, want)
		}
	}
}
