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

// What a notebook card is about (NotebookProposal.Kind).
const (
	NotebookCreate = "create"
	NotebookEdit   = "edit"
	NotebookDelete = "delete"
)

// How a typed step's result is known: a runbook step's and a group run
// cell's Result.
const (
	ResultReported   = "reported"   // the shell's marks (OSC 133) gave an exit status
	ResultUser       = "user"       // the user marked it
	ResultUnreported = "unreported" // the command ended with no status
	ResultAbandoned  = "abandoned"  // the shell went away
)
