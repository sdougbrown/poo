package poo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// The nested tool-call seam.
//
// A tool that runs in the turn may make calls of its own: the script
// engines (and any tool that orchestrates others) get the session's
// tools through the NestedCaller on their ToolEnv. A nested call is
// judged exactly like a model-made call — the permission engine sees
// it, the doom loop counts it, a refusal is data to the caller — but
// its result is recorded without entering the model's history, because
// the model never saw the call.
//
// The records keep the full audit: a nested call writes tool.call (mode
// "nested", with the parent's handle) and tool.result like any other
// call, so a torn tail is recovered the same way, and the recovery's
// message rule already leaves a call with no provider id out of the
// model's history.

// nestedCaller is the NestedCaller a turn's tool calls carry.
//
// It holds the run and the turn the parent call belongs to: the nested
// records are written into the parent's run and turn. The caller is
// safe for concurrent use — a script may fan out — because every field
// it touches is under the session's lock or in the record sink.
type nestedCaller struct {
	session *Session
	run     *runState
	turn    int
}

// Call runs one nested call.
func (n *nestedCaller) Call(ctx context.Context, parent, tool string, raw json.RawMessage) (NestedResult, error) {
	s := n.session
	run := n.run

	// A run that is winding down starts no new effect, and a call whose
	// context is already gone does not start.
	if s.runWindingDown(run) {
		return NestedResult{}, fmt.Errorf("%w: the nested call did not start", errRunWindingDown)
	}
	if err := ctx.Err(); err != nil {
		return NestedResult{}, fmt.Errorf("the nested call did not start: %w", err)
	}

	// The calls the run machinery owns are out of a script's reach: the
	// finish tool would settle the run out from under the script, and
	// the job tools are the run's own.
	if tool == FinishToolName || tool == JobsToolName || tool == CancelJobToolName {
		return NestedResult{}, InvalidArgsf("the tool %q cannot run as a nested call", tool)
	}
	t, ok := s.opts.Tools[tool]
	if !ok {
		return NestedResult{}, InvalidArgsf("unknown tool %q", tool)
	}

	handle := s.nextHandle()

	// The reserved background argument is stripped before the tool's own
	// decoder sees the arguments, like the model's calls are. A nested
	// call never becomes a job, so the argument is noise to remove.
	args := raw
	if s.backgroundModeOf(tool) != BackgroundNever {
		asked, stripped, err := backgroundArg(raw)
		if err != nil {
			return NestedResult{}, InvalidArgsf("the nested call's reserved argument: %v", err)
		}
		if asked {
			// A nested call runs in the turn, whatever the script
			// asked for: the argument is not honoured.
			args = stripped
		}
	}

	prepared, prepareErr := t.Prepare(args, s.prepareEnv())
	if prepareErr == nil {
		if _, bad := prepared.(FinishResult); bad {
			// The finish tool's recognition keys on the prepared
			// value, not the name, so a consumer that renamed the
			// tool is covered here too.
			return NestedResult{}, InvalidArgsf("the tool %q cannot run as a nested call", tool)
		}
	}

	// The doom loop counts a nested call like a model-made one, and the
	// permission engine judges it. The same authorisation the model's
	// calls go through, so a rule, a protected path, an ask and a doom
	// loop's action all apply unchanged.
	primary := s.primaryArgOf(tool)
	exact := exactKeyOf(tool, prepared, args)
	near := nearKeyOf(tool, primary, args, prepareErr != nil)
	if prepareErr != nil {
		exact = "invalid:" + tool
	}
	s.mu.Lock()
	if run.sealed || run.committed {
		s.mu.Unlock()
		return NestedResult{}, fmt.Errorf("%w: the nested call did not start", errRunWindingDown)
	}
	if run.repeats == nil {
		run.repeats = newRepeatCounter()
	}
	if s.repeatCounts == nil {
		s.repeatCounts = map[string]repeatCounts{}
	}
	s.repeatCounts[handle] = run.repeats.count(tool, exact, near)
	s.repeatOrder = append(s.repeatOrder, handle)
	// The permission handle is what ResetRepeats keys on while the
	// policy is in the ask. Concurrent nested calls overwrite each
	// other's, so a reset may miss a concurrent ask's counters; the
	// counters themselves are exact either way.
	run.permissionHandle, run.permissionTool = handle, tool
	s.mu.Unlock()

	decision := Allow()
	var policyErr error
	if policy := s.opts.Policy; policy != nil {
		counts := s.repeatCountFor(handle)
		info := ToolCallInfo{
			Spec:            t.Spec(),
			Prepared:        prepared,
			Arguments:       cloneArguments(args),
			PrepareFailed:   prepareErr != nil,
			Handle:          handle,
			RepeatCount:     counts.exact,
			NearRepeatCount: counts.near,
		}
		decision, policyErr = s.callPolicy(s.activityContext(run), run, policy, n.turn, info)
	}
	s.mu.Lock()
	run.permissionHandle, run.permissionTool = "", ""
	s.mu.Unlock()

	if policyErr != nil {
		// A policy hook that fails settles the run, exactly as it does
		// for a model-made call. The script sees the failure as its
		// call's error.
		s.addCandidate(run, newCandidate(ReasonHookError, SettleDetail{
			Source: hookFailureSource(policyErr, "before_tool"),
			Text:   policyErr.Error(),
		}))
		return NestedResult{}, fmt.Errorf("the permission engine: %w", policyErr)
	}
	if s.runWindingDown(run) || s.interruptPending(run) {
		// The run ended while the policy was thinking: the nested call
		// does not start. The error is the interrupted one, so the
		// parent's classification treats it as a cancellation.
		return NestedResult{}, fmt.Errorf("%w: the nested call was interrupted", errInterrupted)
	}

	// The call is recorded before it runs, like a model-made call. The
	// seal is checked against the record: a run that settled while the
	// script was working stops writing, and the recovery closes the
	// unwritten tail.
	if err := n.writeCallRecord(parent, handle, tool, args); err != nil {
		return NestedResult{}, err
	}

	// A refusal is data: the script sees the denial and decides what to
	// do, and the run carries on.
	if decision.Denied {
		text := decision.Detail
		if text == "" {
			text = "The call was refused."
		}
		return n.recordNestedResult(handle, tool, ToolResult{Text: text, IsError: true}, OutcomeDenied), nil
	}

	// A call that failed its validation never runs: the failure is
	// recoverable data to the script, like it is to the model.
	if prepareErr != nil {
		text := "the nested call was not prepared"
		if prepareErr != nil {
			text = prepareErr.Error()
		}
		return n.recordNestedResult(handle, tool, ToolResult{Text: text, IsError: true}, OutcomeError), nil
	}

	// The nested call's own tool sees no nested caller: the nesting is
	// one call deep.
	env := s.toolEnvFor(run.id, handle)
	callCtx := s.activityContext(run)
	result, err := t.Call(callCtx, prepared, env)

	outcome := OutcomeOK
	switch {
	case err == nil && callCtx.Err() != nil, err == nil && s.interruptPending(run):
		// The call's context ended while it ran: the tool saw the
		// cancellation and gave back what it had.
		result = ToolResult{Text: interruptNote(result.Text), IsError: true}
		outcome = OutcomeCancelled
	case err != nil:
		switch {
		case errors.Is(err, context.Canceled) || errors.Is(err, errInterrupted):
			if s.interruptPending(run) {
				result = ToolResult{Text: interruptNote(result.Text), IsError: true}
			} else {
				text := result.Text
				if text == "" {
					text = err.Error()
				} else {
					text = text + "\n" + err.Error()
				}
				result = ToolResult{Text: text, IsError: true}
			}
			outcome = OutcomeCancelled
		case errors.Is(err, ErrFatal):
			result = ToolResult{Text: err.Error(), IsError: true}
			outcome = OutcomeError
			s.addCandidate(run, newCandidate(ReasonToolFatal, SettleDetail{
				Source: "tool:" + tool,
				Text:   err.Error(),
			}))
		default:
			result = ToolResult{Text: err.Error(), IsError: true}
			outcome = OutcomeError
		}
	}

	return n.recordNestedResult(handle, tool, result, outcome), nil
}

