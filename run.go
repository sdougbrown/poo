package poo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

// Prompt starts a run.
//
// The prompt is the text the model answers. The call returns the run's
// identifier at once, and the run goes on in the background until it
// settles.
func (s *Session) Prompt(ctx context.Context, text string, opts ...InputOption) (string, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", ErrSessionClosed
	}
	if s.broken {
		s.mu.Unlock()
		return "", ErrSessionBroken
	}
	if s.run != nil || s.reserved {
		s.mu.Unlock()
		return "", ErrBusy
	}
	// The delivery note is validated against the session's message-id
	// registry even for a prompt.
	note, err := s.prepareDeliveryNoteLocked(opts)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	// The run is reserved under the lock, so a wake that is starting
	// at the same time does not start a second run.
	s.reserved = true
	// A client message resets the count of the unattended wakes: the
	// chain of job → wake → job is broken by a person.
	s.unattendedWakes = 0
	s.mu.Unlock()

	runID, err := s.start(ctx, text, TriggerPrompt, note)
	s.mu.Lock()
	s.reserved = false
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	return runID, nil
}

// start begins a run and returns its identifier.
//
// The caller has checked the session's state. A run that was woken by job
// results carries them, and the results are appended before the run's
// first request.
func (s *Session) start(ctx context.Context, text string, trigger TriggerKind, note *DeliveryNote, jobs ...jobResult) (string, error) {
	// The admission is the last step of the reservation, and the
	// session's lock decides it together with Close: a close that got
	// the lock first refuses the run, and a run that got it first is
	// the run the close finalizes.
	s.raceHook("start")
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", ErrSessionClosed
	}
	// A session whose store failed takes no new run either: the state
	// was checked in Prompt under an earlier lock hold, and a store
	// failure may have landed between the two.
	if s.broken {
		s.mu.Unlock()
		return "", ErrSessionBroken
	}
	runID := NewULID()
	runCtx, cancel := context.WithCancelCause(ctx)
	run := &runState{
		id:              runID,
		ctx:             runCtx,
		cancel:          cancel,
		startedAt:       time.Now(),
		trigger:         trigger,
		pendingCalls:    map[string]*issuedCall{},
		workDone:        make(chan struct{}, 1),
		finalizeDone:    make(chan struct{}),
		finalizeStarted: make(chan struct{}),
		wake:            make(chan struct{}, 1),
	}
	s.run = run
	// The turn summaries and the results belong to the session, not to
	// the run: a turn keeps its number across the runs, and a handle
	// names a call for the life of the session.
	run.repeats = newRepeatCounter()
	s.transitionLocked(StateRequesting)
	// The results that were held are delivered at the start of the run,
	// before the prompt text. They are the session's own work coming
	// back, and the client asked for them.
	held := s.takeHeldResultsLocked()
	s.mu.Unlock()

	// The run's own record of its beginning. The write is claimed
	// against the seal, so a run that settles while it is in flight
	// cannot get run.settled first.
	if err := s.writeRun(run, &RunStarted{
		RecordMeta: NewRecordMeta("run.started"),
		RunID:      runID,
		PromptHash: hashText(text),
		StartedAt:  nowRFC3339(),
		ModelID:    s.opts.Model.ModelID,
		MaxTurns:   s.opts.MaxTurns,
		ToolsMode:  s.opts.ToolsMode,

		ReasoningEffort: s.opts.ReasoningEffort,
		Trigger:         trigger,
		Jobs:            publicJobResults(jobs),
	}); err != nil {
		// The store refused the run's first record. The run cannot
		// continue, so it goes through the normal finalization: it
		// emits run.settled and takes the store_error code, rather
		// than leaving the caller with a result the table cannot
		// explain.
		s.finalize(run, storeErrorCandidate(err))
		return runID, nil
	}
	s.events.emit(EventRunStarted, map[string]any{
		"run_id":      runID,
		"prompt_hash": hashText(text),
		"trigger":     string(trigger),
		"jobs":        jobPayload(jobs),
	})

	// The wall-clock budget starts when the run is admitted, before the
	// startup hooks and the prompt render, so a blocked hook cannot
	// outlive MaxDuration.
	stopTimer := s.startDurationTimer(run)
	s.mu.Lock()
	run.stopTimer = stopTimer
	s.mu.Unlock()

	// The held results are delivered before the prompt's text, so the
	// model sees them first. A woken run's job results are appended
	// before its first request too, and they are recorded with the
	// delivery the queue gave them.
	//
	// The results are delivered before the before-run hooks, so a woken
	// run's hooks see the results that woke the run.
	for _, result := range held {
		s.deliverJobResult(run, result)
	}
	for _, result := range jobs {
		s.deliverJobResult(run, result)
	}
	if s.runWindingDown(run) {
		return runID, nil
	}

	// The before-run hooks run at the start of every run, before the
	// first request. The trigger says what started the run.
	decisions, err := s.runBeforeRun(run, RunTrigger{
		Kind:   trigger,
		Prompt: text,
		Jobs:   publicJobResults(jobs),
	})
	if err != nil {
		// A hook that failed settles the run with hook_error.
		s.finalize(run, newCandidate(ReasonHookError, SettleDetail{
			Source: hookFailureSource(err, "before_run"),
			Text:   err.Error(),
		}))
		return runID, nil
	}
	// The finalization owns the run: the startup writes nothing more
	// for it.
	if s.runWindingDown(run) {
		return runID, nil
	}

	// The prompt the run carries is the one rendered when the session
	// was built, or the one a hook edited for this run. It is frozen for
	// the length of the run, and a run that is woken reuses the copy the
	// first run recorded.
	promptText, promptVariant := s.runSystemPrompt(decisions.edits)
	if promptText != "" {
		if s.runWindingDown(run) {
			return runID, nil
		}
		// A prompt that differs from the last one is recorded whole,
		// with its hash and variant, so a replay never re-renders it.
		if s.promptChanged(promptText) {
			if err := s.writeRun(run, &SystemPromptRecord{
				RecordMeta: NewRecordMeta("system_prompt"),
				RunID:      runID,
				Variant:    promptVariant,
				Text:       promptText,
				Hash:       hashText(promptText),
			}); err != nil {
				if s.runWindingDown(run) {
					return runID, nil
				}
				s.finalize(run, newCandidate(ReasonStoreError, SettleDetail{
					Source: "store",
					Text:   err.Error(),
				}))
				return runID, nil
			}
			s.rememberPrompt(promptText)
		}
		msg := NewTextMessage(RoleSystem, promptText)
		msg.Origin = OriginSystem
		// The history persists across a session's runs, so the prompt's
		// message is recorded and appended once: a woken run reuses the
		// copy the first run put there, and its own frozen copy is what
		// its request carries.
		if !s.hasSystemMessage() {
			if err := s.writeRun(run, &MessageRecord{
				RecordMeta: NewRecordMetaFrom("message", msg.Origin),
				RunID:      runID,
				Role:       msg.Role,
				Content:    msg.Content,
			}); err != nil {
				if s.runWindingDown(run) {
					return runID, nil
				}
				s.finalize(run, newCandidate(ReasonStoreError, SettleDetail{
					Source: "store",
					Text:   err.Error(),
				}))
				return runID, nil
			}
			s.appendMessages(msg)
		}
		s.mu.Lock()
		run.systemMessage = &msg
		s.mu.Unlock()
	}

	// The messages the before-run hooks appended follow the system
	// prompt.
	s.appendStartMessages(run, decisions)
	if s.runWindingDown(run) {
		return runID, nil
	}

	user := NewTextMessage(RoleUser, withDeliveryNote(text, note))
	user.Origin = OriginUser
	user.DeliveryNote = note
	if err := s.writeRun(run, &MessageRecord{
		RecordMeta:   NewRecordMetaFrom("message", user.Origin),
		RunID:        runID,
		Turn:         0,
		Role:         user.Role,
		Content:      user.Content,
		DeliveryNote: note,
	}); err != nil {
		if s.runWindingDown(run) {
			return runID, nil
		}
		// The store refused a record. The run settles store_error, and
		// the session is broken.
		s.finalize(run, newCandidate(ReasonStoreError, SettleDetail{
			Source: "store",
			Text:   err.Error(),
		}))
		return runID, nil
	}
	s.appendMessages(user)
	if s.runWindingDown(run) {
		return runID, nil
	}

	// The run goes on in the background: the caller gets the run's
	// identifier at once, and Wait tells it how the run ended.
	go s.drive(run)

	return runID, nil
}

