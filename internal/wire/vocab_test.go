package wire

import (
	"slices"
	"testing"
)

// The vocabularies are wire values: the daemon sends them, the window reads
// them and saved conversations hold them. A rename that changes a string
// fails here.
func TestVocabularyValues(t *testing.T) {
	for name, c := range map[string]struct{ got, want []string }{
		"trace": {
			[]string{TraceRead, TraceSkipped, TraceNotes, TraceRunbook, TraceNotebook, TraceNotebookIndex},
			[]string{"read", "skipped", "notes", "runbook", "notebook", "notebook-index"},
		},
		"skip": {
			[]string{SkipNotOpen, SkipNotReadable, SkipReadFailed, SkipNotebookLimit, SkipNotebookMissing, SkipNotebookPrivate},
			[]string{"not-open", "not-readable", "read-failed", "notebook-limit", "notebook-missing", "notebook-private"},
		},
		"note kind": {
			[]string{NoteHost, NoteUser, NoteGroup},
			[]string{"host", "user", "group"},
		},
		"card": {
			[]string{CardOpen, CardSaved, CardDismissed, CardCreated, CardEdited, CardRefused, CardUndone, CardProposed, CardDeleted, CardRestored},
			[]string{"open", "saved", "dismissed", "created", "edited", "refused", "undone", "proposed", "deleted", "restored"},
		},
		"notebook kind": {
			[]string{NotebookCreate, NotebookEdit, NotebookDelete},
			[]string{"create", "edit", "delete"},
		},
		"refused": {
			[]string{RefusedConflict, RefusedEditing, RefusedPrivate, RefusedNotRead, RefusedNotFound, RefusedDeleted, RefusedNoMatch, RefusedRedacted, RefusedTruncated, RefusedTooLarge, RefusedHistory, RefusedInvalid},
			[]string{"conflict", "editing", "private", "not-read", "not-found", "deleted", "no-match", "redacted", "truncated", "too-large", "history", "invalid"},
		},
		"result": {
			[]string{ResultReported, ResultUser, ResultUnreported, ResultAbandoned},
			[]string{"reported", "user", "unreported", "abandoned"},
		},
		"run status": {
			[]string{RunActive, RunFinished, RunStopped},
			[]string{"active", "finished", "stopped"},
		},
		"mode and format": {
			[]string{ModeRolling, ModeParallel, FormatINI, FormatYAML},
			[]string{"rolling", "parallel", "ini", "yaml"},
		},
		"step state": {
			[]string{StepNext, StepWaiting, StepProposed, StepTyping, StepTyped, StepDone, StepFailed, StepSkipped},
			[]string{"next", "waiting", "proposed", "typing", "typed", "done", "failed", "skipped"},
		},
		"capture": {
			[]string{CaptureCaptured, CaptureNoMatch, CaptureBlocked, CaptureRefused, CaptureNoOutput, CaptureExpired, CaptureTooShort, CaptureFull},
			[]string{"captured", "no-match", "blocked", "refused", "no-output", "expired", "too-short", "full"},
		},
		"tunnel": {
			[]string{TunnelLocal, TunnelRemote, TunnelDynamic, TunnelStopped, TunnelConnecting, TunnelReady, TunnelError},
			[]string{"local", "remote", "dynamic", "stopped", "connecting", "ready", "error"},
		},
		"auth": {
			[]string{AuthLocal, AuthPassword, AuthKey, AuthIdentity},
			[]string{"local", "password", "key", "identity"},
		},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s values are %q, want %q", name, c.got, c.want)
		}
	}
}
