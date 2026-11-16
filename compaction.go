package poo

import "context"

// Compactor is a strategy that changes the model's view of the history.
//
// A compactor may only change what the model sees, and the change must be
// visible: the store keeps the original history and records what the
// compactor did.
type Compactor interface {
	// ShouldCompact reports whether the view should be compacted before
	// the next request.
	ShouldCompact(view View, budget ContextBudget) bool
	// Compact compacts the view the environment carries.
	Compact(ctx context.Context, in CompactionEnv) (Compaction, error)
}

// CompactionEnv is what the core hands a compactor: the view to compact,
// the run's budget, and the two ways a strategy reaches the model.
//
// The view is the model's history as it stands now, after any compaction,
// so a strategy never summarizes a window the run has moved past.
type CompactionEnv struct {
	// View is the model's history.
	View View
	// Budget is the run's context budget.
	Budget ContextBudget
	// Fork re-sends the last main request with messages appended. It is
	// the cheapest way to ask the model something, and the only way to
	// see what the model itself saw. A strategy that asks about the
	// conversation uses it.
	Fork ForkFunc
	// Ask sends messages of the strategy's own, with nothing before
	// them. It is how a strategy reads the whole current view. A fork
	// cannot: it carries the window of the last main request, and that
	// window cannot be reshaped.
	Ask AskFunc
}

// ForkFunc sends a request that is byte-identical to the last main
// request, apart from the appended messages and the output limit.
//
// Core binds it to the transport's Fork and to the last request's
// snapshot, so a strategy cannot build a fork request any other way.
type ForkFunc func(ctx context.Context, appended []Message, maxTokens int) (Reply, error)

// AskFunc sends the messages alone, as a request of the strategy's own.
//
// The request is not a turn: it appends nothing to the history, carries
// no tools, and the run reads nothing it produced.
type AskFunc func(ctx context.Context, messages []Message, maxTokens int) (Reply, error)

// Reply is what a request a strategy made produced.
//
// A strategy that records the reply needs the finish reason. A reply the
// output limit cut short is incomplete, and recording it as if it were
// whole hands the model a truncated note or summary.
type Reply struct {
	// Message is the model's reply.
	Message Message
	// Finish is why the model stopped.
	Finish FinishReason
	// Usage is what the request cost.
	Usage UsageReport
}

// Compaction is what a compactor produced.
//
// Exactly one of the four forms is set. The forms change the model's view
// in different ways, and the core validates which one it got.
type Compaction struct {
	// Strategy names the strategy that produced the compaction.
	Strategy string `json:"strategy"`
	// Replace turns a range of completed turns into visible messages.
	Replace *RangeReplacement `json:"replace,omitempty"`
	// Rewrite changes the content of tool results, by handle.
	Rewrite []ToolResultRewrite `json:"rewrite,omitempty"`
	// Window is a provider-native window: the model's own compacted
	// state, used as it is.
	Window *ProviderWindow `json:"window,omitempty"`
	// End refuses to compact: the run settles with the handoff.
	End *EndRun `json:"end,omitempty"`
}

// TurnRange is a range of turns, counting from one.
type TurnRange struct {
	// First is the range's first turn.
	First int `json:"first"`
	// Last is the range's last turn.
	Last int `json:"last"`
}

// RangeReplacement replaces a range of completed turns with messages the
// model can read.
//
// The messages are visible: the model must be able to see that its history
// was replaced, not have it happen silently.
type RangeReplacement struct {
	// Cut is the range of turns the replacement covers.
	Cut TurnRange `json:"cut"`
	// Messages are the messages that replace the range.
	Messages []Message `json:"replacement"`
}

// ToolResultRewrite changes the content of one tool result.
//
// The new content keeps the handle and starts with the compacted marker,
// so the model can tell the result was rewritten.
type ToolResultRewrite struct {
	// Handle is the tool result's handle, e.g. "r1".
	Handle string `json:"handle"`
	// Content is the result's new content.
	Content string `json:"content"`
}

// ProviderWindow is a provider-native compaction: the model's own
// compacted state.
//
// Every item is a `provider` content block, and every one of them is
// required: the window cannot be rebuilt from anything else. The core
// checks this before it accepts a window.
type ProviderWindow struct {
	// Items are the window's blocks.
	Items []ContentBlock `json:"items"`
}

// EndRun is a compactor's refusal to compact.
//
// The run settles as context_exhausted with the handoff in the settle
// detail, so a consumer can carry the work on.
type EndRun struct {
	// Handoff is the note the next agent picks up.
	Handoff string `json:"handoff"`
}

// NativeCompactor is implemented by a transport that can compact the
// provider's own state.
//
// A provider window is only accepted when it came from the session's own
// transport, because the window is the model's own state and nothing else
// can stand in for it.
type NativeCompactor interface {
	// Compact asks the provider to compact its own state and returns the
	// window it produced.
	Compact(ctx context.Context, prev Rendered) (ProviderWindow, error)
}
