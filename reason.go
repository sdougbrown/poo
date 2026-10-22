package poo

// Reason is the closed set of reasons a run can settle for.
//
// The set is closed and typed: the wire, the records and the exit codes
// all name the same reasons, so a consumer can read a settle record
// without guessing.
type Reason string

const (
	// ReasonCancelled means the run was cancelled.
	ReasonCancelled Reason = "cancelled"
	// ReasonStoreError means the session's store failed. The session
	// becomes broken.
	ReasonStoreError Reason = "store_error"
	// ReasonHookError means an executable hook failed or missed its
	// deadline.
	ReasonHookError Reason = "hook_error"
	// ReasonToolFatal means a tool failed with an error that wraps
	// ErrFatal: the environment is unusable.
	ReasonToolFatal Reason = "tool_fatal"
	// ReasonProviderLimit means the provider refused for quota or plan
	// reasons. The session is not broken, so a later prompt may
	// succeed.
	ReasonProviderLimit Reason = "provider_limit"
	// ReasonProviderError means a provider request failed and cannot be
	// retried, or the retry budget was spent.
	ReasonProviderError Reason = "provider_error"
	// ReasonContextExhausted means the context window is full.
	ReasonContextExhausted Reason = "context_exhausted"
	// ReasonHookContinueLimit means the BeforeSettle hooks asked for
	// more continuations than the run allows.
	ReasonHookContinueLimit Reason = "hook_continue_limit"
	// ReasonMaxTurns means the run hit its turn ceiling.
	ReasonMaxTurns Reason = "max_turns"
	// ReasonBudgetExhausted means the run hit a token or time budget.
	ReasonBudgetExhausted Reason = "budget_exhausted"
	// ReasonFinished means the model called the finish tool with a
	// valid result.
	ReasonFinished Reason = "finished"
	// ReasonStopRequested means a hook or a stop condition asked the
	// run to stop.
	ReasonStopRequested Reason = "stop_requested"
	// ReasonOutputTruncated means the provider stopped on the
	// output-token limit.
	ReasonOutputTruncated Reason = "output_truncated"
	// ReasonEndTurn means the model stopped on its own.
	ReasonEndTurn Reason = "end_turn"
	// ReasonInterrupted means the run's outcome is unknown: the process
	// stopped while the run was going.
	//
	// It is never produced live. It is written on load, for a run that
	// has no settle record, and it has no exit code of its own.
	ReasonInterrupted Reason = "interrupted"
)

// reasonPrecedence is the arbitration order.
//
// A lower number wins. The order is the plan's table: cancelled beats
// everything, the failure reasons come next, and the natural reasons come
// last, so a real failure is never hidden by a natural stop.
//
// provider_limit and provider_error share a level, because a single
// failed request produces only one of them.
var reasonPrecedence = map[Reason]int{
	ReasonCancelled:         1,
	ReasonStoreError:        2,
	ReasonHookError:         3,
	ReasonToolFatal:         4,
	ReasonProviderLimit:     5,
	ReasonProviderError:     5,
	ReasonContextExhausted:  6,
	ReasonHookContinueLimit: 7,
	ReasonMaxTurns:          8,
	ReasonBudgetExhausted:   9,
	ReasonFinished:          10,
	ReasonStopRequested:     11,
	ReasonOutputTruncated:   12,
	ReasonEndTurn:           13,
}

// Precedence returns the arbitration order of a reason.
//
// A lower number wins. A reason the table does not know sorts last, so
// a bug never outranks a real outcome.
func Precedence(r Reason) int {
	if p, ok := reasonPrecedence[r]; ok {
		return p
	}
	return len(reasonPrecedence) + 1
}

// IsNatural reports whether a reason is a natural stop: the model
// stopped on its own.
//
// A natural reason is the only kind a finalize may abandon: a run that
// would settle naturally can still take a queued input, while a run that
// is settling because of a failure or a limit cannot.
func IsNatural(r Reason) bool {
	return r == ReasonEndTurn || r == ReasonOutputTruncated
}

// ExitCode returns the process exit code for a settle reason.
//
// The codes are the plan's table. A reason the table does not know maps
// to 1, so a bug never reports success.
func ExitCode(r Reason) int {
	switch r {
	case ReasonEndTurn, ReasonStopRequested, ReasonFinished:
		return 0
	case ReasonMaxTurns:
		return 11
	case ReasonOutputTruncated:
		return 10
	case ReasonHookContinueLimit:
		return 12
	case ReasonContextExhausted:
		return 13
	case ReasonBudgetExhausted:
		return 14
	case ReasonToolFatal:
		return 21
	case ReasonHookError:
		return 22
	case ReasonStoreError:
		return 23
	case ReasonProviderLimit:
		return 24
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
