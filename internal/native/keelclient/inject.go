//lint:file-ignore ST1005 the errors of this file are shown to the user in the app's own words

package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/wire"
)

// The one place keel-native types into a session on the user's behalf (docs/ui-redesign.md §6, rule 1). Keystrokes
// from the terminal view go through Conn.Write; everything else that reaches a PTY goes through an inject frame, and this
// file is the only one that builds one: it alone names wire.TypeInjectChallenge and wire.TypeInject and the parts of an
// inject (wire.AgentType, wire.DirectType, wire.AgentInsert, Notebook), and every inject goes out through one function,
// inject, which first gets keel serve's one-time nonce for the same bytes. internal/native/guard_test.go keeps all of it
// true.
//
// keel serve writes exactly the bytes it is given and adds nothing, and only for a one-time nonce it issued for those
// same bytes to this client, which must be the driver (internal/inject, runtime.Session.InjectTicket).
//
// Three callers, one each:
//
// ApprovedInject is the Approve before type dialog's Type action (internal/native/approve.go). A group run's step
// (docs/ui-redesign.md §6 rule 10) is typed through it too: the group approve dialog's Type action calls it once per ticked
// host, each on that host's own connection with {groupRunId, proposalId, shift} in the agent part of the frame, so each
// host is its own proposal with its own server nonce. For a step that names variables the bytes are the ones keel serve's
// fill returned for that proposal (rule 11); this client never puts a value into them. An agent proposal is typed through
// the same call, with {conversationId, proposalId, shift}: keel serve checks the proposal (the exact bytes it holds, the
// pane, the role, the ⇧ rule) and logs agent.type before it writes, and answers with an agent-type frame, which
// ApprovedInject waits for. A refusal there is final: nothing was typed and nothing is retried. The bytes are the
// proposal's own (Proposal.Bytes: Ctrl-U, the command, a newline), built by keel serve; this client adds nothing to them.
//
// DirectType is for a command keel serve calls plainly read-only and one line (docs/ui-redesign.md §6 rule 13): it types
// the command alone, with no Ctrl-U and no newline, so it waits on the prompt line for the user's Enter. It is called
// only by the window's typeNow (internal/native/typenow.go), and keel serve decides every time: it checks the command,
// the pane's state and the driver again, at the moment of the write, and answers typed, dialog (nothing written; the
// window opens Approve before type) or a refusal.
//
// SuggestInsert is the user's ⇥ / ⇧⇥ on an in-pane suggestion (docs/ui-redesign.md §5.5): it sends the bytes keel serve
// itself gave for that option (Ctrl-U and the command, no newline) and nothing else; the caller names the suggestion, the
// option and ⇧, never the bytes. It is called only by (*workspace).insertSuggestion. keel serve decides, again, at the
// moment of the write. Restoring the user's own words after an insert (⌘Z) is RestoreWords, called only by
// (*workspace).restoreWords.

// The refusals, in the app's words.
var (
	ErrNotDriver  = errors.New("You are following — take or request control to type") // this client is following
	ErrNotReady   = errors.New("Session is not connected")                            // the session is not ready
	ErrNoTicket   = errors.New("Session is not connected")                            // keel serve gave no nonce in time, or refused
	ErrInjectBusy = errors.New("Another type is waiting for keel serve")              // one at a time: never queued

	ErrDirectBusy     = errors.New("Still typing the last command into this pane.")                          // one direct type at a time per pane
	ErrNoDirectAnswer = errors.New("keel serve did not answer. Check the pane before you press Type again.") // it may have been typed: never open a dialog that could type it again
)

// AgentRef names the proposal a Type is for. It goes into the inject frame and nowhere else; keel serve decides. A group
// run's proposal (docs/ui-redesign.md §17, §6 rule 10) names its run, GroupRunID, instead of a conversation: the group
// approve dialog types each ticked host with this same call, once per host, on that host's own connection, each with its
// own proposal and its own one-time nonce.
type AgentRef struct {
	ConversationID string
	GroupRunID     string
	ProposalID     string
	Shift          bool
}

// InjectOption marks where the bytes of an ApprovedInject come from.
type InjectOption int

