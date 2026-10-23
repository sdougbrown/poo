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
	// The compaction itself belongs to the stage that applies it: this
	// stage calls the compactor and moves the run through the
	// compacting state, and the compaction's effect on the view is
	// applied there.
	if s.opts.Compactor != nil {
		should := s.opts.Compactor.ShouldCompact(s.view(), s.budget())
		if should {
			s.transition(StateCompacting)
			compacted, err := s.compact(run)
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
			} else if compacted {
				// The compaction is recorded, and the budget is
				// rebased on the estimate. The view change itself is
				// applied by the stage that validates compactions
				// (Stage 4); the store keeps the original history.
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

	// The notice grew the view, so the room to answer is checked again:
	// the request would otherwise go out with a max_tokens the minimum
	// already forbids.
	maxTokens = s.maxTokens()
	if maxTokens < MinOutputTokens {
		return gateResult{candidates: []candidate{newCandidate(ReasonContextExhausted, SettleDetail{
			Source: "no_output_room",
			Text:   fmt.Sprintf("the computed max_tokens %d is under the %d-token minimum", maxTokens, MinOutputTokens),
		})}}
	}

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

// compact asks the compactor to compact the view.
//
// The compaction's validation and its effect on the view belong to the
// stage that applies them. This stage calls the compactor from the
// compacting state and records what it did, so a compactor that is
// wired up cannot be called from anywhere else.
func (s *Session) compact(run *runState) (bool, error) {
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
		return false, errRunWindingDown
	}
	if err != nil {
		s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": err.Error()})
		return false, err
	}

	// The record of what the compactor did is written even though the
	// view change is applied by the next stage: the store keeps the
	// original history either way.
	_ = s.write(run.ctx, &CompactionRecord{
		RecordMeta:   NewRecordMeta("compaction"),
		RunID:        run.id,
		Strategy:     compaction.Strategy,
		Form:         compactionForm(compaction),
		TokensBefore: tokensBefore,
		TokensAfter:  s.estimatedInput(),
		Compaction:   compaction,
	})
	s.events.emit(EventCompactionEnded, map[string]any{
		"run_id":   run.id,
		"strategy": compaction.Strategy,
	})
	return true, nil
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
		resp, err := readResponse(ctx, reader, nil, run.id, 0, run.requests, nil)
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
	return s.est.estimate(s.history)
}

// rebaseEstimate makes the current view the accounting baseline.
//
// The estimate is recomputed over the whole view, and the next provider
// usage report replaces it.
func (s *Session) rebaseEstimate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.est.rebase(s.history)
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
	return historyChars(s.history)
}
