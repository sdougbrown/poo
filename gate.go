package poo

import (
	"context"
	"errors"
	"fmt"
)

// gateResult is what the next-request gate decided.
type gateResult struct {
	// candidates are the settle candidates the gate produced. An empty
	// list with held unset means the gate opened a request.
	candidates []candidate
	// held reports that the run already holds a candidate, which the
	// finalization will pick up from the run: the gate does not hand it
	// back, so no candidate is counted twice.
	held bool
}

// gate is the next-request gate.
//
// Every path to a new request goes through this one gate: the first
// request of a run, a turn that processed a call, a BeforeSettle
// continuation, an interrupted turn, an input delivered at finalize
// step 1, and an input delivered while waiting.
//
// The gate never opens a request while a candidate is pending.
func (s *Session) gate(run *runState) gateResult {
	// Step 0: a pending candidate settles the run.
	s.mu.Lock()
	held := len(run.candidates) > 0
	s.mu.Unlock()
	if held {
		return gateResult{held: true}
	}

	// Step 1: the compactor.
	//
	// The compaction is applied here: the view changes, the budget is
	// rebased onto the new view, and a compaction that refuses to
	// compact ends the run with its handoff.
	if s.opts.Compactor != nil {
		should := s.opts.Compactor.ShouldCompact(s.view(), s.budget())
		if should {
			s.transition(StateCompacting)
			outcome, err := s.compact(run)
			// The run may have settled while the compactor was
			// running. Nothing the gate does after that may move the
			// state, open a request, or record anything.
			if errors.Is(err, errRunWindingDown) || s.runWindingDown(run) {
				return gateResult{held: true}
			}
			if err != nil {
				// An interrupt that cancelled the compaction is not
				// a compaction failure: the run carries on, and no
				// compaction is applied. The compaction is not a
				// turn, so the interrupt emits no turn events.
				if s.interruptPending(run) {
					s.clearInterrupt(run)
					s.drainInputs(run, false)
					s.transition(StateRequesting)
					return gateResult{}
				}
				// The compactor failed. Below the hard limit the run
				// continues uncompacted; at or above it the run
				// settles context_exhausted.
				if s.overHardLimit() {
					return gateResult{candidates: []candidate{newCandidate(ReasonContextExhausted, SettleDetail{
						Source: "compaction_failed",
						Text:   err.Error(),
					})}}
				}
			} else if outcome.candidate != nil {
				// The compactor refused to compact: the run ends
				// with the handoff it wrote.
				return gateResult{candidates: []candidate{*outcome.candidate}}
			} else if outcome.applied {
				// The compaction was applied, and the budget was
				// rebased onto the new view.
				s.rebaseEstimate()
			}
			s.transition(StateRequesting)
		}
	}

	// Step 2: the budget check.
	maxTokens := s.maxTokens()
	if maxTokens < MinOutputTokens {
		// No room to answer.
		return gateResult{candidates: []candidate{newCandidate(ReasonContextExhausted, SettleDetail{
			Source: "no_output_room",
			Text:   fmt.Sprintf("the computed max_tokens %d is under the %d-token minimum", maxTokens, MinOutputTokens),
		})}}
	}
	if s.overHardLimit() {
		return gateResult{candidates: []candidate{newCandidate(ReasonContextExhausted, SettleDetail{
			Source: "hard_limit",
			Text: fmt.Sprintf("the estimated input of %d tokens is at or above %d%% of the %d-token window",
				s.estimatedInput(), int(s.opts.HardRatio*100), s.opts.ContextLimit),
		})}}
	}

	// Step 3: the context notice, when it is due.
	s.maybeNotice(run)

	// Step 4: the run requests.
	return gateResult{}
}

// overHardLimit reports that the estimated input is at or above the hard
// ratio of the context window.
func (s *Session) overHardLimit() bool {
	limit := s.opts.ContextLimit
	if limit <= 0 {
		return false
	}
	estimated := s.estimatedInput()
	return float64(estimated) >= s.opts.HardRatio*float64(limit)
}

