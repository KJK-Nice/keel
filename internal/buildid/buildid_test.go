package buildid

import (
	"os"
	"path/filepath"
	"testing"
)

func hash(s string) func() string { return func() string { return s } }

func TestACleanRevisionNamesTheBuildAlone(t *testing.T) {
	if got := compose("0123456789ab", false, hash("aaaa")); got != "0123456789ab" {
		t.Fatalf("id %q", got)
	}
	// The binary's hash is not read at all: a re-signed bundle of the same revision keeps its id.
	if got := compose("0123456789ab", false, func() string { t.Fatal("hashed a clean build"); return "" }); got == "" {
		t.Fatal("empty id")
	}
}

func TestADirtyOrRevisionlessBuildAddsTheBinaryHash(t *testing.T) {
	a := compose("0123456789ab", true, hash("aaaa"))
	b := compose("0123456789ab", true, hash("bbbb"))
	if a == b {
		t.Fatal("two dirty builds of one revision must differ")
	}
	if a != "0123456789ab+dirty-aaaa" {
		t.Fatalf("id %q", a)
	}
	if compose("", false, hash("aaaa")) == compose("", false, hash("bbbb")) {
		t.Fatal("two revision-less builds must differ")
	}
	if got := compose("", false, hash("aaaa")); got != "dev-aaaa" {
		t.Fatalf("id %q", got)
	}
	if compose("", false, hash("aaaa")) != compose("", false, hash("aaaa")) {
		t.Fatal("the same binary must give the same id")
	}
}

func TestHashFileFollowsTheBytes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, err := hashFile(write("a", "one"))
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := hashFile(write("a2", "one"))
	b, _ := hashFile(write("b", "two"))
	if a != a2 || a == b || len(a) != 12 {
		t.Fatalf("hashes %q %q %q", a, a2, b)
	}
	if _, err := hashFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file must be an error")
	}
}

// The running test binary is hashed once and the id is stable inside the process: the app and its serve are two
// processes of one executable and compute the same value.
func TestOfIsStableAndNamesTheRunningBinary(t *testing.T) {
	if Of() != Of() {
		t.Fatal("two calls differ")
	}
	if binaryHash() == "unknown" {
		t.Fatal("the test binary could not be hashed")
	}
}
