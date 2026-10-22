package poo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// summary describes the call, for the job's own bookkeeping.
	summary string
}

// executeBatch runs a turn's tool calls.
//
// A batch runs to completion: no stop condition skips the remaining
// calls. Only a cancellation or a fatal error (through the
// finalization) and an interrupt end it early. The calls run in parallel only when every one of
// them belongs to a tool declared ReadOnly; otherwise they run
// sequentially in model order.
//
// It reports whether any call was processed: a call that got a result
// other than skipped: truncated.
func (s *Session) executeBatch(run *runState, turn int, calls []ToolCall, truncated bool) bool {
	plans := s.planCalls(run, turn, calls, truncated)
	if len(plans) == 0 {
		return false
	}
	// The batch is registered before it runs, so every call in it has an
	// owner: its own dispatch, the skip path, or the seal. A call the
	// batch never reaches is the finalization's to settle.
	s.claimBatch(run, turn, plans)
	// A run that is winding down starts no new effect.
	if s.runWindingDown(run) {
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

	// The repeat counters are asked about the calls in model order,
	// before the batch runs: the counts depend on the order the model
	// asked for the calls, and a batch that runs in parallel still has
	// that order.
	s.countRepeats(plans)

	if s.batchIsParallel(plans) {
		s.executeParallel(run, turn, plans)
		// The calls that never started because of an interrupt get a
		// visible result.
		if s.interruptPending(run) {
			s.skipInterruptedCalls(run, turn, callsFromPlans(plans))
		} else if s.batchStopped(run) {
			s.skipCalls(run, turn, callsFromPlans(plans), SkippedSettlingText)
		}
		return processed
	}
	for _, p := range plans {
		// An interrupt skips the calls that have not started, and the
		// run continues.
		if s.interruptPending(run) {
			s.skipInterruptedCalls(run, turn, callsFromPlans(plans))
			return processed
		}
		// A fatal error or a cancellation ends the batch: the calls that
		// have not started do not start.
		if s.batchStopped(run) {
			s.skipCalls(run, turn, callsFromPlans(plans), SkippedSettlingText)
			return processed
		}
		s.executeOne(run, turn, p)
	}
	return processed
}

// batchStopped reports that the run holds a candidate that ends the
// batch.
//
// A batch runs to completion except for a cancellation or a fatal error,
// which end it through the finalization, and an interrupt. A finished
// call is not one of them: the calls around it still run.
func (s *Session) batchStopped(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range run.candidates {
		if c.reason != ReasonFinished {
			return true
		}
	}
	return false
}

// countRepeats records every call of a batch in the model order.
//
// The counts are the doom loop's, and they depend on the order the
// model asked for the calls: a batch that runs in parallel still has
// the model's order.
//
// A call that failed its validation counts toward the invalid near key,
// because the same malformed call repeated is the same loop.
func (s *Session) countRepeats(plans []callPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repeats == nil {
		s.repeats = newRepeatCounter()
	}
	if s.repeatCounts == nil {
		s.repeatCounts = map[string]repeatCounts{}
	}
	if s.repeatKeys == nil {
		s.repeatKeys = map[string]repeatKey{}
	}
	for _, p := range plans {
		primary := s.primaryArgOf(p.call.Name)
		exact := exactKeyOf(p.call.Name, p.call.Arguments)
		near := nearKeyOf(p.call.Name, primary, p.call.Arguments, false)
		if p.invalid {
			exact = "invalid:" + p.call.Name
			near = nearKeyOf(p.call.Name, primary, p.call.Arguments, true)
		}
		counts := s.repeats.count(exact, near)
		s.repeatCounts[p.handle] = counts
		s.repeatKeys[p.handle] = repeatKey{exact: exact, near: near}
		s.repeatOrder = append(s.repeatOrder, p.handle)
	}
}

// callsFromPlans returns the calls a plan holds.
func callsFromPlans(plans []callPlan) []ToolCall {
	out := make([]ToolCall, 0, len(plans))
	for _, p := range plans {
		out = append(out, p.call)
	}
	return out
}

// beginTurn records the turn's calls in the turn's summary.
//
// The turn's summary was made when the turn started, so the calls join
// the summary the run is in.
func (s *Session) beginTurn(run *runState, turn int, plans []callPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.turns {
		if s.turns[i].Session != run.globalTurn {
			continue
		}
		for _, p := range plans {
			s.turns[i].Calls = append(s.turns[i].Calls, ToolCallSummary{
				Handle: p.handle,
				Tool:   p.call.Name,
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
// The plan validates every call, so the repeat counters are asked about
// the calls in the model order and a call that failed its validation is
// counted toward the near key before the batch runs.
func (s *Session) planCalls(run *runState, turn int, calls []ToolCall, truncated bool) []callPlan {
	var plans []callPlan
	for i, call := range calls {
		handle := s.nextHandle()
		plan := callPlan{handle: handle, call: call, index: i, complete: true}
		if truncated && !isCompleteJSONObject(call.Arguments) {
			plan.complete = false
			plans = append(plans, plan)
			continue
		}
		// The call is validated now, so the repeat counters know
		// which calls failed before the batch runs.
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
		prepared, err := tool.Prepare(stripped, s.prepareEnv())
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
			plan.summary = prepared.Summary()
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
// Parallel execution happens only when every call is read-only,
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

// executeParallel runs a read-only batch at the same time.
func (s *Session) executeParallel(run *runState, turn int, plans []callPlan) {
	var wg sync.WaitGroup
	for _, p := range plans {
		wg.Add(1)
		go func(p callPlan) {
			defer wg.Done()
			// An interrupt that arrived before the call started skips
			// it.
			if s.interruptPending(run) || s.batchStopped(run) {
				return
			}
			s.executeOne(run, turn, p)
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
func (s *Session) executeOne(run *runState, turn int, plan callPlan) {
	if s.runWindingDown(run) {
		return
	}
	handle := plan.handle
	call := plan.call

	// The call is registered before the blocking record write, so a run
	// that settles while the write is in flight still has a call for
	// the finalization to settle. It is not marked started until the
	// record is written. A call claimed after the finalization has
	// sealed the run is the finalization's.
	if !s.issueCall(run, turn, handle, call) {
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
		callRecord, ok := s.reserveCallRecord(run, handle)
		if !ok {
			return false
		}
		recorded = true
		defer s.finishCallWrite(run, callRecord)
		if err := s.writeCallRecord(run, callRecord, &ToolCallRecord{
			RecordMeta: NewRecordMetaFrom("tool.call", OriginModel),
			RunID:      run.id,
			Turn:       turn,
			Handle:     handle,
			Tool:       call.Name,
			Arguments:  recordArgs(call.Arguments),
			Mode:       m,
		}); err != nil {
			if errors.Is(err, errCallRecordSealed) {
				// The finalization stopped waiting for the write and
				// sealed the run, so the call does not start and the
				// finalization gives it its result.
				return false
			}
			s.addCandidate(run, newCandidate(ReasonStoreError, SettleDetail{
				Source: "store",
				Text:   err.Error(),
			}))
			return false
		}
		return true
	}
	s.events.emit(EventToolCall, map[string]any{
		"run_id": run.id,
		"handle": handle,
		"tool":   call.Name,
	})

	// An incomplete call in a truncated response is skipped: the model
	// was cut off before it could finish the call.
	if !plan.complete {
		if !record(CallModeSync) {
			return
		}
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
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
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    fmt.Sprintf("unknown tool %q", call.Name),
			IsError: true,
		}, OutcomeError)
		return
	}

	// The call's outcome is decided by the core, not by the tool.
	prepared := plan.prepared
	if prepared == nil {
		// The call failed its validation. The failure is a recoverable
		// error: the model sees the message and may correct the call.
		text := "the call was not prepared"
		if plan.prepareErr != nil {
			text = plan.prepareErr.Error()
		}
		if !record(CallModeSync) {
			return
		}
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    text,
			IsError: true,
		}, OutcomeError)
		return
	}

	// The before-tool hooks run after the validation and before the
	// call, and they may refuse it or answer it themselves.
	decision, interrupted, err := s.runBeforeTool(run, turn, handle, call.Name, prepared, plan)
	// The hook may have outlived the run: a decision that arrives after
	// the finalization began is discarded, and the call starts nothing.
	if s.runWindingDown(run) {
		s.logLateHookDecision(run.id, handle, call.Name, turn)
		return
	}
	if err != nil {
		if !record(CallModeSync) {
			return
		}
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    err.Error(),
			IsError: true,
		}, OutcomeError)
		s.addCandidate(run, newCandidate(ReasonHookError, SettleDetail{
			Source: hookFailureSource(err, "before_tool"),
			Text:   err.Error(),
		}))
		return
	}
	// An interrupted authorization starts no call and admits no job. A
	// result an earlier hook gave before the interrupt is still the
	// call's result.
	if interrupted || s.interruptPending(run) {
		if decision.Result != nil {
			if !record(CallModeSync) {
				return
			}
			s.recordResult(run, turn, handle, call.Name, call.ID, *decision.Result, OutcomeOK)
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
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
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
		s.recordResult(run, turn, handle, call.Name, call.ID, *decision.Result, OutcomeOK)
		return
	}

	// The repeat counters were asked about the call before the batch
	// ran, so the counts the policy slot sees are the ones the plan
	// names.

	// A call that becomes a job returns at once, and its result
	// arrives later as an input. An interrupt that arrived after the
	// authorization refuses the admission.
	if s.callBecomesJob(call.Name, plan.background) {
		if !record(CallModeJob) {
			return
		}
		s.startJobCall(run, turn, handle, call, plan)
		return
	}

	// The call has been issued, so it needs a result even if the run
	// settles before it finishes.
	if !record(CallModeSync) {
		return
	}
	if s.runWindingDown(run) {
		// The finalization owns the issued call's result.
		return
	}
	// The call is marked started only once its tool is about to run, so
	// the finalization never waits for a call that the winding-down run
	// will not start. The claim is made against the seal, so nothing
	// starts after the commit point.
	if !s.startCall(run, handle) {
		return
	}

	callCtx := s.activityContextFor(run)
	result, err := tool.Call(callCtx, prepared, s.toolEnv(run, handle))
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
		if s.wasInterrupted(run) {
			text = interruptNote(text)
		} else {
			text = text + "\n" + CancelledInterruptedText
		}
		result = ToolResult{Text: text, IsError: true}
		outcome = OutcomeCancelled
	case err == nil && s.wasInterrupted(run):
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
			if s.wasInterrupted(run) {
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
	if outcome == OutcomeOK && call.Name == FinishToolName {
		if raw, ok := prepared.(FinishResult); ok {
			s.addCandidate(run, newCandidate(ReasonFinished, SettleDetail{
				Source: "finish",
				Text:   "the model called finish with a valid result",
				Result: raw.FinishResult(),
			}))
		}
	}

	// The after-tool chain runs before the result is recorded or
	// emitted, so the record, the event and the model see the same
	// sanitized result.
	out := s.recordToolResult(run, turn, handle, call.Name, call.ID, s.toolCallInfo(turn, handle, call.Name, prepared, plan), result, outcome)
	if fatalCandidate != nil {
		fatalCandidate.detail.Text = out.result.Text
		s.addCandidate(run, *fatalCandidate)
	}
}

// startJobCall turns a call into a background job.
//
// The job's slot is reserved and the job is registered under the session
// lock, and the job.started record is written before the goroutine
// starts. The call's own result is the "started" one, and the batch
// carries on.
func (s *Session) startJobCall(run *runState, turn int, handle string, call ToolCall, plan callPlan) {
	jobID, ok, winding := s.startJob(run, handle, call.Name, plan.prepared, plan.summary)
	if winding {
		// The run settled while the job was being accepted; nothing
		// else starts, and the finalization owns the run.
		return
	}
	if !ok {
		// There was no free slot. The failure is recoverable: the
		// model sees the message and may wait or cancel one.
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    TooManyJobsText,
			IsError: true,
		}, OutcomeError)
		return
	}

	text := fmt.Sprintf(StartedText, jobID, call.Name)
	s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{Text: text}, OutcomeStarted)
	s.events.emit(EventJobStarted, map[string]any{
		"job":    jobID,
		"handle": handle,
		"tool":   call.Name,
		"run_id": run.id,
	})
}

// prepareEnv builds the environment a tool validates its arguments in.
func (s *Session) prepareEnv() PrepareEnv {
	return PrepareEnv{
		WorkingDir:     s.opts.WorkingDir,
		ProtectedPaths: s.opts.ProtectedPaths,
	}
}

// toolEnv builds the environment a tool runs in.
func (s *Session) toolEnv(run *runState, handle string) ToolEnv {
	return s.toolEnvFor(run.id, handle)
}

// toolEnvFor builds the environment a tool runs in, for a run that may
// already have settled.
//
// A job runs under the session, so the run it names may be gone by the
// time the job runs. The environment still names the run that started
// it, so a tool can tie its work back to the call.
func (s *Session) toolEnvFor(runID, handle string) ToolEnv {
	return ToolEnv{
		PrepareEnv: s.prepareEnv(),
		SessionID:  s.opts.SessionID,
		RunID:      runID,
		Handle:     handle,
		Env:        s.opts.Env,
		Settings:   s.opts.Settings,
		Decider:    s.Decider(),
	}
}

// recordResult records a tool result and appends it to the history.
//
// The result goes back to the model under the call's provider-side id,
// because that is the id the provider matches it against.
//
// A call gets exactly one result. The call claims its result before the
// record is written, so a call that the finalization already gave a
// synthetic result to is not given a second one.
func (s *Session) recordResult(run *runState, turn int, handle, tool, callID string, result ToolResult, outcome Outcome) {
	won, tracked := s.claimResult(run, handle)
	if !won {
		// The finalization already gave the call a synthetic result.
		s.logLateToolResult(run.id, handle, tool, turn)
		return
	}
	s.writeResult(run, turn, handle, tool, callID, result, outcome, afterToolOutcome{result: result})
	if tracked {
		s.finishResultWrite(run)
	}
}

// recordToolResult runs the after-tool chain and records the result.
//
// The chain runs before the result is recorded or emitted, so the
// record, the event and the model all see the same sanitized result. A
// hook that fails settles the run with hook_error, and the call keeps
// the result the tool gave.
func (s *Session) recordToolResult(run *runState, turn int, handle, tool, callID string, info ToolCallInfo, result ToolResult, outcome Outcome) afterToolOutcome {
	out, err := s.runAfterTool(hookCompletionContext(run.ctx), run.id, "", turn, info, result)
	if err != nil {
		s.addCandidate(run, newCandidate(ReasonHookError, SettleDetail{
			Source: hookFailureSource(err, "after_tool"),
			Text:   err.Error(),
		}))
	}
	won, tracked := s.claimResult(run, handle)
	if !won {
		// The finalization already gave the call a synthetic result.
		s.logLateToolResult(run.id, handle, tool, turn)
		return out
	}
	s.writeResult(run, turn, handle, tool, callID, out.result, outcome, out)
	if tracked {
		s.finishResultWrite(run)
	}
	return out
}

// finishResultWrite marks the end of a claimed result's write, so the
// finalization, which waits for it, can go on.
func (s *Session) finishResultWrite(run *runState) {
	s.mu.Lock()
	run.resultWrites--
	done := run.workDone
	s.mu.Unlock()
	if done != nil {
		select {
		case done <- struct{}{}:
		default:
		}
	}
}

// finishCallWrite marks the end of a tool.call record's write.
func (s *Session) finishCallWrite(run *runState, call *issuedCall) {
	s.mu.Lock()
	call.writing = false
	cancel := call.recordCancel
	call.recordCancel = nil
	run.callWrites--
	s.signalWorkLocked(run)
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
func (s *Session) beginCallRecordLocked(run *runState, call *issuedCall) {
	base := run.ctx
	if base == nil {
		base = context.Background()
	}
	call.recorded = true
	call.writing = true
	call.recordCtx, call.recordCancel = context.WithCancelCause(context.WithoutCancel(base))
	if run.workDone == nil {
		run.workDone = make(chan struct{}, 1)
	}
	run.callWrites++
}

// writeResult writes a call's result record and puts it in the model's
// view.
//
// The caller has claimed the call's result. The chain's outcome
// carries the original the hooks replaced and the decisions they made:
// a redacted result's record holds no original at all.
//
// The result's model-facing text starts with the handle line, so the
// model can name the result later and a strategy can find it.
func (s *Session) writeResult(run *runState, turn int, handle, tool, callID string, result ToolResult, outcome Outcome, out afterToolOutcome) {
	text := modelFacingText(handle, tool, result.Text)
	rec := &ToolResultRecord{
		RecordMeta: NewRecordMetaFrom("tool.result", OriginTool),
		RunID:      run.id,
		Turn:       turn,
		Handle:     handle,
		Tool:       tool,
		Outcome:    outcome,
		Text:       text,
		Structured: result.Structured,
		Redacted:   out.redacted,
		RedactedBy: out.redactedBy,
		Hooks:      out.decisions,
	}
	if out.original != nil {
		rec.Original = &ToolResultData{
			Text:       out.original.Text,
			Structured: out.original.Structured,
			IsError:    out.original.IsError,
		}
	}
	if err := s.write(run.ctx, rec); err != nil {
		s.addCandidate(run, newCandidate(ReasonStoreError, SettleDetail{
			Source: "store",
			Text:   err.Error(),
		}))
	}

	// The result goes into the model's view and the turn's summary.
	msg := ToolResultMessage(callID, tool, text, result.IsError)
	msg.Origin = OriginTool
	s.appendMessages(msg)
	result.Text = text
	s.rememberResult(handle, tool, outcome, result)

	_ = s.write(run.ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", msg.Origin),
		RunID:      run.id,
		Turn:       turn,
		Role:       msg.Role,
		Content:    msg.Content,
	})

	s.events.emit(EventToolResult, map[string]any{
		"run_id":  run.id,
		"handle":  handle,
		"tool":    tool,
		"outcome": string(outcome),
		"text":    text,
	})
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
		if s.turns[i].Session != meta.turn {
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
func (s *Session) claimBatch(run *runState, turn int, plans []callPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.sealed {
		return
	}
	if run.pendingCalls == nil {
		run.pendingCalls = map[string]*issuedCall{}
	}
	for _, p := range plans {
		if run.settledCalls[p.handle] {
			continue
		}
		if _, issued := run.pendingCalls[p.handle]; issued {
			continue
		}
		run.pendingCalls[p.handle] = &issuedCall{
			handle:     p.handle,
			tool:       p.call.Name,
			id:         p.call.ID,
			turn:       turn,
			globalTurn: run.globalTurn,
			arguments:  p.call.Arguments,
		}
		s.resultMeta[p.handle] = resultMeta{
			tool:   p.call.Name,
			callID: p.call.ID,
			turn:   run.globalTurn,
		}
	}
}

// issueCall claims a call for its own dispatch, so the dispatch may write
// its record and run it.
//
// The batch registers every call it plans, so a call that another claim
// already owns is not the dispatch's. A call that arrives without its
// batch's registration is registered here and is the dispatch's. The
// claim is made against the seal: it reports false when the run is
// sealed, because the finalization owns every call that is still
// outstanding.
func (s *Session) issueCall(run *runState, turn int, handle string, call ToolCall) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.sealed {
		return false
	}
	if entry, issued := run.pendingCalls[handle]; issued {
		return !entry.recorded && !entry.started
	}
	if run.settledCalls[handle] {
		return false
	}
	run.pendingCalls[handle] = &issuedCall{
		handle:     handle,
		tool:       call.Name,
		id:         call.ID,
		turn:       turn,
		globalTurn: run.globalTurn,
		arguments:  call.Arguments,
	}
	s.resultMeta[handle] = resultMeta{
		tool:   call.Name,
		callID: call.ID,
		turn:   run.globalTurn,
	}
	if run.workDone == nil {
		run.workDone = make(chan struct{}, 1)
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
func (s *Session) reserveCallRecord(run *runState, handle string) (*issuedCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.sealed {
		return nil, false
	}
	call, ok := run.pendingCalls[handle]
	if !ok || call.recorded {
		return nil, false
	}
	s.beginCallRecordLocked(run, call)
	return call, true
}

// startCall claims that a registered call's tool is about to run, so the
// finalization knows it may have an effect to abandon.
//
// The claim is made against the seal: it reports false when the run is
// sealed, so nothing starts after the commit point.
func (s *Session) startCall(run *runState, handle string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.sealed || run.interruptPending {
		return false
	}
	call, ok := run.pendingCalls[handle]
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
func (s *Session) claimResult(run *runState, handle string) (won, tracked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.sealed {
		// The finalization owns every result that is still outstanding.
		return false, false
	}
	_, pending := run.pendingCalls[handle]
	delete(run.pendingCalls, handle)
	if run.settledCalls == nil {
		run.settledCalls = map[string]bool{}
	}
	taken := run.settledCalls[handle]
	if pending {
		// The call was issued, and the finalization may have given
		// it a synthetic result already.
		run.settledCalls[handle] = true
	}
	tracked = pending && !taken
	if tracked {
		run.resultWrites++
	}
	if run.workDone != nil {
		select {
		case run.workDone <- struct{}{}:
		default:
		}
	}
	return !taken, tracked
}

// addCandidate adds a settle candidate to the run.
//
// A candidate arriving after the commit point is logged and ignored: it
// never mutates a settled run.
func (s *Session) addCandidate(run *runState, c candidate) {
	s.mu.Lock()
	if run.committed {
		runID := run.id
		s.mu.Unlock()
		s.logLateCandidate(runID, c)
		return
	}
	run.candidates = append(run.candidates, c)
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
// canonical input, and a near key is the tool plus its primary
// argument. A Prepare failure counts toward the near key, because the
// same malformed call repeated is the same loop.
//
// The counters are kept per key, and a call to another tool does not
// break the streak: a run that reads the same file five times with
// another tool in between is still a loop. The keys name the tool, so
// calls to different tools never share a count.
type repeatCounter struct {
	// exact counts the repeats of each exact key.
	exact map[string]int
	// near counts the repeats of each near key.
	near map[string]int
}

// repeatCounts is what a call's repeat counters hold.
type repeatCounts struct {
	// exact is how many times the model made this exact call.
	exact int
	// near is how many times the model made a call with the same
	// primary argument.
	near int
}

// repeatKey is the pair of keys a call was counted under.
//
// The keys are kept so a policy that allowed a loop can clear the
// counters: the loop starts over.
type repeatKey struct {
	// exact is the call's exact key.
	exact string
	// near is the call's near key.
	near string
}

// newRepeatCounter returns an empty counter.
func newRepeatCounter() *repeatCounter {
	return &repeatCounter{exact: map[string]int{}, near: map[string]int{}}
}

// count records a call and returns the counts it had.
//
// The counts are the number of times the model has made this call, and
// the number of times it has made a call with the same primary
// argument, in the run.
func (c *repeatCounter) count(exactKey, nearKey string) repeatCounts {
	c.exact[exactKey]++
	c.near[nearKey]++
	return repeatCounts{exact: c.exact[exactKey], near: c.near[nearKey]}
}

// reset clears the counters of a key pair.
//
// The counters are the doom loop's, and a policy that allowed a loop
// clears them: the next call of the same shape starts the count over.
func (c *repeatCounter) reset(exactKey, nearKey string) {
	delete(c.exact, exactKey)
	delete(c.near, nearKey)
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
// The key is the tool plus its canonical arguments.
func exactKeyOf(name string, args json.RawMessage) string {
	return name + ":" + canonicalJSON(args)
}

// nearKeyOf returns the near repeat key of a call.
//
// The key is the tool plus its primary argument. A call that failed its
// validation has no valid primary argument, and the key says so.
func nearKeyOf(name, primary string, args json.RawMessage, invalid bool) string {
	if invalid {
		value := ""
		if primary != "" {
			value = primaryArgValue(args, primary)
		}
		if value == "" {
			return name + ":invalid"
		}
		return name + ":invalid:" + value
	}
	if primary == "" {
		return name
	}
	return name + ":" + primaryArgValue(args, primary)
}

// primaryArgValue returns the raw value of a top-level string argument.
//
// The value is the raw text, so two calls that spell the same argument
// the same way share a key.
func primaryArgValue(args json.RawMessage, name string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return ""
	}
	raw, ok := obj[name]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// canonicalJSON returns the arguments in a canonical form.
//
// The keys are sorted, so two calls that spell the same arguments in a
// different order share an exact key.
func canonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(out)
}
