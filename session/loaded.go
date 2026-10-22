package session

import "github.com/sdougbrown/poo"

// Loaded is what a load found.
//
// The records are the session's, in order, after the recovery: a caller
// rebuilds the session from them.
type Loaded struct {
	// ID identifies the session.
	ID string
	// Metadata is the session's header: the profile it was created
	// with, the models it talked to, the tools it was given and the
	// binary that created it.
	Metadata Metadata
	// Records are the session's records, in order.
	Records []poo.Record
	// RecoveredTail reports that the log's last line was cut short and
	// the file was truncated.
	RecoveredTail bool
	// Requeue are the job results that were never delivered, in
	// completion order. A caller delivers them again: a job's outcome
	// is a fact the model must see.
	Requeue []Requeued
	// Warnings report what changed since the session was written: a
	// different build, or different settings. The records are still
	// readable.
	Warnings []string
}
