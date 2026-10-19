package store

import (
	"errors"
	"strings"
	"time"
)

// Notebooks and who may change them (docs/ui-redesign.md §18, §6 rule 12).
//
//   - The user's own saves (UpsertNotebook, SaveUserNotebook) never take
//     Source, Private or the agent marks from the caller: a new notebook is
//     the user's own and not private, an edit keeps what the notebook had.
//     Only SetNotebookPrivate changes Private, and only an HTTP route the
//     page calls reaches it.
//   - The agent reaches notebooks only through the Agent* methods, each of
//     which refuses a private notebook under the store lock: it lists and
//     reads only non-private ones, edits one only at the revision it read
//     (ErrNotebookConflict otherwise), and every edit first keeps the
//     previous version in the notebook history (notebookhistory.go).
//   - Every save bumps Rev, so an edit made against an older revision is
//     seen.

// NotebookSourceAgent marks a notebook the agent created (§18).
const NotebookSourceAgent = "agent"

// NotebookByAgent is LastBy on a notebook whose latest change the agent
// made; "" is the user.
const NotebookByAgent = "agent"

const (
	// MaxNotebookNameBytes and MaxNotebookBytes bound what the agent may
	// write: the name, and the name and every cell together.
	MaxNotebookNameBytes = 120
	MaxNotebookBytes     = 256 * 1024
	// sessionGap: a user save that replaces a version older than this keeps
	// that version in the history first (one entry per editing session, not
	// per keystroke).
	sessionGap = 15 * time.Minute
)

var (
	// ErrNotebookPrivate refuses any agent access to a private notebook.
	ErrNotebookPrivate = errors.New("this notebook is private: the agent cannot read or change it")
	// ErrNotebookConflict refuses a change made against an older revision.
	ErrNotebookConflict = errors.New("the notebook changed since it was read")
	// ErrNotebookTooLarge refuses an agent write over the size limits.
	ErrNotebookTooLarge = errors.New("the notebook would be larger than Keel allows")
)

func (s *Store) ListNotebooks() ([]Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	out := make([]Notebook, len(s.data.Notebooks))
	copy(out, s.data.Notebooks)
	return out, nil
}

func (s *Store) GetNotebook(id string) (Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	if i := s.notebookIndexLocked(id); i >= 0 {
		return s.data.Notebooks[i], nil
	}
	return Notebook{}, ErrNotFound
}

func (s *Store) notebookIndexLocked(id string) int {
	if id == "" {
		return -1
	}
	for i, nb := range s.data.Notebooks {
		if nb.ID == id {
			return i
		}
	}
	return -1
}

// UpsertNotebook creates or replaces a notebook the user wrote (see
// SaveUserNotebook).
func (s *Store) UpsertNotebook(nb Notebook) (Notebook, error) {
	saved, _, err := s.SaveUserNotebook(nb)
	return saved, err
}

// SaveUserNotebook creates or replaces a notebook the user wrote. Source,
// Private and the agent marks are never taken from nb: a new notebook is
// the user's own and not private, and an existing one keeps them. nb.Rev
// is the revision the editor started from. When the version being replaced
// is the agent's, it is kept in the history first, and replacedAgent says
// whether the user's save was made from an older revision (the user typed
// over an agent edit they had not loaded): nothing the user typed is
// lost, and the agent's edit can be restored from History.
func (s *Store) SaveUserNotebook(nb Notebook) (saved Notebook, replacedAgent bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, false, ErrLocked
	}
	now := time.Now().UTC()
	base := nb.Rev
	nb = normalizeNotebook(nb, now)
	nb.Source, nb.Private, nb.LastBy, nb.AgentEditedAt, nb.AgentUnreviewed = "", false, "", nil, false
	if i := s.notebookIndexLocked(nb.ID); i >= 0 {
		ex := s.data.Notebooks[i]
		switch {
		case ex.LastBy == NotebookByAgent && !sameNotebookContent(ex, nb):
			// The agent's version is about to be replaced: keep it.
			s.keepVersionLocked(ex, "agent-version", "", now)
			replacedAgent = base != ex.Rev
		case now.Sub(ex.UpdatedAt) >= sessionGap && !sameNotebookContent(ex, nb):
			// A new editing session: keep the version it starts from.
			s.keepVersionLocked(ex, "session", "", now)
		}
		nb.CreatedAt = ex.CreatedAt
		nb.Source = ex.Source
		nb.Private = ex.Private
		nb.AgentEditedAt = ex.AgentEditedAt
		nb.AgentUnreviewed = ex.AgentUnreviewed
		nb.Rev = ex.Rev + 1
		nb.UpdatedAt = now
		if sameNotebookContent(ex, nb) {
			// A pin or a save without changes: the agent's mark stays.
			nb.LastBy = ex.LastBy
		}
		s.data.Notebooks[i] = nb
		return nb, replacedAgent, s.saveLocked()
	}
	if nb.ID == "" {
		nb.ID = newID()
	}
	nb.CreatedAt = now
	nb.UpdatedAt = now
	nb.Rev = 1
	s.data.Notebooks = append(s.data.Notebooks, nb)
	return nb, false, s.saveLocked()
}

