package poo

// candidate is one reason a run might settle for.
//
// A run collects candidates from every source — turn end, cancellation,
// failures, and failures found while finalizing — and the highest
// precedence one wins at the commit point. The outcome therefore never
// depends on which goroutine arrived first.
type candidate struct {
	// reason is the settle reason.
	reason Reason
	// detail explains the outcome.
	detail SettleDetail
}

// newCandidate returns a candidate for a reason.
func newCandidate(reason Reason, detail SettleDetail) candidate {
	return candidate{reason: reason, detail: detail}
}

// StopCondition is a condition that ends a run.
//
// A stop condition is asked at every turn end, after the whole batch has
// run. It returns whether the run should stop, and a detail string that
// explains it. The detail is never a reason: the reason is the core's.
//
// A custom condition is a plain function of the state, so a consumer
// does not need a type to express one:
//
//	poo.StopCondition(func(s poo.TurnSnapshot) (bool, string) {
//		return s.Turn > 10, "ten turns is enough"
//	})
type StopCondition func(state TurnSnapshot) (stop bool, detail string)

// TurnSnapshot is what a stop condition is asked about.
//
// The state carries what the turn did, so a condition can look at the
// calls that ran and their outcomes.
type TurnSnapshot struct {
	// Turn is the turn that just ended, counting from one.
	Turn int
	// Calls are the calls the turn made, in order.
	Calls []CallState
	// RunTokens is the run's cumulative token count so far.
	RunTokens int
}

// CallState is one call of a finished turn.
type CallState struct {
	// Handle is the call's handle, e.g. "r1".
	Handle string
	// Tool is the name of the tool the model called.
	Tool string
	// Outcome is the call's outcome.
	Outcome Outcome
	// IsError reports that the call's result was an error.
	IsError bool
}

// HasToolCall stops the run when the model calls a named tool.
//
// Only a call whose result is not an error matches. A call that became a
// job counts: `started` is not an error. The condition is asked after
// the whole batch, so the batch always completes first.
func HasToolCall(name string) StopCondition {
	return func(state TurnSnapshot) (bool, string) {
		for _, c := range state.Calls {
			if c.Tool != name || c.IsError {
				continue
			}
			return true, "the model called " + name
		}
		return false, ""
	}
}
