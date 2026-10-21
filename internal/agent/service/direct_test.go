package service

import (
	"errors"
	"testing"
)

// §6 rule 13: only a command that stays one line on a prompt, as typed,
// may go without the approve dialog.
func TestDirectLine(t *testing.T) {
	for _, ok := range []string{"ls -la", "echo 'héllo' | wc -c", "kubectl get pods -n kube-system", "df -h \\"} {
		if !DirectLine(ok) {
			t.Errorf("DirectLine(%q) = false", ok)
		}
	}
	for _, bad := range []string{
		"", "ls\n", "ls\npwd", "ls\r", "ls\ttab", "ls\x1b[A", "ls\x00", "ls\x7f", "\x15ls",
		"ls\u0085", "ls\u009b", "ls\u2028pwd", "ls\u2029", "ls\u200b", "ls\u202e", "ls\ufeff",
		"ls\xff",
	} {
		if DirectLine(bad) {
			t.Errorf("DirectLine(%q) = true", bad)
		}
	}
}

func TestNeedsDialog(t *testing.T) {
	if r, ok := NeedsDialog(needsDialog(ReasonRisk)); !ok || r != ReasonRisk {
		t.Fatalf("NeedsDialog = %q %v", r, ok)
	}
	if _, ok := NeedsDialog(ErrPlaceholder); ok {
		t.Fatal("a refusal is not a dialog")
	}
	if _, ok := NeedsDialog(nil); ok {
		t.Fatal("nil is not a dialog")
	}
	if _, ok := NeedsDialog(errors.Join(errors.New("wrapped"), needsDialog(ReasonLines))); !ok {
		t.Fatal("a wrapped dialog error is still one")
	}
}