// maybeNotice tells the model that the context window is filling up.
//
// The notice fires at most once per run, and it fires again in a later
// run only if the estimate has dropped back below the notice ratio.
func (s *Session) maybeNotice(run *runState) {
	limit := s.opts.ContextLimit
	if limit <= 0 {
		return
	}
	s.mu.Lock()
	estimated := s.estimatedInputLocked()
	if float64(estimated) < s.opts.NoticeRatio*float64(limit) {
		// The usage is below the ratio, which re-arms the notice for
		// the runs to come.
		s.noticeArmed = true
		s.mu.Unlock()
		return
	}
	if !s.noticeArmed || run.noticeFired {
		s.mu.Unlock()
		return
	}
	run.noticeFired = true
	s.noticeArmed = false
	s.mu.Unlock()

	msg := NewTextMessage(RoleUser, ContextNoticeText)
	msg.Origin = OriginUser
	s.appendMessages(msg)
	_ = s.write(run.ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", msg.Origin),
		RunID:      run.id,
		Role:       msg.Role,
		Content:    msg.Content,
	})
	s.events.emit(EventContextNotice, map[string]any{
		"run_id": run.id,
		"ratio":  s.opts.NoticeRatio,
		"input":  estimated,
		"limit":  limit,
	})
}

// compactionOutcome is what a compaction did.
type compactionOutcome struct {
	// applied reports that the compaction changed the view.
	applied bool
	// candidate is the settle candidate the compaction produced.
	//
	// A compaction that refuses to compact ends the run, and the
	// candidate carries the handoff it wrote.
	candidate *candidate
}

// compact asks the compactor to compact the view and applies what it
// produced.
//
// The compaction is validated before it is applied: a compaction the
// core cannot apply is a compactor failure, and the run carries on
// uncompacted below the hard limit.
//
// The compaction's record is written after the view changed, so the
// record's tokens_after is the estimate of the new view.
func (s *Session) compact(run *runState) (compactionOutcome, error) {
	s.events.emit(EventCompactionStarted, map[string]any{"run_id": run.id})

	tokensBefore := s.estimatedInput()
	view := s.view()
	fork := s.forkFunc(run)

	// The compaction runs under the run's activity context, so an
	// interrupt cancels it and no compaction is applied.
	ctx, _ := s.startActivity(run)
	defer s.endActivity(run)

	compaction, err := s.opts.Compactor.Compact(ctx, view, fork)
	// The run may have settled while the compactor was running. The
	// compaction's own record is not written, and the ended event is
	// not emitted, for a run that has already settled.
	if s.runWindingDown(run) {
		return compactionOutcome{}, errRunWindingDown
	}
	if err != nil {
		s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": err.Error()})
		return compactionOutcome{}, err
	}
	if err := s.validateCompaction(compaction); err != nil {
		s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": err.Error()})
		return compactionOutcome{}, err
	}

	outcome := compactionOutcome{}
	// A compaction that refuses to compact leaves the view alone: the
	// run ends with the handoff instead.
	if compaction.End == nil {
		if err := s.applyCompaction(compaction); err != nil {
			s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": err.Error()})
			return compactionOutcome{}, err
		}
		outcome.applied = true
	}

	tokensAfter := s.estimatedInput()
	rec := &CompactionRecord{
		RecordMeta:   NewRecordMeta("compaction"),
		RunID:        run.id,
		Strategy:     compaction.Strategy,
		Form:         compactionForm(compaction),
		TokensBefore: tokensBefore,
		TokensAfter:  tokensAfter,
		Compaction:   compaction,
	}
	if compaction.Replace != nil {
		cut := compaction.Replace.Cut
		rec.Cut = &cut
		rec.Replacement = compaction.Replace.Messages
	}
	if err := s.write(run.ctx, rec); err != nil {
		s.addCandidate(run, storeErrorCandidate(err))
	}

	// The handoff is recorded on its own, so a consumer that carries
	// the work on can read the note without reading the compaction.
	if compaction.End != nil {
		if err := s.write(run.ctx, &HandoffRecord{
			RecordMeta: NewRecordMeta("handoff"),
			RunID:      run.id,
			Strategy:   compaction.Strategy,
			Handoff:    compaction.End.Handoff,
			CreatedAt:  nowRFC3339(),
		}); err != nil {
			s.addCandidate(run, storeErrorCandidate(err))
		}
		outcome.candidate = &candidate{
			reason: ReasonContextExhausted,
			detail: SettleDetail{
				Source:  "compactor:" + compaction.Strategy,
				Text:    "the compactor refused to compact",
				Handoff: compaction.End.Handoff,
			},
		}
	}

	s.events.emit(EventCompactionEnded, map[string]any{
		"run_id":   run.id,
		"strategy": compaction.Strategy,
	})
	return outcome, nil
}

