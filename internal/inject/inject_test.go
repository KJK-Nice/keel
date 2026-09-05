package inject

import (
	"bytes"
	"errors"
	"testing"
)

type denyWriter struct{}

func (denyWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer should not be called")
}

func TestWriteWithoutApproveFailsAndDoesNotTouchWriter(t *testing.T) {
	n, err := Write(denyWriter{}, []byte("rm -rf /\n"), Approval{})
	if !errors.Is(err, ErrNotApproved) {
		t.Fatalf("err = %v", err)
	}
	if n != 0 {
		t.Fatalf("n = %d", n)
	}
}

func TestWriteWithApproveSendsBytes(t *testing.T) {
	var buf bytes.Buffer
	n, err := Write(&buf, []byte("ls\n"), Approve())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || buf.String() != "ls\n" {
		t.Fatalf("n=%d buf=%q", n, buf.String())
	}
}

func TestGuardZeroValue(t *testing.T) {
	if err := Guard(Approval{}); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("zero approval err = %v", err)
	}
	if err := Guard(Approve()); err != nil {
		t.Fatal(err)
	}
}

func TestKeystrokeFromNonTerminalDoesNotTouchWriter(t *testing.T) {
	for _, focused := range []Surface{"", SurfacePalette, SurfaceChrome} {
		n, err := Keystroke(denyWriter{}, []byte("i"), focused)
		if !errors.Is(err, ErrNotTerminalFocus) {
			t.Fatalf("focused %q err = %v", focused, err)
		}
		if n != 0 {
			t.Fatalf("focused %q n = %d", focused, n)
		}
	}
}

func TestKeystrokeFromTermSendsBytes(t *testing.T) {
	var buf bytes.Buffer
	n, err := Keystroke(&buf, []byte("i"), SurfaceTerm)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || buf.String() != "i" {
		t.Fatalf("n=%d buf=%q", n, buf.String())
	}
}

func TestPaletteCannotSilentlyInjectOrType(t *testing.T) {
	n, err := Keystroke(denyWriter{}, []byte("rm\n"), SurfacePalette)
	if !errors.Is(err, ErrNotTerminalFocus) {
		t.Fatalf("palette keystroke err = %v", err)
	}
	if n != 0 {
		t.Fatalf("n = %d", n)
	}
	n, err = Write(denyWriter{}, []byte("rm\n"), Approval{})
	if !errors.Is(err, ErrNotApproved) {
		t.Fatalf("unapproved inject err = %v", err)
	}
	if n != 0 {
		t.Fatalf("n = %d", n)
	}
}

func TestIssuerRejectsEmptyNonceAndReplay(t *testing.T) {
	iss := NewIssuer()
	if _, err := iss.Consume(1, "ls\n", ""); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("empty nonce err = %v", err)
	}
	n, err := Write(denyWriter{}, []byte("ls\n"), Approval{})
	if !errors.Is(err, ErrNotApproved) || n != 0 {
		t.Fatalf("client-only approve n=%d err=%v", n, err)
	}

	nonce, err := iss.Issue(1, "ls\n")
	if err != nil || nonce == "" {
		t.Fatalf("issue nonce=%q err=%v", nonce, err)
	}
	a, err := iss.Consume(1, "ls\n", nonce)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Write(&buf, []byte("ls\n"), a); err != nil || buf.String() != "ls\n" {
		t.Fatalf("first consume buf=%q err=%v", buf.String(), err)
	}
	if _, err := iss.Consume(1, "ls\n", nonce); !errors.Is(err, ErrBadTicket) {
		t.Fatalf("replay err = %v", err)
	}

	nonce, err = iss.Issue(1, "ls\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.Consume(1, "rm\n", nonce); !errors.Is(err, ErrBadTicket) {
		t.Fatalf("command mismatch err = %v", err)
	}
	if _, err := iss.Consume(1, "ls\n", nonce); !errors.Is(err, ErrBadTicket) {
		t.Fatalf("mismatch must spend the ticket, err = %v", err)
	}
}
