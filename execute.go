package poo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// The visible results the core gives the model for the calls that did
// not run.
const (
	// SkippedTruncatedText is the result of a call in a truncated
	// response: the model was cut off before it could finish the call.
	SkippedTruncatedText = "Not run: your response was cut off at the output limit. Re-issue this call."
	// SkippedInterruptedText is the result of a call that an interrupt
	// cut short before it started.
	SkippedInterruptedText = "Not run: interrupted by the user."
	// SkippedSettlingText is the result of a call that did not start
	// because the run was settling.
	SkippedSettlingText = "Not run: the run settled before the call started."
	// SkippedProviderErrorText is the result of a call in a response
	// whose stream broke: the call was never complete enough to run.
	SkippedProviderErrorText = "Not run: the provider's response broke before it finished."
	// CancelledInterruptedText is the result of a call that an
	// interrupt cut short while it ran.
	CancelledInterruptedText = "Cancelled: interrupted by the user."
)

// callPlan is one call of a batch, with everything the core needs to
// run it.
type callPlan struct {
	// handle is the call's handle, e.g. "r1".
	handle string
	// call is the model's request.
	call ToolCall
	// index is the call's position in the batch, counting from zero.
	index int
	// complete reports that the call is complete: its arguments parse
	// as a JSON object and, in a truncated response, it passed Prepare.
	complete bool
	// invalid reports that the call failed its validation, so it
	// counts toward the invalid near key.
	invalid bool
	// prepared is the validated call, when it validated.
	prepared Prepared
	// prepareErr is the failure the validation produced, when it
	// failed.
	prepareErr error
	// background reports that the model asked for the call to run in
	// the background.
	background bool
	// shorthand holds the effective arguments when the tool rewrites a
	// background shorthand. The call keeps the original for the record.
	shorthand json.RawMessage
	// stripped is the call's arguments without the reserved background
	// argument, so a job's duplicate key keys the same data prepare
	// saw.
	stripped json.RawMessage
	// summary describes the call, for the job's own bookkeeping.
	summary string
	// parent is the handle of the call that made this one. It is
	// non-empty exactly when the call is nested.
	parent string
	// callCtx is the context the tool runs under, when it is not the
	// run's activity context: a nested call runs under its caller's
	// context as well, so the script's own deadline ends it. It is nil
	// for a call the model made, whose activity the batch already runs.
	callCtx context.Context
	// policy is the model-ordered permission decision for this call.
	policy    *ToolDecision
	policyErr error
}

// executeBatch runs a turn's tool calls.
//
// It opens the turn's activity, records the calls in the turn's summary
// and runs the batch. A nested batch skips these steps: its calls belong
// to the turn the parent call runs in, whose activity is already open.
//
// It reports whether any call was processed: a call that got a result
// other than skipped: truncated.
func (s *Session) executeBatch(run *runState, turn int, calls []ToolCall, truncated bool) bool {
	plans := s.planCalls(&run.callOwner, turn, calls, truncated, false)
	if len(plans) == 0 {
		return false
	}
	// The batch is registered before it runs, so every call in it has an
	// owner: its own dispatch, the skip path, or the seal. A call the
	// batch never reaches is the finalization's to settle.
	s.claimBatch(&run.callOwner, turn, plans)
	// A run that is winding down starts no new effect.
	if s.runWindingDown(&run.callOwner) {
		return false
	}
	processed := false
	for _, p := range plans {
		if p.complete {
			processed = true
		}
	}

	s.mu.Lock()
	s.transitionLocked(StateExecuting)
	s.mu.Unlock()

	// The turn's summary is built from the calls the model made, so a
	// hook and a stop condition can see what the turn did.
	s.beginTurn(run, turn, plans)

	// The batch runs under the run's activity context, so an interrupt
	// cancels the calls that are running.
	s.startActivity(run)
	defer s.endActivity(run)

	s.runBatch(&run.callOwner, turn, plans, false)
	return processed
}

// runBatch runs the calls of a batch that has its owner and its
// activity.
//
// A batch runs to completion: no stop condition skips the remaining
// calls. Only a cancellation or a fatal error (through the
// finalization) and an interrupt end it early. A batch runs in parallel
// when all calls are read-only or the nested caller explicitly requests
// parallel execution.
//
// The caller has registered the batch and given it its activity:
// executeBatch opens the turn's activity for a turn's batch, and a
// nested batch arrives here from the tool that made it, inside the
// parent call's activity. A nested call is a call of the turn, so the
// turn's summary stays the model's.
func (s *Session) runBatch(owner *callOwner, turn int, plans []callPlan, parallel bool) {
	if parallel || s.batchIsParallel(plans) {
		for i := range plans {
			if s.runWindingDown(owner) || s.interruptPending(owner) {
				break
			}
			s.decideCall(owner, turn, &plans[i])
			if plans[i].policyErr != nil {
				break
			}
		}
		s.executeParallel(owner, turn, plans)
		// The calls that never started because of an interrupt get a
		// visible result.
		if s.interruptPending(owner) {
			s.skipPlans(owner, turn, plans, SkippedInterruptedText)
		} else if s.batchStopped(owner) {
			s.skipPlans(owner, turn, plans, SkippedSettlingText)
		}
		return
	}
	for _, p := range plans {
		// An interrupt skips the calls that have not started, and the
		// run continues.
		if s.interruptPending(owner) {
			s.skipPlans(owner, turn, plans, SkippedInterruptedText)
			return
		}
		// A fatal error or a cancellation ends the batch: the calls that
		// have not started do not start.
		if s.batchStopped(owner) {
			s.skipPlans(owner, turn, plans, SkippedSettlingText)
			return
		}
		s.decideCall(owner, turn, &p)
		s.executeOne(owner, turn, p)
	}
}

// batchStopped reports that the run holds a candidate that ends the
// batch.
//
// A batch runs to completion except for a cancellation or a fatal error,
// which end it through the finalization, and an interrupt. A finished
// call is not one of them: the calls around it still run.
func (s *Session) batchStopped(owner *callOwner) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner.jobOwned {
		// A job's script keeps its remaining calls: its owner is never
		// finalized, so a candidate here would stop the batch without
		// settling anything. The job's end record reports the failure.
		return false
	}
	for _, c := range owner.candidates {
		if c.reason != ReasonFinished {
			return true
		}
	}
	return false
}