// drive runs the loop until the run settles.
//
// Every path out of the loop goes through finalize, so the run always
// has exactly one settle record.
func (s *Session) drive(run *runState) {
	defer func() {
		// A panic in a hook or a tool must not leave the run without
		// a settle record: the promise the log makes is that a
		// run.started is always followed by a run.settled.
		if r := recover(); r != nil {
			s.finalize(run, newCandidate(ReasonToolFatal, SettleDetail{
				Source: "panic",
				Text:   fmt.Sprintf("%v", r),
			}))
		}
	}()

	// The duration budget is a timer: it fires even while a tool is
	// running, and it settles the run through the normal protocol. It
	// started when the run was admitted, before the startup hooks.
	defer s.stopRunTimer(run)

	cands := s.loop(run)
	for {
		_, abandoned := s.finalizeCandidates(run, true, s.settleCandidates(run, cands))
		if !abandoned {
			return
		}
		// A queued input reopened the run, and the loop carries on in
		// this goroutine, under the same timer and recovery.
		var ok bool
		if cands, ok = s.resume(run); !ok {
			return
		}
	}
}

// settleCandidates returns the candidates a loop that ended hands to the
// finalization.
//
// A loop that ended with nothing to say, and with no candidate held by
// the run, ended naturally.
func (s *Session) settleCandidates(run *runState, cands []candidate) []candidate {
	if len(cands) > 0 {
		return cands
	}
	s.mu.Lock()
	held := len(run.candidates) > 0
	s.mu.Unlock()
	if held {
		return nil
	}
	return []candidate{newCandidate(ReasonEndTurn, SettleDetail{Source: "end_turn"})}
}

// hasSystemMessage reports that the history already carries the
// session's system prompt.
//
// The caller must not hold the session's lock.
func (s *Session) hasSystemMessage() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.history {
		if m.Role == RoleSystem {
			return true
		}
	}
	return false
}

// recordAssistantResponse records the model's response as a message: the
// blocks, the calls the response carried, and the id the stream assigned
// at its first delta. A response an interrupt cut short is recorded with
// its partial flag and cause; the shared builder is what keeps the two
// paths' messages identical apart from those fields.
//
// The store error comes back to the caller: the run path turns it into
// the settle candidate, and the interrupt path ignores it, as it did
// before the paths shared this builder.
func (s *Session) recordAssistantResponse(run *runState, turn int, resp *response, partial bool, cause Cause) (Message, error) {
	content := append([]ContentBlock(nil), resp.content...)
	// The calls the model had asked for belong to the message, so the
	// results that follow have a call to answer.
	for _, call := range resp.calls {
		content = append(content, ContentBlock{
			Kind:       ContentToolCall,
			ToolCallID: call.ID,
			Name:       call.Name,
			Arguments:  recordArgs(call.Arguments),
			Origin:     call.Origin,
		})
	}
	msg := Message{Role: RoleAssistant, Content: content, Origin: OriginModel}
	if len(content) == 0 {
		return msg, nil
	}
	if resp.messageID != "" {
		msg.ID = resp.messageID
	} else {
		msg.ID = s.messageIDs.Next()
	}
	msg.Partial = partial
	msg.Cause = cause
	s.appendMessages(msg)
	s.recordMessage(msg)
	s.growSpan(run.globalTurn)
	// The write is claimed against the seal: a message whose record the
	// finalization ends at the seal is abandoned rather than appended
	// after the final flush.
	return msg, s.writeRun(run, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", msg.Origin),
		RunID:      run.id,
		Turn:       turn,
		Role:       msg.Role,
		Content:    msg.Content,
		MessageID:  msg.ID,
		Partial:    partial,
		Cause:      cause,
	})
}

// startDurationTimer starts the run's wall-clock budget.
//
// The timer returns a function that stops it.
func (s *Session) startDurationTimer(run *runState) func() {
	if s.opts.MaxDuration <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		timer := time.NewTimer(s.opts.MaxDuration)
		defer timer.Stop()
		select {
		case <-stop:
		case <-timer.C:
			s.finalize(run, newCandidate(ReasonBudgetExhausted, SettleDetail{
				Source: "max_duration",
				Text:   fmt.Sprintf("the run took its %s limit", s.opts.MaxDuration),
			}))
		}
	}()
	return func() { close(stop) }
}

