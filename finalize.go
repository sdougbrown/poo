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
func (s *Session) finalize(run *runState, cand candidate) Result {
	s.mu.Lock()
	if run.committed {
		// A candidate arriving after the commit point is logged and
		// ignored: it never mutates a settled run.
		s.mu.Unlock()
		return run.result
	}
	if run.finalizing {
		// The finalization is under way. The candidate is added, and
		// the finalization picks it up at the commit point.
		run.candidates = append(run.candidates, cand)
		s.mu.Unlock()
		s.wakeWaiters(run, run.result)
		return run.result
	}
	run.finalizing = true
	run.candidates = append(run.candidates, cand)
	s.mu.Unlock()

	return s.finalizeRun(run)
}

// finalizeRun runs the finalization protocol.
//
// The steps are the plan's: the input check, the cancellation of the
// run's context, the wait for in-flight work, the synthetic results,
// the commit point, the outcome records, and the settled event.
func (s *Session) finalizeRun(run *runState) Result {
	// Step 1: the input check. A run that would settle naturally can
	// still take a queued input; a run that is settling because of a
	// failure or a limit cannot.
	if s.abandonFinalize(run) {
		s.mu.Lock()
		run.finalizing = false
		s.mu.Unlock()
		return Result{}
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
	if err := s.write(cleanupCtx, settle); err != nil {
		// A store that refuses the settle record means the run's
		// outcome may not exist. The reason changes to store_error,
		// unless the run was cancelled: a cancelled run stays
		// cancelled, and the store error goes into the detail.
		detail.Losing = append(detail.Losing, string(ReasonStoreError))
		if reason != ReasonCancelled {
			reason = ReasonStoreError
			detail.Source = "store"
			detail.Text = err.Error()
		}
	}
	if s.opts.Sink != nil {
		_ = s.opts.Sink.Flush(cleanupCtx)
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
	return Result{Reason: reason, Detail: detail, Turns: run.turn}
}

// abandonFinalize reports whether the finalization is abandoned because
// a queued input can still be delivered.
//
// This is the only place a finalization can be abandoned.
func (s *Session) abandonFinalize(run *runState) bool {
	s.mu.Lock()
	// Only a natural candidate may be abandoned: a run that is
	// settling because of a failure or a limit has to settle.
	natural := true
	for _, c := range run.candidates {
		if !IsNatural(c.reason) {
			natural = false
			break
		}
	}
	if !natural {
		s.mu.Unlock()
		return false
	}
	if len(s.steerQueue) == 0 && len(s.followUpQueue) == 0 {
		s.mu.Unlock()
		return false
	}
	// The queues are drained, and the run goes to the next-request
	// gate. The AfterTurn hooks and the stop conditions are not
	// re-run, and this does not count against MaxContinuations.
	go s.resumeRun(run)
	s.mu.Unlock()
	return true
}

// resumeRun puts a run back on the loop after an abandoned
// finalization.
//
// The run's context was not cancelled, so the loop carries on from
// where it was.
func (s *Session) resumeRun(run *runState) {
	s.drainInputs(run, true)
	s.mu.Lock()
	if s.run != run {
		s.mu.Unlock()
		return
	}
	run.finalizing = false
	run.candidates = nil
	s.transitionLocked(StateRequesting)
	s.mu.Unlock()

	cands := s.loop(run)
	if len(cands) == 0 {
		cands = []candidate{newCandidate(ReasonEndTurn, SettleDetail{Source: "end_turn"})}
	}
	s.finalize(run, cands[0])
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
	sort.SliceStable(ordered, func(i, j int) bool {
		return Precedence(ordered[i].reason) < Precedence(ordered[j].reason)
	})
	winner := ordered[0]
	detail := winner.detail
	if detail.Source == "" {
		detail.Source = string(winner.reason)
	}
	var losing []string
	for _, c := range ordered[1:] {
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
		pending := len(run.pendingCalls)
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
	s.mu.Unlock()

	sort.Slice(issued, func(i, j int) bool { return issued[i].handle < issued[j].handle })
	for _, call := range issued {
		outcome := OutcomeSkipped
		text := "Not run: the run settled before the call started."
		if call.started {
			outcome = OutcomeCancelled
			text = "Cancelled: the run settled before the call finished."
		}
		s.recordResult(run, call.turn, call.handle, call.tool, call.id, ToolResult{
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
// The context bounds the wait. When it ends, the run's own context ends
// with it, so the run settles as cancelled: the wait continues for the
// settle, because the settle record is the answer.
func (s *Session) Wait(ctx context.Context) (Result, error) {
	ch := make(chan Result, 1)
	s.mu.Lock()
	if s.run == nil {
		s.mu.Unlock()
		return Result{}, nil
	}
	s.waiters = append(s.waiters, ch)
	s.mu.Unlock()
	select {
	case res := <-ch:
		return res, nil
	case <-ctx.Done():
	}
	// The context ended, and the run's own context ended with it. The
	// run settles on its own, so the wait continues: a bounded grace
	// period covers a run that is finishing its work.
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
	note, err := s.prepareDeliveryNoteLocked(opts)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	s.inputSeq++
	id := fmt.Sprintf("%s-i%d", s.opts.SessionID, s.inputSeq)
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
	// The note is rendered when the input is accepted, so a replay
	// shows exactly what the user saw.
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
