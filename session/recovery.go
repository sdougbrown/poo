package session

import (
	"context"
	"fmt"
	"time"

	"github.com/sdougbrown/poo"
)

// The texts the recovery writes.
//
// A call or a job that was cut short has no outcome, and the model is
// told so: the effect may or may not have happened, and the model
// decides what to do about it.
const (
	// InterruptedCallText is the text of an interrupted tool result.
	InterruptedCallText = "outcome unknown; the process stopped during this call"
	// InterruptedJobText is the text of an interrupted job.
	InterruptedJobText = "outcome unknown; the process stopped during this job"
)

// ReasonInterrupted is the settle reason of a run a crash ended.
//
// The core never produces it: a run that was cut short is marked
// interrupted when the session is loaded.
const ReasonInterrupted poo.Reason = "interrupted"

// Pending is the set of operations a session has not resolved.
//
// It is what a recovery resolves: a run that started and never settled,
// a tool call with no result, a permission ask with no answer, a job
// that started and never ended, and an input that was queued and never
// reached the model.
type Pending struct {
	// SessionID identifies the session.
	SessionID string
	// Runs are the runs that started and never settled, in the order
	// they started.
	Runs []PendingRun
	// Jobs are the jobs that started and never ended, in the order
	// they started.
	Jobs []PendingJob
	// Inputs are the inputs that were queued and never delivered or
	// dropped, in the order they were queued.
	Inputs []PendingInput
}

// IsEmpty reports whether there is nothing to resolve.
func (p *Pending) IsEmpty() bool {
	return p == nil || (len(p.Runs) == 0 && len(p.Jobs) == 0 && len(p.Inputs) == 0)
}

// PendingRun is a run that started and never settled.
type PendingRun struct {
	// RunID identifies the run.
	RunID string
	// Calls are the tool calls the run made and never saw the result
	// of, in the order the model made them.
	Calls []PendingCall
	// Asks are the permission asks the run never got an answer to, in
	// the order the run asked.
	Asks []PendingAsk
}

// PendingCall is a tool call with no result.
type PendingCall struct {
	// RunID identifies the run the call belongs to.
	RunID string
	// Turn is the turn the call belongs to.
	Turn int
	// Handle is the call's handle.
	Handle string
	// Tool is the name of the tool the model called.
	Tool string
}

// PendingAsk is a permission ask with no answer.
type PendingAsk struct {
	// RunID identifies the run that is waiting.
	RunID string
	// AskID names the ask.
	AskID string
	// Tool is the name of the tool the model called.
	Tool string
}

// PendingJob is a job that started and never ended.
type PendingJob struct {
	// Job is the job's identifier, e.g. "j3".
	Job string
	// Handle is the handle of the call the job came from.
	Handle string
	// Tool is the name of the tool the job runs.
	Tool string
	// RunID identifies the run that started the job.
	RunID string
}

// PendingInput is an input that never reached the model.
type PendingInput struct {
	// InputID is the input's identifier.
	InputID string
	// RunID identifies the run the input belongs to.
	RunID string
	// Mode is how the input was accepted.
	Mode poo.InputMode
}

// Requeued is a job result that was never delivered.
//
// The result is delivered again when the session runs next: a job's
// outcome is a fact the model must see, and a crash between the job's
// end and its delivery loses nothing.
type Requeued struct {
	// Job is the job's identifier, e.g. "j3".
	Job string
	// InputID identifies the queued result.
	InputID string
	// Handle is the handle of the call the job came from.
	Handle string
	// Tool is the name of the tool the job ran.
	Tool string
	// RunID identifies the run that started the job.
	RunID string
	// Outcome is how the job ended.
	Outcome poo.JobOutcome
	// Text is the result the model reads.
	Text string
}

// RecoveryPolicy resolves a session's pending operations.
//
// v1 has one policy: every pending operation is marked interrupted. A
// durable session would resolve them differently, and the policy is
// where that difference lives.
type RecoveryPolicy interface {
	// Name names the policy.
	Name() string
	// Resolve appends the records that resolve the pending operations.
	//
	// The resolution appends only what is missing, so it is
	// idempotent: a resolution that failed partway is finished by the
	// next one.
	Resolve(ctx context.Context, sink poo.RecordSink, p *Pending) error
}

// InterruptPolicy marks every pending operation as interrupted.
//
// An interrupted tool call is never re-executed: the effect may have
// happened, and only the model can decide what to do about that. An
// unanswered ask is denied, a run is settled as interrupted, a job's
// outcome is unknown, and an input that never reached the model is
// dropped.
type InterruptPolicy struct {
	// Now is the policy's clock. The default is time.Now.
	Now func() time.Time
}

