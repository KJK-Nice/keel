package providertest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"keel/internal/agent/provider"
)

// Scripted is the test provider `keel serve` uses when started with
// KEEL_AGENT_PROVIDER=fake (end-to-end tests only). It sends nothing
// anywhere and has no way to: it builds a canned reply from the request it
// is given, in this process, and keeps only the last few requests in
// memory for Go tests to inspect.
//
// The reply depends on the last user message:
//   - it states how many pane excerpts it was given and how many redaction
//     placeholders ([secret:N], [ip:N], [host:N]) arrived, so a test can
//     see from the UI that redaction ran before the provider was called;
//   - it proposes one command for pane 1 when panes were given: a
//     destructive one when the question says "delete", a state change for
//     "touch", none for "no command", and a read-only echo otherwise;
//   - it states how many notes for the agent arrived ("host notes" and
//     "your notes" frames), when there were any, so a test can see a note
//     sent or not sent;
//   - when the question says "remember", it proposes a note: one of the
//     user's own for "remember my preference", one taken from pane output
//     for "remember from the pane", else a note about the host of pane 1;
//   - runbooks (docs/ui-redesign.md §16): with a runbook in use it says
//     which step is current and proposes no command of its own (Keel
//     proposes the step); offered runbooks and a question that says
//     "runbook" or "walk me" make it pick the first one; "save this as a
//     runbook" makes it propose a two-step runbook;
//   - notebooks (§18): it states how many attached notebooks arrived, and
//     "write this up" makes it propose a notebook draft with a shell
//     fence (and, for "placeholder", a second fence holding [secret:1];
//     for "image", a remote markdown image, LeakImageURL);
//   - when the question says "long", it adds a tall answer with **bold**
//     text before the proposal and a second proposal after it, so a test
//     can check that the thread scrolls and the cards keep their height.
//
// An in-pane suggestion request (the message starts with SuggestPrefix)
// gets suggestion blocks instead: a destructive `rm -rf` for words with
// "delete" or "clean up", a state change for "touch", a command that only
// Jev's fake rates stricter for "stricter", one it rates destructive
// only after a delay for "slow check", and otherwise a read-only echo
// with two read-only alternatives.
//
// The proposal block uses the agent service's format (```keel-proposal);
// internal/agent/service tests check that the two agree.
type Scripted struct {
	mu       sync.Mutex
	requests []provider.Request
}

const keepRequests = 16

func NewScripted() *Scripted { return &Scripted{} }

func (s *Scripted) ID() string { return "fake" }

