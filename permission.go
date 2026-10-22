package poo

import (
	"context"
	"log/slog"
	"sort"
)

// PermissionAsk identifies a call waiting for authorization.
type PermissionAsk struct {
	// AskID names the ask. A client answers by it.
	AskID string
	// Tool is the name of the tool the model called.
	Tool string
	// Summary describes the call.
	Summary string
	// Class is the tool's class.
	Class ToolClass
	// Reason is why the call is being asked about: "policy" or
	// "doom_loop".
	Reason string
	// RepeatCount holds the doom loop's counts, and is nil for a policy
	// ask.
	RepeatCount *RepeatCounts
}

// PermissionAnswer is the terminal outcome of a permission request.
type PermissionAnswer struct {
	// AskID names the ask the answer resolves.
	AskID string
	// Decision is "allow" or "deny".
	Decision string
	// AnsweredBy names who answered: an Asker, "fallback" for the
	// chain's fallback, or "cancelled" for a run that ended.
	AnsweredBy string
	// Provenance is how the ask was resolved: "answered", "timeout",
	// "default", "cancelled" or "interrupted".
	Provenance string
	// Reason is why the call was being asked about.
	Reason string
}

// The provenances a permission answer can carry.
const (
	// ProvenanceAnswered means an Asker answered the ask.
	ProvenanceAnswered = "answered"
	// ProvenanceTimeout means the chain's deadline ran out.
	ProvenanceTimeout = "timeout"
	// ProvenanceDefault means the chain had no answer to give, and the
	// fallback decided.
	ProvenanceDefault = "default"
	// ProvenanceCancelled means the run ended while the ask was pending.
	ProvenanceCancelled = "cancelled"
	// ProvenanceInterrupted means an interrupt ended the activity while
	// the ask was pending.
	ProvenanceInterrupted = "interrupted"
)

// The reasons a call is being asked about.
const (
	// ReasonPolicy means a policy rule asked about the call.
	ReasonPolicy = "policy"
	// ReasonDoomLoop means the doom loop asked about the call.
	ReasonDoomLoop = "doom_loop"
)

// PermissionReporter is the policy slot's seam to the core.
//
// The core hands the slot a reporter when it calls the slot. The
// reporter writes the ask and answer records and publishes their events,
// so a policy engine never publishes an event itself. It also clears a
// call's repeat counters, which is how a doom loop the policy allowed
// starts over.
//
// The core's reporter also implements PermissionRequests, so the chain
// and the finalization share the run's pending request.
type PermissionReporter interface {
	// Asked reports that the run is waiting for an answer.
	Asked(PermissionAsk)
	// Answered reports the answer that resolved the ask.
	Answered(PermissionAnswer)
	// ResetRepeats clears the repeat counters of the call with the
	// given handle.
	ResetRepeats(handle string)
}

// permissionReporter is the core's side of the policy slot's seam.
type permissionReporter struct {
	session *Session
	run     *runState
}

func (r *permissionReporter) Asked(ask PermissionAsk) {
	r.session.reportPermissionAsked(r.run, ask)
}

func (r *permissionReporter) Answered(answer PermissionAnswer) {
	r.session.reportPermissionAnswered(r.run, answer)
}

func (r *permissionReporter) ResetRepeats(handle string) {
	r.session.resetRepeats(r.run, handle)
}

func (r *permissionReporter) BeginPermission(ask PermissionAsk, opts PermissionRequestOptions) *PermissionRequest {
	return r.session.beginPermission(r.run, ask, opts)
}

// beginPermission registers a request with the run and writes its ask
// record. The request is registered under the session's lock, so the
// seal either sees it or refuses it, and a request that arrives after
// the seal resolves as cancelled without writing anything.
func (s *Session) beginPermission(run *runState, ask PermissionAsk, opts PermissionRequestOptions) *PermissionRequest {
	p := NewPermissionRequest(ask, opts)
	p.logDrop = func(in PermissionInput) {
		s.logWarn("stale permission input dropped", slog.String("run_id", run.id), slog.String("ask_id", ask.AskID), slog.Uint64("attempt", in.Attempt))
	}
	s.mu.Lock()
	if run.sealed || run.committed || run.finalizing {
		p.Send(PermissionInput{Kind: PermissionSeal})
		s.mu.Unlock()
		return p
	}
	if run.asks == nil {
		run.asks = map[string]*PermissionRequest{}
	}
	if held := run.asks[ask.AskID]; held != nil {
		s.mu.Unlock()
		return held
	}
	run.asks[ask.AskID] = p
	s.mu.Unlock()
	s.persistPermission(run, p)
	return p
}

