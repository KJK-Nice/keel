package native

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
)

// The pure rules of notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12): the words of the trace, the picker,
// the cards and their buttons. They are cmd/keel/web/js/lib/notebooks.js (the parts above "history", which
// notebookslogic.go has), pinned there by test/notebooks.test.mjs and here by agentnotebookslogic_test.go. keel serve
// decides everything; nothing here talks to it, draws or types.

// maxAttached is how many notebooks one question may carry (lib/notebooks.js MAX_ATTACHED; keel serve refuses more).
const maxAttached = limits.MaxAttachedNotebooks

var attachLimit = fmt.Sprintf("Attach at most %d notebooks to a question", maxAttached)

// Texts of the app (lib/notebooks.js).
const (
	privateRefused = "This notebook is private: the agent cannot read it. Turn Private off on the notebook first."
	// saveFirst is what "Ask the agent about this notebook" says for a notebook that is not saved (term.js).
	saveFirst = "Save the notebook first, then ask the agent about it"
)

// redactedCount adds up a trace step's redactions, withheld values too.
func redactedCount(counts map[string]int) (n int) {
	for _, c := range counts {
		n += c
	}
	return n
}

func bytesNoun(n int) string {
	if n == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", n)
}

// isNotebookStep reports a trace step about notebooks (lib/notebooks.js isNotebookStep).
func isNotebookStep(s keelclient.TraceStep) bool {
	return s.Status == "notebook" || s.Status == "notebook-index" || strings.HasPrefix(s.Reason, "notebook-")
}

