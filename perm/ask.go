package perm

import (
	"github.com/sdougbrown/poo"
)

// Ask is one permission ask.
//
// The ask carries what a client needs to answer it: the run that is
// waiting, the call, and why the call is being asked about. The history
// and the working directory are the model's own, and they never leave
// the process except through the evidence a decision model is asked
// about.
type Ask struct {
	// SessionID identifies the session the run belongs to.
	SessionID string `json:"session_id"`
	// RunID identifies the run that is waiting.
	RunID string `json:"run_id"`
	// AskID names the ask.
	//
	// The id is unique within the session, and a client answers by it.
	AskID string `json:"ask_id"`
	// Tool is the name of the tool the model called.
	Tool string `json:"tool"`
	// Summary describes the call.
	Summary string `json:"summary"`
	// Class is the tool's class: "read_only" or "side_effecting".
	Class poo.ToolClass `json:"class"`
	// Reason is why the call is being asked about: "policy" or
	// "doom_loop".
	Reason string `json:"reason"`
	// RepeatCount holds the doom loop's counts. It is set for a
	// doom-loop ask, and absent for a policy ask.
	RepeatCount *poo.RepeatCounts `json:"repeat_count,omitempty"`
	// History is the model's history, for an asker that reads
	// evidence.
	History poo.View `json:"-"`
	// WorkingDir is the directory the run works in.
	WorkingDir string `json:"-"`
}
