package service

// The words the service puts in what it returns and saves, in one place.
// They are wire values: the window reads them, and saved conversations
// hold them.

// What the agent did with one thing in scope (TraceStep.Status).
const (
	TraceRead          = "read"           // a pane was read
	TraceSkipped       = "skipped"        // not read: Reason says why
	TraceNotes         = "notes"          // notes for the agent were sent
	TraceRunbook       = "runbook"        // a runbook was sent, or picked
	TraceNotebook      = "notebook"       // a notebook was sent
	TraceNotebookIndex = "notebook-index" // the notebooks' titles were sent
)

// Why a pane or a notebook was skipped (TraceStep.Reason). A notebook's
// own reasons (NotebookProposal.Reason) are listed there.
const (
	SkipNotOpen       = "not-open"       // the pane is closed
	SkipNotReadable   = "not-readable"   // Agent can read is off for its host
	SkipReadFailed    = "read-failed"    // the read failed, or could not be logged
	SkipNotebookLimit = "notebook-limit" // the agent asked for more notebooks than one ask reads
)

// How a typed step's result is known: a runbook step's and a group run
// cell's Result.
const (
	ResultReported   = "reported"   // the shell's marks (OSC 133) gave an exit status
	ResultUser       = "user"       // the user marked it
	ResultUnreported = "unreported" // the command ended with no status
	ResultAbandoned  = "abandoned"  // the shell went away
)
