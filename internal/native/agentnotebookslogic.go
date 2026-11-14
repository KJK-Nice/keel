package native

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
	"keel/internal/wire"
)

// The pure rules of notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12): the words of the trace, the picker,
// the cards and their buttons (agentnotebookslogic_test.go pins them; notebookslogic.go has the rest). keel serve
// decides everything; nothing here talks to it, draws or types.

// maxAttached is how many notebooks one question may carry (keel serve refuses more).
const maxAttached = limits.MaxAttachedNotebooks

var attachLimit = fmt.Sprintf("Attach at most %d notebooks to a question", maxAttached)

const (
	privateRefused = "This notebook is private: the agent cannot read it. Turn Private off on the notebook first."
	// saveFirst is what "Ask the agent about this notebook" says for a notebook that is not saved.
	saveFirst = "Save the notebook first, then ask the agent about it"
)

// redactedCount adds up a trace step's redactions, withheld values too.
func redactedCount(counts map[string]int) (n int) {
	for _, c := range counts {
		n += c
	}
	return n
}

func isNotebookStep(s keelclient.TraceStep) bool {
	return s.Status == wire.TraceNotebook || s.Status == wire.TraceNotebookIndex || strings.HasPrefix(s.Reason, "notebook-")
}

// notebookStepText is a trace line about notebooks: the titles sent, a notebook sent (attached) or read (the agent
// asked), or why one was not.
func notebookStepText(s keelclient.TraceStep) string {
	if s.Status == wire.TraceNotebookIndex {
		return "Sent the titles of " + countWord(s.Count, "notebook", "notebooks") + " (not private)"
	}
	if s.Status == wire.TraceSkipped {
		switch s.Reason {
		case wire.SkipNotebookMissing:
			return "Skipped a notebook · it was deleted"
		case wire.SkipNotebookPrivate:
			return "Not sent · a private notebook (the agent cannot read it)"
		case wire.SkipNotebookLimit:
			return "Stopped reading notebooks · Keel's limit for one question"
		}
	}
	parts := []string{countWord(s.Bytes, "byte", "bytes")}
	if s.Truncated {
		parts = append(parts, "cut by Keel")
	}
	parts = append(parts, fmt.Sprintf("%d redacted", redactedCount(s.Redactions)))
	if s.Asked {
		return fmt.Sprintf("Read notebook %s (%s) · the agent asked", orElse(s.Notebook, "Notebook"), strings.Join(parts, ", "))
	}
	return fmt.Sprintf("Sent notebook %s (%s)", orElse(s.Notebook, "Notebook"), strings.Join(parts, ", "))
}

// ---- attaching ----

