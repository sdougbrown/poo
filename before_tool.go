package poo

import (
	"context"
	"encoding/json"
	"errors"
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

	if plan.policyErr != nil {
		return ToolDecision{}, false, plan.policyErr
	}
	if plan.policy != nil && plan.policy.Denied {
		return *plan.policy, false, nil
	}

	// The consumer hooks run in registration order. A refusal stops the
	// chain, and the first result is kept while the rest of the chain
	// looks for a refusal.
	var result *ToolResult
	for _, h := range s.opts.BeforeTool {
		hookInfo := info
		hookInfo.Arguments = cloneArguments(info.Arguments)
		out, err := s.callHook(ctx, h, func(hctx context.Context) (any, error) {
			return h.BeforeTool(s.hookContext(hctx, turn, false), hookInfo)
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
func (s *Session) callPolicy(ctx context.Context, run *runState, h PolicyHook, turn int, info ToolCallInfo) (ToolDecision, error) {
	// The slot gets the permission seam, so it can report the asks it
	// made and clear the counters of a loop it allowed. An interrupt is
	// its own provenance, so the slot can tell it from a cancelled run.
	hctx := s.hookContext(ctx, turn, false)
	hctx.Permissions = &permissionReporter{session: s, run: run}
	hctx.Interrupted = errors.Is(context.Cause(ctx), errInterrupted)
	decision, err := h.BeforeTool(hctx, info)
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
		Arguments:       cloneArguments(plan.call.Arguments),
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

// resetRepeats clears the repeat counters of one call.
//
// The policy slot calls this when it allows a doom loop: the loop starts
// over. Model-ordered policy dispatch ensures that the next call is
// counted from the reset rather than a precomputed snapshot.
func (s *Session) resetRepeats(run *runState, handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run == nil || s.run != run || run.sealed || run.committed || run.permissionHandle != handle || run.repeats == nil {
		return
	}
	run.repeats.reset(run.permissionTool)
}

// cloneArguments returns a copy of a call's arguments.
//
// A hook gets its own copy, so neither the model's history nor another
// hook sees what it changes.
func cloneArguments(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
