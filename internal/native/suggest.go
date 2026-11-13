package native

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	"keel/internal/native/keelclient"
)

// Plain language → command in a pane (docs/ui-redesign.md §5.5): the state machine and the pure rules, a port of
// cmd/keel/web/js/lib/suggest.js function for function, held by suggest_test.go (every case of test/suggest.test.mjs).
// Nothing here writes to a pane or sends a frame: the pane's code (suggestpane.go) does the I/O the deps name, and keel
// serve decides whether a line is noticed, writes a suggestion only after the user's ⇥ / ⇧⇥, and checks the ⇧ rule again.

const (
	detectPause   = 350 * time.Millisecond // DETECT_PAUSE_MS: the typing pause before the prompt line is checked
	detectCache   = 2 * time.Minute        // DETECT_CACHE_MS: an answer for the same words is kept
	offBackoff    = 15 * time.Second       // OFF_BACKOFF_MS: after "off", wait before asking again
	suggestMaxLen = 300                    // MAX_LINE
	insertedSeen  = 3 * time.Second        // how long an inserted command may stay unseen on the line
	// suggestWait is how long a request may stay loading: keel serve's history pick (1.5 s) and model call (30 s, agent
	// service suggestTimeout) plus a margin. Past it the card shows "No suggestion came back."; keel serve answers every
	// request it accepted, so this is for an answer that was lost.
	suggestWait = 37 * time.Second
)

type sugState string

const (
	sugIdle      sugState = "idle"
	sugNotice    sugState = "notice"
	sugLoading   sugState = "loading"
	sugCard      sugState = "card"
	sugInserting sugState = "inserting"
	sugInserted  sugState = "inserted"
	sugError     sugState = "error"
)

// A shell prompt ends at the first "$ ", "# ", "% ", "> ", "❯ " or "» " on the line.
var promptRe = regexp.MustCompile(`^(.*?[$#%>❯»] )`)

// promptLine is the cursor's line split into the prompt and what was typed after it.
type promptLine struct {
	Prompt, Input string
	Start         int // the index of the input in the line (a cell column for ASCII)
	Row           int // the screen row the line is on (0-based); set by the pane, not by promptInput
}

// promptInput is lib/suggest.js promptInput; nil when no prompt is found.
func promptInput(text string) *promptLine {
	m := promptRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	rest := text[len(m[1]):]
	return &promptLine{Prompt: m[1], Input: strings.TrimRightFunc(rest, unicode.IsSpace), Start: len([]rune(m[1]))}
}

func isControl(r rune) bool { return r <= 0x1f || r == 0x7f }

// worthDetecting is lib/suggest.js worthDetecting: a #-line, or two or more words, within suggestMaxLen.
func worthDetecting(input string) bool {
	s := strings.TrimSpace(input)
	if len(s) < 3 || len(s) > suggestMaxLen || strings.IndexFunc(s, isControl) >= 0 {
		return false
	}
	if strings.HasPrefix(s, "#") {
		return strings.TrimSpace(strings.TrimLeft(s, "#")) != ""
	}
	return len(strings.Fields(s)) >= 2
}

// hashLine is a line the user marked as a request by starting it with #.
func hashLine(input string) bool { return strings.HasPrefix(strings.TrimSpace(input), "#") }

// askable is a line the user's own request (the suggest key) may send: some words, within suggestMaxLen, with no
// control characters. Unlike worthDetecting, one word is enough: the user asked.
func askable(input string) bool {
	s := strings.TrimSpace(input)
	return s != "" && len(s) <= suggestMaxLen && strings.IndexFunc(s, isControl) < 0
}

// sugMode is what the settings let the pane do on its own: nothing (suggestions off), notice only lines the user starts
// with # (Only lines starting with #), or also have keel serve check each line after a typing pause (Notice
// automatically). The suggest key asks in the last two; off says so.
type sugMode int

const (
	sugOff sugMode = iota
	sugHash
	sugAuto
)

var riskOrder = map[string]int{"readonly": 0, "changes": 1, "destructive": 2}

func riskRank(r string) int {
	if n, ok := riskOrder[r]; ok {
		return n
	}
	return 1
}

// applyRisk merges a later, stricter rating into a suggestion (lib/suggest.js applyRisk). It returns a new suggestion,
// or sug itself when the update is for another one. It never loosens what is shown: it can only add ⇧.
func applyRisk(sug *keelclient.Suggestion, id string, upd []keelclient.SuggestOption) *keelclient.Suggestion {
	if sug == nil || id != sug.ID || upd == nil {
		return sug
	}
	out := &keelclient.Suggestion{ID: sug.ID, Options: make([]keelclient.SuggestOption, len(sug.Options))}
	for i, o := range sug.Options {
		if i < len(upd) {
			u := upd[i]
			if riskRank(u.Risk) > riskRank(o.Risk) {
				o.Risk = u.Risk
			}
			o.NeedsShift = o.NeedsShift || u.NeedsShift
			o.Judged = o.Judged || u.Judged
		}
		out.Options[i] = o
	}
	return out
}

