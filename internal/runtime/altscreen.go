package runtime

// altScreen follows whether a pane shows the terminal's alternate screen
// buffer (vim, less, htop, a pager): DECSET / DECRST of private modes 47,
// 1047 and 1049 in the output. keel serve reads it before typing a command
// without the approve dialog (docs/ui-redesign.md §6 rule 13): there the
// bytes would be keystrokes for that program, not a command line.
//
// It parses only CSI ? Pm h / l and keeps its state across writes, so a
// sequence split over two reads is still seen. Anything else is skipped.
// It is fed under Session.mu, with the scrollback ring.
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
)

// maxAltParam bounds the parameter bytes kept for one sequence: a longer
// one is not a mode set this tracker reads.
const maxAltParam = 32

func (a *altScreen) Feed(b []byte) {
	for _, c := range b {
		switch a.state {
		case altGround:
			if c == 0x1b {
				a.state = altEsc
			}
		case altEsc:
			switch c {
			case '[':
				a.state = altCSI
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
			if c == 0x1b {
				a.state = altEsc
			}
		case altPriv:
			switch {
			case c >= '0' && c <= '9' || c == ';':
				if len(a.param) < maxAltParam {
					a.param = append(a.param, c)
				} else {
					a.state = altGround
				}
			case c == 'h' || c == 'l':
				if altModes(a.param) {
					a.on = c == 'h'
				}
				a.state = altGround
			case c == 0x1b:
				a.state = altEsc
			default:
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
func altModes(p []byte) bool {
	start := 0
	for i := 0; i <= len(p); i++ {
		if i < len(p) && p[i] != ';' {
			continue
		}
		switch string(p[start:i]) {
		case "47", "1047", "1049":
			return true
		}
		start = i + 1
	}
	return false
}