// FromNotebook marks the bytes as a notebook fence's: the inject frame says so, and keel serve refuses a redaction
// placeholder in them.
const FromNotebook InjectOption = iota + 1

// agentWait is how long keel serve has to answer a proposal's Type.
const agentWait = 4 * time.Second

// ErrNoAgentAnswer is the message when keel serve does not answer a proposal's Type.
var ErrNoAgentAnswer = errors.New("keel serve did not answer.")

// ticketWait is how long keel serve has to answer an approval request. keel serve keeps a nonce it issued for 60 s
// (internal/inject): the wait here is only this client's patience.
const ticketWait = 2500 * time.Millisecond

// waitFor is how long to wait for keel serve: d, or the connection's own wait when a test set one.
func (c *Conn) waitFor(d time.Duration) time.Duration {
	if c.wait > 0 {
		return c.wait
	}
	return d
}

// claim takes the connection's one typing slot: one type at a time, never queued. direct marks the holder as a direct
// type, so that a second one is told so (ErrDirectBusy). The caller releases the slot when its whole exchange with keel
// serve is over.
func (c *Conn) claim(direct bool) (nonce chan string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case direct && c.directBusy:
		return nil, ErrDirectBusy
	case c.ticket != nil:
		return nil, ErrInjectBusy
	}
	c.ticket, c.directBusy = make(chan string, 1), direct
	return c.ticket, nil
}

func (c *Conn) release() {
	c.mu.Lock()
	c.ticket, c.directBusy = nil, false
	c.mu.Unlock()
}

// inject is the one way this client asks keel serve to type: it asks for a one-time nonce for exactly command, waits for
// it on nonce (the slot the caller claimed), and sends those same bytes with it, once. parts says where the bytes come
// from (Agent, Direct, Suggestion, Notebook); its type, command and nonce are set here and nowhere else. With no nonce in
// time it sends no inject and returns ErrNoTicket. It never retries.
func (c *Conn) inject(ctx context.Context, nonce chan string, command string, parts wire.Control) error {
	if err := c.send(wire.Control{Type: wire.TypeInjectChallenge, Command: command}); err != nil {
		return err
	}
	timer := time.NewTimer(c.waitFor(ticketWait))
	defer timer.Stop()
	var n string
	select {
	case n = <-nonce:
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	if n == "" {
		return ErrNoTicket
	}
	parts.Type, parts.Command, parts.Nonce = wire.TypeInject, command, n
	return c.send(parts)
}

// ApprovedInject asks keel serve for a one-time nonce for exactly these bytes and, if it gets one, sends them. It
// is called only by the Approve before type dialog's Type action. It sends nothing unless this client drives a ready
// session, never retries, and never sends without a nonce. A non-nil agent marks the bytes as an agent proposal: the
// call then returns keel serve's refusal (the message of its agent-type answer) as an error, and nil only when keel
// serve says it typed.
func (c *Conn) ApprovedInject(ctx context.Context, command string, agent *AgentRef, opts ...InjectOption) error {
	if err := c.typingReady(); err != nil {
		return err
	}
	nonce, err := c.claim(false)
	if err != nil {
		return err
	}
	defer c.release()
	var answer chan agentAnswer // registered before the frame goes out, so the answer cannot be missed
	if agent != nil {
		answer = make(chan agentAnswer, 1)
		c.mu.Lock()
		c.agentWaiter, c.agentFor = answer, agent.ProposalID
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.agentWaiter, c.agentFor = nil, ""
			c.mu.Unlock()
		}()
	}
	parts := wire.Control{Agent: agentPart(agent)}
	for _, o := range opts {
		if o == FromNotebook { // keel serve refuses a placeholder in it
			parts.Notebook = true
		}
	}
	if err := c.inject(ctx, nonce, command, parts); err != nil || agent == nil {
		return err
	}
	return waitAnswer(ctx, answer, agentWait, "Not typed.")
}

// agentAnswer is keel serve's agent-type or agent-insert frame: ok when it wrote the bytes, else why not (its own words).
type agentAnswer struct {
	ok      bool
	message string
}