// attachable are the notebooks the picker lists: saved ones, pinned first, then by name. Private ones are listed but
// cannot be ticked.
func attachable(nbs []keelclient.Notebook) []keelclient.Notebook {
	var out []keelclient.Notebook
	for _, n := range nbs {
		if n.ID != "" {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// reconcileAttached keeps the attached ids that still name a notebook the agent may read (not private), once each.
func reconcileAttached(ids []string, nbs []keelclient.Notebook) (out []string) {
	for _, id := range ids {
		if slices.Contains(out, id) {
			continue
		}
		if i := slices.IndexFunc(nbs, func(n keelclient.Notebook) bool { return n.ID == id }); i >= 0 && id != "" && !nbs[i].Private {
			out = append(out, id)
		}
	}
	return out
}

// toggleAttached adds or removes one notebook; never more than maxAttached, never a private one.
func toggleAttached(ids []string, id string, nbs []keelclient.Notebook) []string {
	list := slices.Clone(ids)
	if id == "" {
		return list
	}
	if i := slices.Index(list, id); i >= 0 {
		return slices.Delete(list, i, i+1)
	}
	if len(list) >= maxAttached {
		return list
	}
	if nbs != nil {
		if i := slices.IndexFunc(nbs, func(n keelclient.Notebook) bool { return n.ID == id }); i >= 0 && nbs[i].Private {
			return list
		}
	}
	return append(list, id)
}

func attachHint(n int) string {
	if n == 0 {
		return ""
	}
	if n == 1 {
		return "This notebook is sent, redacted, with each question until you remove it."
	}
	return "These notebooks are sent, redacted, with each question until you remove them."
}

// ---- what the agent did to a notebook (the cards) ----

// warned: a change written after reading a pane gets the warning look.
func warned(p keelclient.NotebookProposal) bool { return p.FromPane || len(p.PanesRead) > 0 }

func paneWarning(p keelclient.NotebookProposal) string {
	where := ""
	if p.FromPane && p.SeenIn != "" {
		where = " Some of it matches pane " + p.SeenIn + "."
	}
	return "The agent wrote this after reading terminal output." + where + " Check every command in it before you type one: text in a pane can be written to mislead the agent."
}

var refusedTexts = map[string]string{
	wire.RefusedConflict:  "You changed it after the agent read it, so Keel did not apply the agent's edit. Your text is as you left it.",
	wire.RefusedEditing:   "You were typing in it, so Keel did not apply the agent's edit.",
	wire.RefusedPrivate:   "It is private, so the agent cannot change it.",
	wire.RefusedNotRead:   "The agent had not read it in this question, so Keel did not apply the edit.",
	wire.RefusedNotFound:  "It is not a notebook the agent may change.",
	wire.RefusedDeleted:   "It was deleted.",
	wire.RefusedNoMatch:   "The text the agent meant to change was not found exactly once, so nothing changed.",
	wire.RefusedRedacted:  "Keel had removed values from it before the agent saw it, so the agent may change it only piece by piece. Nothing changed.",
	wire.RefusedTruncated: "Keel had sent only part of it, so the agent may change it only piece by piece. Nothing changed.",
	wire.RefusedTooLarge:  "The edit would make the notebook larger than Keel allows. Nothing changed.",
	wire.RefusedHistory:   "Keel could not keep the previous version, so it made no change.",
}

func refusedText(reason string) string {
	if t, ok := refusedTexts[reason]; ok {
		return t
	}
	return "The agent's edit was not valid. Nothing changed."
}

func cardHead(p keelclient.NotebookProposal) string {
	switch p.Kind {
	case wire.NotebookCreate:
		if p.Status == wire.CardRefused {
			return "Notebook not created"
		}
		return "Created notebook"
	case wire.NotebookEdit:
		if p.Status == wire.CardRefused {
			return "Notebook not changed"
		}
		return "Edited notebook"
	case wire.NotebookDelete:
		if p.Status == wire.CardProposed {
			return "Delete notebook?"
		}
		return "Delete notebook"
	}
	return "Notebook draft"
}

// cardStatus is the line under the title, or "" for none.
func cardStatus(p keelclient.NotebookProposal) string {
	k, s := p.Kind, p.Status
	switch k {
	case wire.NotebookCreate:
		switch s {
		case wire.CardCreated:
			return "Created by the agent and saved to Notebooks, marked From agent."
		case wire.CardUndone:
			return "Undone. The notebook is in Notebooks · Recently deleted."
		case wire.CardRefused:
			return refusedText(p.Reason)
		}
	case wire.NotebookEdit:
		switch s {
		case wire.CardEdited:
			return "The agent changed this notebook. Keel kept the version before it: Undo puts it back, and History has every version."
		case wire.CardUndone:
			return "Undone. The notebook is back to the version before the agent's edit."
		case wire.CardRefused:
			return refusedText(p.Reason)
		}
	case wire.NotebookDelete:
		switch s {
		case wire.CardProposed:
			return "The agent asks to delete this notebook. Nothing is deleted unless you confirm."
		case wire.CardDeleted:
			return "Deleted. It is in Notebooks · Recently deleted."
		case wire.CardDismissed:
			return "Kept. Nothing was deleted."
		case wire.CardRestored:
			return "Restored to Notebooks."
		}
	case "":
		switch s {
		case wire.CardSaved:
			return "Saved to Notebooks as a new notebook, marked From agent."
		case wire.CardDismissed:
			return "Dismissed. Nothing was saved."
		}
		return "A draft from an earlier version of Keel. It was not saved; ask the agent again."
	}
	return ""
}

// The buttons of a card.
const (
	actOpen    = "open"
	actUndo    = "undo"
	actRestore = "restore"
	actKeep    = "keep"
	actDelete  = "delete"
)

// cardActions lists the buttons a card offers now, in order: open, undo, keep, delete (asks first), restore.
func cardActions(p keelclient.NotebookProposal) []string {
	k, s := p.Kind, p.Status
	if p.Expired {
		if p.NotebookID != "" && (s == wire.CardCreated || s == wire.CardEdited || s == wire.CardUndone || s == wire.CardSaved) && k != wire.NotebookDelete {
			return []string{actOpen}
		}
		return nil
	}
	switch k {
	case wire.NotebookCreate:
		if s == wire.CardCreated {
			return []string{actOpen, actUndo}
		}
		if s == wire.CardUndone && p.VersionID != "" {
			return []string{actRestore}
		}
	case wire.NotebookEdit:
		switch s {
		case wire.CardEdited:
			return []string{actOpen, actUndo}
		case wire.CardUndone, wire.CardRefused:
			if p.NotebookID != "" && p.Reason != wire.RefusedPrivate && p.Reason != wire.RefusedNotFound && p.Reason != wire.RefusedDeleted {
				return []string{actOpen}
			}
			return nil
		}
	case wire.NotebookDelete:
		switch s {
		case wire.CardProposed:
			return []string{actOpen, actKeep, actDelete}
		case wire.CardDeleted:
			if p.VersionID != "" {
				return []string{actRestore}
			}
		case wire.CardRestored, wire.CardDismissed:
			return []string{actOpen}
		}
	case "":
		if s == wire.CardSaved && p.NotebookID != "" {
			return []string{actOpen}
		}
	}
	return nil
}

func deleteConfirmText(p keelclient.NotebookProposal) string {
	return fmt.Sprintf("Delete the notebook “%s”? The agent asked for this. It goes to Notebooks · Recently deleted, where you can restore it for 30 days.", orElse(p.Title, "Notebook"))
}
