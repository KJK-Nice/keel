package session

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type localBridge struct {
	cmd  *exec.Cmd
	file *os.File
	once sync.Once
}

func openLocal(cols, rows int) (Bridge, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			shell = "/bin/bash"
		} else {
			shell = "/bin/sh"
		}
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start local pty: %w", err)
	}
	b := &localBridge{cmd: cmd, file: f}
	if err := b.Resize(cols, rows); err != nil {
		_ = b.Close()
		return nil, err
	}
	return b, nil
}

func (b *localBridge) Read(p []byte) (int, error)  { return b.file.Read(p) }
func (b *localBridge) Write(p []byte) (int, error) { return b.file.Write(p) }

func (b *localBridge) Resize(cols, rows int) error {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return pty.Setsize(b.file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (b *localBridge) Close() error {
	var err error
	b.once.Do(func() {
		_ = b.file.Close()
		if b.cmd.Process != nil {
			err = b.cmd.Process.Kill()
			_ = b.cmd.Wait()
		}
	})
	return err
}

var _ io.ReadWriter = (*localBridge)(nil)
