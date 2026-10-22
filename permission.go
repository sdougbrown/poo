package poo

import (
	"errors"
	"sort"
)

// The permission seam is how the policy slot reaches the core.
//
// The slot decides whether a call runs, and an ask happens inside it:
// the slot blocks on the Asker chain and answers with an allowance or a
// refusal. The core publishes the permission events and keeps the
// repeat counters, so the slot never touches the session itself.

// PermissionAsk is one permission ask.
//
// The ask names the call that is waiting and why it is waiting: a
// policy rule asked about it, or the doom loop did.
type PermissionAsk struct {
	// AskID names the ask. The id is unique within the session, and a
	// client answers by it.
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
	// RepeatCount holds the doom loop's counts. It is set for a
	// doom-loop ask, and nil for a policy ask.
	RepeatCount *RepeatCounts
}

// PermissionAnswer is the answer that resolved a permission ask.
//
// The answer says what was decided, who decided it and how the ask was
// resolved, so a reader can tell an answer from a fallback.
type PermissionAnswer struct {
	// AskID names the ask the answer resolves.
	AskID string
	// Decision is "allow" or "deny".
	Decision string
	// AnsweredBy names who answered: the asker that answered, "fallback"
	// when the chain had no answer to give, or "policy" for a doom loop's
	// deterministic action.
	AnsweredBy string
	// Provenance is how the ask was resolved: "answered", "timeout",
	// "default", "cancelled" or "interrupted".
	Provenance string
	// Reason is why the call was being asked about.
	Reason string
}

