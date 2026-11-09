package poo

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// The nested tool-call seam.
//
// A tool that runs in the turn may make calls of its own: the script
// engines (and any tool that orchestrates others) get the session's
// tools through the NestedCaller on their ToolEnv. A nested call is a
// call of the turn, so it goes through the same path as a call the model
// makes: the same validation, the permission engine, the before-tool
// hooks, the doom loop, the after-tool chain, the same claim machinery
// and the same scheduling — a batch runs in parallel only when every
// call in it is read-only.
//
// The difference is where the result goes. A nested call's record is
// written, but nothing is appended to the model's history and no message
// record is written: the model never saw the call, and the log's
// contract — the messages in the log are the same list the model sees —
// stays intact. The result waits for the tool that made the call to read
// it back. A refusal is data to that tool, not an error.
//
// A nested call is tracked by the run's claim machinery like a
// model-made call is: the finalization waits for its record writes, and
// it owns the result of a nested call that is still outstanding when the
// run settles.

// NestedOutputCapChars is the cap on a nested call's output, in
// characters, as its tool reads it from ToolSettings.
//
// A nested call's output goes to the tool that made the call, not to
// the model, so the cap is not relative to the context window. The bash
// tool's capture buffer holds this many bytes, so a nested bash call
// keeps all it captured.
const NestedOutputCapChars = 1 << 20

// defaultEventCapChars is the cap on a nested result's event text when
// the session sets no observation cap. It is the largest observation the
// model ever sees.
const defaultEventCapChars = 30000

// nestedEventText returns the head of a nested result's text that its
// tool.result event carries, and the number of characters it leaves out.
//
// The caller reads the whole result, which can be NestedOutputCapChars
// long. The event goes to every consumer of the stream and is kept for
// an inspect, so it is held to the session's observation cap.
func (s *Session) nestedEventText(text string) (string, int) {
	limit := s.opts.Settings.ObservationCapChars
	if limit <= 0 {
		limit = defaultEventCapChars
	}
	total := utf8.RuneCountInString(text)
	if total <= limit {
		return text, 0
	}
	i := 0
	for n := 0; n < limit; n++ {
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	return text[:i], total - limit
}

// nestedCaller is the NestedCaller a turn's tool calls carry.
//
// It holds the run, the turn and the parent call the nested records
// belong to: a tool cannot name the wrong parent, because the caller
// stores the handle the seam was built for. The caller is safe for
// concurrent use — a script may fan out — because every field it touches
// is under the session's lock or in the record sink.
type nestedCaller struct {
	session *Session
	run     *runState
	turn    int
	// parent is the handle of the call that is running.
	parent string
}

// Call runs a batch of nested calls and returns their results, in the
// order of the batch.
//
// A batch that never starts is an error: the run is winding down, the
// caller's context is already done, or an entry names a tool a script
// may not call or asks for one to run in the background. Every entry is
// checked before any of the batch is claimed, so a typo in a fan-out has
// no effect.
func (n *nestedCaller) Call(ctx context.Context, calls []NestedCall) ([]NestedResult, error) {
	s := n.session
	run := n.run

	if len(calls) == 0 {
		return nil, nil
	}
	// A run that is winding down starts no new effect, and a batch whose
	// context is already gone does not start.
	if s.runWindingDown(run) {
		return nil, fmt.Errorf("%w: the nested call did not start", errRunWindingDown)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("the nested call did not start: %w", err)
	}

	// The batch is planned and validated before any of it is claimed.
	// The planning is what validates each call's arguments, and it has
	// no effect of its own.
	toolCalls := make([]ToolCall, len(calls))
	for i, call := range calls {
		toolCalls[i] = ToolCall{Name: call.Tool, Arguments: call.Arguments}
	}
	plans := s.planCalls(run, n.turn, toolCalls, false)
	if err := s.validateNestedPlans(plans); err != nil {
		return nil, err
	}

	// The batch runs under both the caller's context and the run's
	// activity context: whichever ends first cancels the calls. The
	// permission chain keeps running under the run's activity context,
	// so an ask is bounded by the policy's own timeout and not by the
	// script's deadline.
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.activityContext(run), cancel)
	defer func() {
		stop()
		cancel()
	}()
	for i := range plans {
		plans[i].parent = n.parent
		// A nested call runs in the turn, and it sees its caller's
		// context. No plan that reaches this point asked for the
		// background: validateNestedPlans refuses the request.
		plans[i].callCtx = callCtx
	}

	// The batch is registered before it runs, so every call in it has an
	// owner: its own dispatch, the skip path, or the seal — exactly like
	// a batch the model made.
	s.claimBatch(run, n.turn, plans)
	if s.runWindingDown(run) {
		return nil, fmt.Errorf("%w: the nested call did not start", errRunWindingDown)
	}
	// No beginTurn and no startActivity: the parent call already runs
	// inside the turn's activity, and the turn's summary stays the
	// model's.
	s.runBatch(run, n.turn, plans)

	results := make([]NestedResult, len(plans))
	for i, p := range plans {
		results[i] = s.takeNestedResult(run, p)
	}
	return results, nil
}

// validateNestedPlans reports the first entry of a batch a script may
// not call.
//
// The run's own tools are out of a script's reach: the finish tool would
// settle the run out from under the script, and the job tools are the
// run's own. The finish tool is recognized by the prepared value as well
// as by the name, so a consumer that renamed it is covered too. A batch
// that asks for a call in the background is refused as well: a nested
// call runs in the turn its parent runs in, so the request is refused
// rather than dropped.
func (s *Session) validateNestedPlans(plans []callPlan) error {
	for _, p := range plans {
		if _, core := coreToolNames[p.call.Name]; core {
			return InvalidArgsf("the tool %q cannot run as a nested call", p.call.Name)
		}
		if _, ok := s.opts.Tools[p.call.Name]; !ok {
			return InvalidArgsf("unknown tool %q", p.call.Name)
		}
		if _, bad := p.prepared.(FinishResult); bad {
			return InvalidArgsf("the tool %q cannot run as a nested call", p.call.Name)
		}
		if p.background {
			return InvalidArgsf("the tool %q cannot run in the background as a nested call", p.call.Name)
		}
	}
	return nil
}

// takeNestedResult takes a nested call's result out of the run.
//
// The result is written before the batch reads it, so a missing one is a
// call the finalization settled: the run ended while the batch was
// working, and the caller is told the call did not run.
func (s *Session) takeNestedResult(run *runState, p callPlan) NestedResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if result, ok := run.nestedResults[p.handle]; ok {
		delete(run.nestedResults, p.handle)
		return result
	}
	return NestedResult{
		Handle:  p.handle,
		Tool:    p.call.Name,
		Outcome: OutcomeSkipped,
		Text:    SkippedSettlingText,
		IsError: true,
	}
}
