package env

import "testing"

func TestGetPrefersSlimeAndFallsBackToKeel(t *testing.T) {
	t.Setenv("SLIME_X_TEST", "")
	t.Setenv("KEEL_X_TEST", "")
	if Get("X_TEST") != "" {
		t.Fatal("neither set: want empty")
	}
	t.Setenv("KEEL_X_TEST", "old")
	if got := Get("X_TEST"); got != "old" {
		t.Fatalf("only KEEL_ set: got %q, want old", got)
	}
	t.Setenv("SLIME_X_TEST", "new")
	if got := Get("X_TEST"); got != "new" {
		t.Fatalf("both set: got %q, want new", got)
	}
}
