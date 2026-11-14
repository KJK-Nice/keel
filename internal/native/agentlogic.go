package native

import (
	"crypto/rand"
	"fmt"
	"strings"
	"unicode/utf8"

	"keel/internal/native/keelclient"
)

// The agent panel's pure rules: scope, counts, the bytes as the card draws them, the trace, and what a proposal's card
// offers. They are cmd/keel/web/js/lib/agent.js (pinned there by test/agent.test.mjs and here by agentlogic_test.go),
// (the notebook and runbook words are agentnotebookslogic.go and runbookslogic.go); the saved-conversation rules are
// agentconvlogic.go.
//
// Nothing here types. typeOffer and typeGate only decide whether the card shows Type into pane and whether a press may
// open the Approve before type dialog; keel serve checks the same rules again before it writes (agent service
// AuthorizeType).

// agentPane is a pane as the panel sees it (term.js agentPanes): the session, how the picker and the card name it, and
// what the rules need. p and t are the window's own pane and tab; the rules never read them.
type agentPane struct {
	sessionID, label, sub                                       string
	focused, inActiveTab, readable, production, driving, remote bool

	p *pane
	t *tab
}

// scopeLabel is the chip text: "Reads 2 of 3 panes".
func scopeLabel(n, m int) string {
	noun := "panes"
	if m == 1 {
		noun = "pane"
	}
	return fmt.Sprintf("Reads %d of %d %s", n, m, noun)
}

// candidatePanes are the panes the panel counts (M): local terminal panes with a session. A joined pane is never in
// scope (§5.7).
func candidatePanes(ps []agentPane) (out []agentPane) {
	for _, p := range ps {
		if p.sessionID != "" && !p.remote {
			out = append(out, p)
		}
	}
	return out
}

// defaultScope picks the panes of a new conversation from the setting: "focused" (the focused pane), "tab" (every pane
// of the active tab) or "pick" (none until the user picks). A host the agent cannot read is never picked.
func defaultScope(mode string, ps []agentPane) []string {
	var ok []agentPane
	for _, p := range candidatePanes(ps) {
		if p.readable {
			ok = append(ok, p)
		}
	}
	out := []string{}
	switch mode {
	case "pick":
	case "tab":
		for _, p := range ok {
			if p.inActiveTab {
				out = append(out, p.sessionID)
			}
		}
	default:
		for _, p := range ok {
			if p.focused {
				return []string{p.sessionID}
			}
		}
	}
	return out
}

