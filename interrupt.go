package poo

// Cause names why a message was cut short.
type Cause string

const (
	// CauseInterrupt means the user interrupted the turn.
	CauseInterrupt Cause = "interrupt"
	// CauseProviderError means the provider's stream broke.
	CauseProviderError Cause = "provider_error"
)

// interruptedActivityTurn finishes a turn that an interrupt cut short.
//
// The turn ends as an interrupted turn: the turn's end is emitted, the
// interrupt is noted on it, the steer queue is drained, the AfterTurn
// hooks run with Interrupted set, the stop conditions are evaluated, and
// the run goes to the next-request gate.
//
// The turn's partial text is recorded as a message with partial set and
// cause "interrupt", and the calls that did not run get a visible
// result. A response the turn had already recorded in full (an interrupt
// during the tool batch) is not recorded again.
func (s *Session) interruptedActivityTurn(run *runState, turn int, resp *response, recorded bool) {
	// The partial text is recorded before the turn ends, so the log
	// shows what the model had said.
	if !recorded {
		s.recordPartial(run, turn, resp)
	}

	// The calls in the partial response did not run, and they get a
	// visible result.
	if resp != nil {
		s.skipInterruptedCalls(run, turn, resp.calls)
	}

	// The turn's end, then the interrupt's note on it.
	s.events.emit(EventTurnEnded, map[string]any{"run_id": run.id, "turn": turn})
	s.emitInterrupted(run, turn)

	// The steer queue is drained, which delivers the interrupt's own
	// message.
	s.drainInputs(run, false)

	// The AfterTurn hooks run with Interrupted set, and their
	// decisions are discarded: a slow hook does nothing on an
	// interrupted turn.
	s.runAfterTurn(run, turn, true)

	// The stop conditions are evaluated, and the interrupted turn counts
	// toward MaxTurns.
	cands := s.evaluateStopConditions(run, turn)
	cands = append(cands, s.evaluateBudgets(run, turn)...)
	if len(cands) > 0 {
		s.mu.Lock()
		run.candidates = append(run.candidates, cands...)
		s.mu.Unlock()
	}
	s.clearInterrupt(run)
}

// interruptedTurnEnd handles an interrupt that arrived during the
// turn-end hooks.
//
// The turn's end has already been emitted, so the remaining turn-end
// steps are skipped: the interrupt is noted, the steer queue is
// drained, and the run goes to the gate. There is no second turn end.
func (s *Session) interruptedTurnEnd(run *runState, turn int) {
	s.emitInterrupted(run, turn)
	s.drainInputs(run, false)
	s.clearInterrupt(run)
}

// emitInterrupted notes the interrupt on the turn that just closed.
//
// The interrupt always follows the turn's end, on every path: a client
// reads it as a note on the turn.
func (s *Session) emitInterrupted(run *runState, turn int) {
	ids := s.takeInterrupt(run)
	s.events.emit(EventTurnInterrupted, map[string]any{
		"run_id":    run.id,
		"turn":      turn,
		"input_ids": ids,
	})
	_ = s.write(run.ctx, &TurnInterrupted{
		RecordMeta:    NewRecordMeta("turn.interrupted"),
		SessionID:     s.opts.SessionID,
		RunID:         run.id,
		Turn:          turn,
		InputIDs:      ids,
		InterruptedAt: nowRFC3339(),
	})
}

// recordPartial records the text a cut-short response carried.
//
// The message is partial, and its cause is the interrupt.
func (s *Session) recordPartial(run *runState, turn int, resp *response) {
	if resp == nil {
		return
	}
	content := append([]ContentBlock(nil), resp.content...)
	// The calls the model had asked for belong to the message, so the
	// skipped results that follow have a call to answer.
	for _, call := range resp.calls {
		content = append(content, ContentBlock{
			Kind:       ContentToolCall,
			ToolCallID: call.ID,
			Name:       call.Name,
			Arguments:  recordArgs(call.Arguments),
		})
	}
	if len(content) == 0 {
		return
	}
	msg := Message{Role: RoleAssistant, Content: content, Origin: OriginModel}
	// The id the stream assigned at its first delta is the one this
	// message carries, so a reference to the streaming reply names it.
	msg.ID = resp.messageID
	if msg.ID == "" {
		msg.ID = s.messageIDs.Next()
	}
	msg.Partial = true
	msg.Cause = CauseInterrupt
	s.appendMessages(msg)
	_ = s.write(run.ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", msg.Origin),
		RunID:      run.id,
		Turn:       turn,
		Role:       msg.Role,
		Content:    msg.Content,
		MessageID:  msg.ID,
		Partial:    true,
		Cause:      CauseInterrupt,
	})
}

// skipInterruptedCalls records the calls that an interrupt stopped
// before they started.
//
// A call that never ran gets a visible result, so the model can see
// that the call was not made.
func (s *Session) skipInterruptedCalls(run *runState, turn int, calls []ToolCall) {
	s.skipCalls(run, turn, calls, SkippedInterruptedText)
}

// skipCalls records the calls of a batch that never started, with the
// visible result text given.
//
// A call that already has a result, or that is running, is left alone.
func (s *Session) skipCalls(run *runState, turn int, calls []ToolCall, text string) {
	handles := s.handlesOf(turn, len(calls))
	for i, call := range calls {
		handle := handles[i]
		s.mu.Lock()
		_, issued := run.pendingCalls[handle]
		summary := s.turnSummaryLocked(turn, handle)
		s.mu.Unlock()
		if issued {
			// The call is running or already has a result.
			continue
		}
		if summary.Outcome != "" {
			// The call already has an outcome.
			continue
		}
		_ = s.write(run.ctx, &ToolCallRecord{
			RecordMeta: NewRecordMetaFrom("tool.call", OriginModel),
			RunID:      run.id,
			Turn:       turn,
			Handle:     handle,
			Tool:       call.Name,
			Arguments:  recordArgs(call.Arguments),
		})
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    text,
			IsError: true,
		}, OutcomeSkipped)
	}
}

// handlesOf returns the handles of a turn's calls.
//
// A turn whose calls were planned already has its handles in the turn's
// summary. A turn that an interrupt cut short before the plan has none
// yet, and its calls get fresh ones.
func (s *Session) handlesOf(turn, n int) []string {
	s.mu.Lock()
	for _, t := range s.turns {
		if t.Index == turn && len(t.Calls) == n {
			handles := make([]string, n)
			for i, c := range t.Calls {
				handles[i] = c.Handle
			}
			s.mu.Unlock()
			return handles
		}
	}
	s.mu.Unlock()
	handles := make([]string, n)
	for i := range handles {
		handles[i] = s.nextHandle()
	}
	return handles
}

// turnSummaryLocked returns the summary of one call of a turn.
//
// The caller holds the session's lock.
func (s *Session) turnSummaryLocked(turn int, handle string) ToolCallSummary {
	for _, t := range s.turns {
		if t.Index != turn {
			continue
		}
		for _, c := range t.Calls {
			if c.Handle == handle {
				return c
			}
		}
	}
	return ToolCallSummary{}
}
