package session

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// FileReader is a bridge that can read a file on its host: the SSH bridge, over a new channel of the
// session's own connection. A local session has none; its files are on this machine.
type FileReader interface {
	ReadFile(name string, max int64) ([]byte, error)
}

// ErrTooLarge is a file over the limit ReadFile was given.
var ErrTooLarge = errors.New("the file is too large to copy")

// RemotePath is name made absolute against dir (the pane's folder from OSC 7) for the remote cat. A path from
// home (~/) stays one; a relative name without a dir is refused, since the exec channel starts in home, not in
// the pane's folder, and would read another file.
func RemotePath(name, dir string) (string, error) {
	switch {
	case name == "" || strings.ContainsRune(name, 0):
		return "", errors.New("no file named")
	case name == "~" || strings.HasPrefix(name, "~/") || path.IsAbs(name):
		return name, nil
	case dir == "":
		return "", errors.New("the shell does not report its folder (OSC 7), so a relative path cannot be found")
	}
	return path.Join(dir, name), nil
}

// catCommand is the remote command that prints name: quoted, so nothing in it runs, and with ~/ kept as $HOME.
func catCommand(name string) string {
	if rest, ok := strings.CutPrefix(name, "~/"); ok {
		return `cat -- "$HOME"/` + shellQuote(rest)
	}
	return "cat -- " + shellQuote(name)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (b *sshBridge) ReadFile(name string, max int64) ([]byte, error) {
	ss, err := b.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	out, err := ss.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	ss.Stderr = &limitWriter{w: &stderr, n: 512}
	if err := ss.Start(catCommand(name)); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(out, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	if err := ss.Wait(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(strings.TrimPrefix(msg, "cat: "))
		}
		return nil, fmt.Errorf("cat: %w", err)
	}
	return data, nil
}

// limitWriter keeps the first n bytes and drops the rest without failing the write.
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if k := min(len(p), l.n); k > 0 {
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
