// Package codemode holds the seam a script engine plugs into.
//
// A codemode engine runs model-written scripts whose only capability is
// calling the session's tools. The nested-call seam (poo.NestedCaller)
// makes the calls, so the permission engine and the doom loop judge
// every one of them, and the script's intermediate results never enter
// the model's history: only the script's output and its return value
// come back.
//
// An engine is linked by the binary that wants it, like a tool or an
// executor is: what the binary leaves out is not reachable at runtime.
package codemode

import (
	"context"
	"encoding/json"

	"github.com/sdougbrown/poo"
)

// Engine runs model-written scripts.
//
// An implementation is a sandboxed language runtime: the script it runs
// can reach nothing but the tool calls the RunOptions hand it. The
// engine enforces the limits, and it reports a script that ran past
// them as a failed run, not as a partial one.
type Engine interface {
	// Run executes one script.
	Run(ctx context.Context, opts RunOptions) (RunResult, error)
}

// RunOptions are one script run's inputs.
type RunOptions struct {
	// Script is the program text.
	Script string
	// Nested makes the tool calls. The engine hands it the calls the
	// script makes, in batches, and every call it makes is judged and
	// recorded by the session.
	Nested poo.NestedCaller
	// Limits bound the run.
	Limits Limits
}

// Limits bound one script run. A limit of zero takes the engine's own
// default.
type Limits struct {
	// TimeoutS is the wall-clock limit of one run, in seconds.
	TimeoutS int
	// ScriptMaxBytes is the longest script the engine accepts.
	ScriptMaxBytes int
	// OutputCapChars is the most printed text a run keeps.
	OutputCapChars int
	// ExecSteps is the engine's own execution budget, in whatever unit
	// the engine counts. A script that spends it is stopped.
	ExecSteps int
}

// RunResult is what one script run produced.
type RunResult struct {
	// Output is the lines the script printed, in order.
	Output []string
	// Truncated is true when the engine cut or dropped printed output at
	// its cap.
	Truncated bool
	// Value is the value the script exited with, as JSON. Nil when the
	// script exited without a value.
	Value json.RawMessage
	// Calls is how many tool calls the script made.
	Calls int
}