// compactionForm names the compaction's form.
//
// Exactly one of the four forms is set; the first one that is present
// names the record.
func compactionForm(c Compaction) string {
	switch {
	case c.Replace != nil:
		return "replace"
	case len(c.Rewrite) > 0:
		return "rewrite"
	case c.Window != nil:
		return "window"
	case c.End != nil:
		return "end"
	default:
		return ""
	}
}

// forkFunc returns the fork a compactor uses to reach the provider.
//
// The fork is bound to the transport and to the last main request's
// snapshot, so a strategy cannot build a fork request any other way.
func (s *Session) forkFunc(run *runState) ForkFunc {
	return func(ctx context.Context, appended []Message, maxTokens int) (Message, UsageReport, error) {
		s.mu.Lock()
		rendered := run.lastRendered
		has := run.hasRendered
		s.mu.Unlock()
		if !has {
			return Message{}, UsageReport{}, fmt.Errorf("poo: there is no request to fork")
		}
		reader, err := s.opts.Transport.Fork(ctx, rendered, appended, maxTokens)
		if err != nil {
			return Message{}, UsageReport{}, err
		}
		defer reader.Close()
		resp, err := readResponse(ctx, reader, nil, run.id, 0, run.requests, s.opts.Sink, nil)
		if err != nil {
			return Message{}, UsageReport{}, err
		}
		msg := Message{Role: RoleAssistant, Content: resp.content, Origin: OriginModel}
		if resp.usage != nil {
			s.mu.Lock()
			run.tokens += resp.usage.InputTokens + resp.usage.OutputTokens
			s.mu.Unlock()
			return msg, *resp.usage, nil
		}
		return msg, UsageReport{}, nil
	}
}

// maxTokens computes the output room for the next request.
//
// The plan's rule: min(model MaxOutput or 8192, context_limit −
// estimated input − 256). The value is always computed, never left to
// the endpoint's default.
func (s *Session) maxTokens() int {
	limit := s.opts.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	room := s.opts.ContextLimit - s.estimatedInput() - OutputReserve
	if room < limit {
		limit = room
	}
	return limit
}

// estimatedInput estimates the prompt tokens of the next request.
func (s *Session) estimatedInput() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.estimatedInputLocked()
}

// estimatedInputLocked estimates the prompt tokens of the next request.
//
// The caller holds the session's lock.
func (s *Session) estimatedInputLocked() int {
	return s.est.estimate(s.modelMessagesLocked())
}

// rebaseEstimate makes the current view the accounting baseline.
//
// The estimate is recomputed over the whole view, and the next provider
// usage report replaces it.
func (s *Session) rebaseEstimate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.est.rebase(s.modelMessagesLocked())
}

// observeUsage calibrates the estimator with a provider's report.
//
// The ratio of the serialized request's characters to the reported input
// tokens becomes the model's characters-per-token, clamped to the
// plan's bounds and kept for the session. The characters are the
// request's own, not the view's now: the view also holds the reply the
// request produced, which the report does not count.
func (s *Session) observeUsage(report *UsageReport, requestChars int) {
	if report == nil || report.InputTokens <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.est.observe(report.InputTokens, requestChars)
}

// historyCharsLocked returns the serialized size of the model's view.
//
// The caller holds the session's lock.
func (s *Session) historyCharsLocked() int {
	chars := 0
	for _, m := range s.modelMessagesLocked() {
		chars += estimateMessageChars(m)
	}
	return chars
}
