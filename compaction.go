package poo

import "context"

// Compactor is a strategy that changes the model's view of the history.
//
// The seam exists in v1; no strategy ships with it but the handoff one.
// A compactor may only change what the model sees, and the change must be
// visible: the store keeps the original history and records what the
// compactor did.
type Compactor interface {
	// ShouldCompact reports whether the view should be compacted before
	// the next request.
	ShouldCompact(view View, budget Budget) bool
	// Compact compacts the view. The fork is the only way to reach the
	// provider: a strategy that needs the model to write something
	// forks the last request.
	Compact(ctx context.Context, view View, fork ForkFunc) (Compaction, error)
}

// ForkFunc sends a request that is byte-identical to the last main
// request, apart from the appended messages and the output ceiling.
//
// Core binds it to the transport's Fork and to the last request's
// snapshot, so a strategy cannot build a fork request any other way.
type ForkFunc func(ctx context.Context, appended []Message, maxTokens int) (Message, UsageReport, error)

// Compaction is what a compactor produced.
//
// Exactly one of the four forms is set. The forms change the model's view
// in different ways, and the core validates which one it got.
type Compaction struct {
	// Strategy names the strategy that produced the compaction.
	Strategy string
	// Replace turns a range of completed turns into visible messages.
	Replace *RangeReplacement
	// Rewrite changes the content of tool results, by handle.
	Rewrite []ToolResultRewrite
	// Window is a provider-native window: the model's own compacted
	// state, used as it is.
	Window *ProviderWindow
	// End refuses to compact: the run settles with the handoff.
	End *EndRun
}

// TurnRange is a range of turns, counting from one.
type TurnRange struct {
	// First is the range's first turn.
	First int
	// Last is the range's last turn.
	Last int
}

// RangeReplacement replaces a range of completed turns with messages the
// model can read.
//
// The messages are visible: the model must be able to see that its history
// was replaced, not have it happen silently.
type RangeReplacement struct {
	// Cut is the range of turns the replacement covers.
	Cut TurnRange
	// Messages are the messages that replace the range.
	Messages []Message
}

// ToolResultRewrite changes the content of one tool result.
//
// The new content keeps the handle and starts with the compacted marker,
// so the model can tell the result was rewritten.
type ToolResultRewrite struct {
	// Handle is the tool result's handle, e.g. "r1".
	Handle string
	// Content is the result's new content.
	Content string
}

// ProviderWindow is a provider-native compaction: the model's own
// compacted state.
//
// Every item is a `provider` content block, and every one of them is
// required: the window cannot be rebuilt from anything else. The core
// checks this before it accepts a window.
type ProviderWindow struct {
	// Items are the window's blocks.
	Items []ContentBlock
}

// EndRun is a compactor's refusal to compact.
//
// The run settles as context_exhausted with the handoff in the settle
// detail, so a consumer can carry the work on.
type EndRun struct {
	// Handoff is the note the next agent picks up.
	Handoff string
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
