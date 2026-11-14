package wire

// The words the agent service puts in what it returns and saves, and the
// window reads. Saved conversations hold them, so a value never changes.

// What the agent did with one thing in scope (a trace step's Status).
const (
	TraceRead          = "read"           // a pane was read
	TraceSkipped       = "skipped"        // not read: Reason says why
	TraceNotes         = "notes"          // notes for the agent were sent
	TraceRunbook       = "runbook"        // a runbook was sent, or picked
	TraceNotebook      = "notebook"       // a notebook was sent
	TraceNotebookIndex = "notebook-index" // the notebooks' titles were sent
)

// Why a pane or a notebook was skipped (a trace step's Reason).
const (
	SkipNotOpen       = "not-open"       // the pane is closed
	SkipNotReadable   = "not-readable"   // Agent can read is off for its host
	SkipReadFailed    = "read-failed"    // the read failed, or could not be logged
	SkipNotebookLimit = "notebook-limit" // the agent asked for more notebooks than one ask reads

	SkipNotebookMissing = "notebook-missing" // the notebook is gone
	SkipNotebookPrivate = "notebook-private" // the notebook is private
)

// A card's Status. A note's and a runbook's card is open, saved or
// dismissed; a notebook's card says what was done to the notebook.
const (
	CardOpen      = "open"
	CardSaved     = "saved"
	CardDismissed = "dismissed"

	CardCreated  = "created"  // the agent created the notebook
	CardEdited   = "edited"   // the agent edited it
	CardRefused  = "refused"  // the change was not made: Reason says why
	CardUndone   = "undone"   // the user undid the change
	CardProposed = "proposed" // a delete waiting for the user
	CardDeleted  = "deleted"  // the user confirmed the delete
	CardRestored = "restored" // the user brought the deleted notebook back
)

// What a notebook card is about (a notebook proposal's Kind).
const (
	NotebookCreate = "create"
	NotebookEdit   = "edit"
	NotebookDelete = "delete"
)

// Why a notebook change was refused (a notebook card's Reason when its
// Status is CardRefused).
const (
	RefusedConflict  = "conflict"  // changed since the agent read it
	RefusedEditing   = "editing"   // unsaved typing
	RefusedPrivate   = "private"   // the notebook is private
	RefusedNotRead   = "not-read"  // not read in this ask
	RefusedNotFound  = "not-found" // not a notebook the agent may change
	RefusedDeleted   = "deleted"   // it was deleted
	RefusedNoMatch   = "no-match"  // the text to change was not found exactly once
	RefusedRedacted  = "redacted"
	RefusedTruncated = "truncated"
	RefusedTooLarge  = "too-large"
	RefusedHistory   = "history"
	RefusedInvalid   = "invalid"
)

// How a typed step's result is known: a runbook step's and a group run
// cell's Result.
const (
	ResultReported   = "reported"   // the shell's marks (OSC 133) gave an exit status
	ResultUser       = "user"       // the user marked it
	ResultUnreported = "unreported" // the command ended with no status
	ResultAbandoned  = "abandoned"  // the shell went away
)

// A runbook run's or group run's Status.
const (
	RunActive   = "active"
	RunFinished = "finished"
	RunStopped  = "stopped"
)

// How a group run goes through its hosts, and how a group exports.
const (
	ModeRolling  = "rolling"
	ModeParallel = "parallel"

	FormatINI  = "ini"
	FormatYAML = "yaml"
)

// A captured value's state, per host. Never the value.
const (
	CaptureCaptured = "captured"
	CaptureNoMatch  = "no-match"
	CaptureBlocked  = "blocked"   // holds a vault value: not kept
	CaptureRefused  = "refused"   // a control character, or too long: not kept
	CaptureNoOutput = "no-output" // no OSC 133 output to read
	CaptureExpired  = "expired"
	CaptureTooShort = "too-short" // under 3 bytes: could not be kept from the model
	CaptureFull     = "full"      // the withheld set is full
)

// A runbook step's State, and a group run cell's (host by step): a cell is
// never StepNext, a step never StepWaiting or StepTyping.
const (
	StepNext     = "next"
	StepWaiting  = "waiting"
	StepProposed = "proposed"
	StepTyping   = "typing" // authorized and being written now
	StepTyped    = "typed"
	StepDone     = "done"
	StepFailed   = "failed"
	StepSkipped  = "skipped"
)

// What a TraceNotes step's NoteKind says the notes were about.
const (
	NoteHost  = "host"
	NoteUser  = "user"
	NoteGroup = "group"
)
