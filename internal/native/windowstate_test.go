package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestWindowStateStore(t *testing.T) {
	dir := t.TempDir()
	s := newWindowStateStore(dir)
	if m, _ := s.LoadWindowStates(); len(m) != 0 {
		t.Fatalf("no file: %v", m)
	}
	in := map[string]mygo.WindowState{
		"main": {X: 40, Y: 60, Width: 900, Height: 700, FullScreen: true},
		"tiny": {Width: 10, Height: 10},
	}
	if err := s.SaveWindowStates(in); err != nil {
		t.Fatal(err)
	}
	m, _ := s.LoadWindowStates()
	if len(m) != 1 || m["main"].X != 40 || m["main"].Width != 900 || m["main"].FullScreen {
		t.Fatalf("restored %+v", m)
	}
	if err := os.WriteFile(filepath.Join(dir, "window-state.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.LoadWindowStates(); len(m) != 0 {
		t.Fatalf("bad file: %v", m)
	}
}
