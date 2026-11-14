package service

import "keel/internal/wire"

// The words the service puts in what it returns and saves are wire values
// (the window reads them, and saved conversations hold them): they live in
// package wire, and the service uses them under these names.

const (
	TraceRead          = wire.TraceRead
	TraceSkipped       = wire.TraceSkipped
	TraceNotes         = wire.TraceNotes
	TraceRunbook       = wire.TraceRunbook
	TraceNotebook      = wire.TraceNotebook
	TraceNotebookIndex = wire.TraceNotebookIndex

	SkipNotOpen       = wire.SkipNotOpen
	SkipNotReadable   = wire.SkipNotReadable
	SkipReadFailed    = wire.SkipReadFailed
	SkipNotebookLimit = wire.SkipNotebookLimit

	CardOpen      = wire.CardOpen
	CardSaved     = wire.CardSaved
	CardDismissed = wire.CardDismissed
	CardCreated   = wire.CardCreated
	CardEdited    = wire.CardEdited
	CardRefused   = wire.CardRefused
	CardUndone    = wire.CardUndone
	CardProposed  = wire.CardProposed
	CardDeleted   = wire.CardDeleted
	CardRestored  = wire.CardRestored

	NotebookCreate = wire.NotebookCreate
	NotebookEdit   = wire.NotebookEdit
	NotebookDelete = wire.NotebookDelete

	ResultReported   = wire.ResultReported
	ResultUser       = wire.ResultUser
	ResultUnreported = wire.ResultUnreported
	ResultAbandoned  = wire.ResultAbandoned

	ModeRolling  = wire.ModeRolling
	ModeParallel = wire.ModeParallel

	CaptureCaptured = wire.CaptureCaptured
	CaptureNoMatch  = wire.CaptureNoMatch
	CaptureBlocked  = wire.CaptureBlocked
	CaptureRefused  = wire.CaptureRefused
	CaptureNoOutput = wire.CaptureNoOutput
	CaptureExpired  = wire.CaptureExpired
	CaptureTooShort = wire.CaptureTooShort
	CaptureFull     = wire.CaptureFull
)
