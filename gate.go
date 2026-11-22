package poo

import (
	"context"
	"errors"
	"fmt"
)

// requestGateResult is what the next-request gate decided.
type requestGateResult struct {
	// candidates are the settle candidates the request gate produced. An empty
	// list with held unset means the request gate opened a request.
	candidates []candidate
	// held reports that the run already holds a candidate, which the
	// finalization will pick up from the run: the request gate does not hand it
	// back, so no candidate is counted twice.
	held bool
}

// requestGate is the next-request gate.
//
// Every path to a new request goes through this one gate: the first
// request of a run, a turn that processed a call, a BeforeSettle
// continuation, an interrupted turn, an input delivered at finalize
// step 1, and an input delivered while waiting.
//
// The request gate never opens a request while a candidate is pending.
func (s *Session) requestGate(run *runState) requestGateResult {
	// The compactor gets one chance in a request gate pass. An interrupt that
	// ends the compaction restarts the request gate for the candidates and the
	// budget, but not for a second compaction the interrupt already ended.
	compactionTried := false
requestGateLoop:
	for {
		// Step 0: a pending candidate settles the run.
		s.mu.Lock()
		held := len(run.candidates) > 0
		s.mu.Unlock()
		if held {
			return requestGateResult{held: true}
		}

		// Step 1: the compactor.
		//
		// The compaction is applied here: the view changes, the budget is
		// rebased onto the new view, and a compaction that refuses to
		// compact ends the run with its handoff.
		//
		// A request the endpoint refused for its length asks for one
		// compaction the strategy itself may decline. The pass takes it
		// here, so a compaction an interrupt ended can be given back.
		//
		// An interrupt that ends the compaction is not a compaction
		// failure: the request gate restarts from its first step, so the pending
		// candidates and the budget are checked again instead of the interrupt
		// opening a request by itself.
		forced, refusal := s.takeForcedCompaction(run)
		for s.opts.Compactor != nil && !compactionTried {
			compactionTried = true
			// A forced compaction skips the strategy's decision. The endpoint
			// has already said the prompt is too long, and that guess is what
			// the strategy's guard exists to make.
			if !forced && !s.opts.Compactor.ShouldCompact(s.view(), s.budget()) {
				break
			}
			s.transition(StateCompacting)
			outcome, err := s.compact(run)
			// The run may have settled while the compactor was running.
			// Nothing the request gate does after that may move the state, open a
			// request, or record anything.
			if errors.Is(err, errRunWindingDown) || s.runWindingDown(&run.callOwner) {
				return requestGateResult{held: true}
			}
			if errors.Is(err, errCompactionInterrupted) {
				// An interrupt that ended the compaction does not spend the
				// refusal's one compaction: the restarted pass takes it again
				// and compacts.
				if forced {
					s.rearmForcedCompaction(run, refusal)
					compactionTried = false
				}
				s.clearInterrupt(run)
				s.drainInputs(run, false)
				s.transition(StateRequesting)
				continue requestGateLoop
			}
			if errors.Is(err, errCompactionStore) {
				// The compaction record was refused, and the run holds
				// the store_error candidate. The request gate never opens a
				// request while a candidate is pending.
				return requestGateResult{held: true}
			}
			if err != nil {
				// A compaction the refusal asked for is the request's last
				// chance: one that failed settles the run with the refusal.
				if forced {
					return requestGateResult{candidates: []candidate{refusal}}
				}
				// The compactor failed. When the next request can still go
				// out the run continues uncompacted; when it cannot, the run
				// settles context_exhausted.
				if blocked, _, _ := s.budget().RequestBlocked(); blocked {
					return requestGateResult{candidates: []candidate{newCandidate(ReasonContextExhausted, SettleDetail{
						Source: "compaction_failed",
						Text:   err.Error(),
					})}}
				}
			} else if outcome.candidate != nil {
				// The compactor refused to compact: the run ends with
				// the handoff it wrote.
				return requestGateResult{candidates: []candidate{*outcome.candidate}}
			}
			s.transition(StateRequesting)
		}
		break requestGateLoop
	}

	// Step 2: the budget check.
	if blocked, source, text := s.budget().RequestBlocked(); blocked {
		return requestGateResult{candidates: []candidate{newCandidate(ReasonContextExhausted, SettleDetail{
			Source: source,
			Text:   text,
		})}}
	}

	// Step 3: the run requests.
	return requestGateResult{}
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

// errCompactionInterrupted reports that an interrupt ended a compaction
// before its result was applied.
var errCompactionInterrupted = errors.New("poo: the compaction was interrupted")

// errCompactionStore reports that the store refused a compaction record.
// The candidate is already on the run, and the request gate re-enters its
// candidate check.
var errCompactionStore = errors.New("poo: the compaction record was refused")

// offersHistoryTool reports that the session's tools include the
// history tool: the recovery pointer names it only when the model can
// call it.
func (s *Session) offersHistoryTool() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, offered := s.opts.Tools["history"]
	_, canRead := s.opts.Sink.(RecordReader)
	return offered && canRead
}

