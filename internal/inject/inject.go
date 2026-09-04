// Package inject is the PTY input seam: live keystrokes only when the
// terminal is focused, and scripted inject only after explicit approve.
package inject

import (
	"errors"
	"io"
)

var (
	ErrNotApproved        = errors.New("inject: write requires explicit approve")
	ErrNotTerminalFocus   = errors.New("keystroke: focused surface is not the terminal")
)

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

func Write(w io.Writer, p []byte, a Approval) (int, error) {
	if err := Guard(a); err != nil {
		return 0, err
	}
	if w == nil {
		return 0, errors.New("inject: missing writer")
	}
	return w.Write(p)
}