// decideCall counts and authorizes one call before the next call is
// counted. A reset therefore applies to every following call, including
// calls whose tools will execute concurrently.
func (s *Session) decideCall(owner *callOwner, turn int, p *callPlan) {
	primary := s.primaryArgOf(p.call.Name)
	exact := exactKeyOf(p.call.Name, p.prepared, p.call.Arguments)
	near := nearKeyOf(p.call.Name, primary, p.call.Arguments, p.invalid)
	if p.invalid {
		exact = "invalid:" + p.call.Name
	}
	s.mu.Lock()
	if owner.sealed || owner.committed {
		s.mu.Unlock()
		return
	}
	if owner.repeats == nil {
		owner.repeats = newRepeatCounter()
	}
	if s.repeatCounts == nil {
		s.repeatCounts = map[string]repeatCounts{}
	}
	s.repeatCounts[p.handle] = owner.repeats.count(p.call.Name, exact, near)
	s.repeatOrder = append(s.repeatOrder, p.handle)
	owner.permissionHandle, owner.permissionTool = p.handle, p.call.Name
	s.mu.Unlock()
	decision := Allow()
	if policy := s.opts.Policy; policy != nil && p.complete && (p.prepared != nil || p.invalid) {
		info := s.toolCallInfo(turn, p.handle, p.call.Name, p.prepared, *p)
		info.PrepareFailed = p.invalid
		decision, p.policyErr = s.callPolicy(s.activityContext(owner), owner, policy, turn, info)
	}
	p.policy = &decision
	s.mu.Lock()
	owner.permissionHandle, owner.permissionTool = "", ""
	s.mu.Unlock()
}

// beginTurn records the turn's calls in the turn's summary.
//
// The turn's summary was made when the turn started, so the calls join
// the summary the run is in.
func (s *Session) beginTurn(run *runState, turn int, plans []callPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.turns {
		if s.turns[i].TurnIndex != run.globalTurn {
			continue
		}
		for _, p := range plans {
			s.turns[i].Calls = append(s.turns[i].Calls, ToolCallSummary{
				Handle:  p.handle,
				Tool:    p.call.Name,
				Summary: p.summary,
			})
		}
		return
	}
}

// nextHandle returns the next tool-result handle, r<n>.
//
// The number is session-monotonic, so a handle names one call for the
// life of the session, whatever run and turn the call was made in.
func (s *Session) nextHandle() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleSeq++
	return fmt.Sprintf("r%d", s.handleSeq)
}

// planCalls turns a turn's calls into the plan the core runs.
//
// A call in a truncated response is complete only when its arguments
// parse as a complete JSON object and pass Prepare. An incomplete call
// is skipped with a visible result and does not count as processed.
//
// The plan validates every call, so a call that failed its validation
// is known before its repeat counts are taken, and is counted toward
// the near key.
func (s *Session) planCalls(owner *callOwner, turn int, calls []ToolCall, truncated, nested bool) []callPlan {
	var plans []callPlan
	for i, call := range calls {
		handle := s.nextHandle()
		plan := callPlan{handle: handle, call: call, index: i, complete: true}
		if truncated && !isCompleteJSONObject(call.Arguments) {
			plan.complete = false
			plans = append(plans, plan)
			continue
		}
		// The call is validated now, so the batch knows which calls
		// failed before it counts them.
		tool, ok := s.opts.Tools[call.Name]
		if !ok {
			plans = append(plans, plan)
			continue
		}
		// The reserved background argument is validated and removed
		// before the tool's own decoder sees the arguments, so a
		// strict decoder never sees it. Only a tool that may become a
		// job reserves the name: a never-background tool may define its
		// own background property. The tool.call record keeps the
		// original arguments.
		asked := false
		stripped := call.Arguments
		if s.backgroundModeOf(call.Name) != BackgroundNever {
			var err error
			asked, stripped, err = backgroundArg(call.Arguments)
			if err != nil {
				plan.invalid = true
				plan.prepareErr = err
				// A call that failed the reserved argument did not
				// pass preparation, so in a truncated response it is
				// incomplete.
				if truncated {
					plan.complete = false
				}
				plans = append(plans, plan)
				continue
			}
		}
		plan.background = asked
		plan.stripped = stripped
		if err := s.argSchemas.validate(call.Name, stripped); err != nil {
			plan.invalid = true
			plan.prepareErr = err
			if truncated {
				plan.complete = false
			}
			plans = append(plans, plan)
			continue
		}
		if !nested && s.backgroundModeOf(call.Name) == BackgroundAllowed {
			if sh, ok := tool.(interface {
				BackgroundShorthand(json.RawMessage) (json.RawMessage, error)
			}); ok {
				effective, err := sh.BackgroundShorthand(call.Arguments)
				if err != nil {
					plan.invalid, plan.prepareErr, plan.complete = true, err, !truncated
					plans = append(plans, plan)
					continue
				}
				if effective != nil {
					plan.background, plan.stripped, err = backgroundArg(effective)
					if err != nil {
						plan.invalid, plan.prepareErr, plan.complete = true, err, !truncated
						plans = append(plans, plan)
						continue
					}
					plan.shorthand = effective
				}
			}
		}
		prepared, err := tool.Prepare(plan.stripped, s.prepareEnv())
		if err != nil {
			plan.invalid = true
			plan.prepareErr = err
			// A call that fails Prepare did not pass the plan's
			// completeness test.
			if truncated {
				plan.complete = false
			}
		} else {
			plan.prepared = prepared
			plan.summary = CapSummary(prepared.Summary())
		}
		plans = append(plans, plan)
	}
	return plans
}

// isCompleteJSONObject reports that the arguments are a complete JSON
// object.
//
// A truncated response leaves the last call's arguments half-written,
// and a half-written object does not parse.
func isCompleteJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil
}