// SetNotebookPrivate turns a notebook's Private flag on or off. Only the
// user's route calls it: the agent has no way to change it. It does not
// change the revision (the text is the same).
func (s *Store) SetNotebookPrivate(id string, private bool) (Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	i := s.notebookIndexLocked(id)
	if i < 0 {
		return Notebook{}, ErrNotFound
	}
	s.data.Notebooks[i].Private = private
	nb := s.data.Notebooks[i]
	return nb, s.saveLocked()
}

// MarkNotebookReviewed clears the "edited by the agent, not reviewed yet"
// mark, the user's Mark reviewed.
func (s *Store) MarkNotebookReviewed(id string) (Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	i := s.notebookIndexLocked(id)
	if i < 0 {
		return Notebook{}, ErrNotFound
	}
	s.data.Notebooks[i].AgentUnreviewed = false
	nb := s.data.Notebooks[i]
	return nb, s.saveLocked()
}

// DeleteNotebook deletes a notebook the user chose to delete. Its last
// version goes to Recently deleted (notebookhistory.go) when the history
// can be written; the delete happens either way.
func (s *Store) DeleteNotebook(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return ErrLocked
	}
	i := s.notebookIndexLocked(id)
	if i < 0 {
		return ErrNotFound
	}
	s.keepVersionLocked(s.data.Notebooks[i], "deleted", "", time.Now().UTC())
	s.removeNotebookLocked(i)
	return s.saveLocked()
}

func (s *Store) removeNotebookLocked(i int) {
	out := make([]Notebook, 0, len(s.data.Notebooks)-1)
	out = append(out, s.data.Notebooks[:i]...)
	out = append(out, s.data.Notebooks[i+1:]...)
	s.data.Notebooks = out
}

// --- the agent's paths: never a private notebook ---

// AgentListNotebooks lists the notebooks the agent may know about: every
// notebook that is not private.
func (s *Store) AgentListNotebooks() ([]Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	out := make([]Notebook, 0, len(s.data.Notebooks))
	for _, nb := range s.data.Notebooks {
		if !nb.Private {
			out = append(out, nb)
		}
	}
	return out, nil
}

// AgentGetNotebook reads a notebook for the agent: ErrNotebookPrivate for
// a private one.
func (s *Store) AgentGetNotebook(id string) (Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	i := s.notebookIndexLocked(id)
	if i < 0 {
		return Notebook{}, ErrNotFound
	}
	if s.data.Notebooks[i].Private {
		return Notebook{}, ErrNotebookPrivate
	}
	return s.data.Notebooks[i], nil
}