// writeCallRecord writes a nested call's tool.call record.
//
// The record carries the mode and the parent's handle, and it is
// written with the model's origin: the call is the session's work, not
// the model's, but the origin names the record's producer family and
// the tool call records are all the model's calls to the consumers.
func (n *nestedCaller) writeCallRecord(parent, handle, tool string, args json.RawMessage) error {
	s := n.session
	s.mu.Lock()
	sealed := n.run.sealed || n.run.committed
	s.mu.Unlock()
	if sealed {
		return fmt.Errorf("%w: the nested call's record was not written", errRunWindingDown)
	}
	rec := &ToolCallRecord{
		RecordMeta: NewRecordMetaFrom("tool.call", OriginModel),
		RunID:      n.run.id,
		Turn:       n.turn,
		Handle:     handle,
		Tool:       tool,
		Arguments:  recordArgs(args),
		Mode:       CallModeNested,
		Parent:     parent,
	}
	if err := s.write(n.run.ctx, rec); err != nil {
		s.noteStoreError(n.run, err)
		return fmt.Errorf("the nested call's record: %w", err)
	}
	s.events.emit(EventToolCall, map[string]any{
		"run_id":  n.run.id,
		"handle":  handle,
		"tool":    tool,
		"nested":  true,
		"parent":  parent,
	})
	return nil
}

// recordNestedResult writes a nested call's tool.result record and
// returns the result the caller sees.
//
// The record is written, but the result is not appended to the model's
// history and no message record is written: the model never saw the
// call, and the log's contract — the messages in the log are the same
// list the model sees — stays intact.
func (n *nestedCaller) recordNestedResult(handle, tool string, result ToolResult, outcome Outcome) NestedResult {
	s := n.session
	text := modelFacingText(handle, tool, result.Text)

	s.mu.Lock()
	sealed := n.run.sealed || n.run.committed
	s.mu.Unlock()
	if !sealed {
		rec := &ToolResultRecord{
			RecordMeta: NewRecordMetaFrom("tool.result", OriginTool),
			RunID:      n.run.id,
			Turn:       n.turn,
			Handle:     handle,
			Tool:       tool,
			Outcome:    outcome,
			IsError:    result.IsError,
			Text:       text,
			Structured: result.Structured,
		}
		if err := s.write(n.run.ctx, rec); err != nil {
			s.noteStoreError(n.run, err)
		}
		s.events.emit(EventToolResult, map[string]any{
			"run_id":  n.run.id,
			"handle":  handle,
			"tool":    tool,
			"outcome": string(outcome),
			"text":    text,
			"nested":  true,
		})
	}
	return NestedResult{
		Handle:     handle,
		Tool:       tool,
		Outcome:    outcome,
		Text:       text,
		Structured: result.Structured,
		IsError:    result.IsError,
	}
}