// batchIsParallel reports that every call in the batch belongs to a
// read-only tool.
//
// By default a batch only runs in parallel when every call is read-only,
// because a side-effecting call may depend on what ran before it.
func (s *Session) batchIsParallel(plans []callPlan) bool {
	if len(plans) < 2 {
		return false
	}
	for _, p := range plans {
		tool, ok := s.opts.Tools[p.call.Name]
		if !ok {
			return false
		}
		if tool.Spec().Class != ClassReadOnly {
			return false
		}
	}
	return true
}

// executeParallel runs the calls of a batch concurrently.
//
// Permission decisions are complete before the tools run concurrently.
func (s *Session) executeParallel(owner *callOwner, turn int, plans []callPlan) {
	var wg sync.WaitGroup
	for _, p := range plans {
		wg.Add(1)
		go func(p callPlan) {
			defer wg.Done()
			// An interrupt that arrived before the call started skips
			// it.
			if p.policy == nil || s.interruptPending(owner) || s.batchStopped(owner) {
				return
			}
			s.executeOne(owner, turn, p)
		}(p)
	}
	wg.Wait()
}

// executeOne runs one call and records its result.
//
// A call that was interrupted before it started is skipped; a call that
// was interrupted while it ran is cancelled. A call in a truncated
// response whose arguments are incomplete is skipped with a visible
// result.
func (s *Session) executeOne(owner *callOwner, turn int, plan callPlan) {
	if s.runWindingDown(owner) {
		return
	}
	handle := plan.handle
	call := plan.call

	// The call is registered before the blocking record write, so a run
	// that settles while the write is in flight still has a call for
	// the finalization to settle. It is not marked started until the
	// record is written. A call claimed after the finalization has
	// sealed the run is the finalization's.
	if !s.issueCall(owner, turn, handle, call, plan.summary, plan.parent) {
		return
	}

	// The call is recorded before it runs, so a log shows what the
	// model asked for even when the call never finished. The record
	// carries the original arguments and the mode the call ran in.
	//
	// The mode is only known once the before-tool hooks have answered,
	// because a refusal never starts a job. The record is therefore
	// written once, at the point the mode is fixed.
	recorded := false
	record := func(m CallMode) bool {
		if recorded {
			return true
		}
		// The write is claimed against the seal, so a dispatch that was
		// descheduled until the run settled writes nothing: the
		// finalization owns the call's record then.
		callRecord, ok := s.reserveCallRecord(owner, handle)
		if !ok {
			return false
		}
		recorded = true
		defer s.finishCallWrite(owner, callRecord)
		// A nested call never runs as a job, whatever mode the model's
		// path would have given it.
		if m != "" && plan.parent != "" {
			m = CallModeNested
		}
		if err := s.writeCallRecord(owner, callRecord, &ToolCallRecord{
			RecordMeta: NewRecordMetaFrom("tool.call", OriginModel),
			RunID:      owner.id,
			Turn:       turn,
			Handle:     handle,
			CallID:     call.CallID,
			Tool:       call.Name,
			Summary:    callRecord.summary,
			Arguments:  recordArgs(call.Arguments),
			Mode:       m,
			Parent:     plan.parent,
		}); err != nil {
			if errors.Is(err, errCallRecordSealed) {
				// The finalization stopped waiting for the write and
				// sealed the run, so the call does not start and the
				// finalization gives it its result.
				return false
			}
			s.addCandidate(owner, newCandidate(ReasonStoreError, SettleDetail{
				Source: "store",
				Text:   err.Error(),
			}))
			return false
		}
		return true
	}
	payload := map[string]any{
		"run_id": owner.id,
		"handle": handle,
		"tool":   call.Name,
	}
	if plan.summary != "" {
		payload["summary"] = plan.summary
	}
	if plan.parent != "" {
		// The parent's presence is what says the call is nested.
		payload["parent"] = plan.parent
	}
	s.events.emit(EventToolCall, payload)

	// An incomplete call in a truncated response is skipped: the model
	// was cut off before it could finish the call.
	if !plan.complete {
		// The incomplete call never started, so its record carries no
		// mode, like every other call that did not run.
		if !record("") {
			return
		}
		s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
			Text:    SkippedTruncatedText,
			IsError: true,
		}, OutcomeSkipped)
		return
	}

	tool, ok := s.opts.Tools[call.Name]
	if !ok {
		if !record(CallModeSync) {
			return
		}
		s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
			Text:    fmt.Sprintf("unknown tool %q", call.Name),
			IsError: true,
		}, OutcomeError)
		return
	}

	// The call's outcome is decided by the core, not by the tool.
	prepared := plan.prepared
	if prepared == nil {
		// The call is recorded before the policy sees it, so a log shows
		// the malformed call the model made even when the policy asks
		// about it.
		if !record(CallModeSync) {
			return
		}
		// The call failed its validation. The doom loop still judges it:
		// the same malformed call repeated is the same loop. The other
		// rules do not apply, because the call is not a valid call.
		if plan.policy != nil {
			decision, err := *plan.policy, plan.policyErr
			if s.runWindingDown(owner) {
				s.logLateHookDecision(owner.id, handle, call.Name, turn)
				return
			}
			if err != nil {
				s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
					Text:    err.Error(),
					IsError: true,
				}, OutcomeError)
				s.addCandidate(owner, newCandidate(ReasonHookError, SettleDetail{
					Source: hookFailureSource(err, "before_tool"),
					Text:   err.Error(),
				}))
				return
			}
			if s.interruptPending(owner) {
				return
			}
			if decision.Denied {
				text := decision.Detail
				if text == "" {
					text = "The call was refused."
				}
				s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
					Text:    text,
					IsError: true,
				}, OutcomeDenied)
				return
			}
		}
		// The failure is a recoverable error: the model sees the message
		// and may correct the call.
		text := "the call was not prepared"
		if plan.prepareErr != nil {
			text = plan.prepareErr.Error()
		}
		s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
			Text:    text,
			IsError: true,
		}, OutcomeError)
		return
	}

	// The before-tool hooks run after the validation and before the
	// call, and they may refuse it or answer it themselves.
	decision, interrupted, err := s.runBeforeTool(owner, turn, handle, call.Name, prepared, plan)
	// The hook may have outlived the run: a decision that arrives after
	// the finalization began is discarded, and the call starts nothing.
	if s.runWindingDown(owner) {
		s.logLateHookDecision(owner.id, handle, call.Name, turn)
		return
	}
	if err != nil {
		if !record(CallModeSync) {
			return
		}
		s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
			Text:    err.Error(),
			IsError: true,
		}, OutcomeError)
		s.addCandidate(owner, newCandidate(ReasonHookError, SettleDetail{
			Source: hookFailureSource(err, "before_tool"),
			Text:   err.Error(),
		}))
		return
	}
	// An interrupted authorization starts no call and admits no job. A
	// result an earlier hook gave before the interrupt is still the
	// call's result.
	if interrupted || s.interruptPending(owner) {
		if decision.Result != nil {
			if !record(CallModeSync) {
				return
			}
			s.recordResult(owner, turn, handle, call.Name, call.CallID, *decision.Result, OutcomeOK)
		}
		return
	}
	if decision.Denied {
		// A refused call is a model-visible error result, and it
		// never becomes a job.
		if !record(CallModeSync) {
			return
		}
		text := decision.Detail
		if text == "" {
			text = "The call was refused."
		}
		s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
			Text:    text,
			IsError: true,
		}, OutcomeDenied)
		return
	}
	if decision.Result != nil {
		// A hook answered the call itself, and it never becomes a
		// job.
		if !record(CallModeSync) {
			return
		}
		s.recordResult(owner, turn, handle, call.Name, call.CallID, *decision.Result, OutcomeOK)
		return
	}

	// A call that becomes a job returns at once, and its result
	// arrives later as an input. An interrupt that arrived after the
	// authorization refuses the admission. A nested call never becomes
	// a job: it runs in the turn its parent runs in.
	// A job's owner has no run, so a call it makes can never start a job:
	// the job's script runs inside the job it already has.
	if plan.parent == "" && owner.run != nil && s.callBecomesJob(call.Name, plan.background) {
		if !record(CallModeJob) {
			return
		}
		s.startJobCall(owner, turn, handle, call, plan)
		return
	}

	// The call has been issued, so it needs a result even if the run
	// settles before it finishes.
	if !record(CallModeSync) {
		return
	}
	if s.runWindingDown(owner) {
		// The finalization owns the issued call's result.
		return
	}
	// The call is marked started only once its tool is about to run, so
	// the finalization never waits for a call that the winding-down run
	// will not start. The claim is made against the seal, so nothing
	// starts after the commit point.
	if !s.startCall(owner, handle) {
		return
	}

	// The call runs under the run's activity context, so an interrupt
	// cancels it. A nested call runs under its caller's context too, so
	// the script's own deadline ends it.
	callCtx := s.activityContext(owner)
	if plan.callCtx != nil {
		callCtx = plan.callCtx
	}
	result, err := tool.Call(callCtx, prepared, s.toolEnv(owner, turn, handle, plan))
	// The outcome is decided by the core, not by the tool.
	outcome := OutcomeOK
	var fatalCandidate *candidate
	switch {
	case err == nil && callCtx.Err() != nil:
		// The call's context ended while the call ran: the tool saw
		// the cancellation and gave back what it had. The outcome says
		// the call was cancelled, and the result carries the partial
		// output with the note.
		text := result.Text
		if s.interruptPending(owner) {
			text = interruptNote(text)
		} else {
			text = text + "\n" + CancelledInterruptedText
		}
		result = ToolResult{Text: text, IsError: true}
		outcome = OutcomeCancelled
	case err == nil && s.interruptPending(owner):
		// The call was cut short by an interrupt, even though the tool
		// returned a result: the tool saw the cancellation and gave
		// back what it had. The outcome says the call was cancelled,
		// and the result carries the partial output with the note.
		result = ToolResult{Text: interruptNote(result.Text), IsError: true}
		outcome = OutcomeCancelled
	case err != nil:
		switch {
		case errors.Is(err, context.Canceled) || errors.Is(err, errInterrupted):
			// The call was cut short. The outcome says which: a call
			// that never started is skipped, and one that was running
			// is cancelled. The partial output the tool returned stays
			// in the result, with the note.
			if s.interruptPending(owner) {
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
			// A tool error that wraps ErrFatal is the environment being
			// unusable: the run settles tool_fatal. The published text
			// is the hook-processed result, so a redaction covers it
			// too; the fatal classification is kept separately.
			result = ToolResult{Text: err.Error(), IsError: true}
			outcome = OutcomeError
			fatalCandidate = &candidate{reason: ReasonToolFatal, detail: SettleDetail{
				Source: "tool:" + call.Name,
				Text:   err.Error(),
			}}
		default:
			// Any other error is recoverable: it goes back to the model
			// and the run continues.
			result = ToolResult{Text: err.Error(), IsError: true}
			outcome = OutcomeError
		}
	}

	// A valid finish call settles the run as finished, with the result.
	if outcome == OutcomeOK {
		// The recognition keys on the prepared value the call carries, not
		// on the tool's name: FinishResult is the finish tool's own, and a
		// consumer that renames the tool keeps the settle.
		if raw, ok := prepared.(FinishResult); ok {
			s.addCandidate(owner, newCandidate(ReasonFinished, SettleDetail{
				Source: "finish",
				Text:   "the model called finish with a valid result",
				Result: raw.FinishResult(),
			}))
		}
	}

	// The after-tool chain runs before the result is recorded or
	// emitted, so the record, the event and the model see the same
	// sanitized result.
	out := s.recordToolResult(owner, turn, handle, call.Name, call.CallID, s.toolCallInfo(turn, handle, call.Name, prepared, plan), result, outcome)
	if fatalCandidate != nil {
		fatalCandidate.detail.Text = out.result.Text
		s.addCandidate(owner, *fatalCandidate)
	}
}

