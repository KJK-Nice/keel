package session

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"slime/internal/session/sshtest"
	"slime/internal/store"
)

func TestRemotePath(t *testing.T) {
	for _, c := range []struct{ name, dir, want string }{
		{"/etc/hosts", "", "/etc/hosts"},
		{"~/a.txt", "", "~/a.txt"},
		{"out/1.png", "/srv/app", "/srv/app/out/1.png"},
		{"../x", "/srv/app", "/srv/x"},
	} {
		if got, err := RemotePath(c.name, c.dir); err != nil || got != c.want {
			t.Errorf("RemotePath(%q, %q) = %q, %v; want %q", c.name, c.dir, got, err, c.want)
		}
	}
	for _, name := range []string{"", "rel.txt", "a\x00b"} {
		if _, err := RemotePath(name, ""); err == nil {
			t.Errorf("RemotePath(%q, \"\") is accepted", name)
		}
	}
}

func TestCatCommandQuotes(t *testing.T) {
	for in, want := range map[string]string{
		"/tmp/a b":     `cat -- '/tmp/a b'`,
		"/tmp/it's":    `cat -- '/tmp/it'\''s'`,
		"/tmp/$(id)":   `cat -- '/tmp/$(id)'`,
		"~/notes/x.md": `cat -- "$HOME"/'notes/x.md'`,
	} {
		if got := catCommand(in); got != want {
			t.Errorf("catCommand(%q) = %s; want %s", in, got, want)
		}
	}
}

func TestSSHReadFile(t *testing.T) {
	srv := sshtest.New(t, sshtest.Options{Exec: func(cmd string) ([]byte, string, int) {
		switch cmd {
		case `cat -- '/srv/app/1.png'`:
			return []byte("PNGDATA"), "", 0
		case `cat -- "$HOME"/'big'`:
			return bytes.Repeat([]byte("x"), 11), "", 0
		}
		return nil, "cat: " + strings.TrimPrefix(cmd, "cat -- ") + ": No such file or directory\n", 1
	}})
	br, err := openSSH(store.Host{Address: srv.Host, Port: srv.Port, Username: srv.User, AuthType: "password", Password: srv.Password},
		80, 24, func(string, string, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer br.Close()
	fr := br.(FileReader)
	if got, err := fr.ReadFile("/srv/app/1.png", 10); err != nil || string(got) != "PNGDATA" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	if _, err := fr.ReadFile("~/big", 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := fr.ReadFile("/nope", 10); err == nil || err.Error() != "'/nope': No such file or directory" {
		t.Fatalf("missing file: %v", err)
	}
}