// insertGate decides what a ⇥ / ⇧⇥ press does on an option: ok inserts; reason needsShiftReason shows "press ⇧⇥".
const needsShiftReason = "needs ⇧"

func insertGate(o *keelclient.SuggestOption, shift bool) (ok bool, reason string) {
	switch {
	case o == nil:
		return false, "none"
	case o.NeedsShift && !shift:
		return false, needsShiftReason
	}
	return true, ""
}

// sugKey is a key press as the machine sees it. Name is "Tab", "Escape", "ArrowUp", "ArrowDown", a single character, or
// a modifier's name ("Shift", "Meta", "Control", "Alt").
type sugKey struct {
	Name                   string
	Shift, Ctrl, Meta, Alt bool
}

// undoKey is the "back to your words" key: ⌘Z on macOS; Ctrl+Shift+Z elsewhere, since Ctrl+Z suspends the job.
func undoKey(k sugKey, mac bool) bool {
	if k.Alt || strings.ToLower(k.Name) != "z" {
		return false
	}
	if mac {
		return k.Meta && !k.Ctrl && !k.Shift
	}
	return k.Ctrl && k.Shift && !k.Meta
}

// optionKey maps a key in the card to a new selection: "1".."9" pick that option, ↑ / ↓ move; -1 for any other key.
func optionKey(k sugKey, selected, n int) int {
	if k.Meta || k.Ctrl || k.Alt || n < 1 {
		return -1
	}
	switch {
	case k.Name == "ArrowDown":
		return (selected + 1) % n
	case k.Name == "ArrowUp":
		return (selected - 1 + n) % n
	case !k.Shift && len(k.Name) == 1 && k.Name[0] >= '1' && k.Name[0] <= '9':
		if i := int(k.Name[0] - '1'); i < n {
			return i
		}
	}
	return -1
}

func isTab(k sugKey) bool { return k.Name == "Tab" && !k.Ctrl && !k.Meta && !k.Alt }
func isModifier(k sugKey) bool {
	switch k.Name {
	case "Shift", "Meta", "Control", "Alt", "CapsLock", "Fn":
		return true
	}
	return false
}

// keyLabels are the glyphs on macOS and words elsewhere.
type keyLabelSet struct {
	Tab, Shift, Esc, Enter, Up, Down string
	Undo, Suggest                    []string
}

func keyLabels(mac bool) keyLabelSet {
	if mac {
		return keyLabelSet{"⇥", "⇧", "esc", "↵", "↑", "↓", []string{"⌘", "Z"}, []string{"⌘", "Y"}}
	}
	return keyLabelSet{"Tab", "Shift", "Esc", "Enter", "↑", "↓", []string{"Ctrl", "Shift", "Z"}, []string{"Ctrl", "Shift", "Y"}}
}

// suggestKeyLabel is the suggest key as the Keyboard page and the settings write it.
func suggestKeyLabel(mac bool) string {
	if mac {
		return "⌘Y"
	}
	return "Ctrl+Shift+Y"
}

// riskWords is the badge text; destructive says what it does when the command makes it plain.
var deletesRe = regexp.MustCompile(`(^|[\s;&|(])(sudo\s+(-\S+\s+)*)?(rm|rmdir|unlink|shred|find\b.*-delete|pg_archivecleanup)\b`)

func riskWords(o keelclient.SuggestOption) string {
	if o.Risk == "destructive" {
		if deletesRe.MatchString(o.Command) {
			return "Deletes files"
		}
		return "Destructive"
	}
	label, _ := riskBadge(o.Risk)
	return label
}

// sourceLabel is the card's head: where the command came from.
func sourceLabel(source string) string {
	switch source {
	case "history":
		return "From this pane's scrollback"
	case "snippet":
		return "From your snippets"
	}
	return "Suggested from your words"
}

// insertResult is what an insert came to: ok, a refusal in keel serve's words, or dropped (the line changed under the
// card, so nothing was written).
type insertResult struct {
	OK, Dropped bool
	Message     string
}

// sugView is what the pane draws.
type sugView struct {
	State    sugState
	Words    string
	Sug      *keelclient.Suggestion
	Selected int
	Note     string
	Message  string
	Command  string // the inserted command
	Seen     bool
	Hash     bool // a notice for a # line: ⇥ is taken there; a line noticed without # leaves ⇥ to the shell
	Seq      int
	at       time.Time
}

