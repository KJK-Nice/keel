//go:build !darwin

package desktop

import "testing"

func TestEnsureMacAppNoop(t *testing.T) {
	if err := EnsureMacApp(); err != nil {
		t.Fatal(err)
	}
}
