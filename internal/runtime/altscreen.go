package runtime

import "strings"

// altScreen follows whether a pane shows the terminal's alternate screen
// buffer (vim, less, htop, a pager): DECSET / DECRST of private modes 47,
// 1047 and 1049 in the output. keel serve reads it before typing a command
// without the approve dialog (docs/ui-redesign.md §6 rule 13): there the
// bytes would be keystrokes for that program, not a command line.
//
// It parses only CSI ? Pm h / l (CSI as ESC [ or as the 8-bit U+009B,
// which xterm.js reads in UTF-8 as c2 9b) and RIS (ESC c, a full reset
// back to the normal screen), and keeps its state across writes, so a
// sequence split over two reads is still seen. Anything else is skipped.
// It fails closed: a private mode sequence too long to read counts as
// entering the alternate screen. It is fed under Session.mu, with the
// scrollback ring.
type altScreen struct {
	on    bool
	state uint8
	param []byte
}

const (
	altGround uint8 = iota
	altEsc          // after ESC
	altCSI          // after ESC [
	altPriv         // after ESC [ ?
	altC2           // after the UTF-8 lead byte c2 (U+0080–U+00BF)
)

// maxAltParam bounds the parameter bytes kept for one sequence: a longer
// one is not a mode set this tracker reads.
const maxAltParam = 32

func (a *altScreen) Feed(b []byte) {
	for _, c := range b {
		if a.state == altC2 {
			a.state = altGround
			if c == 0x9b {
				a.state = altCSI
				continue
			}
		}
		switch a.state {
		case altGround:
			switch c {
			case 0x1b:
				a.state = altEsc
			case 0xc2:
				a.state = altC2
			}
		case altEsc:
			switch c {
			case '[':
				a.state = altCSI
			case 'c':
				// RIS: xterm resets to the normal screen.
				a.on, a.state = false, altGround
			case 0x1b:
				// ESC ESC: still after an ESC.
			default:
				a.state = altGround
			}
		case altCSI:
			if c == '?' {
				a.state, a.param = altPriv, a.param[:0]
				continue
			}
			a.state = altGround
			switch c {
			case 0x1b:
				a.state = altEsc
			case 0xc2:
				a.state = altC2
			}
		case altPriv:
			switch {
			case c >= '0' && c <= '9' || c == ';' || c == ':':
				if len(a.param) < maxAltParam {
					a.param = append(a.param, c)
				} else {
					// Too long to read: it may set the alternate
					// screen, which xterm.js honours. Fail closed.
					a.on, a.state = true, altGround
				}
			case c == 'h' || c == 'l':
				if altModes(a.param) {
					a.on = c == 'h'
				}
				a.state = altGround
			case c == 0x1b:
				a.state = altEsc
			case c == 0xc2:
				a.state = altC2
			default:
				// Anything else in a sequence naming 47, 1047 or 1049 is
				// not read here; xterm.js may still act on it. Fail closed.
				if altModes(a.param) {
					a.on = true
				}
				a.state = altGround
			}
		}
	}
}

// Reset is a new shell: it starts on the normal screen.
func (a *altScreen) Reset() {
	a.on, a.state, a.param = false, altGround, a.param[:0]
}

// altModes reports whether a parameter list names an alternate screen mode.
// Each parameter is read as xterm.js reads it: its number before any ':'
// sub-parameter, leading zeros and all ("01049:1" is 1049).
func altModes(p []byte) bool {
	for _, part := range strings.Split(string(p), ";") {
		n, _, _ := strings.Cut(part, ":")
		switch strings.TrimLeft(n, "0") {
		case "47", "1047", "1049":
			return true
		}
	}
	return false
}
