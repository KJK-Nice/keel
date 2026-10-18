// Package shellmark reads shell-integration marks from a PTY's output:
// OSC 133 (FinalTerm semantic prompts: A prompt start, B command start,
// C output start, D;<exit> command end) and OSC 7 (the working
// directory). See docs/ui-redesign.md §14.
//
// Everything here comes from untrusted remote output. The parser only
// observes the stream: it never changes the bytes, never writes, and a
// mark authorizes nothing. It is strict (printable ASCII payloads, digits
// only for an exit status, caps on every length) and keeps a bounded state
// whatever it is fed, so a hostile or broken program can at worst make
// Keel record a wrong or missing command, never more.
//
// The package imports nothing from Keel, so the agent may use its types
// without reaching a PTY writer (internal/agent/invariants_test.go).
package shellmark

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits. An OSC longer than MaxPayload is skipped whole (its bytes are
// consumed, nothing is kept), so the parser never holds more than this.
const (
	MaxPayload = 2048
	MaxCwd     = 1024
	maxExit    = 1<<31 - 1
)

// Kind is what a mark says.
type Kind byte

const (
	PromptStart  Kind = 'A' // OSC 133;A: the shell is about to draw its prompt
	CommandStart Kind = 'B' // OSC 133;B: the prompt is drawn; the typed command follows
	OutputStart  Kind = 'C' // OSC 133;C: the command was accepted; its output follows
	CommandEnd   Kind = 'D' // OSC 133;D[;exit]: the command finished
	Cwd          Kind = '7' // OSC 7;file://host/path: the working directory
)

// Mark is one parsed sequence. Start is the stream offset of its ESC and
// End the offset just past its terminator (BEL or ESC \).
type Mark struct {
	Kind    Kind
	Start   uint64
	End     uint64
	Exit    int
	HasExit bool
	Cwd     string
}

const (
	stGround = iota
	stC2     // saw 0xC2 outside an OSC: C2 9D is the C1 OSC introducer
	stEsc    // saw ESC
	stOSC    // inside an OSC
	stOSCEsc // saw ESC inside an OSC
	stOSCC2  // saw 0xC2 inside an OSC: C2 9C ends it, other C1 abort it
)

// Parser finds marks in a byte stream fed in chunks of any size. A
// sequence split across chunks is found as if it came in one. The zero
// value is ready to use. It is not safe for concurrent use.
//
// It follows xterm.js's VT500 parser for OSCs, so the marks Keel records
// are the marks the terminal draws: an OSC starts with ESC ] or the C1
// code U+009D (UTF-8 C2 9D); it ends with BEL, ST (ESC \ or U+009C), or
// any other ESC (which then starts a new sequence); CAN and SUB, and the
// other C1 codes, abandon it; C0 controls inside it are dropped. The one
// deliberate difference is the length cap (MaxPayload), which the
// terminal side (js/lib/cmdmarks.js) applies too.
type Parser struct {
	state    int
	buf      []byte
	overflow bool // the payload passed MaxPayload: skip this OSC
	start    uint64
	escAt    uint64 // offset of the last ESC or 0xC2 seen
}

// Reset drops a sequence in progress (a new shell starts on a reconnect;
// half a sequence from the old one must not join the new one's bytes).
func (p *Parser) Reset() {
	p.state = stGround
	p.buf = p.buf[:0]
	p.overflow = false
}

func (p *Parser) begin(at uint64) {
	p.state = stOSC
	p.start = at
	p.buf = p.buf[:0]
	p.overflow = false
}

// Feed scans b, whose first byte is at stream offset base, and calls emit
// for every complete, valid mark in order.
func (p *Parser) Feed(b []byte, base uint64, emit func(Mark)) {
	for i := 0; i < len(b); i++ {
		c := b[i]
		at := base + uint64(i)
		switch p.state {
		case stGround:
			j := nextIntroducer(b[i:])
			if j < 0 {
				return
			}
			i += j
			p.escAt = base + uint64(i)
			if b[i] == 0x1b {
				p.state = stEsc
			} else {
				p.state = stC2
			}
		case stC2:
			if c == 0x9d {
				p.begin(p.escAt)
				continue
			}
			p.state = stGround
			i--
		case stEsc:
			switch c {
			case ']':
				p.begin(p.escAt)
			case 0x1b:
				p.escAt = at
			case 0xc2:
				p.state = stGround
				i--
			default:
				p.state = stGround
			}
		case stOSC:
			switch {
			case c == 0x07:
				p.finish(at+1, emit)
			case c == 0x1b:
				p.state = stOSCEsc
				p.escAt = at
			case c == 0xc2:
				p.state = stOSCC2
				p.escAt = at
			case c == 0x18 || c == 0x1a: // CAN, SUB abandon the sequence
				p.state = stGround
			case c < 0x20:
				// Other C0 controls are dropped, as xterm does.
			default:
				p.put(c)
			}
		case stOSCEsc:
			if c == '\\' {
				p.finish(at+1, emit)
				continue
			}
			// Any other ESC ends the OSC too (xterm ends it and reads the
			// ESC as the start of the next sequence: ESC ] inside an OSC
			// ends one and starts another).
			p.finish(p.escAt, emit)
			p.state = stEsc
			i--
		case stOSCC2:
			switch {
			case c == 0x9c: // ST
				p.finish(at+1, emit)
			case c == 0x9d: // a new OSC; the old one is abandoned
				p.begin(p.escAt)
			case c >= 0x80 && c <= 0x9f: // another C1 code abandons it
				p.state = stGround
			default: // a non-C1 character: payload, not ASCII, so invalid
				p.put(0xc2)
				p.state = stOSC
				i--
			}
		}
	}
}

// nextIntroducer is the index of the next ESC or 0xC2 (the first byte of
// the C1 OSC introducer) in b, or -1.
func nextIntroducer(b []byte) int {
	e := bytes.IndexByte(b, 0x1b)
	if e < 0 {
		e = len(b)
	}
	c := bytes.IndexByte(b[:e], 0xc2)
	if c >= 0 {
		return c
	}
	if e == len(b) {
		return -1
	}
	return e
}

func (p *Parser) put(c byte) {
	if len(p.buf) < MaxPayload {
		p.buf = append(p.buf, c)
	} else {
		p.overflow = true
	}
}

func (p *Parser) finish(end uint64, emit func(Mark)) {
	p.state = stGround
	if p.overflow {
		return
	}
	if m, ok := parsePayload(p.buf); ok {
		m.Start, m.End = p.start, end
		emit(m)
	}
}

// parsePayload reads "133;X[;params]" or "7;file://host/path" (the id may
// have leading zeros, as xterm reads it as a number). Anything else,
// including other OSCs (titles, hyperlinks, clipboard), is ignored.
func parsePayload(b []byte) (Mark, bool) {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return Mark{}, false
		}
	}
	s := string(b)
	semi := strings.IndexByte(s, ';')
	if semi < 1 || semi > 10 {
		return Mark{}, false
	}
	id := strings.TrimLeft(s[:semi], "0")
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return Mark{}, false
		}
	}
	rest := s[semi+1:]
	switch id {
	case "133":
		if rest == "" {
			return Mark{}, false
		}
		k := Kind(rest[0])
		if len(rest) > 1 && rest[1] != ';' {
			return Mark{}, false
		}
		switch k {
		case PromptStart, CommandStart, OutputStart:
			return Mark{Kind: k}, true
		case CommandEnd:
			m := Mark{Kind: k}
			if len(rest) > 2 {
				param := rest[2:]
				if i := strings.IndexByte(param, ';'); i >= 0 {
					param = param[:i]
				}
				m.Exit, m.HasExit = parseExit(param)
			}
			return m, true
		}
		return Mark{}, false
	case "7":
		cwd, ok := parseCwd(rest)
		if !ok {
			return Mark{}, false
		}
		return Mark{Kind: Cwd, Cwd: cwd}, true
	}
	return Mark{}, false
}

// parseExit takes 1–10 ASCII digits and nothing else. A sign, a space, a
// key=value or a number past int32 is no exit status.
func parseExit(s string) (int, bool) {
	if len(s) == 0 || len(s) > 10 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > maxExit {
		return 0, false
	}
	return int(n), true
}

// parseCwd reads file://host/path and returns the decoded path. The host
// is not kept. The path must decode to valid UTF-8 with no control
// characters and be at most MaxCwd bytes.
func parseCwd(s string) (string, bool) {
	const scheme = "file://"
	if len(s) <= len(scheme) || s[:len(scheme)] != scheme {
		return "", false
	}
	rest := s[len(scheme):]
	slash := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			slash = i
			break
		}
	}
	if slash < 0 {
		return "", false
	}
	path, err := url.PathUnescape(rest[slash:])
	if err != nil || len(path) > MaxCwd || !utf8.ValidString(path) {
		return "", false
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return "", false
		}
	}
	return path, true
}
