package runtime

import (
	"errors"
	"testing"
	"time"

	"slime/internal/session"
	"slime/internal/store"
)

// fileBridge is a remote bridge that reads files: it records the path asked for.
type fileBridge struct {
	*fakeBridge
	asked chan string
}

func (f fileBridge) ReadFile(name string, max int64) ([]byte, error) {
	f.asked <- name
	return []byte("data"), nil
}

func TestReadFileResolvesAgainstTheShellsFolder(t *testing.T) {
	st := testStore(t)
	h, err := st.UpsertHost(store.Host{Name: "web", Address: "10.0.0.5", Port: 22, Username: "u", AuthType: "password", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	fb := fileBridge{newFakeBridge(), make(chan string, 4)}
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) { return fb, nil })
	info, err := rt.Spawn(h.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := rt.Get(info.ID)
	waitReady(t, s)

	if _, _, err := s.ReadFile("out/1.png"); err == nil {
		t.Fatal("a relative path was read before the shell reported its folder")
	}
	if _, err := fb.outW.Write([]byte("\x1b]7;file://web/srv/my%20app\x07")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.marksCwd() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	data, p, err := s.ReadFile("out/1.png")
	if err != nil || string(data) != "data" || p != "/srv/my app/out/1.png" || <-fb.asked != p {
		t.Fatalf("ReadFile = %q %q %v", data, p, err)
	}
}

// A bridge that cannot read files (a local shell) refuses.
func TestReadFileRefusesWithoutAFileReader(t *testing.T) {
	_, s, _ := spawnReady(t)
	if _, _, err := s.ReadFile("/etc/hosts"); !errors.Is(err, ErrNoRemoteFile) {
		t.Fatalf("err = %v", err)
	}
}

func (s *Session) marksCwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.marks.Cwd()
}