// AgentCreateNotebook creates a new notebook the agent wrote (the agent
// service, which redacted it). It always creates one, marked "agent", not
// private.
func (s *Store) AgentCreateNotebook(nb Notebook) (Notebook, error) {
	if err := checkAgentSize(nb); err != nil {
		return Notebook{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	now := time.Now().UTC()
	nb = normalizeNotebook(nb, now)
	nb.ID = newID()
	nb.Source = NotebookSourceAgent
	nb.Private, nb.Pinned = false, false
	nb.LastBy = NotebookByAgent
	nb.AgentEditedAt, nb.AgentUnreviewed = nil, false
	nb.Rev = 1
	nb.CreatedAt, nb.UpdatedAt = now, now
	s.data.Notebooks = append(s.data.Notebooks, nb)
	return nb, s.saveLocked()
}

// AgentEditNotebook applies the agent's edit to a notebook that is not
// private and is still at baseRev, the revision the agent read. edit gets
// a copy and returns the new name and cells (the agent service redacted
// what it adds). The version before the edit is kept in the history
// first; when it cannot be kept the edit is refused, so an agent edit can
// always be undone. It answers the notebook after the edit and the id of
// the version kept.
func (s *Store) AgentEditNotebook(id string, baseRev int, summary string, edit func(Notebook) (Notebook, error)) (Notebook, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, "", ErrLocked
	}
	i := s.notebookIndexLocked(id)
	if i < 0 {
		return Notebook{}, "", ErrNotFound
	}
	ex := s.data.Notebooks[i]
	if ex.Private {
		return Notebook{}, "", ErrNotebookPrivate
	}
	if ex.Rev != baseRev {
		return Notebook{}, "", ErrNotebookConflict
	}
	cp := ex
	cp.Cells = append([]NotebookCell(nil), ex.Cells...)
	out, err := edit(cp)
	if err != nil {
		return Notebook{}, "", err
	}
	if err := checkAgentSize(out); err != nil {
		return Notebook{}, "", err
	}
	now := time.Now().UTC()
	out = normalizeNotebook(out, now)
	vid, err := s.keepVersionStrictLocked(ex, "before-agent-edit", summary, now)
	if err != nil {
		return Notebook{}, "", err
	}
	out.ID, out.CreatedAt, out.Source, out.Pinned = ex.ID, ex.CreatedAt, ex.Source, ex.Pinned
	out.Private = false
	out.Rev = ex.Rev + 1
	out.LastBy = NotebookByAgent
	at := now
	out.AgentEditedAt = &at
	out.AgentUnreviewed = true
	out.UpdatedAt = now
	s.data.Notebooks[i] = out
	return out, vid, s.saveLocked()
}

// DeleteNotebookAgentProposed deletes a notebook the agent proposed to
// delete, after the user confirmed it (the agent service, from the user's
// route). A private notebook is refused, and so is one whose revision is
// not rev when rev >= 0. Its last version goes to Recently deleted, and
// the delete is refused when it cannot (so it can be restored).
func (s *Store) DeleteNotebookAgentProposed(id string, rev int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return "", ErrLocked
	}
	i := s.notebookIndexLocked(id)
	if i < 0 {
		return "", ErrNotFound
	}
	ex := s.data.Notebooks[i]
	if ex.Private {
		return "", ErrNotebookPrivate
	}
	if rev >= 0 && ex.Rev != rev {
		return "", ErrNotebookConflict
	}
	vid, err := s.keepVersionStrictLocked(ex, "deleted", "", time.Now().UTC())
	if err != nil {
		return "", err
	}
	s.removeNotebookLocked(i)
	return vid, s.saveLocked()
}

func checkAgentSize(nb Notebook) error {
	if len(nb.Name) > MaxNotebookNameBytes {
		return ErrNotebookTooLarge
	}
	n := len(nb.Name)
	for _, c := range nb.Cells {
		n += len(c.Body)
	}
	if n > MaxNotebookBytes || len(nb.Cells) > 64 {
		return ErrNotebookTooLarge
	}
	return nil
}

// sameNotebookContent reports whether two notebooks hold the same name and
// text (cell ids aside).
func sameNotebookContent(a, b Notebook) bool {
	if a.Name != b.Name || len(a.Cells) != len(b.Cells) {
		return false
	}
	for i := range a.Cells {
		if a.Cells[i].Body != b.Cells[i].Body {
			return false
		}
	}
	return true
}

func normalizeNotebook(nb Notebook, now time.Time) Notebook {
	nb.Name = strings.TrimSpace(nb.Name)
	if nb.Name == "" {
		nb.Name = "Notebook"
	}
	cells := make([]NotebookCell, 0, len(nb.Cells))
	for _, cell := range nb.Cells {
		id := strings.TrimSpace(cell.ID)
		if id == "" {
			id = newID()
		}
		cells = append(cells, NotebookCell{ID: id, Body: cell.Body})
	}
	if len(cells) == 0 {
		cells = []NotebookCell{{ID: newID(), Body: ""}}
	}
	nb.Cells = cells
	if nb.Source != NotebookSourceAgent {
		nb.Source = ""
	}
	if nb.LastBy != NotebookByAgent {
		nb.LastBy = ""
	}
	if nb.UpdatedAt.IsZero() {
		nb.UpdatedAt = now
	}
	return nb
}