// Name names the policy.
func (InterruptPolicy) Name() string { return "interrupt" }

// Resolve appends the records that resolve the pending operations.
//
// The order is the plan's: a run's results, then its answers, then its
// settle record, then the jobs, then the inputs. The settle record is
// last for its run, so a reader of the log sees the run's outcome after
// everything that led to it.
func (p InterruptPolicy) Resolve(ctx context.Context, sink poo.RecordSink, pend *Pending) error {
	if pend == nil {
		return nil
	}
	now := p.Now
	if now == nil {
		now = time.Now
	}
	at := now().UTC().Format(time.RFC3339Nano)
	for _, run := range pend.Runs {
		for _, call := range run.Calls {
			rec := &poo.ToolResultRecord{
				RecordMeta: poo.NewRecordMeta("tool.result"),
				RunID:      call.RunID,
				Turn:       call.Turn,
				Handle:     call.Handle,
				Tool:       call.Tool,
				Outcome:    poo.OutcomeInterrupted,
				Text:       InterruptedCallText,
			}
			if err := sink.Append(ctx, rec); err != nil {
				return err
			}
		}
		for _, ask := range run.Asks {
			rec := &poo.PermissionAnsweredRecord{
				RecordMeta: poo.NewRecordMeta("permission.answered"),
				RunID:      ask.RunID,
				AskID:      ask.AskID,
				Decision:   "deny",
				AnsweredBy: "recovery",
				Provenance: poo.ProvenanceInterrupted,
			}
			if err := sink.Append(ctx, rec); err != nil {
				return err
			}
		}
		rec := &poo.RunSettled{
			RecordMeta: poo.NewRecordMeta("run.settled"),
			RunID:      run.RunID,
			Reason:     ReasonInterrupted,
			Detail: poo.SettleDetail{
				Source: "recovery",
				Text:   "the process stopped during this run",
			},
			SettledAt: at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	for _, job := range pend.Jobs {
		rec := &poo.JobEnded{
			RecordMeta: poo.NewRecordMeta("job.ended"),
			SessionID:  pend.SessionID,
			Job:        job.Job,
			Handle:     job.Handle,
			Tool:       job.Tool,
			RunID:      job.RunID,
			Outcome:    poo.JobOutcomeInterrupted,
			InputID:    recoveredInputID(pend.SessionID, job.Job),
			Text:       InterruptedJobText,
			EndedAt:    at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	for _, input := range pend.Inputs {
		rec := &poo.InputDropped{
			RecordMeta: poo.NewRecordMeta("input.dropped"),
			SessionID:  pend.SessionID,
			RunID:      input.RunID,
			InputID:    input.InputID,
			Mode:       input.Mode,
			Reason:     ReasonInterrupted,
			DroppedAt:  at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

// recoveredInputID returns the id of the input a recovered job's result
// is queued under.
//
// The id names the recovery, so it cannot be the id of an input the
// session queued itself: the two never collide.
func recoveredInputID(sessionID, job string) string {
	return fmt.Sprintf("%s-ji-recovered-%s", sessionID, job)
}

// pendingOf computes the operations a session has not resolved.
func pendingOf(id string, recs []poo.Record) *Pending {
	state := &pendingState{
		runs:     map[string]*PendingRun{},
		settled:  map[string]bool{},
		results:  map[string]bool{},
		asks:     map[string]*PendingAsk{},
		answered: map[string]bool{},
		jobs:     map[string]poo.JobStarted{},
		jobEnded: map[string]bool{},
		inputs:   map[string]poo.InputQueued{},
		fate:     map[string]bool{},
	}
	for _, rec := range recs {
		switch r := rec.(type) {
		case *poo.RunStarted:
			if _, ok := state.runs[r.RunID]; !ok {
				state.runs[r.RunID] = &PendingRun{RunID: r.RunID}
				state.runOrder = append(state.runOrder, r.RunID)
			}
		case *poo.RunSettled:
			state.settled[r.RunID] = true
		case *poo.ToolCallRecord:
			run := state.run(r.RunID)
			run.Calls = append(run.Calls, PendingCall{
				RunID:  r.RunID,
				Turn:   r.Turn,
				Handle: r.Handle,
				Tool:   r.Tool,
			})
		case *poo.ToolResultRecord:
			state.results[callKey(r.RunID, r.Handle)] = true
		case *poo.PermissionAskedRecord:
			if _, ok := state.asks[r.AskID]; !ok {
				state.asks[r.AskID] = &PendingAsk{RunID: r.RunID, AskID: r.AskID, Tool: r.Tool}
				state.askOrder = append(state.askOrder, r.AskID)
			}
		case *poo.PermissionAnsweredRecord:
			state.answered[r.AskID] = true
		case *poo.JobStarted:
			if _, ok := state.jobs[r.Job]; !ok {
				state.jobs[r.Job] = *r
				state.jobOrder = append(state.jobOrder, r.Job)
			}
		case *poo.JobEnded:
			state.jobEnded[r.Job] = true
		case *poo.InputQueued:
			if _, ok := state.inputs[r.InputID]; !ok {
				state.inputs[r.InputID] = *r
				state.inputOrder = append(state.inputOrder, r.InputID)
			}
		case *poo.InputDelivered:
			state.fate[r.InputID] = true
		case *poo.InputDropped:
			state.fate[r.InputID] = true
		}
	}

	pend := &Pending{SessionID: id}
	for _, runID := range state.runOrder {
		run := state.runs[runID]
		if state.settled[runID] {
			continue
		}
		pending := PendingRun{RunID: runID}
		for _, call := range run.Calls {
			if state.results[callKey(call.RunID, call.Handle)] {
				continue
			}
			pending.Calls = append(pending.Calls, call)
		}
		for _, askID := range state.askOrder {
			ask := state.asks[askID]
			if ask.RunID != runID || state.answered[askID] {
				continue
			}
			pending.Asks = append(pending.Asks, *ask)
		}
		pend.Runs = append(pend.Runs, pending)
	}
	for _, jobID := range state.jobOrder {
		if state.jobEnded[jobID] {
			continue
		}
		job := state.jobs[jobID]
		pend.Jobs = append(pend.Jobs, PendingJob{
			Job:    job.Job,
			Handle: job.Handle,
			Tool:   job.Tool,
			RunID:  job.RunID,
		})
	}
	for _, inputID := range state.inputOrder {
		if state.fate[inputID] {
			continue
		}
		input := state.inputs[inputID]
		pend.Inputs = append(pend.Inputs, PendingInput{
			InputID: input.InputID,
			RunID:   input.RunID,
			Mode:    input.Mode,
		})
	}
	return pend
}

// pendingState collects what the records say about a session's
// unresolved operations.
type pendingState struct {
	// runs are the runs that started, by id.
	runs map[string]*PendingRun
	// runOrder is the order the runs started in.
	runOrder []string
	// settled are the runs that settled.
	settled map[string]bool
	// results are the tool calls that have a result, by run and
	// handle.
	results map[string]bool
	// asks are the permission asks, by id.
	asks map[string]*PendingAsk
	// askOrder is the order the asks were made in.
	askOrder []string
	// answered are the asks that have an answer.
	answered map[string]bool
	// jobs are the jobs that started, by id.
	jobs map[string]poo.JobStarted
	// jobOrder is the order the jobs started in.
	jobOrder []string
	// jobEnded are the jobs that ended.
	jobEnded map[string]bool
	// inputs are the inputs that were queued, by id.
	inputs map[string]poo.InputQueued
	// inputOrder is the order the inputs were queued in.
	inputOrder []string
	// fate are the inputs that were delivered or dropped.
	fate map[string]bool
}

// run returns a run's state, creating it when it is new.
func (s *pendingState) run(id string) *PendingRun {
	run, ok := s.runs[id]
	if !ok {
		run = &PendingRun{RunID: id}
		s.runs[id] = run
		s.runOrder = append(s.runOrder, id)
	}
	return run
}

// callKey returns the key a tool call's result is filed under.
func callKey(runID, handle string) string {
	return runID + "\x00" + handle
}

// requeueOf returns the job results that were never delivered.
//
// The results are in completion order: the order the jobs ended in.
func requeueOf(recs []poo.Record) []Requeued {
	delivered := map[string]bool{}
	for _, rec := range recs {
		if d, ok := rec.(*poo.InputDelivered); ok {
			delivered[d.InputID] = true
		}
	}
	var out []Requeued
	for _, rec := range recs {
		ended, ok := rec.(*poo.JobEnded)
		if !ok {
			continue
		}
		if delivered[ended.InputID] {
			continue
		}
		out = append(out, Requeued{
			Job:     ended.Job,
			InputID: ended.InputID,
			Handle:  ended.Handle,
			Tool:    ended.Tool,
			RunID:   ended.RunID,
			Outcome: ended.Outcome,
			Text:    ended.Text,
		})
	}
	return out
}
