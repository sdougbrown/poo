package poo

// Reason is the closed set of reasons a run can settle for.
//
// The skeleton produces only a subset of the v1 enum: the other reasons
// belong to features that arrive in later stages.
type Reason string

const (
	// ReasonEndTurn means the model stopped on its own.
	ReasonEndTurn Reason = "end_turn"
	// ReasonMaxTurns means the run hit its turn ceiling.
	ReasonMaxTurns Reason = "max_turns"
	// ReasonCancelled means the run was cancelled.
	ReasonCancelled Reason = "cancelled"
	// ReasonProviderError means a provider request failed.
	ReasonProviderError Reason = "provider_error"
	// ReasonContextExhausted means the context window is full. The
	// skeleton produces it from a provider context-length error, or from a
	// computed max_tokens under 256.
	ReasonContextExhausted Reason = "context_exhausted"
)

// ExitCode returns the process exit code for a settle reason.
//
// The codes are the v1 table from the plan. A reason the skeleton does not
// produce maps to 1, so a bug never reports success.
func ExitCode(r Reason) int {
	switch r {
	case ReasonEndTurn:
		return 0
	case ReasonMaxTurns:
		return 11
	case ReasonContextExhausted:
		return 13
	case ReasonProviderError:
		return 20
	case ReasonCancelled:
		return 130
	default:
		return 1
	}
}

// The settle sources a cancelled run records. A consumer tells a
// deadline from a plain cancel by the source, not by the text.
const (
	// SourceCancelContext is a run whose context was cancelled.
	SourceCancelContext = "cancel:context"
	// SourceCancelDeadline is a run whose context deadline passed.
	SourceCancelDeadline = "cancel:deadline"
)