// startJobCall turns a call into a background job.
//
// The job's slot is reserved and the job is registered under the session
// lock, and the job.started record is written before the goroutine
// starts. The call's own result is the "started" one, and the batch
// carries on.
func (s *Session) startJobCall(owner *callOwner, turn int, handle string, call ToolCall, plan callPlan) {
	jobID, ok, winding, refusal := s.startJob(owner.run, turn, handle, call.Name, plan.prepared, plan.summary, call.Arguments, plan.stripped)
	if winding {
		// The run settled while the job was being accepted; nothing
		// else starts, and the finalization owns the run.
		return
	}
	if !ok {
		// The admission refused the call: no free slot, or a duplicate
		// of a running job. The failure is recoverable: the model sees
		// the message and may wait or cancel one.
		text := TooManyJobsText
		if refusal != "" {
			text = refusal
		}
		s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{
			Text:    text,
			IsError: true,
		}, OutcomeError)
		return
	}

	text := fmt.Sprintf(StartedText, jobID, call.Name)
	if plan.shorthand != nil {
		text += " The trailing & requested this job."
	}
	s.recordResult(owner, turn, handle, call.Name, call.CallID, ToolResult{Text: text}, OutcomeStarted)
	s.events.emit(EventJobStarted, map[string]any{
		"job":    jobID,
		"handle": handle,
		"tool":   call.Name,
		"run_id": owner.id,
	})
}