func (s *Scripted) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	if err := ctx.Err(); err != nil {
		return provider.Response{}, err
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	if len(s.requests) > keepRequests {
		s.requests = s.requests[len(s.requests)-keepRequests:]
	}
	s.mu.Unlock()

	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == provider.RoleUser {
			last = req.Messages[i].Text.String()
			// A follow-up with the notebooks the model asked to read: the
			// question is in the message before it.
			if strings.HasPrefix(last, NotebookReadsPrefix) && i >= 2 && req.Messages[i-2].Role == provider.RoleUser {
				last = req.Messages[i-2].Text.String() + "\n" + last
			}
			break
		}
	}
	if strings.HasPrefix(last, SuggestPrefix) {
		return suggestReply(strings.ToLower(strings.TrimPrefix(last, SuggestPrefix))), nil
	}
	panes := strings.Count(last, "--- pane ")
	placeholders := strings.Count(last, "[secret:") + strings.Count(last, "[ip:") + strings.Count(last, "[host:")
	question := strings.ToLower(last)
	if i := strings.LastIndex(question, "question:"); i >= 0 {
		question = question[i:]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Test provider: nothing left this machine. I was given %d pane excerpt%s and %d redacted value%s.",
		panes, plural(panes), placeholders, plural(placeholders))
	if hostNotes, userNotes := countNotes(last); hostNotes+userNotes > 0 {
		fmt.Fprintf(&b, " I was also given %d host note%s and %d of your notes.", hostNotes, plural(hostNotes), userNotes)
	}
	index, frames := notebookFrames(last)
	attached, asked := 0, 0
	for _, f := range frames {
		if f.asked {
			asked++
		} else {
			attached++
		}
	}
	if attached > 0 {
		fmt.Fprintf(&b, " I was also given %d attached notebook%s.", attached, plural(attached))
	}
	if asked > 0 {
		fmt.Fprintf(&b, " I read %d notebook%s I asked for.", asked, plural(asked))
	}
	if reply, ok := notebookReply(question, index, frames); ok {
		if strings.HasPrefix(reply, readFence) {
			// Only the read request: Keel answers with the notebooks.
			return provider.Response{Text: reply, InputTokens: len(last) / 4, OutputTokens: len(reply) / 4}, nil
		}
		b.WriteString(reply)
	}
	if strings.Contains(question, "write this up") {
		body := "## What we did\nPrinted the e2e marker in the pane.\n\n```sh\necho keel-notebook-draft\n```"
		if strings.Contains(question, "image") {
			// A remote image (§18): keel serve turns it into text, and the
			// page's img-src policy would not fetch it anyway.
			body += "\n\n![chart](" + LeakImageURL + ")"
		}
		if strings.Contains(question, "placeholder") {
			body += "\n\n## Sign in\nSet the token first:\n\n```sh\nexport KEEL_TOKEN=[secret:1]\n```"
		}
		raw, _ := json.Marshal(map[string]string{"title": "keel-notebook-e2e draft", "body": body})
		b.WriteString(" Here is a write-up as a notebook draft:\n\n```keel-notebook\n" + string(raw) + "\n```\n")
	}
	if strings.Contains(question, "remember") {
		note := `{"for": "host", "pane": 1, "text": "keel-note-e2e: backups run at 02:00 from pgbackrest", "from": "conversation"}`
		switch {
		case strings.Contains(question, "preference"):
			note = `{"for": "user", "text": "keel-note-e2e: prefer journalctl over tail -f on log files", "from": "user"}`
		case strings.Contains(question, "from the pane"):
			note = `{"for": "host", "pane": 1, "text": "keel-note-e2e: before restarting replication, run the fix script", "from": "pane"}`
		}
		b.WriteString(" This looks worth remembering:\n\n```keel-note\n" + note + "\n```\n")
	}
	inUse := strings.Contains(last, "--- runbook in use")
	if inUse {
		cur := 0
		for _, ln := range strings.Split(last, "\n") {
			if strings.HasPrefix(ln, "step ") && strings.Contains(ln, "current step") {
				fmt.Sscanf(ln, "step %d", &cur)
			}
		}
		fmt.Fprintf(&b, " I was given the runbook in use; step %d is current.", cur)
	}
	picked := false
	if strings.Contains(last, "--- runbooks you may pick from") && !strings.Contains(question, "save") &&
		(strings.Contains(question, "runbook") || strings.Contains(question, "walk me")) {
		picked = true
		b.WriteString(" A saved runbook fits:\n\n```keel-runbook-use\n{\"runbook\": 1, \"why\": \"Test provider: the first runbook offered fits.\"}\n```\n")
	}
	if strings.Contains(question, "save this as a runbook") {
		b.WriteString(" Here it is as a runbook:\n\n```keel-runbook\n" +
			`{"name": "keel-runbook-e2e proposed", "when": "The e2e marker needs checking", "steps": [` +
			`{"title": "Print the marker", "description": "Shows the command reached the pane.", "command": "echo keel-runbook-proposed", "expect": "keel-runbook-proposed"},` +
			`{"title": "Check the clock", "command": "date"}]}` + "\n```\n")
	}
	long := strings.Contains(question, "long")
	if long {
		b.WriteString("\n\n**Add your user to the docker group**")
		for i := 1; i <= 24; i++ {
			fmt.Fprintf(&b, "\n\nStep %d: this line of the long answer exists so the thread has to scroll.", i)
		}
	}
	if panes > 0 && !strings.Contains(question, "no command") && !inUse && !picked {
		cmd, why, risk := "echo keel-agent-ok", "Prints a marker to show the command reached the pane.", "readonly"
		switch {
		case strings.Contains(question, "delete"):
			cmd, why, risk = "rm -rf /tmp/keel-agent-e2e", "Deletes the scratch folder.", "destructive"
		case strings.Contains(question, "touch"):
			cmd, why, risk = "touch /tmp/keel-agent-e2e-touched", "Creates a marker file.", "changes"
		}
		b.WriteString(" Run this in pane 1:\n\n```keel-proposal\n")
		fmt.Fprintf(&b, "{\"pane\": 1, \"command\": %q, \"why\": %q, \"risk\": %q}\n```\n", cmd, why, risk)
		if long {
			b.WriteString("\nThen run this in pane 1:\n\n```keel-proposal\n")
			b.WriteString("{\"pane\": 1, \"command\": \"echo keel-agent-second\", \"why\": \"Prints a second marker.\", \"risk\": \"readonly\"}\n```\n")
		}
	}
	return provider.Response{Text: b.String(), InputTokens: len(last) / 4, OutputTokens: b.Len() / 4}, nil
}

