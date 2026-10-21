package poo

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// finalize settles a run.
//
// The first call starts the finalization. Later calls only add
// candidates, until the commit point at step 6. The outcome therefore
// never depends on which goroutine arrived first.
func (s *Session) finalize(run *runState, cands ...candidate) Result {
	res, _ := s.finalizeCandidates(run, false, cands)
	return res
}

// finalizeCandidates adds the candidates and, when it is the first call,
// runs the finalization.
//
// Only the goroutine that drives the run passes resumable: it is the one
// that can carry on after an abandoned finalization, and the second
// result reports that the finalization was abandoned. Any other caller
// has a non-natural candidate, which is never abandoned.
func (s *Session) finalizeCandidates(run *runState, resumable bool, cands []candidate) (Result, bool) {
	s.mu.Lock()
	if run.committed {
		// A candidate arriving after the commit point is logged and
		// ignored: it never mutates a settled run.
		result := run.result
		s.mu.Unlock()
		return result, false
	}
	if run.finalizing {
		// The finalization is under way. The candidates are added, and
		// the finalization picks them up at the commit point.
		run.candidates = append(run.candidates, cands...)
		s.mu.Unlock()
		return Result{}, false
	}
	run.finalizing = true
	run.candidates = append(run.candidates, cands...)
	s.mu.Unlock()

	return s.finalizeRun(run, resumable)
}

// finalizeRun runs the finalization protocol.
//
// The steps are the plan's: the input check, the cancellation of the
// run's context, the wait for in-flight work, the synthetic results,
// the commit point, the outcome records, and the settled event.
func (s *Session) finalizeRun(run *runState, resumable bool) (Result, bool) {
	// Step 1: the input check. A run that would settle naturally can
	// still take a queued input; a run that is settling because of a
	// failure or a limit cannot.
	if resumable && s.abandonFinalize(run) {
		return Result{}, true
	}

	// The queued client messages that will never be delivered are
	// dropped and reported. The job queue belongs to the jobs stage: a
	// queued job result stays on the session's queue.
	undelivered := s.collectUndelivered(run)

	s.transition(StateSettling)
	s.events.emit(EventRunSettling, map[string]any{"run_id": run.id})

	// Step 2: cancel the run's context. The finalization's own
	// cancellation is not a candidate: it never manufactures a
	// cancelled run.
	run.cancel(errFinalizing)

	// Step 3: wait for the in-flight work, bounded at five seconds.
	// The work that finishes before the commit point keeps its real
	// result; the work that is still running is abandoned.
	s.waitForWork(run)

	// Steps 4 and 5: every call that was issued but has no result
	// gets a synthetic one, and every pending ask resolves as
	// cancelled.
	s.settleIssuedCalls(run)

	// Step 6: the commit point. The highest-precedence candidate
	// wins, and the outcome is fixed from here.
	s.mu.Lock()
	reason, detail := s.commitLocked(run)
	run.committed = true
	run.result = Result{Reason: reason, Detail: detail, Turns: run.turn}
	s.mu.Unlock()

	// The dropped inputs are reported with the reason the run settled
	// for.
	if len(undelivered) > 0 {
		detail.UndeliveredInput = undelivered
	}

	// Step 7: the outcome records are written with a context of their
	// own, because the run's context has ended.
	settle := &RunSettled{RecordMeta: NewRecordMeta("run.settled"),
		RunID:     run.id,
		Reason:    reason,
		Detail:    detail,
		Turns:     run.turn,
		SettledAt: nowRFC3339(),
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), recordWriteBudget)
	defer cleanupCancel()
	storeErr := s.write(cleanupCtx, settle)
	if s.opts.Sink != nil {
		if err := s.opts.Sink.Flush(cleanupCtx); err != nil && storeErr == nil {
			storeErr = err
		}
	}
	if storeErr != nil {
		// The store refused the settle record or the flush after it, so
		// the run's outcome may not exist. The session is broken, and
		// the reason changes to store_error, unless the run was
		// cancelled: a cancelled run stays cancelled, and the store
		// error becomes a losing candidate. A store_error that is
		// already the winner is not also its own loser.
		s.markBroken()
		if reason == ReasonCancelled {
			detail.Losing = append(detail.Losing, string(ReasonStoreError))
		} else {
			reason = ReasonStoreError
			detail.Source = "store"
			detail.Text = storeErr.Error()
		}
	}

	// Step 8: emit the settled event, wake every waiter and clear the
	// busy flag.
	s.events.emit(EventRunSettled, map[string]any{
		"run_id": run.id,
		"reason": string(reason),
		"detail": detail,
	})
	// The dropped inputs are reported after the settle event, so the
	// client learns the run's outcome first.
	s.dropUndelivered(run, undelivered, reason)
	s.wakeWaiters(run, Result{Reason: reason, Detail: detail, Turns: run.turn})
	return Result{Reason: reason, Detail: detail, Turns: run.turn}, false
}