// prepareEnv builds the environment a tool validates its arguments in.
func (s *Session) prepareEnv() PrepareEnv {
	return PrepareEnv{
		WorkingDir:     s.opts.WorkingDir,
		ProtectedPaths: s.opts.ProtectedPaths,
		ReadRoots:      s.opts.ReadRoots,
		RestrictRead:   s.opts.RestrictRead,
		RestrictGrep:   s.opts.RestrictGrep,
		RestrictEdit:   s.opts.RestrictEdit,
		RestrictWrite:  s.opts.RestrictWrite,
	}
}

// toolEnv builds the environment a tool runs in.
//
// A call the model made gets the nested seam, so a tool that
// orchestrates others — a script engine, say — can make calls of its
// own. A nested call gets none: the nesting is one call deep.
//
// A nested call's output goes to the tool that made it, not to the
// model, so its caps are NestedOutputCapChars and not the session's
// context-relative ones. The calling tool caps what it passes on.
func (s *Session) toolEnv(owner *callOwner, turn int, handle string, plan callPlan) ToolEnv {
	env := s.toolEnvFor(owner.id, handle)
	if plan.parent == "" {
		env.Nested = &nestedCaller{session: s, owner: owner, turn: turn, parent: handle}
	} else {
		env.Settings.ObservationCapChars = NestedOutputCapChars
		env.Settings.ReadOutputCapChars = NestedOutputCapChars
	}
	return env
}

// toolEnvFor builds the environment a tool runs in, for a run that may
// already have settled.
//
// A job runs under the session, so the run it names may be gone by the
// time the job runs. The environment still names the run that started
// it, so a tool can tie its work back to the call. This builds no nested
// caller: toolEnv gives a turn's call the run's seam, and jobToolEnv
// gives a job's call one owned by the job, where the calls a script makes
// are recorded against the job and keep running after the run ends.
func (s *Session) toolEnvFor(runID, handle string) ToolEnv {
	// The model is swapped under the session's lock while a background job
	// may be starting a call, so the snapshot is taken under it too.
	s.mu.Lock()
	model := s.opts.Model
	s.mu.Unlock()
	env := ToolEnv{
		PrepareEnv: s.prepareEnv(),
		Model:      model,
		SessionID:  s.opts.SessionID,
		RunID:      runID,
		Handle:     handle,
		Env:        s.opts.Env,
		Tools:      s.toolInfos(),
		Settings:   s.opts.Settings,
		Decider:    s.Decider(),
	}
	env.History = s.retainedHistory()
	return env
}

// toolInfos lists the session's tools, in offer order, for a tool that
// names its siblings to the model.
//
// The list comes from the specs the model sees, and holds only the
// tools a script may call: the run's own tools and the background-only
// tools stay out of it.
func (s *Session) toolInfos() []ToolInfo {
	specs := s.toolSpecs()
	infos := make([]ToolInfo, 0, len(specs))
	for _, spec := range specs {
		if !nestedCallable(spec.Name, spec) {
			continue
		}
		infos = append(infos, ToolInfo{Name: spec.Name, Class: spec.Class, Background: spec.Background})
	}
	return infos
}

// retainedHistory returns the store's read side for the session, or nil
// when the sink cannot read its records back: a sink that can lets a
// tool or a strategy read what the store retained.
func (s *Session) retainedHistory() History {
	if reader, ok := s.opts.Sink.(RecordReader); ok {
		return lazyHistory{reader: reader}
	}
	return nil
}

// recordResult records a tool result and appends it to the history.
//
// The result goes back to the model under the call's provider-side id,
// because that is the id the provider matches it against.
//
// A call gets exactly one result. The call claims its result before the
// record is written, so a call that the finalization already gave a
// synthetic result to is not given a second one.
func (s *Session) recordResult(owner *callOwner, turn int, handle, tool, callID string, result ToolResult, outcome Outcome) {
	won, tracked := s.claimResult(owner, handle)
	if !won {
		// The finalization already gave the call a synthetic result.
		s.logLateToolResult(owner.id, handle, tool, turn)
		return
	}
	s.writeResult(owner, turn, handle, tool, callID, result, outcome, afterToolOutcome{result: result})
	if tracked {
		s.finishResultWrite(owner)
	}
}

// recordToolResult runs the after-tool chain and records the result.
//
// The chain runs before the result is recorded or emitted, so the
// record, the event and the model all see the same sanitized result. A
// hook that fails settles the run with hook_error, and the call keeps
// the result the tool gave.
func (s *Session) recordToolResult(owner *callOwner, turn int, handle, tool, callID string, info ToolCallInfo, result ToolResult, outcome Outcome) afterToolOutcome {
	out, err := s.runAfterTool(hookCompletionContext(owner.ctx), owner.id, "", turn, info, result)
	if err != nil {
		s.addCandidate(owner, newCandidate(ReasonHookError, SettleDetail{
			Source: hookFailureSource(err, "after_tool"),
			Text:   err.Error(),
		}))
	}
	won, tracked := s.claimResult(owner, handle)
	if !won {
		// The finalization already gave the call a synthetic result.
		s.logLateToolResult(owner.id, handle, tool, turn)
		return out
	}
	s.writeResult(owner, turn, handle, tool, callID, out.result, outcome, out)
	if tracked {
		s.finishResultWrite(owner)
	}
	return out
}