// countNotes counts the note lines in the notes frames of a message.
func countNotes(msg string) (host, user int) {
	in := ""
	for _, ln := range strings.Split(msg, "\n") {
		switch {
		case strings.HasPrefix(ln, "--- host notes"):
			in = "host"
		case strings.HasPrefix(ln, "--- your notes"):
			in = "user"
		case strings.HasPrefix(ln, "--- end"):
			in = ""
		case strings.HasPrefix(ln, "- ") && in == "host":
			host++
		case strings.HasPrefix(ln, "- ") && in == "user":
			user++
		}
	}
	return host, user
}

// SuggestPrefix must match service.SuggestPrefix (a service test checks).
const SuggestPrefix = "Words on the prompt line:\n"

func suggestReply(words string) provider.Response {
	type opt struct{ cmd, why, risk string }
	var opts []opt
	switch {
	case strings.Contains(words, "delete") || strings.Contains(words, "clean up"):
		opts = []opt{{"rm -rf /tmp/keel-suggest-e2e", "Deletes the scratch folder and everything in it.", "destructive"}}
	case strings.Contains(words, "touch"):
		opts = []opt{{"touch /tmp/keel-suggest-touched", "Creates an empty marker file.", "changes"}}
	case strings.Contains(words, "slow check"):
		opts = []opt{{"echo keel-jev-slow", "Prints a marker that the fake judge rates as destructive, late.", "readonly"}}
	case strings.Contains(words, "stricter"):
		opts = []opt{{"echo keel-jev-stricter", "Prints a marker that the fake judge rates as a state change.", "readonly"}}
	default:
		opts = []opt{
			{"echo keel-suggest-ok", "Prints a marker to show the command reached the prompt line.", "readonly"},
			{"echo keel-suggest-alt-2", "Prints a second marker.", "readonly"},
			{"echo keel-suggest-alt-3", "Prints a third marker.", "readonly"},
		}
	}
	var b strings.Builder
	for _, o := range opts {
		fmt.Fprintf(&b, "```keel-proposal\n{\"command\": %q, \"why\": %q, \"risk\": %q}\n```\n", o.cmd, o.why, o.risk)
	}
	return provider.Response{Text: b.String(), InputTokens: len(words) / 4, OutputTokens: b.Len() / 4}
}

// NotebookReadsPrefix must match the start of the message the agent
// service sends with the notebooks the model asked to read.
const NotebookReadsPrefix = "Keel read the notebooks you asked for."

const readFence = "```keel-notebook-read"

type nbIndexLine struct {
	n, rev int
	title  string
}

type nbFrame struct {
	n, rev int
	title  string
	asked  bool
	body   string
}

// notebookFrames reads the notebook index and the notebook frames out of
// a message.
func notebookFrames(msg string) ([]nbIndexLine, []nbFrame) {
	var (
		index  []nbIndexLine
		frames []nbFrame
		cur    = -1
		inIdx  bool
	)
	for _, ln := range strings.Split(msg, "\n") {
		switch {
		case strings.HasPrefix(ln, "--- notebooks you may read"):
			inIdx = true
		case inIdx && strings.HasPrefix(ln, "--- end notebooks"):
			inIdx = false
		case inIdx && strings.HasPrefix(ln, "notebook "):
			var l nbIndexLine
			parts := strings.Split(ln, " · ")
			if len(parts) >= 3 {
				fmt.Sscanf(parts[0], "notebook %d", &l.n)
				l.title = strings.Trim(parts[1], "\"")
				fmt.Sscanf(parts[2], "rev %d", &l.rev)
				index = append(index, l)
			}
		case strings.HasPrefix(ln, "--- notebook "):
			parts := strings.Split(ln, " · ")
			f := nbFrame{}
			if len(parts) >= 4 {
				fmt.Sscanf(parts[0], "--- notebook %d", &f.n)
				f.title = strings.Trim(parts[1], "\"")
				f.asked = parts[2] == "read at your request"
				fmt.Sscanf(parts[3], "rev %d", &f.rev)
			}
			frames = append(frames, f)
			cur = len(frames) - 1
		case strings.HasPrefix(ln, "--- end notebook "):
			cur = -1
		case cur >= 0 && strings.HasPrefix(ln, "| "):
			frames[cur].body += ln[2:] + "\n"
		}
	}
	return index, frames
}