// reportPermissionAsked registers an ask with the run and records it.
func (s *Session) reportPermissionAsked(run *runState, ask PermissionAsk) {
	if run != nil {
		s.beginPermission(run, ask, PermissionRequestOptions{Context: run.ctx})
	}
}

// reportPermissionAnswered resolves the ask's request and records its
// answer. An answer for an ask that already resolved is dropped, and one
// that arrives after the seal is inert.
func (s *Session) reportPermissionAnswered(run *runState, answer PermissionAnswer) {
	if run == nil {
		return
	}
	s.mu.Lock()
	if run.sealed || run.committed {
		s.mu.Unlock()
		s.logWarn("permission answer after seal", slog.String("run_id", run.id), slog.String("ask_id", answer.AskID))
		return
	}
	p := run.asks[answer.AskID]
	if answer.AskID == "" {
		// Deterministic outcomes have no public ask id, but still have
		// a run-owned record claim and one resolution.
		key := "policy:" + run.permissionHandle
		p = run.asks[key]
		if p == nil {
			p = NewPermissionRequest(PermissionAsk{Reason: answer.Reason}, PermissionRequestOptions{Context: run.ctx})
			p.askedWritten = true
			if run.asks == nil {
				run.asks = map[string]*PermissionRequest{}
			}
			run.asks[key] = p
		}
	}
	if p != nil {
		select {
		case <-p.Done():
			// The request resolved already. The echo needs its record,
			// not a second resolution.
		default:
			p.Send(PermissionInput{Kind: PermissionPolicyAnswer, Answer: answer})
		}
	}
	s.mu.Unlock()
	if p != nil {
		s.persistPermission(run, p)
	}
}

// persistPermission is the only writer of a request's records. The
// seal joins this writer instead of cancelling an ambiguous append and
// issuing a conflicting second answer. Each append has its own cleanup
// budget, independent of run cancellation and the seal's wait budget.
func (s *Session) persistPermission(run *runState, p *PermissionRequest) {
	p.io.Lock()
	defer p.io.Unlock()
	p.mu.Lock()
	p.writing = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.writing = false
		p.mu.Unlock()
		s.mu.Lock()
		s.signalWorkLocked(run)
		s.mu.Unlock()
	}()
	if !p.askedWritten {
		p.askedWritten = true
		ask := p.ask
		rec := &PermissionAskedRecord{RecordMeta: NewRecordMeta("permission.asked"), RunID: run.id, AskID: ask.AskID, Tool: ask.Tool, Summary: ask.Summary, Class: ask.Class, Reason: ask.Reason, RepeatCount: ask.RepeatCount}
		if err := s.write(context.Background(), rec); err != nil {
			s.addCandidate(run, storeErrorCandidate(err))
		}
		payload := map[string]any{"run_id": run.id, "ask_id": ask.AskID, "tool": ask.Tool, "summary": ask.Summary, "class": string(ask.Class), "reason": ask.Reason}
		if ask.RepeatCount != nil {
			payload["repeat_count"] = map[string]any{"exact": ask.RepeatCount.Exact, "near": ask.RepeatCount.Near}
		}
		s.events.emit(EventPermissionAsked, payload)
	}
	answer := p.Result()
	if answer.Decision == "" || p.answerWritten {
		return
	}
	p.answerWritten = true
	rec := &PermissionAnsweredRecord{RecordMeta: NewRecordMeta("permission.answered"), RunID: run.id, AskID: answer.AskID, Decision: answer.Decision, AnsweredBy: answer.AnsweredBy, Provenance: answer.Provenance, Reason: answer.Reason}
	if err := s.write(context.Background(), rec); err != nil {
		s.addCandidate(run, storeErrorCandidate(err))
	}
	s.events.emit(EventPermissionAnswered, map[string]any{"run_id": run.id, "ask_id": answer.AskID, "decision": answer.Decision, "answered_by": answer.AnsweredBy, "provenance": answer.Provenance, "reason": answer.Reason})
}

// settlePendingAsks drains every request's single writer before settlement.
// sealLocked has already resolved the pending requests under the run lock.
func (s *Session) settlePendingAsks(run *runState) {
	s.mu.Lock()
	ids := make([]string, 0, len(run.asks))
	for id := range run.asks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pending := make([]*PermissionRequest, 0, len(ids))
	for _, id := range ids {
		pending = append(pending, run.asks[id])
	}
	s.mu.Unlock()
	for _, p := range pending {
		s.persistPermission(run, p)
	}
}