// finishResultWrite marks the end of a claimed result's write, so the
// finalization, which waits for it, can go on.
func (s *Session) finishResultWrite(owner *callOwner) {
	s.mu.Lock()
	owner.resultWrites--
	done := owner.workDone
	s.mu.Unlock()
	if done != nil {
		select {
		case done <- struct{}{}:
		default:
		}
	}
}

// finishCallWrite marks the end of a tool.call record's write.
func (s *Session) finishCallWrite(owner *callOwner, call *issuedCall) {
	s.mu.Lock()
	call.writing = false
	cancel := call.recordCancel
	call.recordCancel = nil
	owner.callWrites--
	s.signalWorkLocked(owner)
	s.mu.Unlock()
	if cancel != nil {
		cancel(nil)
	}
}

// beginCallRecordLocked marks a call's tool.call record write as in
// flight, and makes the context it runs under.
//
// The context ignores the run's own cancellation, so the record is still
// written when the run ends, and the finalization ends it at the seal, so
// the record never follows run.settled. It has no bound of its own: the
// finalization's wait bounds it.
//
// The caller holds the session's lock.
func (s *Session) beginCallRecordLocked(owner *callOwner, call *issuedCall) {
	base := owner.ctx
	if base == nil {
		base = context.Background()
	}
	call.recorded = true
	call.writing = true
	call.recordCtx, call.recordCancel = context.WithCancelCause(context.WithoutCancel(base))
	if owner.workDone == nil {
		owner.workDone = make(chan struct{}, 1)
	}
	owner.callWrites++
}

// writeResult writes a call's result record and puts it in its reader's
// view.
//
// A result the model's call produced goes into the model's view and the
// turn's summary. A nested call's result goes to the tool that made it
// instead: the model never saw the call, so nothing is appended to the
// view and no message record is written, and the log's contract — the
// messages in the log are the same list the model sees — keeps
// holding.
//
// The caller has claimed the call's result. The chain's outcome
// carries the original the hooks replaced and the decisions they made:
// a redacted result's record holds no original at all.
//
// The result's model-facing text starts with the handle line, so the
// model can name the result later and a strategy can find it.
func (s *Session) writeResult(owner *callOwner, turn int, handle, tool, callID string, result ToolResult, outcome Outcome, out afterToolOutcome) {
	text := modelFacingText(handle, tool, result.Text)
	// A nested call's record and event hold its text to the session's
	// cap; the caller reads the whole result.
	parent := s.callParent(handle)
	recordText, elided := text, 0
	if parent != "" {
		recordText, elided = s.nestedRecordText(text)
	}
	rec := &ToolResultRecord{
		RecordMeta:  NewRecordMetaFrom("tool.result", OriginTool),
		RunID:       owner.id,
		Turn:        turn,
		Handle:      handle,
		CallID:      callID,
		Tool:        tool,
		Outcome:     outcome,
		IsError:     result.IsError,
		Text:        recordText,
		ElidedChars: elided,
		Structured:  result.Structured,
		Redacted:    out.redacted,
		RedactedBy:  out.redactedBy,
		Hooks:       out.decisions,
	}
	if out.original != nil {
		originalText := out.original.Text
		if parent != "" {
			originalText, _ = s.nestedRecordText(originalText)
		}
		rec.Original = &ToolResultData{
			Text:       originalText,
			Structured: out.original.Structured,
			IsError:    out.original.IsError,
		}
	}
	if err := s.write(owner.ctx, rec); err != nil {
		s.noteStoreError(owner, err)
	}

	// A nested call's result is the caller's, and it is recorded for the
	// caller to read back rather than appended to the model's view.
	if parent != "" {
		s.storeNestedResult(owner, NestedResult{
			Handle:     handle,
			Tool:       tool,
			Outcome:    outcome,
			Text:       text,
			Structured: result.Structured,
			IsError:    result.IsError,
		})
		payload := map[string]any{
			"run_id":  owner.id,
			"handle":  handle,
			"tool":    tool,
			"outcome": string(outcome),
			"text":    recordText,
			"parent":  parent,
		}
		if elided > 0 {
			payload["elided_chars"] = elided
		}
		s.events.emit(EventToolResult, payload)
		return
	}

	// The result goes into the model's view and the turn's summary.
	msg := ToolResultMessage(callID, tool, text, result.IsError, result.Attachments)
	msg.Origin = OriginTool
	s.appendMessages(msg)
	result.Text = text
	s.rememberResult(handle, tool, outcome, result)

	if err := s.write(owner.ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", msg.Origin),
		RunID:      owner.id,
		Turn:       turn,
		Role:       msg.Role,
		Content:    msg.Content,
	}); err != nil {
		s.noteStoreError(owner, err)
	}

	s.events.emit(EventToolResult, map[string]any{
		"run_id":  owner.id,
		"handle":  handle,
		"tool":    tool,
		"outcome": string(outcome),
		"text":    text,
	})
}

// storeNestedResult records a nested call's result for the tool that
// made the call.
//
// The result waits under the session's lock until the batch that made
// the call reads it back, and a result the finalization settled for a
// call nobody waits for any more is dropped with the run.
func (s *Session) storeNestedResult(owner *callOwner, result NestedResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner.nestedResults == nil {
		owner.nestedResults = map[string]NestedResult{}
	}
	owner.nestedResults[result.Handle] = result
}

// rememberResult records a call's outcome and result in the turn's
// summary.
func (s *Session) rememberResult(handle, tool string, outcome Outcome, result ToolResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[handle] = copyResult(result)
	// The turn the call was made in is the turn the summary belongs
	// to, and the handle knows it.
	meta, ok := s.resultMeta[handle]
	if !ok {
		return
	}
	for i := range s.turns {
		if s.turns[i].TurnIndex != meta.turn {
			continue
		}
		for j := range s.turns[i].Calls {
			if s.turns[i].Calls[j].Handle == handle {
				s.turns[i].Calls[j].Outcome = outcome
				return
			}
		}
	}
}

