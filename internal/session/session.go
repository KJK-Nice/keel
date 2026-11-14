package session

import (
	"io"
	"strconv"

	"keel/internal/store"
)

type Bridge interface {
	io.ReadWriter
	Resize(cols, rows int) error
	Close() error
}

type ExitKind int

const (
	ExitUnknown ExitKind = iota
	ExitClean
	ExitDrop
)

func BridgeExit(br Bridge) ExitKind {
	if x, ok := br.(interface{ ExitKind() ExitKind }); ok {
		return x.ExitKind()
	}
	return ExitUnknown
}

func Open(host store.Host, cols, rows int, checkKey CheckKey) (Bridge, error) {
	if host.IsLocal() {
		return openLocal(cols, rows)
	}
	return openSSH(host, cols, rows, checkKey)
}

func ParseSize(colsQ, rowsQ string) (int, int) {
	cols, _ := strconv.Atoi(colsQ)
	rows, _ := strconv.Atoi(rowsQ)
	return ClampSize(cols, rows)
}

const maxTerminalDim = 1000

// ClampSize defaults a non-positive size to 80x24 and caps each side so it
// fits the uint16 the PTY and SSH window-change calls take.
func ClampSize(cols, rows int) (int, int) {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return min(cols, maxTerminalDim), min(rows, maxTerminalDim)
}
