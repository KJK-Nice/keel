package applog

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
)

const maxEntries = 2000

type Entry struct {
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	HostID    string    `json:"hostId,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	TunnelID  string    `json:"tunnelId,omitempty"`
	// Lines, Bytes and Redactions describe agent events (agent.read,
	// agent.suggest, agent.insert, agent.type): how much was read or
	// inserted and how many values of each kind were redacted. Content is
	// never logged.
	Lines      int            `json:"lines,omitempty"`
	Bytes      int            `json:"bytes,omitempty"`
	Redactions map[string]int `json:"redactions,omitempty"`
	// Source says where an agent suggestion came from ("generated" by the
	// model provider; later "history" or "snippet"), or, on agent.read,
	// which provider the text was sent to ("jev", "gemini"). Count is how
	// many lines or commands one agent.read entry stands for (detection
	// reads are summarised per pane per minute).
	Source string `json:"source,omitempty"`
	Count  int    `json:"count,omitempty"`
}

type Book struct {
	path string
	mu   sync.Mutex
	buf  []Entry
}

func New(path string) *Book {
	b := &Book{path: path}
	b.load()
	return b
}

func (b *Book) load() {
	f, err := os.Open(b.path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		b.buf = append(b.buf, e)
	}
	if len(b.buf) > maxEntries {
		b.buf = append([]Entry(nil), b.buf[len(b.buf)-maxEntries:]...)
	}
}

func (b *Book) Append(e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, e)
	if len(b.buf) > maxEntries {
		b.buf = append([]Entry(nil), b.buf[len(b.buf)-maxEntries:]...)
	}
	f, err := os.OpenFile(b.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	enc := json.NewEncoder(f)
	_ = enc.Encode(e)
	_ = f.Close()
}

func (b *Book) Recent(limit int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > len(b.buf) {
		limit = len(b.buf)
	}
	out := make([]Entry, limit)
	src := b.buf[len(b.buf)-limit:]
	for i := range src {
		out[limit-1-i] = src[i]
	}
	return out
}