// claimBatch registers a turn's calls with the run, so every call in the
// batch is owned by exactly one of its own dispatch, the skip path and
// the seal.
//
// A registered call is an issued call with no result yet: the dispatch
// takes it over when it writes the call's record, the skip path takes it
// over when the batch never reaches it, and the seal settles whatever is
// left when the run settles. The claim is made against the seal: a batch
// that arrives after the finalization has sealed the run is left alone.
func (s *Session) claimBatch(owner *callOwner, turn int, plans []callPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner.sealed {
		return
	}
	if owner.pendingCalls == nil {
		owner.pendingCalls = map[string]*issuedCall{}
	}
	for _, p := range plans {
		if owner.settledCalls[p.handle] {
			continue
		}
		if _, issued := owner.pendingCalls[p.handle]; issued {
			continue
		}
		owner.pendingCalls[p.handle] = &issuedCall{
			handle:     p.handle,
			tool:       p.call.Name,
			id:         p.call.CallID,
			turn:       turn,
			globalTurn: owner.globalTurn,
			arguments:  p.call.Arguments,
			summary:    p.summary,
			parent:     p.parent,
		}
		s.resultMeta[p.handle] = resultMeta{
			tool:   p.call.Name,
			callID: p.call.CallID,
			turn:   owner.globalTurn,
			parent: p.parent,
		}
	}
}

// issueCall claims a call for its own dispatch, so the dispatch may write
// its record and run it.
//
// The batch registers every call it plans, so a call that another claim
// already owns is not the dispatch's. A call that arrives without its
// batch's registration is registered here and is the dispatch's, with
// the parent the call was given. The claim is made against the seal: it
// reports false when the run is sealed, because the finalization owns
// every call that is still outstanding.
func (s *Session) issueCall(owner *callOwner, turn int, handle string, call ToolCall, summary, parent string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner.sealed {
		return false
	}
	if entry, issued := owner.pendingCalls[handle]; issued {
		return !entry.recorded && !entry.started
	}
	if owner.settledCalls[handle] {
		return false
	}
	owner.pendingCalls[handle] = &issuedCall{
		handle:     handle,
		tool:       call.Name,
		id:         call.CallID,
		turn:       turn,
		globalTurn: owner.globalTurn,
		arguments:  call.Arguments,
		summary:    summary,
		parent:     parent,
	}
	s.resultMeta[handle] = resultMeta{
		tool:   call.Name,
		callID: call.CallID,
		turn:   owner.globalTurn,
		parent: parent,
	}
	if owner.workDone == nil {
		owner.workDone = make(chan struct{}, 1)
	}
	return true
}

// reserveCallRecord claims a call's tool.call record as in-flight work, so
// the finalization waits for it before it writes run.settled.
//
// The claim is made against the seal: it reports false when the run is
// sealed, and the finalization writes the record instead. The returned
// call holds the context the record is written under, so the finalization
// can end the write when it seals the claim.
func (s *Session) reserveCallRecord(owner *callOwner, handle string) (*issuedCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner.sealed {
		return nil, false
	}
	call, ok := owner.pendingCalls[handle]
	if !ok || call.recorded {
		return nil, false
	}
	s.beginCallRecordLocked(owner, call)
	return call, true
}

// startCall claims that a registered call's tool is about to run, so the
// finalization knows it may have an effect to abandon.
//
// The claim is made against the seal: it reports false when the run is
// sealed, so nothing starts after the commit point.
func (s *Session) startCall(owner *callOwner, handle string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner.sealed || owner.interruptPending {
		return false
	}
	call, ok := owner.pendingCalls[handle]
	if !ok {
		return false
	}
	call.started = true
	return true
}

// claimResult claims a call's result.
//
// It reports whether the caller won the claim: a call that the
// finalization already gave a synthetic result to is not given a second
// one. A call that was never issued — a call that was skipped before it
// started — is always claimed.
//
// The second value reports that the claim was of an issued call: its
// write is then counted in run.resultWrites until finishResultWrite, so
// the finalization does not write run.settled while the result is still
// being written.
func (s *Session) claimResult(owner *callOwner, handle string) (won, tracked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner.sealed {
		// The finalization owns every result that is still outstanding.
		return false, false
	}
	_, pending := owner.pendingCalls[handle]
	delete(owner.pendingCalls, handle)
	if owner.settledCalls == nil {
		owner.settledCalls = map[string]bool{}
	}
	taken := owner.settledCalls[handle]
	if pending {
		// The call was issued, and the finalization may have given
		// it a synthetic result already.
		owner.settledCalls[handle] = true
	}
	tracked = pending && !taken
	if tracked {
		owner.resultWrites++
	}
	if owner.workDone != nil {
		select {
		case owner.workDone <- struct{}{}:
		default:
		}
	}
	return !taken, tracked
}

// addCandidate adds a settle candidate to the run.
//
// A candidate arriving after the commit point is logged and ignored: it
// never mutates a settled run.
func (s *Session) addCandidate(owner *callOwner, c candidate) {
	s.mu.Lock()
	if owner.committed {
		runID := owner.id
		s.mu.Unlock()
		s.logLateCandidate(runID, c)
		return
	}
	owner.candidates = append(owner.candidates, c)
	s.mu.Unlock()
}

// FinishToolName is the name the typed-result tool is called by.
//
// The core does not build the tool: a consumer links poo/tool/finish and
// adds it to the session's tools. The core recognizes a finished call by
// this name and by the prepared call carrying a result.
const FinishToolName = "finish"

// FinishResult is implemented by a prepared call that carries a typed
// result.
//
// The core reads the result off the prepared call when the batch
// completes, and the run settles as finished with it.
type FinishResult interface {
	// FinishResult returns the validated result.
	FinishResult() json.RawMessage
}

// interruptNote adds the interrupt's note to a result's text.
//
// The result keeps whatever the tool managed to produce, and the note
// says why the call ended.
func interruptNote(text string) string {
	if text == "" {
		return CancelledInterruptedText
	}
	return text + "\n" + CancelledInterruptedText
}