// waitAnswer waits for keel serve's answer to an inject: nil when it wrote the bytes, its refusal (or refused, when it
// gave no words) as an error, ErrNoAgentAnswer when it did not answer in time.
func waitAnswer(ctx context.Context, answer chan agentAnswer, wait time.Duration, refused string) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case a := <-answer:
		if !a.ok {
			if a.message == "" {
				a.message = refused
			}
			return errors.New(a.message)
		}
		return nil
	case <-timer.C:
		return ErrNoAgentAnswer
	case <-ctx.Done():
		return ctx.Err()
	}
}

// injectFrame hands keel serve's answer to an approval request, an inject or a suggestion question to the call that
// waits for it. It reports whether the frame was one; the nonce is never kept, logged or shown.
func (c *Conn) injectFrame(data []byte) bool {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &head) != nil {
		return false
	}
	switch head.Type {
	case wire.TypeInjectChallenge:
		var m wire.Challenge
		_ = json.Unmarshal(data, &m)
		c.mu.Lock()
		w := c.ticket
		c.mu.Unlock()
		if w != nil {
			select { // the first answer counts; a second never blocks the reader
			case w <- m.Nonce:
			default:
			}
		}
	case wire.TypeTypeDirect, wire.TypeAgentType, wire.TypeAgentInsert:
		var m wire.TypeResult
		_ = json.Unmarshal(data, &m)
		c.typeResult(m)
	case wire.TypeAgentDetect, wire.TypeAgentSuggest, wire.TypeAgentSuggestRisk:
		c.suggestFrame(head.Type, data)
	default:
		return false
	}
	return true
}

// typeResult hands keel serve's answer to an inject to the call it is for: an answer about another request (by its ref,
// its proposal or its suggestion) is not ours.
func (c *Conn) typeResult(m wire.TypeResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch m.Type {
	case wire.TypeTypeDirect:
		if w := c.directWaiters[m.Ref]; w != nil {
			select {
			case w <- directAnswer{ok: m.OK, dialog: m.Dialog, reason: m.Reason, message: m.Message}:
			default:
			}
		}
	case wire.TypeAgentType:
		if c.agentWaiter != nil && m.ProposalID == c.agentFor {
			select {
			case c.agentWaiter <- agentAnswer{m.OK, m.Message}:
			default:
			}
		}
	case wire.TypeAgentInsert:
		if c.insertWaiter != nil && m.SuggestionID == c.insertFor {
			select {
			case c.insertWaiter <- agentAnswer{m.OK, m.Message}:
			default:
			}
		}
	}
}

// agentPart is the agent part of an inject frame: nil for bytes that are no proposal's. A group run's proposal names the
// run, never a conversation.
func agentPart(a *AgentRef) *wire.AgentType {
	if a == nil {
		return nil
	}
	if a.GroupRunID != "" {
		return &wire.AgentType{GroupRunID: a.GroupRunID, ProposalID: a.ProposalID, Shift: a.Shift}
	}
	return &wire.AgentType{ConversationID: a.ConversationID, ProposalID: a.ProposalID, Shift: a.Shift}
}

// DirectRef is the "direct" part of a direct inject (wire.DirectType): where the command comes from, and what the pane's
// terminal shows, which keel serve ORs with its own view (it can only add a dialog). The ref that matches an answer is
// made here.
type DirectRef struct {
	Kind       string // "snippet", "agent" or "notebook" (a group run is not built here)
	SnippetID  string // the snippet, for Kind "snippet": keel serve compares its stored command with the bytes
	NotebookID string // the notebook, for Kind "notebook": keel serve finds the command as the one line of a shell fence of its stored text
	AltScreen  bool   // the terminal shows a full-screen program's screen; a caller that cannot tell says true
	PromptLine string // the line the cursor is on, which keel serve reads for an input prompt
}

// DirectResult is what keel serve answered to a direct type. Exactly one of the three: Typed (the command is on the
// prompt line and nothing ran), Dialog (nothing was written: Approve before type decides; Reason says why, in its words),
// or Message (keel serve refused, in its words).
type DirectResult struct {
	Typed   bool
	Dialog  bool
	Reason  string
	Message string
}

// directAnswer is keel serve's type-direct frame.
type directAnswer struct {
	ok, dialog      bool
	reason, message string
}

// directWait is how long keel serve has to answer a direct type (8 s).
const directWait = 8 * time.Second