// abandonFinalize reports whether the finalization is abandoned because
// a queued input can still be delivered.
//
// This is the only place a finalization can be abandoned. The decision
// and the reopening of the run are one step under the lock, so a
// candidate that joins afterwards starts a finalization of its own.
func (s *Session) abandonFinalize(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Only a natural candidate may be abandoned: a run that is
	// settling because of a failure or a limit has to settle.
	for _, c := range run.candidates {
		if !IsNatural(c.reason) {
			return false
		}
	}
	if len(s.steerQueue) == 0 && len(s.followUpQueue) == 0 {
		return false
	}
	// The natural candidates are discarded, and the run goes back to
	// the next-request gate. The AfterTurn hooks and the stop conditions
	// are not re-run, and this does not count against MaxContinuations.
	run.finalizing = false
	run.candidates = nil
	return true
}

// resume takes the run back after an abandoned finalization and returns
// what the loop produces from the gate on.
//
// The run's context was not cancelled, so the loop carries on from where
// it was. It runs in the goroutine that drives the run, under the run's
// duration timer and panic recovery. It reports false when another
// finalization has taken the run over, in which case that one settles it.
func (s *Session) resume(run *runState) ([]candidate, bool) {
	s.mu.Lock()
	if s.run != run || run.finalizing || run.committed {
		s.mu.Unlock()
		return nil, false
	}
	for _, c := range run.candidates {
		if !IsNatural(c.reason) {
			s.mu.Unlock()
			return nil, false
		}
	}
	s.transitionLocked(StateRequesting)
	s.mu.Unlock()

	s.drainInputs(run, true)
	return s.loop(run), true
}

// collectUndelivered takes the queued client messages that will never
// be delivered.
//
// The inputs are dropped and reported with the reason the run settled
// for. The job queue belongs to the jobs stage: a queued job result
// stays on the session's queue and is never dropped here.
func (s *Session) collectUndelivered(run *runState) []UndeliveredInput {
	s.mu.Lock()
	pending := append(append([]queuedInput(nil), s.steerQueue...), s.followUpQueue...)
	s.steerQueue = nil
	s.followUpQueue = nil
	s.mu.Unlock()

	if len(pending) == 0 {
		return nil
	}
	var out []UndeliveredInput
	for _, input := range pending {
		out = append(out, UndeliveredInput{InputID: input.id, Mode: string(input.mode)})
	}
	return out
}

// dropUndelivered records the inputs that never reached the model.
//
// The drop events are emitted even if the store write fails, so the
// client learns that its message was never delivered.
func (s *Session) dropUndelivered(run *runState, inputs []UndeliveredInput, reason Reason) {
	for _, input := range inputs {
		mode := InputMode(input.Mode)
		_ = s.write(run.ctx, &InputDropped{
			RecordMeta: NewRecordMeta("input.dropped"),
			SessionID:  s.opts.SessionID,
			RunID:      run.id,
			InputID:    input.InputID,
			Mode:       mode,
			Reason:     reason,
			DroppedAt:  nowRFC3339(),
		})
		s.events.emit(EventInputDropped, map[string]any{
			"run_id":   run.id,
			"input_id": input.InputID,
			"mode":     input.Mode,
			"reason":   string(reason),
		})
	}
}

// commitLocked picks the winning candidate and records the losers.
//
// The caller holds the session's lock.
func (s *Session) commitLocked(run *runState) (Reason, SettleDetail) {
	ordered := make([]candidate, len(run.candidates))
	copy(ordered, run.candidates)
	if len(ordered) == 0 {
		// A run that commits holds a candidate. If it holds none, the
		// run ended naturally.
		ordered = append(ordered, newCandidate(ReasonEndTurn, SettleDetail{Source: "end_turn"}))
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return Precedence(ordered[i].reason) < Precedence(ordered[j].reason)
	})
	winner := ordered[0]
	detail := winner.detail
	if detail.Source == "" {
		detail.Source = string(winner.reason)
	}
	// The losing list holds the other candidates: the winner's own
	// reason is not a loser, and the same (reason, source) pair is
	// listed once even when two writes raised it.
	var losing []string
	seen := make(map[string]bool, len(ordered)-1)
	for _, c := range ordered[1:] {
		if c.reason == winner.reason {
			continue
		}
		key := string(c.reason) + "\x00" + c.detail.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		losing = append(losing, string(c.reason))
	}
	if len(losing) > 0 {
		detail.Losing = append(detail.Losing, losing...)
	}
	return winner.reason, detail
}

