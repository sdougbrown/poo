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
	s.mu.Lock()
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
	s.turns = nil
	s.results = map[string]ToolResult{}
	s.repeats = newRepeatCounter()
	s.transitionLocked(StateRequesting)
	// The results that were held are delivered at the start of the run,
	// before the prompt text. They are the session's own work coming
	// back, and the client asked for them.
	held := s.takeHeldResultsLocked()
	s.mu.Unlock()

	// The run's own record of its beginning.
	if err := s.write(ctx, &RunStarted{
		RecordMeta: NewRecordMeta("run.started"),
		RunID:      runID,
		PromptHash: hashText(text),
		StartedAt:  nowRFC3339(),
		ModelID:    s.opts.ModelID,
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
			Source: "before_run",
			Text:   err.Error(),
		}))
		return runID, nil
	}

	// The system prompt is rendered once per run, after the before-run
	// hooks, and it is frozen for the length of the run.
	if promptText := s.renderPrompt(decisions.edits); promptText != "" {
		msg := NewTextMessage(RoleSystem, promptText)
		msg.Origin = OriginSystem
		s.appendMessages(msg)
		if err := s.writeRun(run, &MessageRecord{
			RecordMeta: NewRecordMetaFrom("message", msg.Origin),
			RunID:      runID,
			Role:       msg.Role,
			Content:    msg.Content,
		}); err != nil {
			s.finalize(run, newCandidate(ReasonStoreError, SettleDetail{
				Source: "store",
				Text:   err.Error(),
			}))
			return runID, nil
		}
	}

	// The messages the before-run hooks appended follow the system
	// prompt.
	s.appendStartMessages(run, decisions)

	user := NewTextMessage(RoleUser, withDeliveryNote(text, note))
	user.Origin = OriginUser
	user.DeliveryNote = note
	s.appendMessages(user)
	if err := s.write(ctx, &MessageRecord{
		RecordMeta:   NewRecordMetaFrom("message", user.Origin),
		RunID:        runID,
		Turn:         0,
		Role:         user.Role,
		Content:      user.Content,
		DeliveryNote: note,
	}); err != nil {
		// The store refused a record. The run settles store_error, and
		// the session is broken.
		s.finalize(run, newCandidate(ReasonStoreError, SettleDetail{
			Source: "store",
			Text:   err.Error(),
		}))
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
	// running, and it settles the run through the normal protocol.
	stopTimer := s.startDurationTimer(run)
	defer stopTimer()

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
				Text:   fmt.Sprintf("the run took its %s ceiling", s.opts.MaxDuration),
			}))
		}
	}()
	return func() { close(stop) }
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
		if s.waitWhileWaiting(run) {
			continue
		}

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

		s.events.emit(EventTurnStarted, map[string]any{"run_id": run.id, "turn": turn})

		resp, rendered, err := s.request(run, turn)
		// The run may have settled while the request was in flight.
		// What the request produced then is a late completion: it
		// never mutates the history, the records or the events.
		if s.runWindingDown(run) {
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
		assistant := Message{Role: RoleAssistant, Content: resp.content, Origin: OriginModel}
		for _, call := range resp.calls {
			assistant.Content = append(assistant.Content, ContentBlock{
				Kind:       ContentToolCall,
				ToolCallID: call.ID,
				Name:       call.Name,
				Arguments:  recordArgs(call.Arguments),
			})
		}
		if len(assistant.Content) > 0 {
			// The id is taken only for a message that is recorded.
			if resp.messageID != "" {
				assistant.ID = resp.messageID
			} else {
				assistant.ID = s.messageIDs.Next()
			}
			s.appendMessages(assistant)
			if err := s.write(run.ctx, &MessageRecord{
				RecordMeta: NewRecordMetaFrom("message", assistant.Origin),
				RunID:      run.id,
				Turn:       turn,
				Role:       assistant.Role,
				Content:    assistant.Content,
				MessageID:  assistant.ID,
			}); err != nil {
				return []candidate{newCandidate(ReasonStoreError, SettleDetail{
					Source: "store",
					Text:   err.Error(),
				})}
			}
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
			// An interrupt during the batch cut the turn short. The
			// unstarted calls are skipped, and the run continues.
			if s.interruptPending(run) {
				s.interruptedActivityTurn(run, turn, resp, true)
				continue
			}
		}

		// The turn ended. The turn-end algorithm decides what happens
		// next: it may settle the run, or it may go to the gate.
		outcome := s.turnEnd(run, turn, processed, truncated)
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
func (s *Session) turnEnd(run *runState, turn int, processed, truncated bool) turnOutcome {
	// Step 1: the turn is over.
	s.events.emit(EventTurnEnded, map[string]any{"run_id": run.id, "turn": turn})

	// Step 2: drain the steer queue, then the job queue.
	//
	// A follow-up is not drained here: it waits for a natural stop, and
	// finalize step 1 delivers it.
	appended := s.drainInputs(run, false)

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
			Source: "before_settle",
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
		Calls:     s.callStates(turn),
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
			Text:   fmt.Sprintf("the run reached its %d-turn ceiling", s.opts.MaxTurns),
		}))
	}
	s.mu.Lock()
	tokens := run.tokens
	elapsed := time.Since(run.startedAt)
	s.mu.Unlock()
	if s.opts.MaxRunTokens > 0 && tokens >= s.opts.MaxRunTokens {
		cands = append(cands, newCandidate(ReasonBudgetExhausted, SettleDetail{
			Source: "max_run_tokens",
			Text:   fmt.Sprintf("the run used %d tokens, and the ceiling is %d", tokens, s.opts.MaxRunTokens),
		}))
	}
	if s.opts.MaxDuration > 0 && elapsed >= s.opts.MaxDuration {
		cands = append(cands, newCandidate(ReasonBudgetExhausted, SettleDetail{
			Source: "max_duration",
			Text:   fmt.Sprintf("the run took %s, and the ceiling is %s", elapsed.Round(time.Millisecond), s.opts.MaxDuration),
		}))
	}
	return cands
}