// directReady: nothing goes out for a kind this client does not build, or for a session this client cannot type into (a
// follower, or one that is not ready).
func (c *Conn) directReady(ref DirectRef) error {
	if ref.Kind != wire.DirectSnippet && ref.Kind != wire.DirectAgent && ref.Kind != wire.DirectNotebook {
		return errors.New("keelclient: no such kind of direct type")
	}
	return c.typingReady()
}

// newDirectWaiter registers the answer a request with a fresh ref waits for, before the request goes out.
func (c *Conn) newDirectWaiter() (string, chan directAnswer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.directSeq++
	ref := "d" + strconv.Itoa(c.directSeq)
	if c.directWaiters == nil {
		c.directWaiters = map[string]chan directAnswer{}
	}
	w := make(chan directAnswer, 1)
	c.directWaiters[ref] = w
	return ref, w
}

func (c *Conn) dropDirectWaiter(ref string) {
	c.mu.Lock()
	delete(c.directWaiters, ref)
	c.mu.Unlock()
}

// waitDirect waits for the answer to ref; ok is false when keel serve did not answer in time.
func (c *Conn) waitDirect(ctx context.Context, w chan directAnswer) (a directAnswer, ok bool, err error) {
	timer := time.NewTimer(c.waitFor(directWait))
	defer timer.Stop()
	select {
	case a = <-w:
		return a, true, nil
	case <-timer.C:
		return a, false, nil
	case <-ctx.Done():
		return a, false, ctx.Err()
	}
}

// DirectType asks keel serve to type command into this session without Enter, and without the Approve before type
// dialog. It is called only by the window's typeNow. Nothing goes out unless this client drives a ready session and the
// command is one plain line with no control or invisible character; a command that is not goes back as Dialog with no
// frame. The command is sent as it is: no Ctrl-U, no newline. keel serve gets a fresh nonce for exactly these bytes
// first (no nonce: Dialog, and no inject frame), then the inject with the direct part (and agent, for a proposal), and
// its answer decides: Typed, Dialog or a refusal in Message. One at a time (ErrDirectBusy), never queued, never retried.
// No answer in time is ErrNoDirectAnswer: it may have been typed, so the caller must not open a dialog.
func (c *Conn) DirectType(ctx context.Context, command string, ref DirectRef, agent *AgentRef) (DirectResult, error) {
	if err := c.directReady(ref); err != nil {
		return DirectResult{}, err
	}
	if !DirectLine(command) {
		return DirectResult{Dialog: true}, nil
	}
	nonce, err := c.claim(true)
	if err != nil {
		return DirectResult{}, err
	}
	defer c.release()

	id, answer := c.newDirectWaiter() // the ref goes into the frame here and nowhere else
	defer c.dropDirectWaiter(id)
	parts := wire.Control{Agent: agentPart(agent), Direct: &wire.DirectType{
		Ref: id, Kind: ref.Kind, SnippetID: ref.SnippetID, NotebookID: ref.NotebookID, AltScreen: ref.AltScreen, PromptLine: ref.PromptLine}}
	switch err := c.inject(ctx, nonce, command, parts); {
	case errors.Is(err, ErrNoTicket):
		return DirectResult{Dialog: true}, nil // no nonce, no inject
	case err != nil:
		return DirectResult{}, err
	}
	a, ok, err := c.waitDirect(ctx, answer)
	switch {
	case err != nil:
		return DirectResult{}, err
	case !ok:
		return DirectResult{}, ErrNoDirectAnswer
	case a.dialog:
		return DirectResult{Dialog: true, Reason: a.reason}, nil
	case !a.ok:
		if a.message == "" {
			a.message = "Not typed."
		}
		return DirectResult{Message: a.message}, nil
	}
	return DirectResult{Typed: true}, nil
}

// ---- in-pane suggestions (internal/httpapi/agentsuggest.go) ----

// SuggestOption is one command keel serve offered. It holds no bytes: the bytes an insert sends stay in this package.
// Risk is RiskReadOnly, RiskChanges or RiskDestructive.
type SuggestOption struct {
	Command, Why, Risk, Source       string
	NeedsShift, Judged, Placeholders bool
}