// waitForWork waits for the in-flight work, bounded at five seconds.
func (s *Session) waitForWork(run *runState) {
	deadline := time.After(recordWriteBudget)
	for {
		s.mu.Lock()
		pending := len(run.pendingCalls) + run.resultWrites
		done := run.workDone
		s.mu.Unlock()
		if pending == 0 {
			return
		}
		select {
		case <-done:
		case <-deadline:
			return
		}
	}
}

// settleIssuedCalls gives every call that was issued but has no result
// a synthetic one.
//
// A call that was cancelled or abandoned is `cancelled`; a call that
// never started is `skipped`. Either way the history stays valid for
// the next run.
func (s *Session) settleIssuedCalls(run *runState) {
	s.mu.Lock()
	issued := make([]*issuedCall, 0, len(run.pendingCalls))
	for _, c := range run.pendingCalls {
		issued = append(issued, c)
	}
	run.pendingCalls = map[string]*issuedCall{}
	if run.settledCalls == nil {
		run.settledCalls = map[string]bool{}
	}
	// The calls are marked as settled before their records are
	// written, so a call that finished at the same moment does not get
	// a second result.
	for _, c := range issued {
		run.settledCalls[c.handle] = true
	}
	s.mu.Unlock()

	sort.Slice(issued, func(i, j int) bool { return issued[i].handle < issued[j].handle })
	for _, call := range issued {
		outcome := OutcomeSkipped
		text := SkippedSettlingText
		if call.started {
			outcome = OutcomeCancelled
			text = "Cancelled: the run settled before the call finished."
		}
		// The call is already marked as settled, so the record is
		// written directly.
		s.writeResult(run, call.turn, call.handle, call.tool, call.id, ToolResult{
			Text:    text,
			IsError: true,
		}, outcome)
	}
}

// wakeWaiters releases every waiter of a run and clears the busy flag.
func (s *Session) wakeWaiters(run *runState, result Result) {
	s.mu.Lock()
	if s.run == run {
		s.run = nil
		s.transitionLocked(StateSettled)
	}
	s.lastResult = result
	s.lastRunID = run.id
	waiters := s.waiters
	s.waiters = nil
	s.mu.Unlock()
	for _, w := range waiters {
		w <- result
	}
}

// Cancel ends the active run.
//
// The run settles `cancelled` unless a higher-precedence candidate has
// already been committed. Cancelling a session with no run is a no-op.
func (s *Session) Cancel(reason string) {
	if reason == "" {
		reason = "cancel:client"
	}
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	if run == nil {
		return
	}
	s.finalize(run, newCandidate(ReasonCancelled, SettleDetail{
		Source: reason,
		Text:   "the run was cancelled",
	}))
}

// Wait waits for the active run to settle.
//
// It returns the run's result. A session with no run returns the zero
// result at once.
//
// The context bounds the wait, and it does not cancel the run. When it
// ends, the wait gives the run a short grace period to settle, which
// covers a run whose own context is the same one, and then returns the
// context's error while the run goes on. Cancel ends the run.
func (s *Session) Wait(ctx context.Context) (Result, error) {
	ch := make(chan Result, 1)
	s.mu.Lock()
	if s.run == nil {
		// The run has already settled, and its outcome is the answer.
		result := s.lastResult
		s.mu.Unlock()
		return result, nil
	}
	s.waiters = append(s.waiters, ch)
	s.mu.Unlock()
	select {
	case res := <-ch:
		return res, nil
	case <-ctx.Done():
	}
	// The context ended. A run started under the same context settles on
	// its own, so a bounded grace period covers a run that is finishing
	// its work.
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(recordWriteBudget):
		return Result{}, ctx.Err()
	}
}

