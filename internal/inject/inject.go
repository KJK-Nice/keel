// Package inject is the PTY input seam: live keystrokes only when the
// terminal is focused, and scripted inject only after explicit approve.
package inject

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"
)

var (
	ErrNotApproved      = errors.New("inject: write requires explicit approve")
	ErrNotTerminalFocus = errors.New("keystroke: focused surface is not the terminal")
	ErrBadTicket        = errors.New("inject: missing or spent approve ticket")
)

const ticketTTL = 60 * time.Second

// Surface is the UI that currently owns keyboard focus.
type Surface string

const (
	SurfaceTerm    Surface = "term"
	SurfacePalette Surface = "palette"
	SurfaceChrome  Surface = "chrome"
)

func CanType(focused Surface) error {
	if focused != SurfaceTerm {
		return ErrNotTerminalFocus
	}
	return nil
}

func Keystroke(w io.Writer, p []byte, focused Surface) (int, error) {
	if err := CanType(focused); err != nil {
		return 0, err
	}
	if w == nil {
		return 0, errors.New("inject: missing writer")
	}
	return w.Write(p)
}

// Approval is an explicit user confirmation. The zero value is not approved.
type Approval struct {
	ok bool
}

func Approve() Approval {
	return Approval{ok: true}
}

func Guard(a Approval) error {
	if !a.ok {
		return ErrNotApproved
	}
	return nil
}

type ticket struct {
	nonce   string
	command string
	client  uint64
	exp     time.Time
}

// Issuer mints one-shot approve tickets. WS inject must Consume a ticket
// the server issued; a client approved:true flag is not enough.
type Issuer struct {
	mu      sync.Mutex
	pending map[string]ticket
	latest  map[uint64]string
}

func NewIssuer() *Issuer {
	return &Issuer{pending: map[string]ticket{}, latest: map[uint64]string{}}
}

func (i *Issuer) Issue(clientID uint64, command string) (string, error) {
	if i == nil {
		return "", ErrBadTicket
	}
	if command == "" {
		return "", ErrNotApproved
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(raw[:])
	now := time.Now()
	i.mu.Lock()
	defer i.mu.Unlock()
	if old := i.latest[clientID]; old != "" {
		delete(i.pending, old)
	}
	i.pending[nonce] = ticket{nonce: nonce, command: command, client: clientID, exp: now.Add(ticketTTL)}
	i.latest[clientID] = nonce
	return nonce, nil
}

func (i *Issuer) Consume(clientID uint64, command, nonce string) (Approval, error) {
	if nonce == "" {
		return Approval{}, ErrNotApproved
	}
	if i == nil {
		return Approval{}, ErrBadTicket
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	t, ok := i.pending[nonce]
	if ok {
		delete(i.pending, nonce)
		if i.latest[t.client] == nonce {
			delete(i.latest, t.client)
		}
	}
	if !ok || t.client != clientID || t.command != command || time.Now().After(t.exp) {
		return Approval{}, ErrBadTicket
	}
	return Approval{ok: true}, nil
}

func Write(w io.Writer, p []byte, a Approval) (int, error) {
	if err := Guard(a); err != nil {
		return 0, err
	}
	if w == nil {
		return 0, errors.New("inject: missing writer")
	}
	return w.Write(p)
}