// The provenances a permission answer can carry.
const (
	// ProvenanceAnswered means an asker answered the ask.
	ProvenanceAnswered = "answered"
	// ProvenanceTimeout means the chain's deadline ran out.
	ProvenanceTimeout = "timeout"
	// ProvenanceDefault means the chain had no answer to give, and the
	// fallback decided.
	ProvenanceDefault = "default"
	// ProvenanceCancelled means the run ended while the ask was
	// pending.
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
// The core hands the slot a reporter when it calls the slot, and the
// slot reports the asks it made and the answers that resolved them. The
// core publishes the permission events from those reports, so a policy
// engine never publishes an event itself.
//
// The reporter also clears a call's repeat counters, which is how a
// doom loop the policy allowed starts over.
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
//
// The reporter writes the ask and answer records, publishes their
// events and clears the counters, so the slot only ever decides. The
// reporter belongs to one run: an ask or an answer that arrives after
// the run is sealed is inert.
type permissionReporter struct {
	// session is the session the records and events go to.
	session *Session
	// run is the run the slot is deciding for.
	run *runState
}

// Asked reports that the run is waiting for an answer.
func (r *permissionReporter) Asked(ask PermissionAsk) {
	r.session.reportPermissionAsked(r.run, ask)
}

// Answered reports the answer that resolved the ask.
func (r *permissionReporter) Answered(answer PermissionAnswer) {
	r.session.reportPermissionAnswered(r.run, answer)
}

// ResetRepeats clears the repeat counters of a call.
func (r *permissionReporter) ResetRepeats(handle string) {
	r.session.resetRepeats(handle)
}

// pendingAsk is one ask that has no answer yet.
type pendingAsk struct {
	// ask is the ask the run published.
	ask PermissionAsk
	// resolved reports that an answer tried to resolve the ask.
	resolved bool
	// answered reports that an answer's record was written.
	answered bool
}

// reportPermissionAsked registers an ask with the run and records it.
//
// The ask is registered and its record write is claimed under the
// session's lock, so the finalization either waits for the record or
// owns the ask. An ask that arrives after the seal is inert.
func (s *Session) reportPermissionAsked(run *runState, ask PermissionAsk) {
	if run == nil {
		return
	}
	s.mu.Lock()
	if run.sealed || run.committed {
		s.mu.Unlock()
		return
	}
	if run.asks == nil {
		run.asks = map[string]*pendingAsk{}
	}
	if _, duplicate := run.asks[ask.AskID]; duplicate {
		s.mu.Unlock()
		return
	}
	run.asks[ask.AskID] = &pendingAsk{ask: ask}
	ctx, release, ok := s.beginRunWriteLocked(run)
	s.mu.Unlock()
	if !ok {
		return
	}
	defer release()
	rec := &PermissionAskedRecord{
		RecordMeta:  NewRecordMeta("permission.asked"),
		RunID:       run.id,
		AskID:       ask.AskID,
		Tool:        ask.Tool,
		Summary:     ask.Summary,
		Class:       ask.Class,
		Reason:      ask.Reason,
		RepeatCount: ask.RepeatCount,
	}
	if err := s.appendRecord(ctx, rec, run); err != nil && !errors.Is(err, errCallRecordSealed) {
		s.addCandidate(run, storeErrorCandidate(err))
	}
	if s.runSealed(run) {
		return
	}
	payload := map[string]any{
		"run_id":  run.id,
		"ask_id":  ask.AskID,
		"tool":    ask.Tool,
		"summary": ask.Summary,
		"class":   string(ask.Class),
		"reason":  ask.Reason,
	}
	if ask.RepeatCount != nil {
		payload["repeat_count"] = map[string]any{
			"exact": ask.RepeatCount.Exact,
			"near":  ask.RepeatCount.Near,
		}
	}
	s.events.emit(EventPermissionAsked, payload)
}

// reportPermissionAnswered records an answer and resolves its ask once.
//
// An answer for an ask that is already resolved, or that arrives after
// the run is sealed, is inert: the first resolution wins.
func (s *Session) reportPermissionAnswered(run *runState, answer PermissionAnswer) {
	if run == nil {
		return
	}
	s.mu.Lock()
	if run.sealed || run.committed {
		s.mu.Unlock()
		return
	}
	var pending *pendingAsk
	if answer.AskID != "" {
		held, found := run.asks[answer.AskID]
		if !found || held.resolved {
			s.mu.Unlock()
			return
		}
		held.resolved = true
		pending = held
	}
	ctx, release, ok := s.beginRunWriteLocked(run)
	s.mu.Unlock()
	if !ok {
		return
	}
	defer release()
	rec := &PermissionAnsweredRecord{
		RecordMeta: NewRecordMeta("permission.answered"),
		RunID:      run.id,
		AskID:      answer.AskID,
		Decision:   answer.Decision,
		AnsweredBy: answer.AnsweredBy,
		Provenance: answer.Provenance,
		Reason:     answer.Reason,
	}
	if err := s.appendRecord(ctx, rec, run); err != nil && !errors.Is(err, errCallRecordSealed) {
		s.addCandidate(run, storeErrorCandidate(err))
	}
	if pending != nil {
		s.mu.Lock()
		if _, held := run.asks[answer.AskID]; held {
			pending.answered = true
			delete(run.asks, answer.AskID)
		}
		s.mu.Unlock()
	}
	if s.runSealed(run) {
		return
	}
	s.events.emit(EventPermissionAnswered, map[string]any{
		"run_id":      run.id,
		"ask_id":      answer.AskID,
		"decision":    answer.Decision,
		"answered_by": answer.AnsweredBy,
		"provenance":  answer.Provenance,
		"reason":      answer.Reason,
	})
}

// settlePendingAsks resolves every ask the run left unanswered.
//
// The finalization owns the asks after the seal. Each one is refused
// with the cancelled provenance: a run that is ending never allows a
// call, and an answer that arrives later is inert.
func (s *Session) settlePendingAsks(run *runState) {
	s.mu.Lock()
	pending := make([]PermissionAsk, 0, len(run.asks))
	for id, ask := range run.asks {
		if ask.answered {
			continue
		}
		ask.resolved = true
		ask.answered = true
		pending = append(pending, ask.ask)
		delete(run.asks, id)
	}
	s.mu.Unlock()
	sort.Slice(pending, func(i, j int) bool { return pending[i].AskID < pending[j].AskID })
	for _, ask := range pending {
		rec := &PermissionAnsweredRecord{
			RecordMeta: NewRecordMeta("permission.answered"),
			RunID:      run.id,
			AskID:      ask.AskID,
			Decision:   "deny",
			AnsweredBy: "cancelled",
			Provenance: ProvenanceCancelled,
			Reason:     ask.Reason,
		}
		if err := s.write(run.ctx, rec); err != nil {
			s.addCandidate(run, storeErrorCandidate(err))
		}
		s.events.emit(EventPermissionAnswered, map[string]any{
			"run_id":      run.id,
			"ask_id":      ask.AskID,
			"decision":    "deny",
			"answered_by": "cancelled",
			"provenance":  ProvenanceCancelled,
			"reason":      ask.Reason,
		})
	}
}

// runSealed reports that the finalization has sealed the run.
func (s *Session) runSealed(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return run.sealed
}