// notebookStepText is a trace line about notebooks: the titles sent, a notebook sent (attached) or read (the agent
// asked), or why one was not (lib/notebooks.js notebookStepText).
func notebookStepText(s keelclient.TraceStep) string {
	if s.Status == "notebook-index" {
		noun := "notebooks"
		if s.Count == 1 {
			noun = "notebook"
		}
		return fmt.Sprintf("Sent the titles of %d %s (not private)", s.Count, noun)
	}
	if s.Status == "skipped" {
		switch s.Reason {
		case "notebook-missing":
			return "Skipped a notebook · it was deleted"
		case "notebook-private":
			return "Not sent · a private notebook (the agent cannot read it)"
		case "notebook-limit":
			return "Stopped reading notebooks · Keel's limit for one question"
		}
	}
	parts := []string{bytesNoun(s.Bytes)}
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
// cannot be ticked (lib/notebooks.js attachable).
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

// reconcileAttached keeps the attached ids that still name a notebook the agent may read (not private), once each
// (lib/notebooks.js reconcileAttached).
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

// toggleAttached adds or removes one notebook; never more than maxAttached, never a private one (lib/notebooks.js
// toggleAttached).
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

// attachHint is the line under the chips (lib/notebooks.js attachHint).
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

// warned: a change written after reading a pane gets the warning look (lib/notebooks.js warned).
func warned(p keelclient.NotebookProposal) bool { return p.FromPane || len(p.PanesRead) > 0 }

// paneWarning is the warning's text (lib/notebooks.js paneWarning).
func paneWarning(p keelclient.NotebookProposal) string {
	where := ""
	if p.FromPane && p.SeenIn != "" {
		where = " Some of it matches pane " + p.SeenIn + "."
	}
	return "The agent wrote this after reading terminal output." + where + " Check every command in it before you type one: text in a pane can be written to mislead the agent."
}

var refusedTexts = map[string]string{
	"conflict":  "You changed it after the agent read it, so Keel did not apply the agent's edit. Your text is as you left it.",
	"editing":   "You were typing in it, so Keel did not apply the agent's edit.",
	"private":   "It is private, so the agent cannot change it.",
	"not-read":  "The agent had not read it in this question, so Keel did not apply the edit.",
	"not-found": "It is not a notebook the agent may change.",
	"deleted":   "It was deleted.",
	"no-match":  "The text the agent meant to change was not found exactly once, so nothing changed.",
	"redacted":  "Keel had removed values from it before the agent saw it, so the agent may change it only piece by piece. Nothing changed.",
	"truncated": "Keel had sent only part of it, so the agent may change it only piece by piece. Nothing changed.",
	"too-large": "The edit would make the notebook larger than Keel allows. Nothing changed.",
	"history":   "Keel could not keep the previous version, so it made no change.",
}

// refusedText says why an edit was refused (lib/notebooks.js refusedText).
func refusedText(reason string) string {
	if t, ok := refusedTexts[reason]; ok {
		return t
	}
	return "The agent's edit was not valid. Nothing changed."
}

// cardHead is the card's title line (lib/notebooks.js cardHead).
func cardHead(p keelclient.NotebookProposal) string {
	switch p.Kind {
	case "create":
		if p.Status == "refused" {
			return "Notebook not created"
		}
		return "Created notebook"
	case "edit":
		if p.Status == "refused" {
			return "Notebook not changed"
		}
		return "Edited notebook"
	case "delete":
		if p.Status == "proposed" {
			return "Delete notebook?"
		}
		return "Delete notebook"
	}
	return "Notebook draft"
}

// cardStatus is the line under the title, or "" for none (lib/notebooks.js cardStatus).
func cardStatus(p keelclient.NotebookProposal) string {
	k, s := p.Kind, p.Status
	switch k {
	case "create":
		switch s {
		case "created":
			return "Created by the agent and saved to Notebooks, marked From agent."
		case "undone":
			return "Undone. The notebook is in Notebooks · Recently deleted."
		case "refused":
			return refusedText(p.Reason)
		}
	case "edit":
		switch s {
		case "edited":
			return "The agent changed this notebook. Keel kept the version before it: Undo puts it back, and History has every version."
		case "undone":
			return "Undone. The notebook is back to the version before the agent's edit."
		case "refused":
			return refusedText(p.Reason)
		}
	case "delete":
		switch s {
		case "proposed":
			return "The agent asks to delete this notebook. Nothing is deleted unless you confirm."
		case "deleted":
			return "Deleted. It is in Notebooks · Recently deleted."
		case "dismissed":
			return "Kept. Nothing was deleted."
		case "restored":
			return "Restored to Notebooks."
		}
	case "":
		switch s {
		case "saved":
			return "Saved to Notebooks as a new notebook, marked From agent."
		case "dismissed":
			return "Dismissed. Nothing was saved."
		}
		return "A draft from an earlier version of Keel. It was not saved; ask the agent again."
	}
	return ""
}

// The buttons of a card (lib/notebooks.js cardActions).
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
		if p.NotebookID != "" && (s == "created" || s == "edited" || s == "undone" || s == "saved") && k != "delete" {
			return []string{actOpen}
		}
		return nil
	}
	switch k {
	case "create":
		if s == "created" {
			return []string{actOpen, actUndo}
		}
		if s == "undone" && p.VersionID != "" {
			return []string{actRestore}
		}
	case "edit":
		switch s {
		case "edited":
			return []string{actOpen, actUndo}
		case "undone", "refused":
			if p.NotebookID != "" && p.Reason != "private" && p.Reason != "not-found" && p.Reason != "deleted" {
				return []string{actOpen}
			}
			return nil
		}
	case "delete":
		switch s {
		case "proposed":
			return []string{actOpen, actKeep, actDelete}
		case "deleted":
			if p.VersionID != "" {
				return []string{actRestore}
			}
		case "restored", "dismissed":
			return []string{actOpen}
		}
	case "":
		if s == "saved" && p.NotebookID != "" {
			return []string{actOpen}
		}
	}
	return nil
}

// deleteConfirmText is the question a proposed delete asks before it posts (lib/notebooks.js deleteConfirmText).
func deleteConfirmText(p keelclient.NotebookProposal) string {
	return fmt.Sprintf("Delete the notebook “%s”? The agent asked for this. It goes to Notebooks · Recently deleted, where you can restore it for 30 days.", orElse(p.Title, "Notebook"))
}