// stopRunTimer stops the run's wall-clock timer, if it is still running.
func (s *Session) stopRunTimer(run *runState) {
	s.mu.Lock()
	stop := run.stopTimer
	run.stopTimer = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// loop drives the requesting/executing cycle and returns the candidates
// the run's end produced.
//
// The candidates the run already holds (a fatal tool error, a refused
// write) are not returned again: the finalization reads them from the
// run.
//
// The loop is the plan's FSM: requesting, streaming, executing,
// compacting, settling. Every path to a new request goes through the
// next-request gate.
func (s *Session) loop(run *runState) []candidate {
	for {
		// A cancellation of the run's own context ends the run.
		if err := run.ctx.Err(); err != nil {
			if cancelledByFinalize(run) {
				return nil
			}
			return []candidate{newCandidate(ReasonCancelled, cancelDetail(err))}
		}

		// A waiting run blocks until a message or a job result arrives.
		// Nothing is requested, no tool runs and no hook fires while the
		// run waits.
		s.waitWhileWaiting(run)

		// The next-request gate. Every path to a new request comes
		// through it.
		//
		// A pending candidate settles the run: the gate never opens a
		// request while a candidate is pending. The run already holds
		// it, so the loop returns nothing new.
		gate := s.gate(run)
		if gate.held {
			return nil
		}
		if len(gate.candidates) > 0 {
			return gate.candidates
		}

		s.mu.Lock()
		run.turn++
		run.requests++
		turn := run.turn
		s.transitionLocked(StateRequesting)
		s.mu.Unlock()

		// The turn starts here: the session numbers it, and the
		// span it occupies in the view begins.
		run.globalTurn = s.openTurn(turn)

		s.events.emit(EventTurnStarted, map[string]any{"run_id": run.id, "turn": turn})

		resp, rendered, err := s.request(run, turn)
		// The run may have settled while the request was in flight.
		// What the request produced then is a late completion: it
		// never mutates the history, the records or the events. The
		// response is dropped and logged once; its deltas are not
		// logged one by one.
		if s.runWindingDown(run) {
			s.logLateProviderResponse(run.id, turn)
			return nil
		}
		if rendered.Body != nil || rendered.Messages > 0 {
			s.mu.Lock()
			run.lastRendered = rendered
			run.hasRendered = true
			s.mu.Unlock()
		}
		if err != nil {
			// An interrupt during the request aborts the attempt
			// and turns it into an interrupted turn. The attempt
			// does not count against the retry budget.
			if s.interruptPending(run) {
				// The aborted request is still charged once before the
				// interrupted turn's budget check.
				if !s.accountInterruptedRequest(run, turn, resp) {
					return nil
				}
				s.interruptedActivityTurn(run, turn, resp, false)
				continue
			}
			if cancelledByFinalize(run) {
				return nil
			}
			// The stream carried part of an answer: what it said
			// belongs to the history, even though the run ends with
			// the failure. Its calls are recorded and never run, and each
			// gets a visible result, so a later request pairs every call
			// with its result.
			if resp != nil && (len(resp.content) > 0 || len(resp.calls) > 0) {
				s.recordPartialCause(run, turn, resp, CauseProviderError)
				s.skipCalls(run, turn, resp.calls, SkippedProviderErrorText)
			}
			reason, detail, serr := s.settleForError(err)
			if serr != nil {
				return []candidate{newCandidate(ReasonCancelled, cancelDetail(serr))}
			}
			// The request failed, but the usage it produced is still
			// written: every request gets a usage record.
			if resp != nil && resp.usage != nil {
				s.writeUsage(run, turn, resp.usage, resp.requestChars)
			}
			return []candidate{newCandidate(reason, detail)}
		}

		// The model's response is part of the history whether or not
		// it asked for tool calls.
		assistant, err := s.recordAssistantResponse(run, turn, resp, false, "")
		if err != nil {
			return []candidate{newCandidate(ReasonStoreError, SettleDetail{
				Source: "store",
				Text:   err.Error(),
			})}
		}

		if resp.usage != nil {
			if !s.writeUsage(run, turn, resp.usage, resp.requestChars) {
				// The refused record is the run's candidate; the
				// finalization settles it.
				return nil
			}
		} else {
			// The endpoint reported no usage, and the run's token
			// budget still has to count the turn.
			s.addEstimatedUsage(run, resp, assistant)
		}

		// A truncated response: the complete calls run, and the
		// incomplete ones are skipped with a visible result.
		truncated := resp.finish == FinishLength

		// The turn's calls run before the turn ends.
		processed := false
		if len(resp.calls) > 0 {
			processed = s.executeBatch(run, turn, resp.calls, truncated)
			// The turn's messages are all in the view now, so the
			// turn's span is complete.
			s.growSpan(run.globalTurn)
			// An interrupt during the batch cut the turn short. The
			// unstarted calls are skipped, and the run continues.
			if s.interruptPending(run) {
				s.interruptedActivityTurn(run, turn, resp, true)
				continue
			}
		}

		// A failure the transport reported about the response is
		// visible: the model is told, and the record holds it. A note
		// that opened another turn is a continuation, like a steer.
		noted := false
		if len(resp.errors) > 0 {
			var err error
			noted, err = s.reportResponseErrors(run, turn, resp.errors)
			if err != nil {
				var perr *ProviderError
				if errors.As(err, &perr) {
					reason, detail, _ := s.settleForError(perr)
					return []candidate{newCandidate(reason, detail)}
				}
				return []candidate{newCandidate(ReasonStoreError, SettleDetail{
					Source: "store",
					Text:   err.Error(),
				})}
			}
		} else {
			s.resetRecoverableNotes(run)
		}

		// The turn ended. The turn-end algorithm decides what happens
		// next: it may settle the run, or it may go to the gate.
		outcome := s.turnEnd(run, turn, processed, truncated, noted)
		if len(outcome.candidates) > 0 {
			return outcome.candidates
		}
		// A turn that appended something, or that processed a call,
		// goes to the gate again. The gate is where the run's next
		// request is opened.
	}
}

// turnOutcome is what the turn-end algorithm decided.
type turnOutcome struct {
	// candidates are the settle candidates the turn produced.
	candidates []candidate
}

// turnEnd runs the plan's turn-end algorithm.
//
// The steps are normative: emit turn.ended, drain the queues, run the
// AfterTurn hooks, evaluate the stop conditions, and then either settle
// or go to the next-request gate.
func (s *Session) turnEnd(run *runState, turn int, processed, truncated, noted bool) turnOutcome {
	// Step 1: the turn is over.
	s.events.emit(EventTurnEnded, map[string]any{"run_id": run.id, "turn": turn})

	// Step 2: drain the steer queue, then the job queue.
	//
	// A follow-up is not drained here: it waits for a natural stop, and
	// finalize step 1 delivers it. A response note that opened another
	// turn counts like a steer: the model has something new to read.
	appended := s.drainInputs(run, false) || noted

	// Step 3: the AfterTurn hooks run on every turn, tool-free turns
	// included.
	hookAppended, stop := s.runAfterTurn(run, turn, false)
	appended = appended || hookAppended

	// An interrupt that arrived during the hooks cancels them, and
	// their decisions are discarded: that is not a hook error. The
	// turn's end has already been emitted, so the remaining turn-end
	// steps are skipped.
	if s.interruptPending(run) {
		s.interruptedTurnEnd(run, turn)
		return turnOutcome{}
	}

	// Step 4: every stop condition is evaluated.
	var cands []candidate
	if stop != nil {
		cands = append(cands, *stop)
	}
	// A parent context that ended while the hooks ran is external
	// cancellation, and it outranks the hooks' own outcomes.
	if detail, ok := s.externalCancellation(run); ok {
		cands = append(cands, newCandidate(ReasonCancelled, detail))
	}
	cands = append(cands, s.evaluateStopConditions(run, turn)...)
	cands = append(cands, s.evaluateBudgets(run, turn)...)

	// Step 5: a candidate settles the run, and the highest-precedence
	// one wins.
	if len(cands) > 0 {
		return turnOutcome{candidates: cands}
	}

	// Step 6: the turn processed a call, or something was appended, so
	// the run goes to the gate. A call skipped as truncated is not
	// processed.
	if processed || appended {
		return turnOutcome{}
	}

	// Step 7: the response was a natural stop.
	//
	// A truncated response with no complete call is a natural stop with
	// the truncated reason.
	natural := ReasonEndTurn
	if truncated {
		natural = ReasonOutputTruncated
	}

	// A job started by this run that is still running holds the run
	// open: the run goes to the waiting state, and the settle hooks do
	// not run, because the run is not ending.
	if s.runHasRunningJob(run) {
		waiting, drained := s.enterWaiting(run)
		if waiting || drained {
			// Waiting holds the run; a drained message goes to the
			// gate, so the model answers it with another request.
			return turnOutcome{}
		}
	}

	decision, continued, err := s.runBeforeSettle(run, turn, natural)
	if s.interruptPending(run) {
		// An interrupt that arrived during the settle hooks discards
		// their decisions, and that is not a hook error. There is no
		// second turn end.
		s.interruptedTurnEnd(run, turn)
		return turnOutcome{}
	}
	// A parent context that ended while the settle hooks ran is
	// external cancellation: it becomes the cancelled candidate, unlike
	// the hook interruption above, which is internal.
	if detail, ok := s.externalCancellation(run); ok {
		return turnOutcome{candidates: []candidate{newCandidate(ReasonCancelled, detail)}}
	}
	if err != nil {
		// A hook that asked for more continuations than the run may
		// take settles with its own reason, not with hook_error.
		var limit *hookContinueLimitError
		if errors.As(err, &limit) {
			return turnOutcome{candidates: []candidate{newCandidate(ReasonHookContinueLimit, SettleDetail{
				Source: "before_settle",
				Text:   limit.Error(),
			})}}
		}
		return turnOutcome{candidates: []candidate{newCandidate(ReasonHookError, SettleDetail{
			Source: hookFailureSource(err, "before_settle"),
			Text:   err.Error(),
		})}}
	}
	switch {
	case decision.Stop:
		return turnOutcome{candidates: []candidate{newCandidate(ReasonStopRequested, SettleDetail{
			Source: "before_settle",
			Text:   decision.Detail,
		})}}
	case continued:
		return turnOutcome{}
	default:
		return turnOutcome{candidates: []candidate{newCandidate(natural, SettleDetail{Source: string(natural)})}}
	}
}

// evaluateStopConditions asks every stop condition and collects the
// candidates they produce.
func (s *Session) evaluateStopConditions(run *runState, turn int) []candidate {
	s.mu.Lock()
	tokens := run.tokens
	s.mu.Unlock()
	state := StopState{
		Turn:      turn,
		Calls:     s.callStates(run.globalTurn),
		RunTokens: tokens,
	}
	var cands []candidate
	for _, c := range s.opts.StopConditions {
		if c == nil {
			continue
		}
		stop, detail := c(state)
		if !stop {
			continue
		}
		cands = append(cands, newCandidate(ReasonStopRequested, SettleDetail{
			Source: "stop_condition",
			Text:   detail,
		}))
	}
	return cands
}

// evaluateBudgets asks the run's budgets whether they are spent.
//
// The budgets are the plan's three: MaxTurns, MaxRunTokens and
// MaxDuration. Each one that is spent adds a candidate, and the
// precedence table decides which wins.
func (s *Session) evaluateBudgets(run *runState, turn int) []candidate {
	var cands []candidate
	if s.opts.MaxTurns > 0 && turn >= s.opts.MaxTurns {
		cands = append(cands, newCandidate(ReasonMaxTurns, SettleDetail{
			Source: "max_turns",
			Text:   fmt.Sprintf("the run reached its %d-turn limit", s.opts.MaxTurns),
		}))
	}
	s.mu.Lock()
	tokens := run.tokens
	elapsed := time.Since(run.startedAt)
	s.mu.Unlock()
	if s.opts.MaxRunTokens > 0 && tokens >= s.opts.MaxRunTokens {
		cands = append(cands, newCandidate(ReasonBudgetExhausted, SettleDetail{
			Source: "max_run_tokens",
			Text:   fmt.Sprintf("the run used %d tokens, and the limit is %d", tokens, s.opts.MaxRunTokens),
		}))
	}
	if s.opts.MaxDuration > 0 && elapsed >= s.opts.MaxDuration {
		cands = append(cands, newCandidate(ReasonBudgetExhausted, SettleDetail{
			Source: "max_duration",
			Text:   fmt.Sprintf("the run took %s, and the limit is %s", elapsed.Round(time.Millisecond), s.opts.MaxDuration),
		}))
	}
	return cands
}

// callStates returns the calls of a turn and their outcomes.
//
// The turn is the session's number for the turn, so the summaries of
// the earlier runs are not mixed in.
func (s *Session) callStates(turn int) []CallState {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []CallState
	for _, t := range s.turns {
		if t.Session != turn {
			continue
		}
		for _, c := range t.Calls {
			result := s.results[c.Handle]
			out = append(out, CallState{
				Handle:  c.Handle,
				Tool:    c.Tool,
				Outcome: c.Outcome,
				IsError: result.IsError,
			})
		}
	}
	return out
}

// drainInputs delivers the queued inputs and reports whether anything
// was appended.
//
// The steer queue comes first, then the job queue, then the follow-up
// queue. A job result is a user message with origin "job", and it never
// carries a delivery note.
func (s *Session) drainInputs(run *runState, includeFollowUp bool) bool {
	delivered := false
	for {
		s.mu.Lock()
		if run.committed || run.finalizing || len(s.steerQueue) == 0 || !s.steerQueue[0].accepted {
			s.mu.Unlock()
			break
		}
		input := s.steerQueue[0]
		s.steerQueue = s.steerQueue[1:]
		// The input is taken under the same lock as the queue, so the
		// finalization can see that it is in flight and report it if
		// the delivery is abandoned.
		run.delivering = append(run.delivering, input)
		s.mu.Unlock()
		ok := s.deliverInput(run, input)
		delivered = true
		if !ok {
			// The store refused a delivery's record, or the run settled
			// while the delivery was in flight. The inputs still
			// queued stay there, so the finalization reports them
			// instead of losing them.
			return delivered
		}
	}
	// The job queue drains next: the results are the session's own work
	// coming back, and they reach the model before a follow-up does.
	if s.drainJobQueue(run) {
		delivered = true
	}
	if !includeFollowUp {
		return delivered
	}
	for {
		s.mu.Lock()
		if run.committed || run.finalizing || len(s.followUpQueue) == 0 || !s.followUpQueue[0].accepted {
			s.mu.Unlock()
			break
		}
		input := s.followUpQueue[0]
		s.followUpQueue = s.followUpQueue[1:]
		run.delivering = append(run.delivering, input)
		s.mu.Unlock()
		ok := s.deliverInput(run, input)
		delivered = true
		if !ok {
			return delivered
		}
	}
	return delivered
}

// deliverMessageRecords carries out the delivery protocol one accepted
// input follows: the message record, then the input.delivered record,
// then the history append, the delivery's finish, and the delivery
// event. The records come before the history append, so a delivery the
// finalization abandons must not mutate the view.
//
// The two delivery families differ in what a failed write does to the
// input: the caller's fail hook handles the record writes' errors (the
// client input is dropped and reported; a job result goes back on the
// queue), and done releases the family's own claim.
func (s *Session) deliverMessageRecords(run *runState, msg Message, inputID string, mode InputMode, note *DeliveryNote, fail func(error), done func()) bool {
	if err := s.writeDelivery(run.ctx, run, &MessageRecord{
		RecordMeta:   NewRecordMetaFrom("message", msg.Origin),
		RunID:        run.id,
		Role:         msg.Role,
		Content:      msg.Content,
		DeliveryNote: note,
	}); err != nil {
		fail(err)
		return false
	}
	if err := s.writeDelivery(run.ctx, run, &InputDelivered{
		RecordMeta:  NewRecordMeta("input.delivered"),
		SessionID:   s.opts.SessionID,
		RunID:       run.id,
		InputID:     inputID,
		Mode:        mode,
		DeliveredAt: nowRFC3339(),
	}); err != nil {
		fail(err)
		return false
	}
	s.appendMessages(msg)
	done()
	s.events.emit(EventInputDelivered, map[string]any{
		"run_id":   run.id,
		"input_id": inputID,
		"mode":     string(mode),
	})
	return true
}

// deliverInput appends one input's message to the model's view and
// records the delivery.
//
// The delivery note is rendered here, from the reply as it was recorded:
// a reference accepted while the reply was still streaming is resolved
// once the stream has been finalized.
func (s *Session) deliverInput(run *runState, input queuedInput) bool {
	s.mu.Lock()
	if run.committed || run.finalizing {
		s.abandonDeliveryLocked(run, input)
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	note := s.renderForDelivery(input.note)
	msg := NewTextMessage(RoleUser, withDeliveryNote(input.text, note))
	msg.Origin = originForMode(input.mode)
	msg.DeliveryNote = note
	// The message never reached the model, so the input is dropped and
	// reported, even though the store is what failed.
	fail := func(err error) {
		s.abandonDelivery(run, input)
		if !errors.Is(err, errDeliveryAbandoned) {
			s.noteStoreError(run, err)
		}
	}
	return s.deliverMessageRecords(run, msg, input.id, input.mode, note, fail, func() {
		s.finishDelivery(run, input)
	})
}

// errDeliveryAbandoned reports that a delivery write was abandoned
// because the run settled before the write finished.
var errDeliveryAbandoned = errors.New("poo: the delivery was abandoned")

// writeDelivery writes a record for an input's delivery.
//
// Unlike write, a cancellation from the run's finalization abandons the
// write instead of breaking the session: the delivery is in-flight work
// that finalization step 3 may abandon. A write the close has fenced is
// abandoned for the same reason.
func (s *Session) writeDelivery(ctx context.Context, run *runState, rec Record) error {
	if s.opts.Sink == nil {
		return nil
	}
	s.mu.Lock()
	if s.sunk {
		s.mu.Unlock()
		return errors.New("poo: the session store is broken")
	}
	// The delivery's decision and its write's registration are one step
	// under the lock, so a close that sees the run sees the write too.
	abandoned := run.committed || run.finalizing || ctx.Err() != nil
	gate, ok := s.beginWriteLocked(rec.Meta().Record)
	s.mu.Unlock()
	if abandoned {
		s.endWrite(gate)
		return errDeliveryAbandoned
	}
	if !ok {
		return errDeliveryAbandoned
	}
	wctx, cancel := context.WithTimeout(ctx, s.recordBudget())
	defer cancel()
	if err := s.appendToSink(wctx, rec, gate); err != nil {
		if errors.Is(err, errWriteFenced) {
			return errDeliveryAbandoned
		}
		s.mu.Lock()
		abandoned := run.committed || run.finalizing || ctx.Err() != nil
		if !abandoned {
			s.sunk = true
			s.broken = true
		}
		s.mu.Unlock()
		if abandoned {
			return errDeliveryAbandoned
		}
		return fmt.Errorf("poo: writing %s: %w", rec.Meta().Record, err)
	}
	return nil
}

// finishDelivery releases a delivered input's in-flight claim.
func (s *Session) finishDelivery(run *runState, input queuedInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range run.delivering {
		if run.delivering[i].id == input.id {
			run.delivering = append(run.delivering[:i], run.delivering[i+1:]...)
			break
		}
	}
	s.signalWorkLocked(run)
}

// abandonDelivery moves an in-flight delivery to the inputs the
// finalization reports as dropped.
func (s *Session) abandonDelivery(run *runState, input queuedInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abandonDeliveryLocked(run, input)
}

// abandonDeliveryLocked moves an in-flight delivery to the inputs the
// finalization reports as dropped.
//
// The caller holds the session's lock.
func (s *Session) abandonDeliveryLocked(run *runState, input queuedInput) {
	for i := range run.delivering {
		if run.delivering[i].id == input.id {
			run.delivering = append(run.delivering[:i], run.delivering[i+1:]...)
			run.abandonedInputs = append(run.abandonedInputs, input)
			s.signalWorkLocked(run)
			return
		}
	}
}

// signalWorkLocked wakes the finalization, which waits for in-flight
// work.
//
// The caller holds the session's lock.
func (s *Session) signalWorkLocked(run *runState) {
	if run.workDone == nil {
		return
	}
	select {
	case run.workDone <- struct{}{}:
	default:
	}
}

// storeErrorCandidate is the settle candidate for a record the store
// refused.
func storeErrorCandidate(err error) candidate {
	return newCandidate(ReasonStoreError, SettleDetail{Source: "store", Text: err.Error()})
}

// noteStoreError reports a record the store refused.
//
// A write the run's finalization sealed is not a failure: the
// finalization owns the record, and the store refusing it means the
// record is no longer wanted.
func (s *Session) noteStoreError(run *runState, err error) {
	if err == nil || errors.Is(err, errCallRecordSealed) {
		return
	}
	s.addCandidate(run, storeErrorCandidate(err))
}

// originForMode returns the origin an input's message carries.
func originForMode(mode InputMode) Origin {
	switch mode {
	case Interrupt:
		return OriginInterrupt
	case FollowUp:
		return OriginFollowUp
	default:
		return OriginSteer
	}
}

// request sends one provider request and reads the whole response.
//
// A response that broke part way through is returned along with the
// error, so the caller can still write the usage it produced.
func (s *Session) request(run *runState, turn int) (*response, Rendered, error) {
	// The model is read once, here: the request's body, its token limit and
	// every model info-derived decision come from this one value, so a
	// switch between runs cannot reach a request half way.
	s.mu.Lock()
	model := s.opts.Model
	messages := s.modelMessagesLocked()
	tools := s.toolSpecs()
	requestChars := s.historyCharsLocked()
	maxTokens := model.MaxTokens(s.estimatedInputLocked())
	s.mu.Unlock()

	// The request runs under the run's activity context, so an
	// interrupt aborts it without ending the run.
	actCtx, _ := s.startActivity(run)
	defer s.endActivity(run)

	reader, rendered, err := s.opts.Transport.Stream(actCtx, ChatRequest{
		Model:     model.ModelID,
		Info:      model,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: maxTokens,
		OnAttempt: func(a Attempt) { s.reportAttempt(run, turn, a) },
	})
	if err != nil {
		return nil, rendered, err
	}
	defer reader.Close()

	s.transition(StateStreaming)
	// The message id is assigned at the stream's first delta, before the
	// message is recorded: a delivery note may name a message that is
	// still streaming.
	ids := s.messageIDs
	resp, err := readResponse(actCtx, reader, ids, run.id, turn, run.requests, func(name EventName, payload map[string]any) {
		// A stream that outlives the run is a late completion: what it
		// carries never reaches the history, the records or the
		// events.
		if s.runWindingDown(run) {
			return
		}
		s.events.emit(name, payload)
	})
	if resp != nil {
		resp.requestChars = requestChars
	}
	if err != nil && resp != nil && resp.usage == nil {
		if reporter, ok := reader.(UsageReporter); ok {
			if usage := reporter.ReportUsage(); usage != nil {
				resp.usage = usage
			}
		}
	}
	return resp, rendered, err
}

// reportAttempt writes the record of an attempt that failed.
//
// The attempts are written for the audit: a reader of the log sees what
// the request tried and what every attempt failed with. The failure
// that ended the request is in the run's settle record as well.
func (s *Session) reportAttempt(run *runState, turn int, attempt Attempt) {
	rec := &ProviderAttempt{
		RecordMeta: NewRecordMeta("provider.attempt"),
		RunID:      run.id,
		Turn:       turn,
		Request:    run.requests,
		Attempt:    attempt.Number,
		Error:      attempt.Err.Error(),
		DelayMS:    int(attempt.Delay.Milliseconds()),
	}
	var perr *ProviderError
	if errors.As(attempt.Err, &perr) {
		rec.Kind = string(perr.Kind)
	}
	if err := s.writeRun(run, rec); err != nil {
		s.logger().Warn("the attempt record could not be written", "error", err)
	}
	if !attempt.Retried {
		// No other attempt followed: the failure ended the request, and
		// the run's settle record says so.
		return
	}
	s.events.emit(EventProviderRetry, map[string]any{
		"run_id":   run.id,
		"turn":     turn,
		"request":  run.requests,
		"attempt":  attempt.Number,
		"error":    rec.Error,
		"kind":     rec.Kind,
		"delay_ms": rec.DelayMS,
	})
}

// reportResponseErrors makes the failures the transport reported about
// the response visible.
//
// A recoverable failure is a note for the model: the note goes into the
// view, so the model reads it in its next turn, and the record holds
// it. A failure that is not recoverable ends the run.
//
// The note comes after the turn's tool results, so the request that
// follows it keeps the order the wire wants: a tool result follows the
// assistant's message that asked for it.
func (s *Session) reportResponseErrors(run *runState, turn int, errs []StreamError) (bool, error) {
	notes := make([]string, 0, len(errs))
	for _, e := range errs {
		if !e.Recoverable {
			return false, &ProviderError{Kind: ProviderKindInvalidRequest, Message: e.Message}
		}
		notes = append(notes, e.Message)
	}
	if len(notes) == 0 {
		return false, nil
	}
	msg := NewTextMessage(RoleUser, strings.Join(notes, "\n"))
	msg.Origin = OriginUser
	s.appendMessages(msg)
	if err := s.write(run.ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", msg.Origin),
		RunID:      run.id,
		Turn:       turn,
		Role:       msg.Role,
		Content:    msg.Content,
	}); err != nil {
		return false, err
	}
	return s.noteRecoverable(run), nil
}

// maxRecoverableNotes bounds the consecutive turns a recoverable
// response failure may open.
//
// A model that keeps reporting the same failure would otherwise keep the
// run asking forever: MaxTurns is unlimited by default, and the
// doom-loop counter only sees tool calls. After this many consecutive
// notes, the note is still recorded, and the run settles naturally.
const maxRecoverableNotes = 3

// noteRecoverable counts a recoverable note and reports whether it opens
// another turn.
func (s *Session) noteRecoverable(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run.recoverableNotes++
	return run.recoverableNotes < maxRecoverableNotes
}

// resetRecoverableNotes ends a run of recoverable notes.
func (s *Session) resetRecoverableNotes(run *runState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run.recoverableNotes = 0
}

// toolSpecs returns the tools the model may call, in offer order.
func (s *Session) toolSpecs() []ToolSpec {
	specs := make([]ToolSpec, 0, len(s.opts.ToolOrder))
	for _, name := range s.opts.ToolOrder {
		if _, ok := s.opts.Tools[name]; ok {
			specs = append(specs, s.toolSpecFor(name))
		}
	}
	return specs
}

// writeUsage writes a request's token accounting.
//
// A refused write follows the failure table: it is the store_error
// candidate, and the session is broken. The call reports whether the
// record was written.
func (s *Session) writeUsage(run *runState, turn int, usage *UsageReport, requestChars int) bool {
	s.mu.Lock()
	run.tokens += usage.InputTokens + usage.OutputTokens
	s.mu.Unlock()
	s.observeUsage(usage, requestChars)
	if err := s.write(run.ctx, &Usage{
		RecordMeta:      NewRecordMeta("usage"),
		RunID:           run.id,
		Turn:            turn,
		Request:         run.requests,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		CacheReadTokens: usage.CacheReadTokens,
		TTFTMS:          usage.TTFTMS,
	}); err != nil {
		s.noteStoreError(run, err)
		return false
	}
	s.events.emit(EventUsage, map[string]any{
		"run_id":            run.id,
		"turn":              turn,
		"request":           run.requests,
		"input_tokens":      usage.InputTokens,
		"output_tokens":     usage.OutputTokens,
		"cache_read_tokens": usage.CacheReadTokens,
	})
	return true
}

// addEstimatedUsage counts a turn the endpoint reported no usage for.
//
// The estimate is the serialized request plus the response's text, tool
// calls and reasoning, divided by the session's characters-per-token
// ratio. A turn always counts at least one token, so a budget of one is
// spent by the first request.
func (s *Session) addEstimatedUsage(run *runState, resp *response, assistant Message) {
	s.addEstimatedChars(run, resp.requestChars+estimateMessageChars(assistant))
}

// accountInterruptedRequest charges a request that an interrupt aborted,
// exactly once: the reported usage when there is one, and the estimate
// otherwise.
//
// It reports whether the charge was recorded. A refused usage write is
// the store_error candidate, and the finalization settles it.
func (s *Session) accountInterruptedRequest(run *runState, turn int, resp *response) bool {
	if resp == nil {
		return true
	}
	if resp.usage != nil {
		return s.writeUsage(run, turn, resp.usage, resp.requestChars)
	}
	// The estimate is the one a completed turn would have received:
	// the request plus the assistant message the response would have
	// produced, calls included, through the same accounting helper.
	assistant := Message{Role: RoleAssistant, Content: resp.content}
	for _, call := range resp.calls {
		assistant.Content = append(assistant.Content, ContentBlock{
			Kind:       ContentToolCall,
			ToolCallID: call.ID,
			Name:       call.Name,
			Arguments:  recordArgs(call.Arguments),
		})
	}
	s.addEstimatedChars(run, resp.requestChars+estimateMessageChars(assistant))
	return true
}

// addEstimatedChars counts a request's estimated tokens.
func (s *Session) addEstimatedChars(run *runState, chars int) {
	s.mu.Lock()
	tokens := int(math.Ceil(float64(chars) / s.est.charsPerToken))
	if tokens < 1 {
		tokens = 1
	}
	run.tokens += tokens
	s.mu.Unlock()
}

// errRunWindingDown reports that a run settled while a step of the loop
// was still running.
var errRunWindingDown = errors.New("poo: the run is winding down")

// runWindingDown reports that the run has started or finished
// finalizing, so nothing else may mutate it.
func (s *Session) runWindingDown(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return run.committed || run.finalizing
}

// cancelledByFinalize reports that the run's context ended because the
// run is being finalized, which is never a cancelled candidate.
func cancelledByFinalize(run *runState) bool {
	return context.Cause(run.ctx) == errFinalizing
}

// externalCancellation reports that the run's own context ended for a
// reason other than the finalization's own cancellation.
//
// A client interrupt ends the activity, not the run, and the
// finalization's cause is never a candidate. Any other ending is the
// consumer's parent context, which is external cancellation.
func (s *Session) externalCancellation(run *runState) (SettleDetail, bool) {
	s.mu.Lock()
	err := run.ctx.Err()
	s.mu.Unlock()
	if err == nil {
		return SettleDetail{}, false
	}
	cause := context.Cause(run.ctx)
	if cause == errFinalizing {
		return SettleDetail{}, false
	}
	if cause == nil {
		cause = err
	}
	return cancelDetail(cause), true
}

// cancelDetail explains a cancelled settle. A deadline has its own
// source, so a consumer can tell it from a plain cancel without reading
// the text.
func cancelDetail(err error) SettleDetail {
	source := SourceCancelContext
	if errors.Is(err, context.DeadlineExceeded) {
		source = SourceCancelDeadline
	}
	return SettleDetail{Source: source, Text: err.Error()}
}

// settleForError maps a request error to a settle reason.
func (s *Session) settleForError(err error) (Reason, SettleDetail, error) {
	// A cancellation and a deadline are the same outcome: the run was
	// stopped before the model finished.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ReasonCancelled, cancelDetail(err), nil
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		kind := string(pe.Kind)
		// The wait the endpoint asked for travels with the failure:
		// the plan's provider_limit row settles on a Retry-After over
		// thirty seconds, so the record has to carry the number it
		// settles on. It is rounded up, so a wait just over thirty
		// seconds is not recorded as thirty, which the rule reads as
		// retryable.
		retryAfter := int((pe.RetryAfter + time.Second - 1) / time.Second)
		provider := &ProviderDetail{Kind: kind, Status: pe.Status, Code: pe.Code, RetryAfterSec: retryAfter}
		// The time the endpoint says its limit resets travels with the
		// failure too, so a consumer can tell when another attempt
		// could succeed.
		if !pe.ResetsAt.IsZero() {
			provider.ResetsAt = pe.ResetsAt.UTC().Format(time.RFC3339)
		}
		switch pe.Kind {
		case ProviderKindContextLength:
			return ReasonContextExhausted, SettleDetail{
				Source:   "provider:context_length",
				Text:     pe.Message,
				Provider: provider,
			}, nil
		case ProviderKindUsageLimit, ProviderKindPlanInactive:
			return ReasonProviderLimit, SettleDetail{
				Source:   "provider:" + kind,
				Text:     pe.Message,
				Provider: provider,
			}, nil
		case ProviderKindRateLimited:
			// A rate limit with a wait over thirty seconds is a
			// provider limit: the run settles, and a later run may
			// succeed.
			if pe.RetryAfter > MaxRetryAfter {
				return ReasonProviderLimit, SettleDetail{
					Source:   "provider:" + kind,
					Text:     pe.Message,
					Provider: provider,
				}, nil
			}
			return ReasonProviderError, SettleDetail{
				Source:   "provider:" + kind,
				Text:     pe.Message,
				Provider: provider,
			}, nil
		default:
			return ReasonProviderError, SettleDetail{
				Source:   "provider:" + kind,
				Text:     pe.Message,
				Provider: provider,
			}, nil
		}
	}
	return "", SettleDetail{}, err
}

