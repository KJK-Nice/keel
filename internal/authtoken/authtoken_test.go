package authtoken

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"testing"

	"keel/internal/paths"
)

func TestLoadOrCreatePersists(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read before create = %v, want ErrNotExist", err)
	}
	a, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 {
		t.Fatalf("token %q: want 64 hex chars", a)
	}
	b, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("second LoadOrCreate made a new token")
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(paths.TokenFile(dir))
		if err != nil {
			t.Fatal(err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Fatalf("token file mode %o, want 600", perm)
		}
	}
}

func TestLoadOrCreateConcurrentCallersAgree(t *testing.T) {
	dir := t.TempDir()
	const n = 16
	toks := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			toks[i], errs[i] = LoadOrCreate(dir)
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if toks[i] != toks[0] {
			t.Fatalf("callers got different tokens: %q vs %q", toks[i], toks[0])
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("data dir has %d entries, want only the token file", len(entries))
	}
}

func TestReadRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(paths.TokenFile(dir), []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("malformed token accepted")
	}
}

func TestEqual(t *testing.T) {
	if Equal("", "") {
		t.Fatal("empty token must never match")
	}
	if !Equal("abc", "abc") || Equal("abd", "abc") || Equal("ab", "abc") {
		t.Fatal("Equal mismatch")
	}
}

func TestProof(t *testing.T) {
	nonce, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	p := Proof("tok", nonce)
	if !CheckProof("tok", nonce, p) {
		t.Fatal("proof rejected")
	}
	if CheckProof("other", nonce, p) || CheckProof("tok", nonce+"x", p) || CheckProof("tok", nonce, "") {
		t.Fatal("bad proof accepted")
	}
}
