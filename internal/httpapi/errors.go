package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/hostimport"
	"keel/internal/runtime"
	"keel/internal/share"
	"keel/internal/store"
	"keel/internal/wire"
)

// One error policy for every route: writeErr is the only place that turns
// an error into a status and a body. The error's type decides. A route
// that needs its own words for an error says so on the error value
// (worded), and one whose callee reports plain errors names the status
// they get (otherwise); neither writes a status itself.

// rule answers every error that is err (errors.Is).
type rule struct {
	err    error
	status int
	// reason is the body's "reason", the machine word a client acts on.
	reason string
	// words is the body's "error"; "" is the error's own text, with tail
	// after it.
	words, tail string
}

// rules are checked in order; the first match answers.
var rules = []rule{
	// Group runs, notebooks, notes and runbooks of the agent.
	{err: service.ErrNoGroupRun, status: http.StatusNotFound},
	{err: service.ErrModeLocked, status: http.StatusConflict},
	{err: service.ErrTyping, status: http.StatusConflict},
	{err: service.ErrResultPending, status: http.StatusConflict},
	{err: service.ErrTooManyRuns, status: http.StatusConflict},
	{err: service.ErrNotebookClosed, status: http.StatusConflict},
	{err: store.ErrNotebookConflict, status: http.StatusConflict, reason: "conflict", words: "The notebook changed since then. Open History on the notebook to restore an earlier version."},
	{err: store.ErrNotebookPrivate, status: http.StatusForbidden, reason: "private", words: "This notebook is private now. Keel did not change it."},
	{err: service.ErrNotebookExpired, status: http.StatusGone},
	{err: store.ErrHistoryUnavailable, status: http.StatusConflict, reason: "history"},
	{err: service.ErrNoNotebooks, status: http.StatusServiceUnavailable},
	{err: service.ErrNoteClosed, status: http.StatusConflict},
	{err: service.ErrNoteRefused, status: http.StatusForbidden},
	{err: service.ErrNoteNotReadable, status: http.StatusForbidden},
	{err: service.ErrNoteExpired, status: http.StatusGone},
	{err: service.ErrNoNotes, status: http.StatusServiceUnavailable},
	{err: service.ErrRunbookClosed, status: http.StatusConflict},
	{err: service.ErrStepNotCurrent, status: http.StatusConflict},
	{err: service.ErrNoRun, status: http.StatusConflict},
	{err: service.ErrRunbookExpired, status: http.StatusGone},
	{err: service.ErrNothingToDraft, status: http.StatusUnprocessableEntity},
	{err: service.ErrNoRunbooks, status: http.StatusServiceUnavailable},
	{err: store.ErrMemoryUnreadable, status: http.StatusConflict, tail: "; Delete all notes in Settings · Agent starts a fresh file"},
	{err: service.ErrGroupVarSecret, status: http.StatusBadRequest, words: strings.TrimPrefix(service.ErrGroupVarSecret.Error(), "agent: ")},

	// The agent's conversations and keel mcp.
	{err: service.ErrNoKey, status: http.StatusPreconditionFailed, reason: "no-key"},
	{err: service.ErrBadRequest, status: http.StatusBadRequest},
	{err: service.ErrNotProvider, status: http.StatusBadRequest},
	{err: service.ErrBusy, status: http.StatusConflict},
	{err: service.ErrNotFound, status: http.StatusNotFound},
	{err: service.ErrNoPaneList, status: http.StatusServiceUnavailable},
	{err: agent.ErrNotReadable, status: http.StatusForbidden},
	// The client stopped waiting; nobody reads this.
	{err: context.Canceled, status: http.StatusRequestTimeout, words: "ask cancelled"},

	// The vault and what it holds.
	{err: store.ErrLocked, status: http.StatusLocked, words: "vault locked"},
	{err: store.ErrPassphrase, status: http.StatusForbidden, words: "incorrect passphrase"},
	{err: store.ErrVaultExists, status: http.StatusConflict},
	{err: store.ErrNotFound, status: http.StatusNotFound, words: "not found"},
	{err: store.ErrLocalDuplicate, status: http.StatusBadRequest},
	{err: store.ErrGroupCycle, status: http.StatusBadRequest},
	{err: store.ErrGroupDepth, status: http.StatusBadRequest},
	{err: store.ErrHostField, status: http.StatusBadRequest},
	{err: store.ErrIdentityField, status: http.StatusBadRequest},
	{err: store.ErrAgentSettings, status: http.StatusBadRequest},
	{err: store.ErrAgentKey, status: http.StatusBadRequest, words: "the key is empty, too long, or has spaces"},
	{err: store.ErrRole, status: http.StatusBadRequest},
	{err: store.ErrNotebookName, status: http.StatusBadRequest},
	{err: store.ErrNoOffer, status: http.StatusConflict},

	// Sessions and sharing.
	{err: runtime.ErrNotFound, status: http.StatusNotFound},
	{err: share.ErrNotShared, status: http.StatusNotFound},
	{err: share.ErrNotResponding, status: http.StatusServiceUnavailable},
	{err: share.ErrNoRequest, status: http.StatusConflict},
}

