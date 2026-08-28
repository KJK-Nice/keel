package session

import (
	"fmt"
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

func Open(host store.Host, cols, rows int, checkKey func(hostname, fingerprint string) error) (Bridge, error) {
	if host.AuthType == "local" {
		return openLocal(cols, rows)
	}
	return openSSH(host, cols, rows, checkKey)
}

func ParseSize(colsQ, rowsQ string) (int, int) {
	cols, _ := strconv.Atoi(colsQ)
	rows, _ := strconv.Atoi(rowsQ)
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return cols, rows
}

func ConnectingMessage(h store.Host) string {
	if h.AuthType == "local" {
		return "Opening local shell…"
	}
	return fmt.Sprintf("Connecting to %s@%s…", h.Username, h.Address)
}
