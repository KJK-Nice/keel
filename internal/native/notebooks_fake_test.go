package native

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"keel/internal/native/keelclient"
)

// The notebooks half of fakeBackend: what the window asks of keel serve, recorded, and answered as the test says. Nothing
// here reads a pane or types.

type fakeNotebooks struct {
	list    []keelclient.Notebook
	seq     int
	reqs    []string // "post name", "put id rev=N", "patch id", "delete id", "delete id permanent", "private id on", "reviewed id", ...
	saves   []keelclient.NotebookSave
	history map[string][]keelclient.NotebookVersion
	trash   []keelclient.NotebookVersion

	replaced   bool  // the next save answers replacedAgentEdit
	saveErr    error // every save fails with it
	deleteErr  error // the first delete fails with it (then it clears)
	restoreErr error
	listCalls  int
}

func (f *fakeBackend) nbf() *fakeNotebooks {
	if f.nb == nil {
		f.nb = &fakeNotebooks{history: map[string][]keelclient.NotebookVersion{}}
	}
	return f.nb
}

// addNotebook puts a notebook into the fake, as keel serve would have it.
func (f *fakeBackend) addNotebook(name, md string) keelclient.Notebook {
	n := f.nbf()
	n.seq++
	nb := keelclient.Notebook{ID: fmt.Sprintf("nb%d", n.seq), Name: name, Rev: 1, UpdatedAt: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		Cells: []keelclient.NotebookCell{{ID: fmt.Sprintf("c%d", n.seq), Body: md}}}
	n.list = append(n.list, nb)
	return nb
}

func (f *fakeBackend) nbIndex(id string) int {
	for i, nb := range f.nbf().list {
		if nb.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeBackend) Notebooks(context.Context) ([]keelclient.Notebook, error) {
	n := f.nbf()
	n.listCalls++
	return append([]keelclient.Notebook(nil), n.list...), nil
}

func (f *fakeBackend) CreateNotebook(_ context.Context, in keelclient.NotebookSave) (keelclient.Notebook, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, "post "+in.Name)
	n.saves = append(n.saves, in)
	if n.saveErr != nil {
		return keelclient.Notebook{}, n.saveErr
	}
	n.seq++
	nb := keelclient.Notebook{ID: fmt.Sprintf("nb%d", n.seq), Name: in.Name, Pinned: in.Pinned, Rev: 1, UpdatedAt: time.Now(), CreatedAt: time.Now(),
		Cells: []keelclient.NotebookCell{{ID: fmt.Sprintf("c%d", n.seq), Body: in.Cells[0].Body}}}
	n.list = append(n.list, nb)
	return nb, nil
}

func (f *fakeBackend) SaveNotebook(_ context.Context, id string, in keelclient.NotebookSave) (keelclient.SavedNotebook, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, fmt.Sprintf("put %s rev=%d", id, in.Rev))
	n.saves = append(n.saves, in)
	if n.saveErr != nil {
		return keelclient.SavedNotebook{}, n.saveErr
	}
	i := f.nbIndex(id)
	if i < 0 {
		return keelclient.SavedNotebook{}, &keelclient.StatusError{Code: 404, Msg: "notebook not found"}
	}
	old := n.list[i]
	// keel serve keeps a version when a new editing session starts, or the agent's when the user replaces it.
	if old.LastBy == byAgent || old.Markdown() != in.Cells[0].Body {
		n.history[id] = append([]keelclient.NotebookVersion{{ID: fmt.Sprintf("v%d", len(n.history[id])+1), NotebookID: id, Name: old.Name, Cells: old.Cells, Rev: old.Rev, By: "user", Event: "session", At: old.UpdatedAt, Bytes: len(old.Markdown())}}, n.history[id]...)
	}
	old.Name, old.Pinned, old.Cells, old.Rev, old.UpdatedAt = in.Name, in.Pinned, []keelclient.NotebookCell{{ID: orElse(in.Cells[0].ID, old.Cells[0].ID), Body: in.Cells[0].Body}}, old.Rev+1, time.Now()
	old.LastBy = ""
	n.list[i] = old
	out := keelclient.SavedNotebook{Notebook: old, ReplacedAgentEdit: n.replaced}
	n.replaced = false
	return out, nil
}

func (f *fakeBackend) PatchNotebook(_ context.Context, id string, name *string, pinned *bool) (keelclient.Notebook, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, "patch "+id)
	i := f.nbIndex(id)
	if i < 0 {
		return keelclient.Notebook{}, &keelclient.StatusError{Code: 404, Msg: "notebook not found"}
	}
	if name != nil {
		if len(*name) > 120 {
			return keelclient.Notebook{}, &keelclient.StatusError{Code: 400, Msg: "a notebook name is at most 120 bytes"}
		}
		n.list[i].Name = *name
		n.list[i].Rev++
		n.reqs = append(n.reqs, "name="+*name)
	}
	if pinned != nil {
		n.list[i].Pinned = *pinned
		n.reqs = append(n.reqs, fmt.Sprintf("pinned=%v", *pinned))
	}
	return n.list[i], nil
}