// sugDeps are the machine's I/O. insert starts an insert and must call done once, on the window's thread.
type sugDeps struct {
	readLine    func() *promptLine
	canSuggest  func() bool
	mode        func() sugMode // nil: sugAuto
	sendDetect  func(seq int, line string)
	sendSuggest func(seq int, line string)
	insert      func(sug *keelclient.Suggestion, index int, shift bool, words string, done func(insertResult))
	restore     func(words string)
	render      func(sugView)
	now         func() time.Time
	mac         bool
}

type detectHit struct {
	plain bool
	at    time.Time
}

// sugMachine is one pane's suggestion state: idle → notice → loading → card → inserting → inserted, plus error.
type sugMachine struct {
	d         sugDeps
	st        sugView
	seq       int
	pendSeq   int // the detect in flight, 0 for none
	pendInput string
	dueAt     time.Time // when the typing pause ends; zero for none
	loadDue   time.Time // when a loading request gives up
	offUntil  time.Time
	dismissed string
	cache     map[string]detectHit
	cacheKeys []string
}

func newSugMachine(d sugDeps) *sugMachine {
	if d.now == nil {
		d.now = time.Now
	}
	return &sugMachine{d: d, st: sugView{State: sugIdle}, cache: map[string]detectHit{}}
}

func (m *sugMachine) State() sugState { return m.st.State }
func (m *sugMachine) View() sugView   { return m.st }

func (m *sugMachine) set(v sugView) {
	m.st = v
	if m.d.render != nil {
		m.d.render(v)
	}
}

func (m *sugMachine) idle() {
	if m.st.State != sugIdle {
		m.set(sugView{State: sugIdle})
	}
}

func (m *sugMachine) can() bool { return m.d.canSuggest != nil && m.d.canSuggest() }

func (m *sugMachine) mode() sugMode {
	if m.d.mode == nil {
		return sugAuto
	}
	return m.d.mode()
}

// live is can, and suggestions on: the machine follows a line only then. The one thing shown while they are off is the
// request's answer, "off in Settings · Agent".
func (m *sugMachine) live() bool {
	return m.can() && (m.mode() != sugOff || m.st.State == sugError)
}

// offMessage is what the suggest key says while suggestions are off.
const offMessage = "Suggestions in panes are off. Turn them on in Settings · Agent."

// ModeChanged follows a change of the settings: nothing asked before counts any more. The cache of plain lines, a
// detection in flight, the typing pause and the backoff go; a notice goes (it was for the old rule); with suggestions
// off a loading request or a card goes too, since keel serve would refuse it.
func (m *sugMachine) ModeChanged() {
	m.cache, m.cacheKeys = map[string]detectHit{}, nil
	m.dueAt, m.pendSeq, m.offUntil = time.Time{}, 0, time.Time{}
	switch m.st.State {
	case sugNotice:
		m.idle()
	case sugLoading, sugCard, sugInserting, sugInserted:
		if m.mode() == sugOff {
			m.idle()
		}
	}
}

// Request is the user's own ask, the suggest key: a suggestion for the words on the prompt line now, any words, no #
// needed. It sends the one question startSuggest sends and writes nothing; the card's ⇥ is the only way a command is inserted.
func (m *sugMachine) Request() {
	if !m.can() {
		return
	}
	switch m.st.State {
	case sugLoading, sugInserting, sugCard:
		return
	}
	line := m.line()
	if line == nil { // not at a prompt, or a full-screen program
		return
	}
	m.dueAt, m.dismissed = time.Time{}, ""
	switch {
	case m.mode() == sugOff:
		m.set(sugView{State: sugError, Words: line.Input, Message: offMessage})
	case !askable(line.Input):
		m.set(sugView{State: sugError, Words: line.Input, Message: "Type what you want to do first, then ask again."})
	default:
		m.st = sugView{State: sugIdle, Words: line.Input}
		m.startSuggest()
	}
}

func (m *sugMachine) line() *promptLine {
	if m.d.readLine == nil {
		return nil
	}
	return m.d.readLine()
}

func (m *sugMachine) restore() {
	words := m.st.Words
	m.dismissed = words
	m.idle()
	if m.d.restore != nil {
		m.d.restore(words)
	}
}

// Due reports when the next timer ends (the typing pause, or a loading request giving up), and whether one is waiting.
func (m *sugMachine) Due() (time.Time, bool) {
	due := m.dueAt
	if m.st.State == sugLoading && (due.IsZero() || m.loadDue.Before(due)) {
		due = m.loadDue
	}
	return due, !due.IsZero()
}

