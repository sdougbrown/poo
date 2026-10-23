package poo

import (
	"context"
	"fmt"
)

// The before-tool hooks run after the call is validated and before it
// runs. A hook may refuse the call or answer it itself.
//
// The arbitration between the hooks — the policy slot first, the first
// result winning — belongs to the stage that adds the rest of the hook
// points. The core runs the hooks in registration order and takes the
// first refusal, so a refusal always wins.

// runBeforeTool runs the before-tool hooks and returns their decision.
//
// A hook that fails settles the run with hook_error. A hook that misses
// its deadline settles the run with hook_error too: the decision never
// arrives, and the call's fate is unknown.
func (s *Session) runBeforeTool(run *runState, turn int, handle, tool string, prepared Prepared, plan callPlan) (ToolDecision, error) {
	if len(s.opts.BeforeTool) == 0 {
		return ToolDecision{Allowed: true}, nil
	}
	ctx := s.activityContext(run)
	counts := s.repeatCountFor(handle)
	info := ToolCallInfo{
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
	for _, h := range s.opts.BeforeTool {
		out, err := s.callHook(ctx, h, func(hctx context.Context) (any, error) {
			return h.BeforeTool(s.hookContext(hctx, turn, false), info)
		})
		if out.interrupted {
			// The hook's decision is discarded, and that is not a
			// hook error.
			return ToolDecision{Allowed: true}, nil
		}
		if err != nil {
			return ToolDecision{}, fmt.Errorf("poo: the %s hook: %w", hookName(h), err)
		}
		decision, _ := out.value.(ToolDecision)
		// A refusal wins over everything else.
		if decision.Denied {
			return decision, nil
		}
		if decision.Result != nil {
			return decision, nil
		}
	}
	return ToolDecision{Allowed: true}, nil
}

// repeatCountFor returns the repeat counts of one call.
//
// A call that was never counted has no counts.
func (s *Session) repeatCountFor(handle string) repeatCounts {
	counts := s.RepeatCountsFor(handle)
	return repeatCounts{exact: counts.Exact, near: counts.Near}
}
