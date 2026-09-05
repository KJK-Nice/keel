package scrollback

import (
	"strings"
	"testing"
)

func TestReadRedactsSecretsByDefault(t *testing.T) {
	raw := strings.Join([]string{
		"login ok",
		"password=super-secret-token",
		"Authorization: Bearer eyJhbGciOi.secret",
		"AKIAIOSFODNN7EXAMPLE",
		"AGE-SECRET-KEY-1ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"abc123secretkeybytes",
		"-----END OPENSSH PRIVATE KEY-----",
		"done",
	}, "\n")
	got := string(Read([]byte(raw)))
	for _, leak := range []string{
		"super-secret-token",
		"eyJhbGciOi.secret",
		"AKIAIOSFODNN7EXAMPLE",
		"AGE-SECRET-KEY-1ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"abc123secretkeybytes",
		"BEGIN OPENSSH PRIVATE KEY",
	} {
		if strings.Contains(got, leak) {
			t.Fatalf("read leaked %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "login ok") || !strings.Contains(got, "done") {
		t.Fatalf("innocent text disappeared: %s", got)
	}
	if strings.Count(got, Mask) < 5 {
		t.Fatalf("expected multiple redactions, got %q", got)
	}
}

func TestReadLeavesPlainTextAlone(t *testing.T) {
	in := []byte("echo hello\nls -la\n")
	got := string(Read(in))
	if got != string(in) {
		t.Fatalf("got %q", got)
	}
}

func TestReadCopiesInput(t *testing.T) {
	in := []byte("password=hunter2")
	got := Read(in)
	if strings.Contains(string(in), Mask) {
		t.Fatal("Read mutated the caller buffer")
	}
	if strings.Contains(string(got), "hunter2") {
		t.Fatalf("got %q", got)
	}
}
