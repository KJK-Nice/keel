package native

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// windowStateStore keeps the main window's frame in window-state.json beside keel-native.json (the data directory,
// so a test's scratch directory holds its own). MyGo does the rest: it saves the normal bounds when the window closes
// or the app quits, and a saved frame that would show on no connected display is ignored, so the window gets its
// default. Full screen is not restored as a frame: the entry keeps the bounds only. Nothing here touches a session.
type windowStateStore struct{ path string }

func newWindowStateStore(dataDir string) windowStateStore {
	return windowStateStore{filepath.Join(dataDir, "window-state.json")}
}

// LoadWindowStates drops an entry with no usable size and the full screen flag; an unreadable file is no states.
func (s windowStateStore) LoadWindowStates() (map[string]mygo.WindowState, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil, nil
	}
	var m map[string]mygo.WindowState
	if json.Unmarshal(b, &m) != nil {
		return nil, nil
	}
	for k, st := range m {
		if st.Width < 200 || st.Height < 150 {
			delete(m, k)
			continue
		}
		st.FullScreen = false
		m[k] = st
	}
	return m, nil
}

func (s windowStateStore) SaveWindowStates(m map[string]mygo.WindowState) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}
