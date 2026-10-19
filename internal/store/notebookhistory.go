package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Notebook history (docs/ui-redesign.md §18): earlier versions of each
// notebook, so an agent edit can be undone and a deleted notebook
// restored. It lives in keel.notebook-history.json beside keel.json (the
// notebooks themselves are in keel.json, so it is kept the same way), and
// is written only when a version is kept: before every agent edit, when a
// user save replaces the agent's version, at the start of a new editing
// session (a user save replacing a version at least sessionGap old),
// before a restore, and on delete. Never on every keystroke.
//
// Bounds: per notebook the newest maxVersionsPerNotebook versions; nothing
// older than versionTTL; at most maxDeletedNotebooks in Recently deleted;
// and at most maxHistoryBytes of text in all (the oldest go first). The
// file is written under the store lock; it is small and written rarely.

const (
	notebookHistoryFile    = "keel.notebook-history.json"
	maxVersionsPerNotebook = 20
	maxDeletedNotebooks    = 50
	versionTTL             = 30 * 24 * time.Hour
	maxHistoryBytes        = 8 << 20
)

// ErrHistoryUnavailable refuses a change that must keep a version first
// (an agent edit, a delete the agent proposed) when the history cannot be
// read or written.
var ErrHistoryUnavailable = errors.New("the notebook history cannot be read or written, so Keel will not make a change it could not undo")