// ErrFatal marks a tool error as fatal.
//
// A fatal error means the environment is unusable — the working
// directory is gone, for instance — and the run settles tool_fatal.
var ErrFatal = errors.New("poo: the environment is unusable")

// Fatalf wraps a message in a fatal error.
func Fatalf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFatal, fmt.Sprintf(format, args...))
}

// repeatCounter counts the consecutive repeats of a call in a run.
//
// The counters are the doom loop's: an exact key is the tool plus its
// canonical prepared input, and a near key is the tool plus its primary
// argument. A Prepare failure counts toward the near key, because the
// same malformed call repeated is the same loop.
//
// The counters are kept per tool, and a few calls to other tools do not
// break the streak: a run that reads the same file five times with
// another tool in between is still a loop. A call with a different key
// for the same tool starts the streak over, which is what makes the
// repeats consecutive.
//
// A streak only lives inside a window of recent calls. A loop repeats
// inside a short stretch of the run's work, so a tool the run returns to
// after more calls to other tools than maxRepeatGap has not been
// repeating: it has been working, and its streak starts over. Without
// the window a poll the run makes every few dozen calls, or a malformed
// call it made long ago, would collect a count that no loop earned.
type repeatCounter struct {
	// streaks are the per-tool streaks.
	streaks map[string]*toolStreak
	// calls counts the calls the counter has seen.
	calls int
}

// maxRepeatGap is how many calls to other tools may separate two calls
// of the same tool and still count as consecutive repeats.
//
// The count is in calls, not turns, so an alternating A, B, A, B, A loop
// keeps its streak: its gap is one. A gap this small is the run's own
// work around a repeat; a longer one is a different moment of the run.
const maxRepeatGap = 4

// toolStreak is one tool's consecutive repeats.
type toolStreak struct {
	// exactKey is the last exact key the tool was called with.
	exactKey string
	// exact is how many times that key repeated consecutively.
	exact int
	// nearKey is the last near key the tool was called with.
	nearKey string
	// near is how many times that key repeated consecutively.
	near int
	// last is the number of the call that last joined the streak, in
	// the counter's own numbering.
	last int
}

// repeatCounts is what a call's repeat counters hold.
type repeatCounts struct {
	// exact is how many times the model made this exact call.
	exact int
	// near is how many times the model made a call with the same
	// primary argument.
	near int
}

// newRepeatCounter returns an empty counter.
func newRepeatCounter() *repeatCounter {
	return &repeatCounter{streaks: map[string]*toolStreak{}}
}

// count records a call and returns the counts it had.
//
// The counts are the number of times the model has made this call
// consecutively, and the number of times it has made a call with the
// same primary argument consecutively, in the run.
func (c *repeatCounter) count(tool, exactKey, nearKey string) repeatCounts {
	c.calls++
	streak, ok := c.streaks[tool]
	if ok && c.calls-streak.last-1 > maxRepeatGap {
		// More calls to other tools separate this one from the last
		// than the window allows: the streak is stale, and this call
		// starts a new one.
		delete(c.streaks, tool)
		ok = false
	}
	if !ok {
		streak = &toolStreak{}
		c.streaks[tool] = streak
	}
	streak.last = c.calls
	if streak.exactKey == exactKey {
		streak.exact++
	} else {
		streak.exactKey = exactKey
		streak.exact = 1
	}
	if streak.nearKey == nearKey {
		streak.near++
	} else {
		streak.nearKey = nearKey
		streak.near = 1
	}
	return repeatCounts{exact: streak.exact, near: streak.near}
}

// reset clears a tool's streak.
//
// The counters are the doom loop's, and a policy that allowed a loop
// clears them: the next call of the same shape starts the count over.
func (c *repeatCounter) reset(tool string) {
	delete(c.streaks, tool)
}

// primaryArgOf returns the name of a tool's primary argument.
func (s *Session) primaryArgOf(name string) string {
	tool, ok := s.opts.Tools[name]
	if !ok {
		return ""
	}
	return tool.Spec().PrimaryArg
}

// exactKeyOf returns the exact repeat key of a call.
//
// The key is the tool plus its canonical prepared input, re-encoded as
// JSON with sorted keys. An explicit CanonicalKeyer takes precedence
// over encoding. A tool that provides neither falls back to its
// canonicalized raw arguments.
func exactKeyOf(name string, prepared Prepared, args json.RawMessage) string {
	if key, ok := canonicalPrepared(prepared); ok {
		return name + ":" + key
	}
	return name + ":" + canonicalJSON(args)
}

// canonicalPrepared returns a prepared call's canonical JSON, when it
// has one the core can use.
func canonicalPrepared(prepared Prepared) (string, bool) {
	if prepared == nil {
		return "", false
	}
	if keyer, ok := prepared.(CanonicalKeyer); ok {
		return canonicalJSON(keyer.CanonicalKey()), true
	}
	if raw, err := json.Marshal(prepared); err == nil {
		if canonical := canonicalJSON(raw); canonical != "{}" && canonical != "null" {
			return canonical, true
		}
	}
	return "", false
}

// nearKeyOf returns the near repeat key of a call.
//
// The key is the tool plus its primary argument. A call that failed its
// validation has no valid primary argument, and the key says so.
func nearKeyOf(name, primary string, args json.RawMessage, invalid bool) string {
	if invalid {
		if primary == "" {
			return name + ":invalid"
		}
		value, ok := primaryArgValue(args, primary)
		if !ok {
			return name + ":invalid"
		}
		return name + ":invalid:" + value
	}
	if primary == "" {
		return name
	}
	value, ok := primaryArgValue(args, primary)
	if !ok {
		return name
	}
	return name + ":" + value
}

// primaryArgValue returns the raw value of a top-level string argument.
//
// The second result reports that the field was present as a string, so
// an empty string is not confused with a missing field.
func primaryArgValue(args json.RawMessage, name string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return "", false
	}
	raw, ok := obj[name]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// canonicalJSON returns a JSON value in a canonical form.
//
// The keys are sorted, so two calls that spell the same arguments in a
// different order share an exact key. Numbers keep their original
// precision, so two distinct large integers never compare equal.
func canonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(out)
}
