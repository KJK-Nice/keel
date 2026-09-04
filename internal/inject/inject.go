// Package inject is the approve-before-type seam for PTY writes that are
// not live keystrokes (palette, snippets, scripted inject). Zero-value
// Approval is denied; callers must pass Approve() from an explicit confirm.
package inject

import (
	"errors"
	"io"
)

var ErrNotApproved = errors.New("inject: write requires explicit approve")

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

func Write(w io.Writer, p []byte, a Approval) (int, error) {
	if err := Guard(a); err != nil {
		return 0, err
	}
	if w == nil {
		return 0, errors.New("inject: missing writer")
	}
	return w.Write(p)
}