// Tick runs the detection when the typing pause has passed, and ends a request that has waited too long (the page's timer).
func (m *sugMachine) Tick() {
	now := m.d.now()
	if m.st.State == sugLoading && !now.Before(m.loadDue) {
		m.OnSuggest(m.st.Seq, false, nil, "", "timeout")
	}
	if m.dueAt.IsZero() || now.Before(m.dueAt) {
		return
	}
	m.detectNow()
}

func (m *sugMachine) detectNow() {
	m.dueAt = time.Time{}
	if !m.can() {
		m.idle()
		return
	}
	if m.mode() == sugOff { // off is known here: nothing is asked
		if m.st.State == sugNotice {
			m.idle()
		}
		return
	}
	if m.st.State != sugIdle && m.st.State != sugNotice {
		return
	}
	line := m.line()
	if line == nil || !worthDetecting(line.Input) || m.mode() == sugHash && !hashLine(line.Input) {
		m.idle() // in "Only lines starting with #" a line without # is never sent: it is the user's until they ask
		return
	}
	input := line.Input
	now := m.d.now()
	if input == m.dismissed || now.Before(m.offUntil) {
		return
	}
	if hit, ok := m.cache[input]; ok && now.Sub(hit.at) < detectCache {
		if hit.plain {
			m.set(sugView{State: sugNotice, Words: input, Hash: hashLine(input)})
		}
		return
	}
	m.seq++
	m.pendSeq, m.pendInput = m.seq, input
	if m.d.sendDetect != nil {
		m.d.sendDetect(m.seq, input)
	}
}

func (m *sugMachine) startSuggest() {
	m.seq++
	m.pendSeq = 0
	m.loadDue = m.d.now().Add(suggestWait)
	m.set(sugView{State: sugLoading, Words: m.st.Words, Seq: m.seq})
	if m.d.sendSuggest != nil {
		m.d.sendSuggest(m.seq, m.st.Words)
	}
}

func (m *sugMachine) doInsert(shift bool) {
	st := m.st
	if st.Sug == nil || st.Selected < 0 || st.Selected >= len(st.Sug.Options) {
		return
	}
	opt := st.Sug.Options[st.Selected]
	if ok, _ := insertGate(&opt, shift); !ok {
		k := keyLabels(m.d.mac)
		st.Note = "Press " + k.Shift + k.Tab + " to insert this command."
		m.set(st)
		return
	}
	st.State, st.Note = sugInserting, ""
	m.set(st)
	sug, selected, words := st.Sug, st.Selected, st.Words
	m.d.insert(sug, selected, shift, words, func(r insertResult) {
		if m.st.State != sugInserting {
			return
		}
		switch {
		case r.Dropped: // the prompt line changed under the card: nothing was written
			m.dismissed = words
			m.set(sugView{State: sugIdle})
		case r.OK:
			m.set(sugView{State: sugInserted, Words: words, Command: opt.Command, at: m.d.now()})
		default:
			cur := m.st.Sug // a risk update may have come in while the insert was asked
			if cur == nil {
				cur = sug
			}
			note := r.Message
			if note == "" {
				note = "Not inserted."
			}
			m.set(sugView{State: sugCard, Sug: cur, Selected: selected, Words: words, Note: note})
		}
	})
}

// OnInput: the user typed. data is what went to the shell.
func (m *sugMachine) OnInput(data string) {
	if strings.ContainsRune(data, 13) { // the user pressed Enter
		m.dueAt = time.Time{}
		m.dismissed = ""
		m.idle()
		return
	}
	if m.st.State == sugInserted || m.st.State == sugNotice {
		m.idle()
	}
	if m.st.State == sugIdle {
		m.dueAt = m.d.now().Add(detectPause)
	}
}

// OnRender: the terminal redrew; follow the line or let go of it.
func (m *sugMachine) OnRender() {
	if m.st.State == sugIdle {
		return
	}
	if !m.live() {
		m.idle()
		return
	}
	line := m.line()
	if line == nil {
		m.idle()
		return
	}
	switch m.st.State {
	case sugNotice:
		if line.Input != m.st.Words {
			m.idle()
			return
		}
	case sugInserted:
		if line.Input == m.st.Command {
			m.st.Seen = true
			break
		}
		if m.st.Seen || m.d.now().Sub(m.st.at) > insertedSeen {
			m.idle()
		}
		return
	}
	if m.d.render != nil {
		m.d.render(m.st)
	}
}