func (f *fakeBackend) DeleteNotebook(_ context.Context, id string, permanent bool) error {
	n := f.nbf()
	req := "delete " + id
	if permanent {
		req += " permanent"
	}
	n.reqs = append(n.reqs, req)
	if n.deleteErr != nil && !permanent {
		err := n.deleteErr
		n.deleteErr = nil
		return err
	}
	i := f.nbIndex(id)
	if i < 0 {
		return &keelclient.StatusError{Code: 404, Msg: "notebook not found"}
	}
	nb := n.list[i]
	n.list = append(n.list[:i], n.list[i+1:]...)
	if !permanent {
		n.trash = append([]keelclient.NotebookVersion{{ID: "t" + id, NotebookID: id, Name: nb.Name, Cells: nb.Cells, Event: "deleted", KeptAt: time.Now()}}, n.trash...)
	}
	return nil
}

func (f *fakeBackend) PutNotebookPrivacy(_ context.Context, id string, private bool) (keelclient.Notebook, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, fmt.Sprintf("private %s %v", id, private))
	i := f.nbIndex(id)
	if i < 0 {
		return keelclient.Notebook{}, &keelclient.StatusError{Code: 404}
	}
	n.list[i].Private = private
	return n.list[i], nil
}

func (f *fakeBackend) MarkNotebookReviewed(_ context.Context, id string) (keelclient.Notebook, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, "reviewed "+id)
	i := f.nbIndex(id)
	if i < 0 {
		return keelclient.Notebook{}, &keelclient.StatusError{Code: 404}
	}
	n.list[i].AgentUnreviewed = false
	return n.list[i], nil
}

func (f *fakeBackend) NotebookHistory(_ context.Context, id string) ([]keelclient.NotebookVersion, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, "history "+id)
	var out []keelclient.NotebookVersion
	for _, v := range n.history[id] {
		v.Cells = nil // a list carries no text
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeBackend) NotebookVersion(_ context.Context, id, vid string) (keelclient.NotebookVersion, error) {
	for _, v := range f.nbf().history[id] {
		if v.ID == vid {
			return v, nil
		}
	}
	return keelclient.NotebookVersion{}, &keelclient.StatusError{Code: 404, Msg: "version not found"}
}

func (f *fakeBackend) RestoreNotebookVersion(_ context.Context, id, vid string, rev int) (keelclient.Notebook, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, fmt.Sprintf("restore %s %s rev=%d", id, vid, rev))
	if n.restoreErr != nil {
		return keelclient.Notebook{}, n.restoreErr
	}
	i := f.nbIndex(id)
	for _, v := range n.history[id] {
		if i >= 0 && v.ID == vid {
			if rev != n.list[i].Rev {
				return keelclient.Notebook{}, &keelclient.StatusError{Code: 409, Msg: "The notebook changed meanwhile. Look at History again before you restore.", Reason: "conflict"}
			}
			n.list[i].Cells, n.list[i].Rev = v.Cells, n.list[i].Rev+1
			return n.list[i], nil
		}
	}
	return keelclient.Notebook{}, &keelclient.StatusError{Code: 404, Msg: "version not found"}
}

func (f *fakeBackend) PurgeNotebookVersion(_ context.Context, id, vid string) error {
	n := f.nbf()
	n.reqs = append(n.reqs, "purge "+id+" "+vid)
	n.history[id] = keepVersions(n.history[id], vid)
	return nil
}

func keepVersions(vs []keelclient.NotebookVersion, drop string) (out []keelclient.NotebookVersion) {
	for _, v := range vs {
		if v.ID != drop {
			out = append(out, v)
		}
	}
	return out
}

func (f *fakeBackend) DeletedNotebooks(context.Context) ([]keelclient.NotebookVersion, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, "trash")
	var out []keelclient.NotebookVersion
	for _, v := range n.trash {
		v.Cells = nil
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeBackend) DeletedNotebook(_ context.Context, vid string) (keelclient.NotebookVersion, error) {
	for _, v := range f.nbf().trash {
		if v.ID == vid {
			return v, nil
		}
	}
	return keelclient.NotebookVersion{}, errors.New("not in the trash")
}

func (f *fakeBackend) RestoreDeletedNotebook(_ context.Context, vid string) (keelclient.Notebook, error) {
	n := f.nbf()
	n.reqs = append(n.reqs, "restore-deleted "+vid)
	for _, v := range n.trash {
		if v.ID == vid {
			n.trash = keepVersions(n.trash, vid)
			nb := keelclient.Notebook{ID: v.NotebookID, Name: v.Name, Cells: v.Cells, Rev: 1, UpdatedAt: time.Now()}
			n.list = append(n.list, nb)
			return nb, nil
		}
	}
	return keelclient.Notebook{}, errors.New("not in the trash")
}

func (f *fakeBackend) PurgeDeletedNotebook(_ context.Context, vid string) error {
	n := f.nbf()
	n.reqs = append(n.reqs, "purge-deleted "+vid)
	n.trash = keepVersions(n.trash, vid)
	return nil
}

// reqsWith counts the recorded requests that start with prefix.
func (n *fakeNotebooks) reqsWith(prefix string) (c int) {
	for _, r := range n.reqs {
		if strings.HasPrefix(r, prefix) {
			c++
		}
	}
	return c
}
