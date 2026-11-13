package service

import (
	"errors"
	"testing"
)

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