// The risk words of a proposal, a step and a suggestion's option (wire.Risk).
const (
	RiskReadOnly    = string(wire.RiskReadOnly)
	RiskChanges     = string(wire.RiskChanges)
	RiskDestructive = string(wire.RiskDestructive)
)

// Why keel serve sent no suggestion, when the window acts on it (SuggestEvent.Reason; the rest are wire.Reason…).
const (
	ReasonOff         = wire.ReasonOff
	ReasonNoKey       = wire.ReasonNoKey
	ReasonNotReadable = wire.ReasonNotReadable
	ReasonFollowing   = wire.ReasonFollowing
)

// Suggestion is the card keel serve sent for the words on a prompt line.
type Suggestion struct {
	ID      string
	Options []SuggestOption
}

// Kinds of SuggestEvent.
const (
	SuggestDetected = "detect"  // keel serve's answer to SuggestDetect: Seq, Plain, Source
	SuggestAnswered = "suggest" // its answer to SuggestAsk: Seq, OK, Suggestion or Message and Reason
	SuggestRated    = "risk"    // Jev rated the shown suggestion stricter: ID and Options (Risk, NeedsShift, Judged)
)

// SuggestEvent is one answer keel serve pushed for a suggestion; the window reads it on its next frame.
type SuggestEvent struct {
	Kind            string
	Seq             int
	Plain           bool
	Source          string
	OK              bool
	Suggestion      *Suggestion
	Message, Reason string
	ID              string
	Options         []SuggestOption
}

// SuggestionRef names the option an insert is for. The bytes are not part of it.
type SuggestionRef struct {
	ID     string
	Option int
	Shift  bool // exactly as the key was pressed: this package never sets it
}

// keptSuggestion is what keel serve gave as the bytes of each option of a suggestion.
type keptSuggestion struct {
	id    string
	bytes []string
}

const keptSuggestions = 8 // keel serve expires them itself; a few are enough for the one card on screen

// ErrSuggestionGone is keel serve's own word for an insert that is too late.
var ErrSuggestionGone = errors.New(wire.MsgInsertExpired)

// ErrMultiLine refuses a command that spans lines.
var ErrMultiLine = errors.New("This command spans lines; copy it instead.")

// insertWait is how long keel serve has to answer an insert.
const insertWait = 4 * time.Second

func (c *Conn) sendSuggest(kind string, seq int, line string) error {
	if err := c.typingReady(); err != nil {
		return err
	}
	return c.send(wire.Control{Type: kind, Seq: seq, Line: line})
}

// SuggestDetect asks keel serve whether the words on the prompt line are plain language (agent-detect). It writes
// nothing to the session; keel serve's answer comes back as a SuggestDetected event with the same seq.
func (c *Conn) SuggestDetect(seq int, line string) error {
	return c.sendSuggest(wire.TypeAgentDetect, seq, line)
}

// SuggestAsk asks keel serve for a command for the words (agent-suggest). It writes nothing to the session; the
// answer is a SuggestAnswered event, then perhaps a SuggestRated one.
func (c *Conn) SuggestAsk(seq int, line string) error {
	return c.sendSuggest(wire.TypeAgentSuggest, seq, line)
}

// suggestFrame handles keel serve's detect, suggest and risk frames (the insert's answer is typeResult's).
func (c *Conn) suggestFrame(typ string, data []byte) {
	switch typ {
	case wire.TypeAgentDetect:
		var m wire.Detect
		if json.Unmarshal(data, &m) == nil {
			c.pushSuggest(SuggestEvent{Kind: SuggestDetected, Seq: m.Seq, Plain: m.Plain, Source: m.Source})
		}
	case wire.TypeAgentSuggest:
		var m wire.Suggest
		if json.Unmarshal(data, &m) != nil {
			return
		}
		e := SuggestEvent{Kind: SuggestAnswered, Seq: m.Seq, OK: m.OK, Message: m.Message, Reason: m.Reason}
		if s := m.Suggestion; s != nil {
			sug := &Suggestion{ID: s.ID}
			kept := keptSuggestion{id: s.ID}
			for _, o := range s.Options {
				sug.Options = append(sug.Options, SuggestOption{Command: o.Command, Why: o.Why, Risk: string(o.Risk), Source: o.Source,
					NeedsShift: o.NeedsShift, Judged: o.Judged, Placeholders: o.Placeholders})
				kept.bytes = append(kept.bytes, o.Bytes)
			}
			e.Suggestion = sug
			c.mu.Lock()
			c.sugKept = append(c.sugKept, kept)
			if len(c.sugKept) > keptSuggestions {
				c.sugKept = c.sugKept[len(c.sugKept)-keptSuggestions:]
			}
			c.mu.Unlock()
		}
		c.pushSuggest(e)
	case wire.TypeAgentSuggestRisk:
		var m wire.SuggestRisk
		if json.Unmarshal(data, &m) != nil {
			return
		}
		e := SuggestEvent{Kind: SuggestRated, ID: m.SuggestionID}
		for _, o := range m.Options {
			e.Options = append(e.Options, SuggestOption{Risk: string(o.Risk), NeedsShift: o.NeedsShift, Judged: o.Judged})
		}
		c.pushSuggest(e)
	}
}