// callStates returns the calls of a turn and their outcomes.
func (s *Session) callStates(turn int) []CallState {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []CallState
	for _, t := range s.turns {
		if t.Index != turn {
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

	// The records come before the history append: a delivery that the
	// finalization abandons must not mutate the view. A record the
	// store refuses is a store_error candidate, and the drain stops
	// there.
	if err := s.writeDelivery(run.ctx, run, &MessageRecord{
		RecordMeta:   NewRecordMetaFrom("message", msg.Origin),
		RunID:        run.id,
		Role:         msg.Role,
		Content:      msg.Content,
		DeliveryNote: note,
	}); err != nil {
		// The message never reached the model, so the input is
		// dropped and reported, even though the store is what failed.
		s.abandonDelivery(run, input)
		if !errors.Is(err, errDeliveryAbandoned) {
			s.addCandidate(run, storeErrorCandidate(err))
		}
		return false
	}
	if err := s.writeDelivery(run.ctx, run, &InputDelivered{
		RecordMeta:  NewRecordMeta("input.delivered"),
		SessionID:   s.opts.SessionID,
		RunID:       run.id,
		InputID:     input.id,
		Mode:        input.mode,
		DeliveredAt: nowRFC3339(),
	}); err != nil {
		s.abandonDelivery(run, input)
		if !errors.Is(err, errDeliveryAbandoned) {
			s.addCandidate(run, storeErrorCandidate(err))
		}
		return false
	}
	s.appendMessages(msg)
	s.finishDelivery(run, input)
	s.events.emit(EventInputDelivered, map[string]any{
		"run_id":   run.id,
		"input_id": input.id,
		"mode":     string(input.mode),
	})
	return true
}

// errDeliveryAbandoned reports that a delivery write was abandoned
// because the run settled before the write finished.
var errDeliveryAbandoned = errors.New("poo: the delivery was abandoned")

// writeDelivery writes a record for an input's delivery.
//
// Unlike write, a cancellation from the run's finalization abandons the
// write instead of breaking the session: the delivery is in-flight work
// that finalization step 3 may abandon.
func (s *Session) writeDelivery(ctx context.Context, run *runState, rec Record) error {
	if s.opts.Sink == nil {
		return nil
	}
	s.mu.Lock()
	if s.sunk {
		s.mu.Unlock()
		return errors.New("poo: the session store is broken")
	}
	abandoned := run.committed || run.finalizing || ctx.Err() != nil
	s.mu.Unlock()
	if abandoned {
		return errDeliveryAbandoned
	}
	wctx, cancel := context.WithTimeout(ctx, recordWriteBudget)
	defer cancel()
	if err := s.opts.Sink.Append(wctx, rec); err != nil {
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
	s.mu.Lock()
	messages := CloneMessages(s.history)
	tools := s.toolSpecs()
	requestChars := s.historyCharsLocked()
	s.mu.Unlock()

	// The request runs under the run's activity context, so an
	// interrupt aborts it without ending the run.
	actCtx, _ := s.startActivity(run)
	defer s.endActivity(run)

	reader, rendered, err := s.opts.Transport.Stream(actCtx, ChatRequest{
		Model:     s.opts.ModelID,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: s.maxTokens(),
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
	resp, err := readResponse(actCtx, reader, ids, run.id, turn, run.requests, s.opts.Sink, func(name EventName, payload map[string]any) {
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
		s.addCandidate(run, storeErrorCandidate(err))
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
	chars := resp.requestChars
	for _, block := range resp.content {
		chars += len(block.Text) + len(block.Name) + len(block.ToolCallID) + len(block.Arguments)
	}
	for _, call := range resp.calls {
		chars += len(call.ID) + len(call.Name) + len(call.Arguments)
	}
	s.addEstimatedChars(run, chars)
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
func readResponse(ctx context.Context, reader StreamReader, ids *MessageIDs, runID string, turn, request int, sink RecordSink, emit func(EventName, map[string]any)) (*response, error) {
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
				emit(EventMessageDelta, map[string]any{
					"run_id":     runID,
					"message_id": resp.messageID,
					"attempt":    request,
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
			if len(ev.ToolCall.Arguments) > 0 {
				existing.Arguments = append(existing.Arguments, ev.ToolCall.Arguments...)
			}
		case "usage":
			if ev.Usage != nil {
				usage := *ev.Usage
				resp.usage = &usage
			}
		case "finish":
			if ev.FinishReason != "" {
				resp.finish = ev.FinishReason
			}
		}
	}

	resp.collect(text.String(), thinking.String(), calls, order)
	return resp, nil
}