// response is what one provider request produced.
type response struct {
	// content is the assistant message's blocks.
	content []ContentBlock
	// calls are the tool calls the model asked for.
	calls []ToolCall
	// finish is why the model stopped.
	finish FinishReason
	// usage is the token accounting the endpoint reported, if any.
	usage *UsageReport
	// messageID is the id the assistant message carries.
	//
	// The id is assigned at the stream's first delta, before the
	// message is recorded, so a delivery note may name a message that
	// is still streaming.
	messageID string
	// requestChars is the serialized size of the view the request sent,
	// which is what the endpoint's input token count measures.
	requestChars int
	// errors are the failures the transport reported about the
	// response.
	errors []StreamError
}

// collect fills the response with what the stream carried.
//
// A reasoning stream becomes its own content block, so the model's
// thinking survives without breaking the text. The calls are ordered by
// the provider's index, so the model sees them in the order it asked for
// them.
func (r *response) collect(text, thinking string, calls map[int]*ToolCall, order []int) {
	if thinking != "" {
		r.content = append(r.content, ContentBlock{Kind: ContentThinking, Text: thinking})
	}
	if text != "" {
		r.content = append(r.content, ContentBlock{Kind: ContentText, Text: text})
	}
	sort.Ints(order)
	for _, idx := range order {
		c := calls[idx]
		if c.Name == "" {
			continue
		}
		r.calls = append(r.calls, *c)
	}
}

