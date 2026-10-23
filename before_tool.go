package poo

import (
	"context"
	"fmt"
)

// The before-tool hooks run after the call is validated and before it
// runs. A hook may refuse the call or answer it itself.
//
// The arbitration between the hooks is the plan's: the policy slot
// runs first, a refusal wins over everything, and the first result in
// registration order becomes the call's result. The hooks after that
// result still run, and only to look for a refusal.

// runBeforeTool runs the before-tool hooks and returns their decision.
//
// A hook that fails settles the run with hook_error. A hook that misses
// its deadline settles the run with hook_error too: the decision never
// arrives, and the call's fate is unknown.
//
// The second result reports that an interrupt ended the chain. The call
// must not start then, and the first result collected before the
// interrupt still becomes the call's result.
func (s *Session) runBeforeTool(run *runState, turn int, handle, tool string, prepared Prepared, plan callPlan) (ToolDecision, bool, error) {
	ctx := s.activityContext(run)
	info := s.toolCallInfo(turn, handle, tool, prepared, plan)

	// The policy slot runs first. It is exempt from the hook deadline:
	// the permission engine keeps its own chain deadline and always
	// returns a decision.
	if policy := s.opts.Policy; policy != nil {
		decision, err := s.callPolicy(ctx, policy, turn, info)
		if err != nil {
			return ToolDecision{}, false, err
		}
		if decision.Denied {
			return decision, false, nil
		}
	}

	// The consumer hooks run in registration order. A refusal stops the
	// chain, and the first result is kept while the rest of the chain
	// looks for a refusal.
	var result *ToolResult
	for _, h := range s.opts.BeforeTool {
		out, err := s.callHook(ctx, h, func(hctx context.Context) (any, error) {
			return h.BeforeTool(s.hookContext(hctx, turn, false), info)
		})
		if out.interrupted {
			// The hook's decision is discarded, and that is not a
			// hook error. The chain stops, the call does not start,
			// and the first result collected before the interrupt
			// still becomes the call's result.
			return ToolDecision{Allowed: result == nil, Result: result}, true, nil
		}
		if err != nil {
			return ToolDecision{}, false, &hookFailure{point: "before_tool", hook: hookName(h), err: fmt.Errorf("poo: the %s hook: %w", hookName(h), err)}
		}
		decision, _ := out.value.(ToolDecision)
		// A refusal wins over everything else.
		if decision.Denied {
			return decision, false, nil
		}
		if decision.Result != nil && result == nil {
			// The first result in registration order becomes the
			// call's result. The hooks after it still run, and only
			// to look for a refusal.
			result = decision.Result
		}
	}
	if result != nil {
		return ToolDecision{Allowed: true, Result: result}, false, nil
	}
	return ToolDecision{Allowed: true}, false, nil
}

// callPolicy runs the policy slot.
//
// The slot is not subject to the hook deadline, so it is called
// directly: the permission engine enforces its own chain deadline and
// always returns a decision. A run's cancellation still cancels the
// slot's context, and a decision that arrives after the cancellation is
// discarded: the call is refused rather than allowed.
func (s *Session) callPolicy(ctx context.Context, h PolicyHook, turn int, info ToolCallInfo) (ToolDecision, error) {
	decision, err := h.BeforeTool(s.hookContext(ctx, turn, false), info)
	if ctx.Err() != nil {
		// The run ended while the slot was thinking. Its decision is
		// discarded, and the call is refused: a run that is ending
		// must not start a call. The cancellation is not a hook
		// failure, even when the slot returns its context's error.
		return Deny("The run ended before the policy answered."), nil
	}
	if err != nil {
		return ToolDecision{}, &hookFailure{point: "before_tool", hook: hookName(h), err: fmt.Errorf("poo: the %s policy hook: %w", hookName(h), err)}
	}
	// The policy slot only ever allows or denies. A result it returns
	// is ignored: the slot decides whether the call runs at all, not
	// what it returns.
	decision.Result = nil
	return decision, nil
}

// toolCallInfo describes a call to the hooks that ask about it.
//
// The counts are the doom loop's, and the policy slot reads them when
// it is asked about a call.
func (s *Session) toolCallInfo(turn int, handle, tool string, prepared Prepared, plan callPlan) ToolCallInfo {
	counts := s.repeatCountFor(handle)
	return ToolCallInfo{
		// The spec is the one the model was offered: a background-capable
		// tool's schema carries the background argument in it, so a hook
		// that validates arguments against the advertised schema sees
		// the same shape the call has.
		Spec:            s.toolSpecFor(tool),
		Prepared:        prepared,
		Handle:          handle,
		RepeatCount:     counts.exact,
		NearRepeatCount: counts.near,
		BatchIndex:      plan.index,
	}
}

// repeatCountFor returns the repeat counts of one call.
//
// A call that was never counted has no counts.
func (s *Session) repeatCountFor(handle string) repeatCounts {
	counts := s.RepeatCountsFor(handle)
	return repeatCounts{exact: counts.Exact, near: counts.Near}
}
