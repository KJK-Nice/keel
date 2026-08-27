package session

import (
	"os"
	"testing"
)

func TestParseSize(t *testing.T) {
	cols, rows := ParseSize("120", "40")
	if cols != 120 || rows != 40 {
		t.Fatalf("got %d x %d", cols, rows)
	}
	cols, rows = ParseSize("", "")
	if cols != 80 || rows != 24 {
		t.Fatalf("defaults: %d x %d", cols, rows)
	}
}

func TestLocalWorkingDirIsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	if got := localWorkingDir(); got != home {
		t.Fatalf("local shell dir %q, want home %q", got, home)
	}
}
