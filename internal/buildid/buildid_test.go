package buildid

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestOfChangesWithTheUI(t *testing.T) {
	a := fstest.MapFS{"js/term.js": {Data: []byte("v1")}, "index.html": {Data: []byte("x")}}
	b := fstest.MapFS{"js/term.js": {Data: []byte("v2")}, "index.html": {Data: []byte("x")}}
	renamed := fstest.MapFS{"js/other.js": {Data: []byte("v1")}, "index.html": {Data: []byte("x")}}
	same := fstest.MapFS{"index.html": {Data: []byte("x")}, "js/term.js": {Data: []byte("v1")}}
	if Of(a) != Of(same) {
		t.Fatal("same UI, different ids")
	}
	if Of(a) == Of(b) {
		t.Fatal("a changed file must change the id")
	}
	if Of(a) == Of(renamed) {
		t.Fatal("a renamed file must change the id")
	}
	if id := Of(a); !strings.Contains(id, "-") || len(id) < 14 {
		t.Fatalf("id %q: want <revision>-<web hash>", id)
	}
}