// Send delivers a message to the active run.
//
// The mode says when the message reaches the model: a steer at the next
// turn end, an interrupt with the current activity, a follow-up only
// when the run would settle naturally.
//
// The call returns the input's identifier. The input's fate is exactly
// one of delivered or dropped, and both are recorded.
func (s *Session) Send(text string, mode InputMode, opts ...InputOption) (string, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", ErrSessionClosed
	}
	if s.broken {
		s.mu.Unlock()
		return "", ErrSessionBroken
	}
	if s.run == nil {
		s.mu.Unlock()
		return "", ErrNotRunning
	}
	run := s.run
	// A run that is settling cannot take a message: the outcome is
	// already fixed, and a message that arrived now would be dropped.
	if s.state == StateSettling || run.finalizing {
		s.mu.Unlock()
		return "", ErrNotRunning
	}
	note, err := s.prepareDeliveryNoteLocked(opts)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	id := NewULID()
	input := queuedInput{id: id, mode: mode, text: text, note: note}

	switch mode {
	case FollowUp:
		// A follow-up waits for a real settle: it is delivered only
		// when the run would settle naturally.
		s.followUpQueue = append(s.followUpQueue, input)
	default:
		// A steer waits for the turn end, and an interrupt waits for
		// the abort. Both keep their arrival order.
		s.steerQueue = append(s.steerQueue, input)
	}
	s.mu.Unlock()

	// The input is recorded before the call returns, so an accepted
	// input is never lost.
	if err := s.write(context.Background(), &InputQueued{
		RecordMeta:   NewRecordMeta("input.queued"),
		SessionID:    s.opts.SessionID,
		RunID:        run.id,
		InputID:      id,
		Mode:         mode,
		Text:         text,
		DeliveryNote: note,
		QueuedAt:     nowRFC3339(),
	}); err != nil {
		return "", err
	}
	s.events.emit(EventInputQueued, map[string]any{
		"run_id":   run.id,
		"input_id": id,
		"mode":     string(mode),
	})

	// An interrupt aborts the activity that is running.
	if mode == Interrupt {
		s.addInterrupt(run, id)
	}
	return id, nil
}

// prepareDeliveryNoteLocked validates a delivery note against the
// session's message-id registry.
//
// The caller holds the session's lock.
func (s *Session) prepareDeliveryNoteLocked(opts []InputOption) (*DeliveryNote, error) {
	req := &inputRequest{}
	for _, o := range opts {
		o(req)
	}
	if req.delivered == nil {
		return nil, nil
	}
	note := req.delivered
	if note.MessageID == "" {
		return nil, &ErrInvalidInput{Message: "poo: the delivery note names no message"}
	}
	if note.UpToChars < 0 {
		return nil, &ErrInvalidInput{Message: "poo: the delivery note's character count is negative"}
	}
	if !s.messageIDs.Knows(note.MessageID) {
		return nil, &ErrInvalidInput{Message: fmt.Sprintf("poo: the message %s is not in this session", note.MessageID)}
	}
	// The note is rendered now when the reply is already recorded, so the
	// accepted input shows it. A reply that is still streaming is
	// rendered again at delivery.
	note.Text = s.renderDeliveryNoteLocked(note)
	return note, nil
}

// renderDeliveryNoteLocked renders the note that says how much of a
// reply the user had seen.
//
// The caller holds the session's lock.
func (s *Session) renderDeliveryNoteLocked(note *DeliveryNote) string {
	msg, ok := s.messagesByIDLocked(note.MessageID)
	if !ok {
		return ""
	}
	return renderDeliveryNote(note, msg)
}

// renderForDelivery returns the note an input is delivered with.
//
// The note is rendered from the original recorded message when the input
// is delivered, after any interrupted stream has been finalized.
func (s *Session) renderForDelivery(note *DeliveryNote) *DeliveryNote {
	if note == nil {
		return nil
	}
	rendered := *note
	s.mu.Lock()
	rendered.Text = s.renderDeliveryNoteLocked(&rendered)
	s.mu.Unlock()
	return &rendered
}

// messagesByIDLocked returns the message an id names.
//
// The caller holds the session's lock.
func (s *Session) messagesByIDLocked(id string) (Message, bool) {
	for _, m := range s.history {
		if m.ID == id {
			return m, true
		}
	}
	return Message{}, false
}

// Close ends the session.
//
// An active run is cancelled, the observers are stopped and drained, and
// the session's records are flushed.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	run := s.run
	s.mu.Unlock()

	if run != nil {
		s.finalize(run, newCandidate(ReasonCancelled, SettleDetail{
			Source: "cancel:close",
			Text:   "the session was closed",
		}))
	}
	s.events.stop()
	if s.opts.Sink != nil {
		ctx, cancel := context.WithTimeout(context.Background(), recordWriteBudget)
		defer cancel()
		return s.opts.Sink.Flush(ctx)
	}
	return nil
}