// reconcileScope keeps the chosen ids that still name a readable pane, once each, in their order.
func reconcileScope(ids []string, ps []agentPane) []string {
	ok := map[string]bool{}
	for _, p := range candidatePanes(ps) {
		if p.readable {
			ok[p.sessionID] = true
		}
	}
	out := []string{}
	for _, id := range ids {
		if ok[id] && !contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// toggleScope adds or removes one pane. A pane the agent cannot read, or a joined one, is never added.
func toggleScope(ids []string, p *agentPane) []string {
	out := append([]string{}, ids...)
	switch {
	case p == nil:
		return out
	case contains(ids, p.sessionID):
		out = out[:0]
		for _, id := range ids {
			if id != p.sessionID {
				out = append(out, id)
			}
		}
		return out
	case !p.readable || p.remote:
		return out
	}
	return append(out, p.sessionID)
}

// lineCount counts lines the way keel serve logs them: a trailing newline ends the last line, it does not start one.
func lineCount(bytes string) int {
	s := strings.TrimRight(bytes, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// countLabel is the card's "1 line · 20 bytes".
func countLabel(bytes string) string {
	l, b := lineCount(bytes), len(bytes)
	ln, bn := "lines", "bytes"
	if l == 1 {
		ln = "line"
	}
	if b == 1 {
		bn = "byte"
	}
	return fmt.Sprintf("%d %s · %d %s", l, ln, b, bn)
}

// bytesRun is a stretch of the bytes Type sends: plain text, or a control character drawn as a glyph (k-ctl).
type bytesRun struct {
	text string
	ctl  bool
}

var ctlGlyphs = map[rune]string{'\n': "↵", '\t': "⇥", '\r': "⏎", 0x1b: "⎋", 0x7f: "⌫"}

// bytesRuns shows the exact bytes Type will send, every control character as a visible glyph: ↵ newline, ⇥ tab, ⎋ escape,
// ^X for the rest. A newline still breaks the line after its glyph, and the last one does not start another (agent.js
// bytesHTML).
func bytesRuns(bytes string) (out []bytesRun) {
	plain := func(r rune) {
		if n := len(out); n > 0 && !out[n-1].ctl {
			out[n-1].text += string(r)
			return
		}
		out = append(out, bytesRun{text: string(r)})
	}
	for _, r := range bytes {
		switch g, ok := ctlGlyphs[r]; {
		case ok:
			out = append(out, bytesRun{text: g, ctl: true})
			if r == '\n' {
				out = append(out, bytesRun{text: "\n"})
			}
		case r < 0x20:
			out = append(out, bytesRun{text: "^" + string(r+64), ctl: true})
		case r >= 0x80 && r < 0xa0:
			out = append(out, bytesRun{text: fmt.Sprintf(`\x%x`, r), ctl: true})
		default:
			plain(r)
		}
	}
	if n := len(out); n > 0 && !out[n-1].ctl && out[n-1].text == "\n" {
		out = out[:n-1]
	}
	return out
}

// bytesText is bytesRuns as one string (for tests and the accessible label).
func bytesText(bytes string) string {
	var b strings.Builder
	for _, r := range bytesRuns(bytes) {
		b.WriteString(r.text)
	}
	return b.String()
}

// riskBadge is the card's kind: Read-only, Changes state or Destructive. An unknown kind is never shown as read-only.
func riskBadge(risk string) (label, kind string) {
	switch risk {
	case keelclient.RiskReadOnly:
		return "Read-only", "tide"
	case keelclient.RiskDestructive:
		return "Destructive", "flare"
	}
	return "Changes state", "signal"
}

// changesState reports a rated command that is not read-only; an unrated one ("") is not counted.
func changesState(risk string) bool { return risk != "" && risk != keelclient.RiskReadOnly }

func redactedNote(counts map[string]int) string {
	held, n := 0, 0
	for kind, c := range counts { // captured values and group variables went as {{name}}
		if kind == "captured" || kind == "variable" {
			held += c
		} else {
			n += c
		}
	}
	out := ""
	if n > 0 {
		out = fmt.Sprintf(" · %d redacted", n)
	}
	if held > 0 {
		noun := "values"
		if held == 1 {
			noun = "value"
		}
		out += fmt.Sprintf(" · %d %s withheld by Keel", held, noun)
	}
	return out
}

func lines(n int) string {
	if n == 1 {
		return "line"
	}
	return "lines"
}

// traceText is one line of the trace: what the agent read, or why not.
func traceText(s keelclient.TraceStep) string {
	switch s.Status {
	case "notes":
		switch s.NoteKind {
		case "user":
			return fmt.Sprintf("Used your notes (%d)", s.Notes)
		case "group":
			return fmt.Sprintf("Used group notes · %s (%d)", orElse(s.Host, "a group"), s.Notes)
		}
		return fmt.Sprintf("Used host notes · %s (%d)", orElse(s.Host, "a host"), s.Notes)
	}
	if isNotebookStep(s) { // §18: the titles sent, a notebook sent or read, or why not
		return notebookStepText(s)
	}
	if s.Status == "runbook" {
		return "Used runbook · " + orElse(s.Runbook, "a runbook")
	}
	host := orElse(s.Host, "a pane")
	switch {
	case s.Status == "read" && s.Command != "": // from the shell's OSC 133 marks
		return fmt.Sprintf("Read %s · last command, %s · %d %s of output%s", host, s.Command, s.Lines, lines(s.Lines), redactedNote(s.Redactions))
	case s.Status == "read":
		return fmt.Sprintf("Read %s · last %d %s%s", host, s.Lines, lines(s.Lines), redactedNote(s.Redactions))
	case s.Status != "skipped":
		return "Used " + s.Status // a step this client has no words for: shown, never hidden
	}
	switch s.Reason {
	case "not-readable":
		return fmt.Sprintf("Skipped %s · the agent can't read this host", host)
	case "not-open":
		return "Skipped a pane · it is closed"
	}
	return fmt.Sprintf("Skipped %s · could not read it", host)
}

// What a proposal's card offers for its target pane.
const (
	offerType        = "type"              // Type into pane
	offerShift       = "needs-shift-offer" // Type into pane, but only with ⇧ (destructive, or changes state on a production host)
	offerFollowing   = "following"         // no Type: you follow that pane (§6.6)
	offerClosed      = "closed"            // no Type: the pane is not open in this window
	offerNone        = "none"              // no Type: the proposal names no pane
	offerExpired     = "expired"           // no Type: a saved proposal from an earlier run
	offerPlaceholder = "placeholder"       // no Type: it holds [secret:N], which a shell would read as a pattern
)

// typeOffer is agent.js typeOffer.
func typeOffer(p keelclient.Proposal, pane *agentPane) string {
	switch {
	case p.Expired:
		return offerExpired
	case p.Placeholders:
		return offerPlaceholder
	case p.SessionID == "":
		return offerNone
	case pane == nil:
		return offerClosed
	case pane.remote:
		return offerFollowing
	case !pane.driving:
		return offerFollowing
	case p.NeedsShift:
		return offerShift
	}
	return offerType
}

// gateShift is typeGate's reason when a proposal needs ⇧ and it was not held.
const gateShift = "needs-shift"

// typeGate says whether a Type press may open the Approve before type dialog, and if not why (agent.js typeGate).
func typeGate(p keelclient.Proposal, pane *agentPane, shift bool) (ok bool, reason string) {
	switch offer := typeOffer(p, pane); {
	case offer == offerType:
		return true, ""
	case offer == offerShift && shift:
		return true, ""
	case offer == offerShift:
		return false, gateShift
	default:
		return false, offer
	}
}

// shiftReason says why a proposal needs ⇧.
func shiftReason(p keelclient.Proposal) string {
	if p.Risk == keelclient.RiskDestructive {
		return "Destructive"
	}
	return "Changes state on a production host"
}

// atTrigger reports whether the text ends with an @ that starts a word: "@" alone or after a space or newline opens the
// pane picker; an @ inside a word (user@host) does not.
func atTrigger(s string) bool {
	if !strings.HasSuffix(s, "@") {
		return false
	}
	rest := s[:len(s)-1]
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(rest)
	return r == ' ' || r == '\n' || r == '\t' || r == '\r'
}

// newConversationID makes an id for keel serve's in-memory history: it matches the server's ^[A-Za-z0-9_-]{8,64}$.
func newConversationID() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 19)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "c" + string(b)
}

// cutNotice is the line under an answer the provider cut before the model finished it: a cut answer is never shown as
// if it were whole. "" for a finished one.
func cutNotice(cut string) string {
	switch cut {
	case "":
		return ""
	case "length":
		return "Answer cut off at the length limit. Ask the agent to continue."
	case "safety":
		return "Answer cut off by the provider's safety filter."
	case "recitation":
		return "Answer cut off by the provider: it matched a published source."
	}
	return "Answer cut off by the provider before it ended."
}

func orElse(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
