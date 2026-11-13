package keelclient

import "time"

// The shapes of a group run (docs/ui-redesign.md §17; internal/agent/service/grouprun.go, groupvars.go): a runbook walked
// across a folder's hosts, every host × step a proposal the user approves. They carry names, states and lengths only, never
// a variable's or a captured value: the one route that returns values is the fill (FillGroupStep), called by the user's Type.

// VarRef is a variable a step's command names: {{name}} or {{name|q}}.
type VarRef struct {
	Name   string `json:"name"`
	Quoted bool   `json:"quoted"`
}

// CaptureView is one host's capture for a step: its state and length, never the value.
type CaptureView struct {
	Name      string     `json:"name"`
	State     string     `json:"state"` // captured | no-match | blocked | refused | too-short | full | no-output | expired
	Bytes     int        `json:"bytes"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// GroupProposal is keel serve's proposal for one host's step: its own id, made from the saved step command with the
// variable names in. Bytes is exactly what Type sends for a step that names no variable (Ctrl-U, the command, a newline);
// for one that does, the bytes Type sends are the fill's.
type GroupProposal struct {
	ID         string   `json:"id"`
	SessionID  string   `json:"sessionId"`
	HostID     string   `json:"hostId"`
	Host       string   `json:"host"`
	Production bool     `json:"production"`
	Command    string   `json:"command"`
	Bytes      string   `json:"bytes"`
	Risk       string   `json:"risk"`
	NeedsShift bool     `json:"needsShift"`
	Vars       []VarRef `json:"vars"`
}

// GroupRunCell is one host × step. State is waiting, proposed, typing, typed, done, failed or skipped; Result says where an
// end came from: "reported" (the exit status the pane's shell reported: a claim, never proof), "user", "unreported" or
// "abandoned".
type GroupRunCell struct {
	HostID    string         `json:"hostId"`
	Host      string         `json:"host"`
	State     string         `json:"state"`
	SessionID string         `json:"sessionId"`
	Proposal  *GroupProposal `json:"proposal"`
	Exit      *int           `json:"exit"`
	Result    string         `json:"result"`
	Confirm   bool           `json:"confirm"`
	Blocked   string         `json:"blocked"`
	Pending   bool           `json:"pending"`
	Captures  []CaptureView  `json:"captures"`
}

// GroupRunHost names a host.
type GroupRunHost struct {
	HostID string `json:"hostId"`
	Host   string `json:"host"`
}

// GroupRunMember is a host of the group. Excluded says why a member is never targeted ("Agent can read is off").
type GroupRunMember struct {
	HostID     string `json:"hostId"`
	Host       string `json:"host"`
	Role       string `json:"role"`
	Production bool   `json:"production"`
	Excluded   string `json:"excluded"`
}

// GroupRunStep is a step of the run with its hosts. Captures and Vars are names and patterns only.
type GroupRunStep struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Command     string         `json:"command"`
	Expect      string         `json:"expect"`
	Risk        string         `json:"risk"`
	Role        string         `json:"role"`
	OneHost     bool           `json:"oneHost"`
	Skipped     bool           `json:"skipped"`
	Done        bool           `json:"done"`
	Cells       []GroupRunCell `json:"cells"`
	Candidates  []GroupRunHost `json:"candidates"`
	Captures    []StepCapture  `json:"captures"`
	Vars        []VarRef       `json:"vars"`
}

// GroupRun is one run, as the run page shows it.
type GroupRun struct {
	ID         string           `json:"id"`
	RunbookID  string           `json:"runbookId"`
	Name       string           `json:"name"`
	GroupID    string           `json:"groupId"`
	Group      string           `json:"group"`
	Mode       string           `json:"mode"`   // rolling | parallel
	Status     string           `json:"status"` // active | finished | stopped
	StartedAt  time.Time        `json:"startedAt"`
	Members    []GroupRunMember `json:"members"`
	Steps      []GroupRunStep   `json:"steps"`
	Current    int              `json:"current"` // the step being walked, -1 when none is
	Halted     string           `json:"halted"`  // the host whose failure stopped a rolling step
	ModeLocked bool             `json:"modeLocked"`
	NeedPane   []GroupRunHost   `json:"needPane"`
}

// FillUsed says where one value of a fill came from ("group variable", "captured on cp-1 · step 1"): a name and a place,
// never the value.
type FillUsed struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	Quoted bool   `json:"quoted"`
}

// GroupFill is keel serve's answer to the fill for one proposal: the exact bytes Type will send for that host, with the
// values put in (the only place a value leaves keel serve), where each came from, or why none could be made. The window
// shows these bytes in the group approve dialog and sends exactly them; it never builds or edits them.
// ProposalRef is the proposal's id.
type GroupFill struct {
	ProposalRef string     `json:"proposalId"`
	HostID      string     `json:"hostId"`
	Host        string     `json:"host"`
	Bytes       string     `json:"bytes"`
	Used        []FillUsed `json:"used"`
	Risk        string     `json:"risk"`
	NeedsShift  bool       `json:"needsShift"`
	Error       string     `json:"error"`
}

// String leaves the bytes out: a fill may hold a value, and nothing may print one by accident.
func (f GroupFill) String() string {
	return "{" + f.Host + " " + f.Risk + " " + f.Error + "}"
}

// GoString is String for %#v.
func (f GroupFill) GoString() string { return "keelclient.GroupFill" + f.String() }