func (c *Conn) pushSuggest(e SuggestEvent) {
	c.mu.Lock()
	if len(c.sugEvents) < 64 {
		c.sugEvents = append(c.sugEvents, e)
	}
	fn := c.onChange
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// SuggestEvents hands over what keel serve answered since the last call, oldest first.
func (c *Conn) SuggestEvents() []SuggestEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.sugEvents
	c.sugEvents = nil
	return out
}

// suggestBytes is what keel serve gave as the bytes of option i of suggestion id.
func (c *Conn) suggestBytes(id string, i int) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range c.sugKept {
		if k.id == id && i >= 0 && i < len(k.bytes) {
			return k.bytes[i], true
		}
	}
	return "", false
}

// SuggestInsert is the user's ⇥ / ⇧⇥ on a suggestion card: it asks keel serve for a one-time nonce for the bytes keel
// serve gave for that option, sends them with the suggestion reference (id, option and ⇧ as pressed), and returns keel
// serve's refusal (its own words) or nil when it says it wrote them. keel serve checks the suggestion, the bytes, the ⇧
// rule and that this client drives, logs agent.insert, then writes; the bytes end without a newline, so nothing runs.
// It is called only by (*workspace).insertSuggestion. One at a time, never queued, never retried. Nothing goes out
// unless this client drives a ready session, the suggestion is one keel serve sent, and its bytes are one line.
func (c *Conn) SuggestInsert(ctx context.Context, ref SuggestionRef) error {
	if err := c.typingReady(); err != nil {
		return err
	}
	bytes, ok := c.suggestBytes(ref.ID, ref.Option)
	switch {
	case !ok:
		return ErrSuggestionGone
	case strings.IndexFunc(bytes, func(r rune) bool { return r == 13 || r == 10 }) >= 0:
		return ErrMultiLine
	}
	nonce, err := c.claim(false)
	if err != nil {
		return err
	}
	defer c.release()
	answer := make(chan agentAnswer, 1) // registered before the frame goes out, so the answer cannot be missed
	c.mu.Lock()
	c.insertWaiter, c.insertFor = answer, ref.ID
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.insertWaiter, c.insertFor = nil, ""
		c.mu.Unlock()
	}()
	parts := wire.Control{Suggestion: &wire.AgentInsert{SuggestionID: ref.ID, Option: ref.Option, Shift: ref.Shift}}
	if err := c.inject(ctx, nonce, bytes, parts); err != nil {
		return err
	}
	return waitAnswer(ctx, answer, c.waitFor(insertWait), "Not inserted.")
}

// restoreBytes is what ⌘Z sends to put the user's own words back: Ctrl-U to clear the inserted command, then the words
// with every control character dropped, so a restore can never press Enter.
func restoreBytes(words string) string {
	return "\x15" + strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, words)
}

// RestoreWords is ⌘Z after an insert: the user's own words back, as their keystrokes, to this session only (keel serve
// takes them as ordinary typing from the driver). Nothing goes out unless this client drives a ready session. It is
// called only by (*workspace).restoreWords.
func (c *Conn) RestoreWords(words string) error {
	if err := c.typingReady(); err != nil {
		return err
	}
	return c.write(websocket.BinaryMessage, []byte(restoreBytes(words)))
}