// OnKey returns true when it used the key (the caller then keeps it from the shell). ⇥ is used only while a line is
// noticed or a card shows; everywhere else it stays the shell's completion key.
func (m *sugMachine) OnKey(k sugKey) bool {
	if m.st.State == sugIdle {
		return false
	}
	if !m.live() {
		m.idle()
		return false
	}
	switch m.st.State {
	case sugNotice:
		if isTab(k) && m.st.Hash { // a # line is a request: ⇥ asks. A line noticed on its own leaves ⇥ to the shell
			m.startSuggest()
			return true
		}
		if k.Name == "Escape" {
			m.dismissed = m.st.Words
			m.idle()
			return true // not the shell's: sh reads Esc plus the next key as Meta+key and loses that key
		}
		return false
	case sugLoading, sugInserting:
		if isTab(k) {
			return true
		}
		if isModifier(k) {
			return false
		}
		if m.st.State == sugLoading {
			m.seq++
		}
		m.dismissed = m.st.Words
		m.idle()
		return k.Name == "Escape"
	case sugError:
		if isModifier(k) {
			return false
		}
		m.dismissed = m.st.Words
		m.idle()
		return isTab(k) || k.Name == "Escape"
	case sugCard:
		if isTab(k) {
			m.doInsert(k.Shift)
			return true
		}
		if k.Name == "Escape" {
			m.dismissed = m.st.Words
			m.idle()
			return true
		}
		if next := optionKey(k, m.st.Selected, len(m.st.Sug.Options)); next >= 0 {
			st := m.st
			st.Selected, st.Note = next, ""
			m.set(st)
			return true
		}
		if isModifier(k) {
			return false
		}
		m.dismissed = m.st.Words
		m.idle()
		return false
	case sugInserted:
		if undoKey(k, m.d.mac) {
			m.restore()
			return true
		}
	}
	return false
}

// Undo is the undo key's action; false unless right after an insert.
func (m *sugMachine) Undo() bool {
	if m.st.State != sugInserted {
		return false
	}
	m.restore()
	return true
}

// Select picks option i of the card (a click on it).
func (m *sugMachine) Select(i int) {
	if m.st.State == sugCard && i >= 0 && i < len(m.st.Sug.Options) {
		st := m.st
		st.Selected, st.Note = i, ""
		m.set(st)
	}
}

// OnDetect takes keel serve's answer to a detection.
func (m *sugMachine) OnDetect(seq int, plain bool, source string) {
	if m.pendSeq == 0 || seq != m.pendSeq {
		return
	}
	input := m.pendInput
	m.pendSeq = 0
	now := m.d.now()
	if source == "off" {
		m.offUntil = now.Add(offBackoff)
	}
	if source == "jev" || source == "hash" || source == "hash-only" {
		if _, ok := m.cache[input]; !ok {
			m.cacheKeys = append(m.cacheKeys, input)
		}
		m.cache[input] = detectHit{plain, now}
		if len(m.cache) > 200 {
			delete(m.cache, m.cacheKeys[0])
			m.cacheKeys = m.cacheKeys[1:]
		}
	}
	if !plain || m.st.State != sugIdle || !m.can() || m.mode() == sugOff {
		return
	}
	if line := m.line(); line != nil && line.Input == input {
		m.set(sugView{State: sugNotice, Words: input, Hash: hashLine(input)})
	}
}

// OnSuggest takes keel serve's answer to a suggestion.
func (m *sugMachine) OnSuggest(seq int, ok bool, sug *keelclient.Suggestion, message, reason string) {
	if m.st.State != sugLoading || seq != m.st.Seq {
		return
	}
	if ok && sug != nil && len(sug.Options) > 0 {
		m.set(sugView{State: sugCard, Sug: sug, Words: m.st.Words})
		return
	}
	switch reason {
	case "off", "no-key", "not-readable", "following":
		m.offUntil = m.d.now().Add(offBackoff)
	}
	if message == "" {
		message = "No suggestion came back."
	}
	m.set(sugView{State: sugError, Words: m.st.Words, Message: message})
}

// OnRisk: Jev rated the shown suggestion stricter after the card appeared.
func (m *sugMachine) OnRisk(id string, opts []keelclient.SuggestOption) {
	if (m.st.State != sugCard && m.st.State != sugInserting) || m.st.Sug == nil {
		return
	}
	next := applyRisk(m.st.Sug, id, opts)
	if next == m.st.Sug {
		return
	}
	st := m.st
	st.Sug = next
	m.set(st)
}

// Reset drops everything in flight and goes idle.
func (m *sugMachine) Reset() {
	m.dueAt = time.Time{}
	m.pendSeq = 0
	m.idle()
}
