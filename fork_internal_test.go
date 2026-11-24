package poo

import "testing"

// The fork's output limit: the fork re-sends the whole window plus the
// messages it appends, so a limit over the room they leave is a refusal
// at the provider.

// TestAForkLimitWithinTheRoomStays covers a fork whose ask fits: the
// limit goes out as the strategy asked it.
func TestAForkLimitWithinTheRoomStays(t *testing.T) {
	budget := ContextBudget{ContextLimit: 128000, EstimatedInput: 100000}
	if got := forkOutputLimit(budget, 0, 16384); got != 16384 {
		t.Errorf("the fork's limit is %d, want 16384", got)
	}
}

// TestAForkLimitCountsTheAppendedInput covers the appended messages:
// the fork re-sends them after the window, so their input narrows the
// room the limit may claim.
func TestAForkLimitCountsTheAppendedInput(t *testing.T) {
	budget := ContextBudget{ContextLimit: 128000, EstimatedInput: 110000, CharsPerToken: 4}
	// The room is 128000 - 110000 - 500 - 256 = 17244.
	if got := forkOutputLimit(budget, 500, 20000); got != 17244 {
		t.Errorf("the fork's limit is %d, want the room of 17244", got)
	}
}

// TestAForkLimitOverTheRoomClampsToTheRoom covers the clamp: the limit
// lands on the room the window leaves after the input and the reserve.
func TestAForkLimitOverTheRoomClampsToTheRoom(t *testing.T) {
	budget := ContextBudget{ContextLimit: 128000, EstimatedInput: 110000}
	// The room is 128000 - 110000 - 256 = 17744.
	if got := forkOutputLimit(budget, 0, 20000); got != 17744 {
		t.Errorf("the fork's limit is %d, want the room of 17744", got)
	}
}

// TestAForkLimitOverATinyRoomFloorsAtTheMinimum covers a window whose
// room is under the gate's minimum: the limit floors at the minimum,
// because a request whose max_tokens is under it fails at the provider
// anyway, and a well-formed request fails with a readable error.
func TestAForkLimitOverATinyRoomFloorsAtTheMinimum(t *testing.T) {
	budget := ContextBudget{ContextLimit: 1000, EstimatedInput: 900}
	if got := forkOutputLimit(budget, 0, 20000); got != MinOutputTokens {
		t.Errorf("the fork's limit is %d, want the %d-token minimum", got, MinOutputTokens)
	}
}

// TestAForkLimitWithoutAWindowStays covers a model that declares no
// window: there is nothing to clamp against.
func TestAForkLimitWithoutAWindowStays(t *testing.T) {
	budget := ContextBudget{EstimatedInput: 100000}
	if got := forkOutputLimit(budget, 0, 16384); got != 16384 {
		t.Errorf("the fork's limit is %d, want 16384", got)
	}
}

// TestAForkLimitWithoutAnAskStays covers a fork that asks for no limit
// of its own: it keeps asking for none, so the body carries no
// max_tokens key to replace.
func TestAForkLimitWithoutAnAskStays(t *testing.T) {
	budget := ContextBudget{ContextLimit: 1000, EstimatedInput: 900}
	if got := forkOutputLimit(budget, 0, 0); got != 0 {
		t.Errorf("the fork's limit is %d, want 0", got)
	}
}
