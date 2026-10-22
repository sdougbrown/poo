package poo

import (
	"context"
	"log/slog"
	"sort"
)

// PermissionAsk identifies a call waiting for authorization.
type PermissionAsk struct {
	AskID       string
	Tool        string
	Summary     string
	Class       ToolClass
	Reason      string
	RepeatCount *RepeatCounts
}

// PermissionAnswer is the terminal outcome of a permission request.
type PermissionAnswer struct {
	AskID      string
	Decision   string
	AnsweredBy string
	Provenance string
	Reason     string
}

const (
	ProvenanceAnswered    = "answered"
	ProvenanceTimeout     = "timeout"
	ProvenanceDefault     = "default"
	ProvenanceCancelled   = "cancelled"
	ProvenanceInterrupted = "interrupted"
	ReasonPolicy          = "policy"
	ReasonDoomLoop        = "doom_loop"
)

// PermissionReporter records policy outcomes and resets allowed loops.
// Core also implements PermissionRequests so the chain and seal share
// the same pending request.
type PermissionReporter interface {
	Asked(PermissionAsk)
	Answered(PermissionAnswer)
	ResetRepeats(handle string)
}

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

func (s *Session) reportPermissionAsked(run *runState, ask PermissionAsk) {
	if run != nil {
		s.beginPermission(run, ask, PermissionRequestOptions{Context: run.ctx})
	}
}

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
		p.Send(PermissionInput{Kind: PermissionPolicyAnswer, Answer: answer})
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
