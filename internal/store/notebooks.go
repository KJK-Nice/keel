package store

import (
	"strings"
	"time"
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
	for _, nb := range s.data.Notebooks {
		if nb.ID == id {
			return nb, nil
		}
	}
	return Notebook{}, ErrNotFound
}

// NotebookSourceAgent marks a notebook saved from an agent's draft (§18).
const NotebookSourceAgent = "agent"

// UpsertNotebook creates or replaces a notebook the user wrote. Its Source
// is never taken from nb: a new notebook is the user's own, and an
// existing one keeps the source it had (a notebook saved from an agent's
// draft stays "From agent" after the user edits it).
func (s *Store) UpsertNotebook(nb Notebook) (Notebook, error) {
	nb.Source = ""
	return s.putNotebook(nb)
}

// AddAgentNotebook creates a new notebook from an agent's draft that the
// user reviewed and saved (agent service, §18). It always creates one: the
// agent cannot replace or edit an existing notebook.
func (s *Store) AddAgentNotebook(nb Notebook) (Notebook, error) {
	nb.ID = ""
	nb.Source = NotebookSourceAgent
	return s.putNotebook(nb)
}

func (s *Store) putNotebook(nb Notebook) (Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Notebook{}, ErrLocked
	}
	now := time.Now().UTC()
	nb = normalizeNotebook(nb, now)
	if nb.ID == "" {
		nb.ID = newID()
		nb.CreatedAt = now
		nb.UpdatedAt = now
		s.data.Notebooks = append(s.data.Notebooks, nb)
		return nb, s.saveLocked()
	}
	for i, existing := range s.data.Notebooks {
		if existing.ID == nb.ID {
			nb.CreatedAt = existing.CreatedAt
			nb.Source = existing.Source
			nb.UpdatedAt = now
			s.data.Notebooks[i] = nb
			return nb, s.saveLocked()
		}
	}
	nb.CreatedAt = now
	nb.UpdatedAt = now
	s.data.Notebooks = append(s.data.Notebooks, nb)
	return nb, s.saveLocked()
}

func (s *Store) DeleteNotebook(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return ErrLocked
	}
	out := s.data.Notebooks[:0]
	found := false
	for _, nb := range s.data.Notebooks {
		if nb.ID == id {
			found = true
			continue
		}
		out = append(out, nb)
	}
	if !found {
		return ErrNotFound
	}
	s.data.Notebooks = out
	return s.saveLocked()
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
	if nb.UpdatedAt.IsZero() {
		nb.UpdatedAt = now
	}
	return nb
}
