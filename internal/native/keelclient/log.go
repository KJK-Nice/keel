package keelclient

import (
	"regexp"
	"strings"

	"keel/internal/wire"
)

// LogLine is one line of a pane's connection log: Kind is "user" (what the user asked for), "work" (a step) or "fail".
type LogLine struct{ Kind, Text string }

// logMax is lib/workspace.js FAIL_LOG_MAX.
const logMax = 40

var (
	// logNoise is keel serve's words that are no news (wire.Msg…), in lower case.
	logNoise = func() map[string]bool {
		m := map[string]bool{"": true}
		for _, s := range []string{wire.MsgAttached, wire.MsgAttaching, wire.MsgJoined, wire.MsgLeft, wire.MsgConnected, wire.MsgControlPassed, wire.MsgTookControl} {
			m[strings.ToLower(s)] = true
		}
		return m
	}()
	logFail = regexp.MustCompile(`(?i)fail|error|refused|timed out|timeout`)
)

// recordLog is term.js recordFailStatus and lib/workspace.js logPane: a status frame adds a line to the log. A pane that
// is ready forgets its log; a pane that starts connecting again after a failure begins a new one; noise (attached, joined,
// …) and a repeat of the last line add nothing; the last 40 lines are kept. prev is the status before this frame.
func (s *State) recordLog(prev, status, message string) {
	if wire.Status(status) == wire.StatusReady {
		s.Log = nil
		return
	}
	line := strings.Join(strings.Fields(message), " ")
	if logNoise[strings.ToLower(line)] {
		return
	}
	kind := "fail"
	switch wire.Status(status) {
	case wire.StatusConnecting:
		if p := wire.Status(prev); p == wire.StatusError || p == wire.StatusExited { // a reconnect begins a new log
			s.Log = nil
		}
		if kind = "work"; logFail.MatchString(line) {
			kind = "fail"
		}
	case wire.StatusExited, wire.StatusError:
	default:
		return
	}
	if n := len(s.Log); n > 0 && s.Log[n-1].Text == line {
		return
	}
	// A new slice each time: a State handed out earlier keeps the lines it had.
	log := append(append([]LogLine(nil), s.Log...), LogLine{kind, line})
	if len(log) > logMax {
		log = log[len(log)-logMax:]
	}
	s.Log = log
}