// NotebookVersion is one kept version of a notebook.
type NotebookVersion struct {
	ID         string         `json:"id"`
	NotebookID string         `json:"notebookId"`
	Name       string         `json:"name"`
	Cells      []NotebookCell `json:"cells,omitempty"`
	Rev        int            `json:"rev"`
	// By made this version: "agent" or "user".
	By string `json:"by"`
	// Event is why it was kept: "before-agent-edit", "agent-version" (the
	// agent's version, kept when the user replaced it), "session" (the
	// version a new editing session started from), "before-restore" or
	// "deleted" (the notebook was deleted; Recently deleted lists these).
	Event string `json:"event"`
	// Summary is the agent's one-line summary of the edit that followed,
	// redacted (before-agent-edit), or "".
	Summary string `json:"summary,omitempty"`
	// At is when this version was saved; KeptAt when it was kept here.
	At     time.Time `json:"at"`
	KeptAt time.Time `json:"keptAt"`
	Bytes  int       `json:"bytes"`
	// What a deleted notebook is restored with.
	Source    string    `json:"source,omitempty"`
	Pinned    bool      `json:"pinned,omitempty"`
	Private   bool      `json:"private,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	// Unreviewed: a deleted notebook held agent text the user had not
	// reviewed; restoring it keeps the mark.
	Unreviewed bool `json:"unreviewed,omitempty"`
}

type nbHistory struct {
	versions []NotebookVersion
	err      error // the file could not be read: nothing is written over it
}

type nbHistoryDisk struct {
	Version  int               `json:"version"`
	Versions []NotebookVersion `json:"versions"`
}

func (s *Store) historyPath() string {
	return filepath.Join(filepath.Dir(s.path), notebookHistoryFile)
}

// historyLocked loads the history on first use, and again on the next use
// while it could not be read (a fixed file is picked up without a
// restart). Call with mu held.
func (s *Store) historyLocked() *nbHistory {
	if s.nbHist != nil && s.nbHist.err == nil {
		return s.nbHist
	}
	h := &nbHistory{}
	b, err := os.ReadFile(s.historyPath())
	switch {
	case err == nil:
		var disk nbHistoryDisk
		if jerr := json.Unmarshal(b, &disk); jerr != nil {
			h.err = fmt.Errorf("parse notebook history: %w", jerr)
		} else {
			h.versions = disk.Versions
		}
	case !os.IsNotExist(err):
		h.err = err
	}
	s.nbHist = h
	return h
}

func versionBytes(nb Notebook) int {
	n := len(nb.Name)
	for _, c := range nb.Cells {
		n += len(c.Body)
	}
	return n
}

// keepVersionStrictLocked keeps nb as a version and writes the history,
// or fails (ErrHistoryUnavailable). Call with mu held.
func (s *Store) keepVersionStrictLocked(nb Notebook, event, summary string, now time.Time) (string, error) {
	h := s.historyLocked()
	if h.err != nil {
		return "", ErrHistoryUnavailable
	}
	prev := h.versions
	v := s.addVersionLocked(h, nb, event, summary, now)
	if err := s.writeHistoryLocked(h); err != nil {
		h.versions = prev
		return "", fmt.Errorf("%w: %v", ErrHistoryUnavailable, err)
	}
	return v.ID, nil
}

// keepVersionLocked keeps nb as a version when it can; a user's own save
// or delete goes on when it cannot. Call with mu held.
func (s *Store) keepVersionLocked(nb Notebook, event, summary string, now time.Time) {
	h := s.historyLocked()
	if h.err != nil {
		return
	}
	prev := h.versions
	s.addVersionLocked(h, nb, event, summary, now)
	if err := s.writeHistoryLocked(h); err != nil {
		h.versions = prev
	}
}

func (s *Store) addVersionLocked(h *nbHistory, nb Notebook, event, summary string, now time.Time) NotebookVersion {
	by := "user"
	if nb.LastBy == NotebookByAgent {
		by = "agent"
	}
	v := NotebookVersion{
		ID: "nbv_" + newID(), NotebookID: nb.ID, Name: nb.Name,
		Cells: append([]NotebookCell(nil), nb.Cells...), Rev: nb.Rev, By: by,
		Event: event, Summary: summary, At: nb.UpdatedAt, KeptAt: now, Bytes: versionBytes(nb),
	}
	if event == "deleted" {
		v.Source, v.Pinned, v.Private, v.CreatedAt, v.Unreviewed = nb.Source, nb.Pinned, nb.Private, nb.CreatedAt, nb.AgentUnreviewed
	}
	// The same text kept twice in a row (an undo of an undo) is one entry.
	if event != "deleted" {
		for i := len(h.versions) - 1; i >= 0; i-- {
			o := h.versions[i]
			if o.NotebookID != nb.ID || o.Event == "deleted" {
				continue
			}
			if o.Rev == v.Rev && o.Name == v.Name && sameCells(o.Cells, v.Cells) {
				return o
			}
			break
		}
	}
	h.versions = append(h.versions, v)
	h.versions = pruneVersions(h.versions, now)
	return v
}

func sameCells(a, b []NotebookCell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Body != b[i].Body {
			return false
		}
	}
	return true
}

// pruneVersions applies the bounds: oldest first goes.
func pruneVersions(vs []NotebookVersion, now time.Time) []NotebookVersion {
	out := make([]NotebookVersion, 0, len(vs))
	for _, v := range vs {
		if now.Sub(v.KeptAt) <= versionTTL {
			out = append(out, v)
		}
	}
	// Newest first. Each notebook keeps maxVersionsPerNotebook of the
	// user's versions and as many of the agent's, so agent edits never
	// push out the user's own; the byte budget goes to the user's
	// versions and deleted notebooks first, then the agent's.
	keep := make([]bool, len(out))
	per := map[string]int{}
	deleted := 0
	total := 0
	for pass := 0; pass < 2; pass++ {
		for i := len(out) - 1; i >= 0; i-- {
			v := out[i]
			agentV := v.Event != "deleted" && v.By == "agent"
			if agentV != (pass == 1) {
				continue
			}
			if v.Event == "deleted" {
				if deleted >= maxDeletedNotebooks {
					continue
				}
				deleted++
			} else {
				key := v.NotebookID + "\x00" + v.By
				if per[key] >= maxVersionsPerNotebook {
					continue
				}
				per[key]++
			}
			if total+v.Bytes > maxHistoryBytes {
				continue
			}
			total += v.Bytes
			keep[i] = true
		}
	}
	res := out[:0]
	for i, v := range out {
		if keep[i] {
			res = append(res, v)
		}
	}
	return res
}

func (s *Store) writeHistoryLocked(h *nbHistory) error {
	b, err := json.Marshal(nbHistoryDisk{Version: 1, Versions: h.versions})
	if err != nil {
		return err
	}
	return writePrivate(s.historyPath(), b)
}

// NotebookHistory lists a notebook's kept versions, newest first, without
// their text (NotebookVersionByID has it). Deleted entries are not listed.
func (s *Store) NotebookHistory(id string) ([]NotebookVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	h := s.historyLocked()
	if h.err != nil {
		return nil, ErrHistoryUnavailable
	}
	out := []NotebookVersion{}
	for i := len(h.versions) - 1; i >= 0; i-- {
		v := h.versions[i]
		if v.NotebookID == id && v.Event != "deleted" {
			v.Cells = nil
			out = append(out, v)
		}
	}
	return out, nil
}

// NotebookVersionByID is one kept version of notebook id, with its text.
func (s *Store) NotebookVersionByID(id, vid string) (NotebookVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return NotebookVersion{}, ErrLocked
	}
	v, _, err := s.versionLocked(id, vid)
	return v, err
}

func (s *Store) versionLocked(id, vid string) (NotebookVersion, int, error) {
	h := s.historyLocked()
	if h.err != nil {
		return NotebookVersion{}, -1, ErrHistoryUnavailable
	}
	for i, v := range h.versions {
		if v.ID == vid && (id == "" || v.NotebookID == id) {
			v.Cells = append([]NotebookCell(nil), v.Cells...)
			return v, i, nil
		}
	}
	return NotebookVersion{}, -1, ErrNotFound
}

// RestoreNotebookVersion puts a kept version back as the notebook's text
// (the user's Restore, or Undo of an agent edit). When expectRev >= 0 the
// notebook must still be at that revision (ErrNotebookConflict: it changed
// since). The version it replaces is kept first, so a restore can itself
// be undone. The restored text is the user's (LastBy ""), and the agent's
// unreviewed mark is cleared.
func (s *Store) RestoreNotebookVersion(id, vid string, expectRev int) (Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	i := s.notebookIndexLocked(id)
	if i < 0 {
		return Notebook{}, ErrNotFound
	}
	v, _, err := s.versionLocked(id, vid)
	if err != nil {
		return Notebook{}, err
	}
	if v.Event == "deleted" {
		return Notebook{}, ErrNotFound
	}
	ex := s.data.Notebooks[i]
	if expectRev >= 0 && ex.Rev != expectRev {
		return Notebook{}, ErrNotebookConflict
	}
	now := time.Now().UTC()
	if _, err := s.keepVersionStrictLocked(ex, "before-restore", "", now); err != nil {
		return Notebook{}, err
	}
	nb := ex
	nb.Name = v.Name
	nb.Cells = append([]NotebookCell(nil), v.Cells...)
	nb = normalizeNotebook(nb, now)
	nb.Rev = ex.Rev + 1
	nb.LastBy = ""
	// Restoring the agent's text brings back its "not reviewed" mark.
	nb.AgentUnreviewed = v.By == "agent"
	if nb.AgentUnreviewed {
		at := now
		nb.AgentEditedAt = &at
	}
	nb.UpdatedAt = now
	s.data.Notebooks[i] = nb
	return nb, s.saveLocked()
}

// DeletedNotebooks lists Recently deleted, newest first, without text.
func (s *Store) DeletedNotebooks() ([]NotebookVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	h := s.historyLocked()
	if h.err != nil {
		return nil, ErrHistoryUnavailable
	}
	out := []NotebookVersion{}
	for i := len(h.versions) - 1; i >= 0; i-- {
		if v := h.versions[i]; v.Event == "deleted" {
			v.Cells = nil
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].KeptAt.After(out[b].KeptAt) })
	return out, nil
}

// RestoreDeletedNotebook brings a deleted notebook back (the user's
// Restore) with its id when no notebook has it now, and takes it out of
// Recently deleted. It keeps its source, pin and Private flag.
func (s *Store) RestoreDeletedNotebook(vid string) (Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	v, idx, err := s.versionLocked("", vid)
	if err != nil {
		return Notebook{}, err
	}
	if v.Event != "deleted" {
		return Notebook{}, ErrNotFound
	}
	now := time.Now().UTC()
	nb := Notebook{ID: v.NotebookID, Name: v.Name, Cells: v.Cells, Pinned: v.Pinned, Source: v.Source, Private: v.Private, CreatedAt: v.CreatedAt, Rev: v.Rev + 1, AgentUnreviewed: v.Unreviewed}
	if s.notebookIndexLocked(nb.ID) >= 0 {
		nb.ID = newID()
	}
	nb = normalizeNotebook(nb, now)
	nb.UpdatedAt = now
	if nb.CreatedAt.IsZero() {
		nb.CreatedAt = now
	}
	h := s.historyLocked()
	prev := h.versions
	h.versions = append(append([]NotebookVersion(nil), h.versions[:idx]...), h.versions[idx+1:]...)
	if err := s.writeHistoryLocked(h); err != nil {
		h.versions = prev
		return Notebook{}, fmt.Errorf("%w: %v", ErrHistoryUnavailable, err)
	}
	s.data.Notebooks = append(s.data.Notebooks, nb)
	return nb, s.saveLocked()
}

// DeletedNotebookByID is one entry of Recently deleted, with its text, for
// the preview.
func (s *Store) DeletedNotebookByID(vid string) (NotebookVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return NotebookVersion{}, ErrLocked
	}
	v, _, err := s.versionLocked("", vid)
	if err != nil {
		return NotebookVersion{}, err
	}
	if v.Event != "deleted" {
		return NotebookVersion{}, ErrNotFound
	}
	return v, nil
}

// PurgeNotebookVersion removes a kept version for good: one of notebook
// id's versions, or (id "") an entry of Recently deleted. The history is
// plain text like keel.json; the user can remove text before 30 days.
func (s *Store) PurgeNotebookVersion(id, vid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return ErrLocked
	}
	v, idx, err := s.versionLocked(id, vid)
	if err != nil {
		return err
	}
	if (id == "") != (v.Event == "deleted") {
		return ErrNotFound
	}
	h := s.historyLocked()
	prev := h.versions
	h.versions = append(append([]NotebookVersion(nil), h.versions[:idx]...), h.versions[idx+1:]...)
	if err := s.writeHistoryLocked(h); err != nil {
		h.versions = prev
		return fmt.Errorf("%w: %v", ErrHistoryUnavailable, err)
	}
	return nil
}