// splitKeyBase is where the keys of calls that share a provider's index
// start, above the text parser's own.
const splitKeyBase = 1 << 20

// callSlot names a call by the index and the id a provider sent it with.
type callSlot struct {
	index int
	id    string
}

// readResponse drains a stream into a response.
//
// The usage is collected rather than written, so the caller can write it
// after the assistant's message: the log then reads in the order the
// events happened.
//
// A stream that fails part way through returns the response it managed to
// read along with the failure, so the caller can still write the usage.
func readResponse(ctx context.Context, reader StreamReader, ids *MessageIDs, runID string, turn, request int, emit func(EventName, map[string]any)) (*response, error) {
	resp := &response{finish: FinishStop}
	var text strings.Builder
	var thinking strings.Builder
	calls := make(map[int]*ToolCall)
	var order []int
	// current maps a provider index to the call its latest fragment
	// joined.
	current := make(map[int]int)
	// byID maps a provider index and a call id to the call they name.
	byID := make(map[callSlot]int)
	// splitKey numbers a call that arrived at an index in use. It
	// starts above the text parser's calls, which are numbered from
	// 1000, and skips any key a provider's own index has taken.
	splitKey := splitKeyBase

	for {
		ev, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			resp.collect(text.String(), thinking.String(), calls, order)
			return resp, err
		}
		switch ev.Kind {
		case "delta":
			if ev.Delta == nil {
				continue
			}
			// The message id is assigned at the first delta, before
			// the message is recorded, so a client can name a reply
			// that is still streaming.
			if resp.messageID == "" && ids != nil {
				resp.messageID = ids.Next()
			}
			kind := ev.Delta.Kind
			if kind != "thinking" {
				kind = "text"
			}
			if emit != nil && resp.messageID != "" {
				attempt := ev.Attempt
				if attempt == 0 {
					attempt = 1
				}
				emit(EventMessageDelta, map[string]any{
					"run_id":     runID,
					"message_id": resp.messageID,
					"attempt":    attempt,
					"kind":       kind,
					"text":       ev.Delta.Text,
				})
			}
			switch kind {
			case "thinking":
				thinking.WriteString(ev.Delta.Text)
			default:
				text.WriteString(ev.Delta.Text)
			}
		case "tool_call":
			if ev.ToolCall == nil {
				continue
			}
			// A provider may split one call across several events,
			// and may send several calls in one event. The index
			// groups the fragments of one call, but some providers
			// send every parallel call at index 0, each with its own
			// id. So a fragment with an id already seen at its index
			// continues that call, a fragment with a new id at an index
			// in use starts a new call, and a fragment with no id
			// continues the latest call at its index.
			idx, id := ev.ToolCall.Index, ev.ToolCall.ID
			key, ok := current[idx]
			if id != "" {
				if k, seen := byID[callSlot{idx, id}]; seen {
					key, ok = k, true
				} else if ok && calls[key].ID != "" {
					ok = false
				}
			}
			if !ok {
				key = idx
				for {
					if _, taken := calls[key]; !taken {
						break
					}
					key = splitKey
					splitKey++
				}
				calls[key] = &ToolCall{}
				order = append(order, key)
			}
			existing := calls[key]
			current[idx] = key
			if id != "" {
				byID[callSlot{idx, id}] = key
			}
			if ev.ToolCall.ID != "" {
				existing.ID = ev.ToolCall.ID
			}
			if ev.ToolCall.Name != "" {
				existing.Name = ev.ToolCall.Name
			}
			if ev.ToolCall.Origin != "" {
				existing.Origin = ev.ToolCall.Origin
			}
			if len(ev.ToolCall.Arguments) > 0 {
				existing.Arguments = append(existing.Arguments, ev.ToolCall.Arguments...)
			}
		case "usage":
			if ev.Usage != nil {
				usage := *ev.Usage
				resp.usage = &usage
			}
		case "error":
			// A failure the transport reported about the response: a
			// tool call the model wrote as text and that could not be
			// parsed. The text is not lost, and the run carries on.
			if ev.Error == nil {
				continue
			}
			resp.errors = append(resp.errors, *ev.Error)
		case "finish":
			if ev.FinishReason != "" {
				resp.finish = ev.FinishReason
			}
		}
	}

	resp.collect(text.String(), thinking.String(), calls, order)
	return resp, nil
}