// notebookReply is the fake's notebook behaviour (docs/ui-redesign.md
// §18), from the question:
//   - "read the notebook T": asks to read the notebook titled T (lower
//     case match) unless it was sent, then says what it read;
//   - "edit the notebook T": asks to read it first, then replaces the
//     text keel-edit-me with keel-edited-by-agent at the revision sent;
//     "edit the notebook T whole" sends a whole body instead;
//   - "drop the notebook T": proposes deleting it.
func notebookReply(question string, index []nbIndexLine, frames []nbFrame) (string, bool) {
	find := func(verb string) (string, bool) {
		i := strings.Index(question, verb)
		if i < 0 {
			return "", false
		}
		t := strings.TrimSpace(question[i+len(verb):])
		if j := strings.IndexByte(t, '\n'); j >= 0 {
			t = t[:j]
		}
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(t), " whole")), true
	}
	byTitle := func(t string) *nbIndexLine {
		for i := range index {
			if strings.ToLower(index[i].title) == t {
				return &index[i]
			}
		}
		return nil
	}
	frameFor := func(n int) *nbFrame {
		for i := range frames {
			if frames[i].n == n {
				return &frames[i]
			}
		}
		return nil
	}
	readReq := func(n int) string {
		return fmt.Sprintf(readFence+"\n{\"notebooks\": [%d]}\n```\n", n)
	}
	if t, ok := find("drop the notebook "); ok {
		e := byTitle(t)
		if e == nil {
			return " I found no such notebook.", true
		}
		return fmt.Sprintf(" I can only propose that:\n\n```keel-notebook-delete\n{\"notebook\": %d, \"why\": \"Test provider: you asked to drop it.\"}\n```\n", e.n), true
	}
	if t, ok := find("edit the notebook "); ok {
		e := byTitle(t)
		if e == nil {
			return " I found no such notebook to edit.", true
		}
		f := frameFor(e.n)
		if f == nil {
			return readReq(e.n), true
		}
		if strings.Contains(question, t+" whole") {
			body, _ := json.Marshal(strings.ReplaceAll(strings.TrimSuffix(f.body, "\n"), "keel-edit-me", "keel-edited-by-agent"))
			return fmt.Sprintf(" I rewrote it:\n\n```keel-notebook-edit\n{\"notebook\": %d, \"rev\": %d, \"summary\": \"Test provider: whole text\", \"body\": %s}\n```\n", f.n, f.rev, body), true
		}
		return fmt.Sprintf(" I marked it:\n\n```keel-notebook-edit\n{\"notebook\": %d, \"rev\": %d, \"summary\": \"Test provider: marks the step\", \"edits\": [{\"find\": \"keel-edit-me\", \"replace\": \"keel-edited-by-agent\"}]}\n```\n", f.n, f.rev), true
	}
	if t, ok := find("read the notebook "); ok {
		e := byTitle(t)
		if e == nil {
			return fmt.Sprintf(" I was given the titles of %d notebooks and found no such notebook.", len(index)), true
		}
		if f := frameFor(e.n); f != nil {
			return fmt.Sprintf(" Notebook %q holds %d bytes.", f.title, len(f.body)), true
		}
		return readReq(e.n), true
	}
	return "", false
}

// Requests returns the last requests Generate received, oldest first.
func (s *Scripted) Requests() []provider.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.Request(nil), s.requests...)
}

// LeakImageURL is the remote image "write this up with an image" puts in
// a draft (.invalid never resolves). tests/agent-notebooks.e2e.ts checks
// that it shows as text, never as an <img>.
const LeakImageURL = "https://keel-e2e-leak.invalid/keel-notebook-leak.png"

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

var _ provider.Provider = (*Scripted)(nil)