// inputError is a problem with what the user sent: 400 with its message.
type inputError struct{ msg string }

func (e inputError) Error() string { return e.msg }

func badInput(msg string) error { return inputError{msg} }

// wordedError is an error with a route's own words for it.
type wordedError struct {
	error
	words string
}

func (e wordedError) Unwrap() error { return e.error }

// worded gives err the words a route answers with when it is target. The
// status and the reason stay the rule's.
func worded(err, target error, words string) error {
	if err != nil && errors.Is(err, target) {
		return wordedError{err, words}
	}
	return err
}

// fallbackError is an error with the status it gets when no rule knows it.
type fallbackError struct {
	error
	status int
}

func (e fallbackError) Unwrap() error { return e.error }

// otherwise names the status err gets when no rule knows it, in place of
// 500: for a callee that reports what the user asked for as a plain error
// (400 or 409), or one that fails on a far server (502).
func otherwise(err error, status int) error {
	if err == nil {
		return nil
	}
	return fallbackError{err, status}
}

func answerFor(err error) (int, any) {
	if key, ok := store.RefusedHostKey(err); ok {
		return http.StatusConflict, struct {
			Error string `json:"error"`
			wire.HostKey
		}{err.Error(), key}
	}
	var (
		in      inputError
		say     wordedError
		other   fallbackError
		prov    *service.ProviderError
		impReq  *hostimport.RequestError
		impJump *hostimport.JumpError
		impKey  *hostimport.KeyError
	)
	switch {
	case errors.As(err, &in):
		return http.StatusBadRequest, map[string]any{"error": in.msg}
	case errors.As(err, &prov):
		return http.StatusBadGateway, map[string]any{"error": prov.Msg, "reason": "provider"}
	case errors.As(err, &impReq):
		return http.StatusBadRequest, map[string]any{"error": impReq.Msg}
	case errors.As(err, &impJump):
		return http.StatusBadRequest, map[string]any{"error": impJump.Error(), "errorKind": "jump", "hosts": impJump.Hosts}
	case errors.As(err, &impKey):
		return http.StatusBadRequest, map[string]any{"error": impKey.Msg, "errorKind": impKey.Kind, "key": impKey.Path}
	}
	status, body := http.StatusInternalServerError, map[string]any{"error": err.Error()}
	if errors.As(err, &other) {
		status = other.status
	}
	for _, r := range rules {
		if !errors.Is(err, r.err) {
			continue
		}
		status = r.status
		if r.words != "" {
			body["error"] = r.words
		}
		if r.tail != "" {
			body["error"] = err.Error() + r.tail
		}
		if r.reason != "" {
			body["reason"] = r.reason
		}
		if errors.As(err, &say) {
			body["error"] = say.words
		}
		break
	}
	return status, body
}

func writeErr(w http.ResponseWriter, err error) {
	status, body := answerFor(err)
	writeJSON(w, status, body)
}