// compact asks the compactor to compact the view and applies what it
// produced.
//
// The whole compaction is claimed against the run's seal, like a call
// record: the finalization waits for it, a claim that arrives after the
// seal is refused, and the seal ends an abandoned record write. The view
// is committed only after the record landed, and never for a run the
// seal owns.
//
// The compaction is validated before it is applied: a compaction the
// core cannot apply is a compactor failure, and the run carries on
// uncompacted when the next request can still go out.
//
// The compaction's record is written after the new view is known, so the
// record's tokens_after is the estimate of the new view at the plan's
// rebased ratio.
func (s *Session) compact(run *runState) (compactionOutcome, error) {
	claimCtx, release, ok := s.beginRunWrite(run)
	if !ok {
		return compactionOutcome{}, errRunWindingDown
	}
	defer release()
	if s.runWindingDown(&run.callOwner) {
		return compactionOutcome{}, errRunWindingDown
	}

	s.events.emit(EventCompactionStarted, map[string]any{"run_id": run.id})

	tokensBefore := s.estimatedInput()
	env := CompactionEnv{
		View:        s.view(),
		Budget:      s.budget(),
		SessionID:   s.ID(),
		HistoryTool: s.offersHistoryTool(),
		History:     s.retainedHistory(),
		Fork:        s.forkFunc(run),
		Ask:         s.askFunc(run),
	}

	// The compaction runs under the run's activity context, so an
	// interrupt cancels it and no compaction is applied.
	actCtx, _ := s.startActivity(run)
	defer s.endActivity(run)

	compaction, err := s.opts.Compactor.Compact(actCtx, env)
	if s.interruptPending(&run.callOwner) || errors.Is(context.Cause(actCtx), errInterrupted) {
		// A cooperative compactor may return a value while it unwinds.
		// The activity was interrupted, so the value is discarded.
		if !s.runWindingDown(&run.callOwner) {
			s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": errCompactionInterrupted.Error()})
		}
		return compactionOutcome{}, errCompactionInterrupted
	}
	// The run may have settled while the compactor was running. The
	// compaction's own record is not written, and the ended event is
	// not emitted, for a run that has already settled.
	if s.runWindingDown(&run.callOwner) {
		return compactionOutcome{}, errRunWindingDown
	}
	if err != nil {
		s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": err.Error()})
		return compactionOutcome{}, err
	}
	// A provider window is the transport's own compacted state, so the
	// core asks the transport for it. A strategy's items are never
	// applied: it can only ask for the native compaction.
	if compaction.Window != nil && compaction.Replace == nil && len(compaction.Rewrite) == 0 && compaction.End == nil {
		s.mu.Lock()
		requestChars := s.historyCharsLocked()
		s.mu.Unlock()
		result, werr := s.nativeWindow(actCtx)
		// The endpoint billed the answer it gave, so the cost is accounted
		// before the window is judged. A window this run discards still
		// spent the tokens it spent. An answer that carried no window was
		// billed all the same for what it did carry.
		s.accountNativeCompaction(run, result, requestChars)
		if s.interruptPending(&run.callOwner) || errors.Is(context.Cause(actCtx), errInterrupted) {
			if !s.runWindingDown(&run.callOwner) {
				s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": errCompactionInterrupted.Error()})
			}
			return compactionOutcome{}, errCompactionInterrupted
		}
		if werr != nil {
			s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": werr.Error()})
			return compactionOutcome{}, werr
		}
		compaction.Window = &result.Window
	}
	if err := s.validateCompaction(compaction); err != nil {
		s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": err.Error()})
		return compactionOutcome{}, err
	}

	outcome := compactionOutcome{}
	tokensAfter := tokensBefore
	var change *compactionView
	// A compaction that refuses to compact leaves the view alone: the
	// run ends with the handoff instead.
	if compaction.End == nil {
		built, verr := s.buildCompactionView(compaction)
		if verr != nil {
			s.events.emit(EventCompactionEnded, map[string]any{"run_id": run.id, "error": verr.Error()})
			return compactionOutcome{}, verr
		}
		change = &built
		// The rebase uses the plan's whole-view ratio, and the record
		// carries the estimate the next request will use.
		tokensAfter = int(float64(estimateHistoryChars(built.history)) / CharsPerToken)
		outcome.applied = true
	}

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
	if err := s.appendRecord(claimCtx, rec, run, nil); err != nil {
		if errors.Is(err, errCallRecordSealed) || s.runWindingDown(&run.callOwner) {
			return compactionOutcome{}, errRunWindingDown
		}
		s.noteStoreError(&run.callOwner, err)
		return compactionOutcome{}, errCompactionStore
	}
	if change != nil && !s.commitCompactionView(run, *change) {
		return compactionOutcome{}, errRunWindingDown
	}

	// The handoff is recorded on its own, so a consumer that carries
	// the work on can read the note without reading the compaction.
	if compaction.End != nil {
		if err := s.appendRecord(claimCtx, &HandoffRecord{
			RecordMeta: NewRecordMeta("handoff"),
			RunID:      run.id,
			Strategy:   compaction.Strategy,
			Handoff:    compaction.End.Handoff,
			CreatedAt:  nowRFC3339(),
		}, run, nil); err != nil {
			if errors.Is(err, errCallRecordSealed) || s.runWindingDown(&run.callOwner) {
				return compactionOutcome{}, errRunWindingDown
			}
			s.noteStoreError(&run.callOwner, err)
			return compactionOutcome{}, errCompactionStore
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

	// The run may have started finalizing while the record was written.
	// The record is its own now, and its ended event is not emitted for
	// a run that is going down.
	if s.runWindingDown(&run.callOwner) {
		return compactionOutcome{}, errRunWindingDown
	}
	s.events.emit(EventCompactionEnded, map[string]any{
		"run_id":   run.id,
		"strategy": compaction.Strategy,
		"side":     compaction.Side,
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
	return func(ctx context.Context, appended []Message, maxTokens int, opts ForkOptions) (Reply, error) {
		s.mu.Lock()
		rendered := run.lastRendered
		has := run.hasRendered
		// The fork's own size is the view it re-sends plus what it
		// appends, measured the way the run's requests are.
		chars := s.historyCharsLocked()
		s.mu.Unlock()
		if !has {
			return Reply{}, fmt.Errorf("poo: there is no request to fork")
		}
		reader, err := s.opts.Transport.Fork(ctx, rendered, appended, maxTokens, opts)
		if err != nil {
			return Reply{}, err
		}
		defer reader.Close()
		return s.readSideReply(ctx, run, reader, chars+estimateHistoryChars(appended))
	}
}

// askFunc returns the ask a compactor uses for a request of its own.
//
// The request carries the messages the strategy wrote and nothing else:
// no tools, and no reasoning setting of the run's, so it is not a turn.
// The session is derived from the run's own, so a gateway still reads a
// session header on the side request, but the identifier names the
// compaction: the side ask can reach a second provider, and the main
// session's id is not that provider's correlation key to keep.
func (s *Session) askFunc(run *runState) AskFunc {
	return func(ctx context.Context, messages []Message, maxTokens int) (Reply, error) {
		s.mu.Lock()
		transport, model := s.opts.Transport, s.opts.Model
		if s.opts.SideTransport != nil {
			transport, model = s.opts.SideTransport, s.opts.SideModel
		}
		s.mu.Unlock()
		reader, _, err := transport.Stream(ctx, ChatRequest{
			Model:     model.ModelID,
			ModelInfo: model,
			Messages:  CloneMessages(messages),
			MaxTokens: maxTokens,
			SessionID: s.ID() + "-compaction",
		})
		if err != nil {
			return Reply{}, err
		}
		defer reader.Close()
		return s.readSideReply(ctx, run, reader, estimateHistoryChars(messages))
	}
}

// readSideReply reads a request a compactor made and reports what it
// produced.
//
// A side request is not a turn: it appends nothing to the history and
// records no message. Its tokens still count against the run's ceiling,
// because the endpoint charged for them. An endpoint that reports no
// usage is charged the estimate a turn would get, measured the same way
// and so in the same unit: a fork re-sends the whole conversation, so
// counting it as free would hide the cost of compacting.
func (s *Session) readSideReply(ctx context.Context, run *runState, reader StreamReader, requestChars int) (Reply, error) {
	resp, err := readResponse(ctx, reader, nil, run.id, 0, run.requests, nil)
	if err != nil {
		return Reply{}, err
	}
	reply := Reply{Message: assistantMessage(resp), Finish: resp.finish}
	if resp.usage != nil {
		// The endpoint's own count, reported to the caller as what it is: a
		// request a compaction made.
		reply.Usage = *resp.usage
	}
	s.chargeSideRequest(run, resp.usage, reply.Message, requestChars)
	return reply, nil
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
	return estimateHistoryChars(s.modelMessagesLocked())
}
