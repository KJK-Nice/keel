package applog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecentNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keel.log")
	b := New(path)
	b.Append(Entry{Kind: "a", Message: "one"})
	b.Append(Entry{Kind: "b", Message: "two"})
	got := b.Recent(10)
	if len(got) != 2 || got[0].Message != "two" || got[1].Message != "one" {
		t.Fatalf("got %+v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("expected log file")
	}
	b2 := New(path)
	reload := b2.Recent(1)
	if len(reload) != 1 || reload[0].Message != "two" {
		t.Fatalf("reload %+v", reload)
	}
}
