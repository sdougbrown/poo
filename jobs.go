package poo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sdougbrown/poo/prompt"
)

// A job is a background tool call. It belongs to the session, not to the
// run that started it: an interrupt, a cancel and every settle stop a
// run's synchronous work and never its jobs. Only cancel_job, the job
// timeout and closing the session stop a job.
//
// The job's result arrives later as an input, and the input is what the
// model reads.

// JobOptions configure a session's jobs.
//
// Every jobs setting maps to one of these fields, and nothing about jobs
// is hardcoded in the core.
type JobOptions struct {
	// MaxRunning is how many jobs may run at once, per session. Zero
	// means DefaultMaxRunning.
	MaxRunning int
	// Timeout bounds one job. Zero means DefaultJobTimeout.
	Timeout time.Duration
	// OnIdle says what happens to a result that arrives while the
	// session is idle. It defaults to OnIdleWake.
	OnIdle OnIdle
	// MaxUnattendedWakes caps the woken runs that follow one another
	// with no client input in between. Zero means no limit.
	MaxUnattendedWakes int
}

// OnIdle says what a session does with a job result that arrives while
// it is idle.
type OnIdle string

const (
	// OnIdleWake starts a new run with the result.
	OnIdleWake OnIdle = "wake"
	// OnIdleHold holds the result until the next run starts.
	OnIdleHold OnIdle = "hold"
)

// Defaults the plan fixes for jobs.
const (
	// DefaultMaxRunning is how many jobs may run at once, per session.
	DefaultMaxRunning = 4
	// DefaultJobTimeout bounds one job.
	DefaultJobTimeout = 1800 * time.Second
)

// The texts the plan fixes.
const (
	// StartedText is the model-facing text of a call that became a job.
	StartedText = "[started job %s · %s; its result will arrive as a message]"
	// TooManyJobsText is the model-facing text of a call that found no
	// free job slot.
	TooManyJobsText = "too many jobs running; wait or cancel one"
)

// BackgroundArgName is the reserved argument that asks for a background
// call.
//
// The name is reserved: a tool whose own schema defines it cannot be
// registered, because the core would not know whether the model meant the
// tool's argument or the core's.
const BackgroundArgName = "background"

// JobOutcome is how a job ended.
type JobOutcome string

const (
	// JobOutcomeOK means the job finished its work.
	JobOutcomeOK JobOutcome = "ok"
	// JobOutcomeError means the job failed, or ran out of time.
	JobOutcomeError JobOutcome = "error"
	// JobOutcomeCancelled means the job was stopped on purpose: by
	// cancel_job, by the client, or by the session closing.
	JobOutcomeCancelled JobOutcome = "cancelled"
	// JobOutcomeInterrupted means the job's outcome is unknown: the
	// process stopped while the job ran.
	JobOutcomeInterrupted JobOutcome = "interrupted"
)

// JobCause says why a job ended, when the ending was not the job's own
// doing.
type JobCause string

const (
	// CauseSessionClosed means the session closed while the job ran.
	CauseSessionClosed JobCause = "session_closed"
)

// The reasons a result is held.
const (
	// HeldOnIdle means the results were held because the session was
	// idle and the jobs setting says to hold them.
	HeldOnIdle = "on_idle"
	// HeldWakeLimit means the results were held because the session had
	// taken as many unattended wakes as it may.
	HeldWakeLimit = "wake_limit"
	// HeldSessionClosed means the results were held because the session
	// closed.
	HeldSessionClosed = "session_closed"
)

// JobStarted records that a job was accepted and started.
//
// The record is the acceptance: the job, the call it came from, the run
// that started it and the tool it runs.
type JobStarted struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Job is the job's identifier, e.g. "j3".
	Job string `json:"job"`
	// Handle is the handle of the tool call the job came from, e.g.
	// "r1". The call returned at once with a "started" outcome.
	Handle string `json:"handle"`
	// Tool is the name of the tool the job runs.
	Tool string `json:"tool"`
	// RunID identifies the run that started the job.
	RunID string `json:"run_id"`
	// StartedAt is the RFC 3339 start time in UTC.
	StartedAt string `json:"started_at"`
}

// JobEnded records that a job finished.
//
// The record carries the result the model will read, so a held result
// survives a crash and a load can deliver it.
type JobEnded struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Job is the job's identifier, e.g. "j3".
	Job string `json:"job"`
	// Handle is the handle of the tool call the job came from.
	Handle string `json:"handle"`
	// Tool is the name of the tool the job ran.
	Tool string `json:"tool"`
	// RunID identifies the run that started the job.
	RunID string `json:"run_id"`
	// Outcome is how the job ended.
	Outcome JobOutcome `json:"outcome"`
	// Cause says why the job ended, when something else ended it.
	Cause JobCause `json:"cause,omitempty"`
	// TimedOut reports that the job hit the jobs timeout.
	TimedOut bool `json:"timed_out,omitempty"`
	// InputID identifies the queued result. It is the id the delivery
	// records carry.
	InputID string `json:"input_id"`
	// Text is the result the model reads.
	Text string `json:"text"`
	// EndedAt is the RFC 3339 end time in UTC.
	EndedAt string `json:"ended_at"`
}

// InputHeld records that job results were held instead of delivered.
//
// The results are not lost: a later run delivers them, or a load
// recovers them.
type InputHeld struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run the results were held for, empty when
	// the session had no run.
	RunID string `json:"run_id,omitempty"`
	// Jobs are the jobs whose results were held.
	Jobs []string `json:"jobs"`
	// Reason says why they were held: "on_idle", "wake_limit" or
	// "session_closed".
	Reason string `json:"reason"`
	// HeldAt is the RFC 3339 time the results were held, in UTC.
	HeldAt string `json:"held_at"`
}

// jobState is one job the session knows about.
type jobState struct {
	// id is the job's identifier, e.g. "j3".
	id string
	// handle is the handle of the tool call the job came from.
	handle string
	// tool is the name of the tool the job runs.
	tool string
	// runID identifies the run that started the job.
	runID string
	// summary describes the call, for the jobs tool.
	summary string
	// startedAt is when the job started.
	startedAt time.Time
	// ctx is the job's context. It comes from the session, so an
	// interrupt, a cancel and a settle never stop the job.
	ctx context.Context
	// cancel stops the job.
	cancel context.CancelCauseFunc
	// done is closed when the job's goroutine has finished.
	done chan struct{}
	// ended reports that the job has a job.ended record.
	ended bool
	// partialText is what the job's tool had produced when it stopped.
	// The close keeps it in the job.ended record it writes.
	partialText string
}

// jobResult is a job's finished result, waiting for its delivery.
type jobResult struct {
	// id is the input id the result is delivered under.
	id string
	// job is the job the result came from.
	job string
	// handle is the handle of the tool call the job came from.
	handle string
	// tool is the name of the tool the job ran.
	tool string
	// runID identifies the run that started the job.
	runID string
	// outcome is how the job ended.
	outcome JobOutcome
	// text is the result the model reads.
	text string
	// fatal reports that the job failed with an error that wraps
	// ErrFatal. The receiving run settles tool_fatal.
	fatal bool
}

// jobQueueItem is a queued result, in completion order.
type jobQueueItem struct {
	// result is the result the model will read.
	result jobResult
	// held reports that the result is waiting for the next run.
	held bool
}

// WithJobs sets the session's job configuration.
//
// The option is the whole configuration: nothing about jobs is hardcoded,
// and a session without one gets the plan's defaults.
func WithJobs(o JobOptions) Option {
	return func(opts *Options) {
		opts.Jobs = o
	}
}

// startJob turns a prepared call into a background job.
//
// The slot is reserved and the job is registered under the session lock,
// so two calls that race for the last slot admit exactly one of them.
// The job.started record is written before the goroutine starts, so a
// log shows the job was accepted even when it never ran.
func (s *Session) startJob(run *runState, handle, tool string, prepared Prepared, summary string) (string, bool, bool) {
	// The slot is reserved under the lock, and the job is registered
	// there too, so a job that is admitted is a job the session knows
	// about. A run that is winding down admits nothing: no new effect
	// starts after the finalization has begun.
	s.mu.Lock()
	if run.committed || run.finalizing {
		s.mu.Unlock()
		return "", false, true
	}
	if s.closed || s.broken {
		s.mu.Unlock()
		return "", false, false
	}
	if s.runningJobCountLocked() >= s.jobLimit() {
		s.mu.Unlock()
		return "", false, false
	}
	s.jobSeq++
	jobID := fmt.Sprintf("j%d", s.jobSeq)
	ctx, cancel := context.WithCancelCause(s.jobCtx)
	job := &jobState{
		id:        jobID,
		handle:    handle,
		tool:      tool,
		runID:     run.id,
		summary:   summary,
		startedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	s.jobs[jobID] = job
	s.jobOrder = append(s.jobOrder, jobID)
	// The acceptance record is tracked work: the finalization waits for
	// it, because a job that is accepted must have its record before
	// the run settles.
	run.jobStarts++
	s.mu.Unlock()

	// The acceptance record is written before the goroutine starts. A
	// write the run's finalization abandons is not a store failure: the
	// run settled while the record was in flight.
	err := s.writeDelivery(run.ctx, run, &JobStarted{
		RecordMeta: NewRecordMeta("job.started"),
		SessionID:  s.opts.SessionID,
		Job:        jobID,
		Handle:     handle,
		Tool:       tool,
		RunID:      run.id,
		StartedAt:  nowRFC3339(),
	})
	s.mu.Lock()
	winding := run.committed || run.finalizing
	s.mu.Unlock()
	if err != nil {
		s.mu.Lock()
		run.jobStarts--
		s.mu.Unlock()
		s.signalWorkLocked(run)
		s.removeJobLocked(jobID)
		cancel(errFinalizing)
		if errors.Is(err, errDeliveryAbandoned) || winding {
			return "", false, true
		}
		s.addCandidate(run, storeErrorCandidate(err))
		return "", false, false
	}
	if winding {
		// The run went down while the acceptance was being recorded, so
		// the job never launches. It was accepted, so it still gets its
		// terminal outcome. The acceptance claim is held until the
		// terminal record is written, so the finalization cannot commit
		// before the job has one.
		s.finishUnlaunchedJob(job)
		s.mu.Lock()
		run.jobStarts--
		s.mu.Unlock()
		s.signalWorkLocked(run)
		cancel(errFinalizing)
		return jobID, false, true
	}
	s.mu.Lock()
	run.jobStarts--
	s.mu.Unlock()
	s.signalWorkLocked(run)

	go s.runJob(job, prepared)
	return jobID, true, false
}

// finishUnlaunchedJob gives an accepted job that never started its
// terminal record.
//
// The job is stopped, and its outcome is cancelled: it never ran, so
// there is no result to deliver. The record is what makes the job's
// lifecycle complete.
func (s *Session) finishUnlaunchedJob(job *jobState) {
	s.mu.Lock()
	if job.ended {
		s.mu.Unlock()
		return
	}
	job.ended = true
	s.mu.Unlock()

	err := s.write(context.WithoutCancel(job.ctx), &JobEnded{
		RecordMeta: NewRecordMeta("job.ended"),
		SessionID:  s.opts.SessionID,
		Job:        job.id,
		Handle:     job.handle,
		Tool:       job.tool,
		RunID:      job.runID,
		Outcome:    JobOutcomeCancelled,
		Text:       "the run settled before the job started",
		EndedAt:    nowRFC3339(),
	})
	if err != nil {
		s.markBroken()
		s.addStoreError(job.runID)
	}
	s.events.emit(EventJobEnded, map[string]any{
		"job":     job.id,
		"handle":  job.handle,
		"tool":    job.tool,
		"outcome": string(JobOutcomeCancelled),
	})
	close(job.done)
}

// removeJobLocked forgets a job that never ran.
//
// The caller holds the session's lock.
func (s *Session) removeJobLocked(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, jobID)
	for i, id := range s.jobOrder {
		if id == jobID {
			s.jobOrder = append(s.jobOrder[:i], s.jobOrder[i+1:]...)
			return
		}
	}
}

// runningJobCountLocked returns how many jobs are running.
//
// The caller holds the session's lock.
func (s *Session) runningJobCountLocked() int {
	n := 0
	for _, id := range s.jobOrder {
		if job, ok := s.jobs[id]; ok && !job.ended {
			n++
		}
	}
	return n
}

// jobLimit returns how many jobs may run at once.
func (s *Session) jobLimit() int {
	limit := s.opts.Jobs.MaxRunning
	if limit <= 0 {
		limit = DefaultMaxRunning
	}
	return limit
}

// jobTimeout returns the bound on one job.
func (s *Session) jobTimeout() time.Duration {
	timeout := s.opts.Jobs.Timeout
	if timeout <= 0 {
		timeout = DefaultJobTimeout
	}
	return timeout
}

// onIdle returns what the session does with a result that arrives while
// it is idle.
func (s *Session) onIdle() OnIdle {
	if s.opts.Jobs.OnIdle == OnIdleHold {
		return OnIdleHold
	}
	return OnIdleWake
}

// runJob runs one job and finishes it.
//
// The job runs under the session's context, so it survives the turn, the
// interrupt and the settle. It stops when the job's timeout fires, when
// it is cancelled, or when the session closes.
func (s *Session) runJob(job *jobState, prepared Prepared) {
	defer close(job.done)

	tool, ok := s.opts.Tools[job.tool]
	if !ok {
		s.finishJob(job, OutcomeError, "the tool is gone", false, false)
		return
	}

	ctx, cancel := context.WithTimeout(job.ctx, s.jobTimeout())
	defer cancel()

	env := s.jobToolEnv(job)
	result, err := tool.Call(ctx, prepared, env)
	// The partial output is kept even when the close writes the record:
	// the close stopped the job, but what it produced is still its
	// result.
	s.mu.Lock()
	job.partialText = result.Text
	s.mu.Unlock()
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	// A fatal error means the environment is unusable. The failure
	// belongs to the job, and the receiving run settles tool_fatal.
	fatal := err != nil && errors.Is(err, ErrFatal)

	// A job that the session closed stops is not recorded here: the
	// close writes the record with its cause, so a job gets exactly
	// one job.ended.
	if s.jobCtx.Err() != nil {
		return
	}

	// The job's context decides a timeout or a cancellation before a
	// nil error can call the job successful: a cooperative tool returns
	// its partial output and no error when it is stopped.
	switch {
	case timedOut:
		// The job ran out of time. The outcome is an error with the
		// timeout flag, and the partial output is the result.
		s.finishJob(job, OutcomeError, stoppedText(result.Text, err, "the job ran out of time"), true, fatal)
	case errors.Is(ctx.Err(), context.Canceled):
		// The job was stopped on purpose.
		s.finishJob(job, OutcomeCancelled, stoppedText(result.Text, err, "the job was cancelled"), false, fatal)
	case err == nil:
		s.finishJob(job, OutcomeOK, result.Text, false, false)
	case errors.Is(err, context.Canceled):
		s.finishJob(job, OutcomeCancelled, stoppedText(result.Text, err, "the job was cancelled"), false, fatal)
	default:
		s.finishJob(job, OutcomeError, err.Error(), false, fatal)
	}
}

// stoppedText returns the partial output a stopped job produced, and the
// reason when it produced none.
func stoppedText(partial string, err error, reason string) string {
	if partial != "" {
		return partial
	}
	if err != nil {
		return err.Error()
	}
	return reason
}

// jobToolEnv builds the environment a job runs in.
//
// The environment names the job, so a tool can tell a job from a call in
// the turn.
func (s *Session) jobToolEnv(job *jobState) ToolEnv {
	env := s.toolEnvFor(job.runID, job.handle)
	env.Job = job.id
	return env
}

// finishJob records a job's outcome and queues its result.
//
// The result is queued on the session's job queue, and the queue is
// acted on by the delivery rules: a run that is open takes it at the
// next turn end, and an idle session wakes or holds it.
func (s *Session) finishJob(job *jobState, outcome Outcome, text string, timedOut, fatal bool) {
	s.mu.Lock()
	if job.ended {
		s.mu.Unlock()
		return
	}
	job.ended = true
	jobOutcome := jobOutcomeOf(outcome)
	s.inputSeq++
	inputID := fmt.Sprintf("%s-ji%d", s.opts.SessionID, s.inputSeq)
	runID := job.runID
	handle := job.handle
	tool := job.tool
	s.mu.Unlock()

	// The hook-processed result is what gets recorded and emitted, so a
	// held result never leaks what a hook would have redacted. The
	// after-tool chain is Stage 4's; the core records the tool's own
	// result for now.
	if err := s.write(context.WithoutCancel(job.ctx), &JobEnded{
		RecordMeta: NewRecordMeta("job.ended"),
		SessionID:  s.opts.SessionID,
		Job:        job.id,
		Handle:     handle,
		Tool:       tool,
		RunID:      runID,
		Outcome:    jobOutcome,
		TimedOut:   timedOut,
		InputID:    inputID,
		Text:       text,
		EndedAt:    nowRFC3339(),
	}); err != nil {
		// The store refused the job's end. The session is broken at
		// once: the wakes stop, and the run that is still open gets
		// the store_error candidate.
		s.markBroken()
		s.addStoreError(job.runID)
		return
	}
	s.events.emit(EventJobEnded, map[string]any{
		"job":      job.id,
		"handle":   handle,
		"tool":     tool,
		"outcome":  string(jobOutcome),
		"input_id": inputID,
	})

	s.queueJobResult(jobResult{
		id:      inputID,
		job:     job.id,
		handle:  handle,
		tool:    tool,
		runID:   runID,
		outcome: jobOutcome,
		text:    text,
		fatal:   fatal,
	})
}

// addStoreError gives the open run the store_error candidate.
//
// The run that owns the job may already have settled, and then the
// candidate is dropped: a settled run is never mutated. The candidate
// goes through finalize, so a waiting run is woken and settles instead
// of waiting for a job that will never deliver.
func (s *Session) addStoreError(runID string) {
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	if run == nil || run.id != runID {
		return
	}
	s.finalize(run, newCandidate(ReasonStoreError, SettleDetail{
		Source: "store",
		Text:   "the store refused the job's end",
	}))
}

// jobOutcomeOf maps a call outcome to a job outcome.
func jobOutcomeOf(outcome Outcome) JobOutcome {
	switch outcome {
	case OutcomeOK:
		return JobOutcomeOK
	case OutcomeCancelled:
		return JobOutcomeCancelled
	case OutcomeInterrupted:
		return JobOutcomeInterrupted
	default:
		return JobOutcomeError
	}
}

// queueJobResult puts a finished result on the session's job queue and
// acts on it.
//
// The queue is the session's, not the run's: a settling never drops a
// queued result, and an idle session either wakes a run with it or holds
// it for the next one.
func (s *Session) queueJobResult(result jobResult) {
	s.mu.Lock()
	// A result is never held at queue time: on_idle is the idle row of
	// the delivery table. A run that is open or waiting takes the
	// result; only the idle decision (or the close) holds it.
	s.jobQueue = append(s.jobQueue, jobQueueItem{result: result})
	queued := s.jobQueued
	s.mu.Unlock()

	// The signal tells a test that is waiting at a boundary that the
	// result is queued.
	select {
	case queued <- struct{}{}:
	default:
	}

	s.deliverJobResults()
}

// deliverJobResults acts on the queued results.
//
// The delivery depends on the session's state: an open run takes the
// results at the next turn end, a waiting run takes them at once, and an
// idle session wakes or holds them.
//
// The check and the take are one step under the session lock, so a
// result that arrives at the same moment as the run's entry into the
// waiting state is taken exactly once.
func (s *Session) deliverJobResults() {
	s.mu.Lock()
	switch {
	case s.run == nil:
		s.mu.Unlock()
		s.handleIdleJobResults()
		return
	case s.state == StateWaiting:
		run := s.run
		s.mu.Unlock()
		if !s.drainJobQueue(run) {
			return
		}
		// The run leaves the waiting state and goes to the gate.
		s.signalWake(run)
		return
	default:
		// The run is open or settling. The result is delivered at the
		// next turn end, or after the run has settled.
		s.mu.Unlock()
	}
}

// handleIdleJobResults wakes or holds the results that arrived while the
// session was idle.
func (s *Session) handleIdleJobResults() {
	s.mu.Lock()
	if s.run != nil || s.reserved || s.closed || s.broken {
		s.mu.Unlock()
		return
	}
	if len(s.jobQueue) == 0 {
		s.mu.Unlock()
		return
	}
	if s.onIdle() == OnIdleHold {
		s.holdJobResultsLocked(HeldOnIdle)
		s.mu.Unlock()
		s.writePendingHeld()
		return
	}
	// The wake limit counts the woken runs that follow one another with
	// no client input in between.
	if s.opts.Jobs.MaxUnattendedWakes > 0 && s.unattendedWakes >= s.opts.Jobs.MaxUnattendedWakes {
		s.holdJobResultsLocked(HeldWakeLimit)
		s.mu.Unlock()
		s.writePendingHeld()
		return
	}
	// The reservation is taken under the lock, so a wake and a
	// concurrent prompt race for one reservation: the loser of a prompt
	// race gets busy, and nothing is accepted.
	s.reserved = true
	s.mu.Unlock()

	// The seam the race is tested at: the wake has the session's
	// reservation, and a prompt that arrives now is refused.
	s.raceHook("wake")

	// Every result that is queued at this moment joins the woken run.
	// A held result waits for the next client prompt, so it does not
	// join the wake.
	s.mu.Lock()
	var results []jobResult
	kept := s.jobQueue[:0]
	for _, item := range s.jobQueue {
		if item.held {
			kept = append(kept, item)
			continue
		}
		results = append(results, item.result)
	}
	s.jobQueue = kept
	if len(results) == 0 {
		s.reserved = false
		s.mu.Unlock()
		return
	}
	s.unattendedWakes++
	s.mu.Unlock()

	// The woken run is a normal run: it starts with the results, and
	// the first request goes through the gate.
	_, _ = s.start(context.Background(), "", TriggerJob, nil, results...)

	s.mu.Lock()
	s.reserved = false
	s.mu.Unlock()
}

// holdJobResultsLocked marks the queued results as held.
//
// The caller holds the session's lock. The record is written after the
// lock is released, because the write takes it too.
func (s *Session) holdJobResultsLocked(reason string) {
	if len(s.jobQueue) == 0 {
		return
	}
	jobs := make([]string, 0, len(s.jobQueue))
	for i := range s.jobQueue {
		s.jobQueue[i].held = true
		jobs = append(jobs, s.jobQueue[i].result.job)
	}
	runID := ""
	if s.run != nil {
		runID = s.run.id
	}
	s.pendingHeld = &InputHeld{
		RecordMeta: NewRecordMeta("input.held"),
		SessionID:  s.opts.SessionID,
		RunID:      runID,
		Jobs:       jobs,
		Reason:     reason,
		HeldAt:     nowRFC3339(),
	}
}

// writePendingHeld writes the held record, if there is one.
//
// The write happens outside the session's lock, because the write takes
// it too.
func (s *Session) writePendingHeld() {
	s.mu.Lock()
	held := s.pendingHeld
	s.pendingHeld = nil
	s.mu.Unlock()
	if held == nil {
		return
	}
	_ = s.write(context.Background(), held)
	s.events.emit(EventInputHeld, map[string]any{
		"jobs":   held.Jobs,
		"reason": held.Reason,
	})
}

// takeHeldResultsLocked takes the results that were held for the next
// run.
//
// The caller holds the session's lock.
func (s *Session) takeHeldResultsLocked() []jobResult {
	var out []jobResult
	kept := s.jobQueue[:0]
	for _, item := range s.jobQueue {
		if item.held {
			out = append(out, item.result)
			continue
		}
		kept = append(kept, item)
	}
	s.jobQueue = kept
	return out
}

// jobOutcomeToCallOutcome maps a job outcome to the outcome a delivered
// result carries.
func jobOutcomeToCallOutcome(outcome JobOutcome) Outcome {
	switch outcome {
	case JobOutcomeOK:
		return OutcomeOK
	case JobOutcomeCancelled:
		return OutcomeCancelled
	case JobOutcomeInterrupted:
		return OutcomeInterrupted
	default:
		return OutcomeError
	}
}

// deliverJobResult appends one job result to the model's view.
//
// The result is a user-role message with origin "job", because the
// call's result slot already holds the "started" result. It never
// carries a delivery note.
//
// The delivery is tracked like an input's, so the finalization waits for
// a write that is in flight. A result the finalization abandons goes
// back on the session's queue, because settling never drops a job
// result.
func (s *Session) deliverJobResult(run *runState, result jobResult) bool {
	item := jobQueueItem{result: result}
	s.mu.Lock()
	run.deliveringJobs = append(run.deliveringJobs, item)
	s.mu.Unlock()
	return s.deliverTrackedJobResult(run, item)
}

// deliverTrackedJobResult delivers a result whose in-flight claim is
// already recorded on the run.
//
// The records come before the history append, so a delivery the
// finalization abandons does not mutate the view. A record the store
// refuses is a store_error candidate, the result goes back on the queue,
// and the drain stops there.
//
// A result that carries a fatal error adds the tool_fatal candidate to
// the receiving run, so the run settles before it makes another
// request.
func (s *Session) deliverTrackedJobResult(run *runState, item jobQueueItem) bool {
	result := item.result
	s.mu.Lock()
	if run.committed || run.finalizing {
		s.abandonJobDeliveryLocked(run, item)
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	msg := NewTextMessage(RoleUser, jobResultText(result))
	msg.Origin = OriginJob
	if err := s.writeDelivery(run.ctx, run, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", msg.Origin),
		RunID:      run.id,
		Role:       msg.Role,
		Content:    msg.Content,
	}); err != nil {
		s.failJobDelivery(run, item, err)
		return false
	}
	if err := s.writeDelivery(run.ctx, run, &InputDelivered{
		RecordMeta:  NewRecordMeta("input.delivered"),
		SessionID:   s.opts.SessionID,
		RunID:       run.id,
		InputID:     result.id,
		Mode:        InputModeJob,
		DeliveredAt: nowRFC3339(),
	}); err != nil {
		s.failJobDelivery(run, item, err)
		return false
	}
	s.appendMessages(msg)
	s.finishJobDelivery(run, item)
	s.events.emit(EventInputDelivered, map[string]any{
		"run_id":   run.id,
		"input_id": result.id,
		"mode":     string(InputModeJob),
	})
	if result.fatal {
		s.addCandidate(run, newCandidate(ReasonToolFatal, SettleDetail{
			Source: "tool:" + result.tool,
			Text:   result.text,
		}))
	}
	return true
}

// failJobDelivery puts a result whose delivery failed back on the queue
// and reports a refused write.
//
// A write the finalization abandoned is not a store error: the result
// simply goes back on the queue.
func (s *Session) failJobDelivery(run *runState, item jobQueueItem, err error) {
	s.mu.Lock()
	s.abandonJobDeliveryLocked(run, item)
	s.mu.Unlock()
	if !errors.Is(err, errDeliveryAbandoned) {
		s.addCandidate(run, storeErrorCandidate(err))
	}
}

// finishJobDelivery releases a delivered result's in-flight claim.
func (s *Session) finishJobDelivery(run *runState, item jobQueueItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeJobDeliveryLocked(run, item)
	s.signalWorkLocked(run)
}

// abandonJobDeliveryLocked puts an in-flight result back at the front of
// the session's queue and releases its claim.
//
// The caller holds the session's lock.
func (s *Session) abandonJobDeliveryLocked(run *runState, item jobQueueItem) {
	s.removeJobDeliveryLocked(run, item)
	s.jobQueue = append([]jobQueueItem{item}, s.jobQueue...)
	s.signalWorkLocked(run)
}

// removeJobDeliveryLocked drops a result's in-flight claim.
//
// The caller holds the session's lock.
func (s *Session) removeJobDeliveryLocked(run *runState, item jobQueueItem) {
	for i := range run.deliveringJobs {
		if run.deliveringJobs[i].result.id == item.result.id {
			run.deliveringJobs = append(run.deliveringJobs[:i], run.deliveringJobs[i+1:]...)
			return
		}
	}
}

// jobResultText renders the text the model reads for a job result.
func jobResultText(result jobResult) string {
	return fmt.Sprintf("[job %s · %s finished (%s): %s]", result.job, result.tool, result.outcome, result.text)
}

// publicJobResults turns the internal results into the public ones the
// run.started record carries.
func publicJobResults(results []jobResult) []JobResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]JobResult, 0, len(results))
	for _, r := range results {
		out = append(out, JobResult{
			Job:     r.job,
			Handle:  r.handle,
			Tool:    r.tool,
			Text:    r.text,
			Outcome: jobOutcomeToCallOutcome(r.outcome),
			Fatal:   r.fatal,
		})
	}
	return out
}

// drainJobQueue delivers the queued results to an open run.
//
// The drain happens at the turn end, after the steer queue. The result is
// taken under the same lock as the queue and tied to the run, so the
// finalization can wait for the delivery or put it back. A drain that is
// interrupted or fails to write leaves the results queued, in order. A
// held result waits for the next client prompt, so it is skipped here.
func (s *Session) drainJobQueue(run *runState) bool {
	delivered := false
	for {
		s.mu.Lock()
		item, ok := s.takeJobResultLocked(run)
		s.mu.Unlock()
		if !ok {
			break
		}
		delivered = true
		if !s.deliverTrackedJobResult(run, item) {
			return delivered
		}
	}
	return delivered
}

// takeJobResultLocked takes the first unheld result and ties it to the
// run.
//
// The caller holds the session's lock.
func (s *Session) takeJobResultLocked(run *runState) (jobQueueItem, bool) {
	if run.committed || run.finalizing {
		return jobQueueItem{}, false
	}
	for i, item := range s.jobQueue {
		if item.held {
			continue
		}
		s.jobQueue = append(s.jobQueue[:i], s.jobQueue[i+1:]...)
		run.deliveringJobs = append(run.deliveringJobs, item)
		return item, true
	}
	return jobQueueItem{}, false
}

// jobsRunningFor returns the jobs a run started that are still running.
//
// The jobs of an earlier run never hold a run open, so the list is the
// jobs the run itself started.
func (s *Session) jobsRunningFor(runID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, id := range s.jobOrder {
		job, ok := s.jobs[id]
		if !ok || job.ended {
			continue
		}
		if job.runID != runID {
			continue
		}
		out = append(out, job.id)
	}
	return out
}

// runningJobIDs returns every job that is still running, in start order.
func (s *Session) runningJobIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, id := range s.jobOrder {
		job, ok := s.jobs[id]
		if !ok || job.ended {
			continue
		}
		out = append(out, job.id)
	}
	return out
}

// runHasRunningJob reports that a job the run started is still running.
//
// Only the run's own jobs hold it open: the jobs of an earlier run wake
// the session or are held, and they never keep a run open.
func (s *Session) runHasRunningJob(run *runState) bool {
	return len(s.jobsRunningFor(run.id)) > 0
}

// enterWaiting moves the run into the waiting state.
//
// The check is made under the session lock: if the steer or the job
// queue is non-empty, the queues are drained, the messages are
// appended, and the run goes to the gate instead of waiting.
//
// The call reports whether the run is waiting, and whether a queued
// message was drained instead, in which case the run goes to the gate.
func (s *Session) enterWaiting(run *runState) (waiting, drained bool) {
	// The seam the race is tested at: the drain has happened, and the
	// run has not waited yet.
	s.raceHook("waiting")

	s.mu.Lock()
	if s.run != run {
		s.mu.Unlock()
		// The run is no longer the session's: it is winding down, and
		// the loop's next pass sees it.
		return false, true
	}
	if s.hasDrainableInputLocked() || len(s.jobQueue) > 0 {
		s.mu.Unlock()
		// A message is waiting, so the run does not wait: the queues
		// are drained and the run goes to the gate.
		s.drainInputs(run, false)
		return false, true
	}
	// The transition and the check are one step, so a result that
	// arrives at the same time either finds the run waiting and wakes
	// it, or is seen here and drained.
	s.transitionLocked(StateWaiting)
	// A wake that arrived before the run waited is stale, and it is
	// dropped here: the run must wait for something new.
	select {
	case <-run.wake:
	default:
	}
	s.mu.Unlock()
	return true, false
}

// waitWhileWaiting blocks while the run is waiting.
//
// A waiting run has nothing to do: no request, no tool and no hook. It
// wakes when a job result arrives, when the client sends a message, or
// when the run is cancelled.
//
// The call reports that the run is still waiting, so the loop goes
// round again.
func (s *Session) waitWhileWaiting(run *runState) bool {
	s.mu.Lock()
	if s.state != StateWaiting || s.run != run {
		s.mu.Unlock()
		return false
	}
	wake := run.wake
	s.mu.Unlock()

	select {
	case <-wake:
	case <-run.ctx.Done():
	}
	s.mu.Lock()
	if s.run == run && s.state == StateWaiting {
		// The run leaves the waiting state and goes to the gate.
		s.transitionLocked(StateRequesting)
	}
	s.mu.Unlock()
	return false
}

// signalWake wakes a waiting run.
//
// The signal is what tells the run's goroutine that something arrived
// while it waited. A run that is not waiting drops the signal, so a
// stale one never makes a run leave the waiting state for nothing.
func (s *Session) signalWake(run *runState) {
	if run == nil {
		return
	}
	s.mu.Lock()
	if s.run != run || s.state != StateWaiting {
		s.mu.Unlock()
		return
	}
	wake := run.wake
	s.mu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// deliverWaitingInput delivers a message to a waiting run.
//
// A waiting run has nothing to abort, so a steer and an interrupt are
// the same there: the message is delivered at once and the run goes to
// the gate. The jobs keep running.
func (s *Session) deliverWaitingInput(run *runState, input queuedInput) {
	s.mu.Lock()
	if s.run != run || s.state != StateWaiting {
		s.mu.Unlock()
		return
	}
	// The message was queued with the other steers; it is taken out
	// again so it is delivered exactly once, and tied to the run so the
	// finalization waits for the delivery or reports it.
	for i, queued := range s.steerQueue {
		if queued.id == input.id {
			s.steerQueue = append(s.steerQueue[:i], s.steerQueue[i+1:]...)
			run.delivering = append(run.delivering, input)
			break
		}
	}
	s.mu.Unlock()

	s.deliverInput(run, input)
	s.signalWake(run)
}

// cancelJob stops one job.
//
// The job's partial output is delivered like any result. A job that is
// already finished is not touched.
func (s *Session) cancelJob(jobID string) error {
	s.mu.Lock()
	job, ok := s.jobs[jobID]
	if !ok || job.ended {
		s.mu.Unlock()
		return ErrUnknownJob
	}
	cancel := job.cancel
	s.mu.Unlock()
	cancel(ErrJobCancelled)
	return nil
}

// CancelJob stops one of the session's jobs.
//
// The job's partial output is delivered like any result. A job that is
// unknown or already finished is reported as `ErrUnknownJob`, and
// stopping it twice is not an error the caller has to handle.
func (s *Session) CancelJob(jobID string) error {
	return s.cancelJob(jobID)
}

// ErrUnknownJob is the error a cancel of an unknown job returns.
var ErrUnknownJob = errors.New("poo: there is no such job")

// ErrJobCancelled is the cancellation cause a cancelled job carries.
var ErrJobCancelled = errors.New("poo: the job was cancelled")

// closeJobs stops every running job and waits for the teardown.
//
// A job that has no job.ended gets one, with the outcome cancelled and
// the cause session_closed, and the queued results are recorded as held:
// they are delivered by a later load, never in the closing process.
func (s *Session) closeJobs() {
	s.mu.Lock()
	jobs := make([]*jobState, 0, len(s.jobs))
	for _, id := range s.jobOrder {
		if job, ok := s.jobs[id]; ok {
			jobs = append(jobs, job)
		}
	}
	s.mu.Unlock()

	// The session's job context is cancelled, so every job stops.
	if s.jobCancel != nil {
		s.jobCancel(ErrJobCancelled)
	}

	// The teardown is bounded once for the whole wait, so a job that
	// ignores its context never holds the close up, however many jobs
	// are still running.
	teardown, stopTeardown := context.WithTimeout(context.Background(), recordWriteBudget)
	defer stopTeardown()
	for _, job := range jobs {
		select {
		case <-job.done:
		case <-teardown.Done():
		}
	}

	// Every job with no job.ended gets one, carrying its partial
	// output.
	for _, job := range jobs {
		s.writeJobEndedOnClose(job)
	}

	// The queued results are recorded as held.
	s.mu.Lock()
	if len(s.jobQueue) > 0 {
		s.holdJobResultsLocked(HeldSessionClosed)
	}
	s.mu.Unlock()
	s.writePendingHeld()
}

// writeJobEndedOnClose writes the job.ended record of a job that the
// close stopped.
func (s *Session) writeJobEndedOnClose(job *jobState) {
	s.mu.Lock()
	if job.ended {
		s.mu.Unlock()
		return
	}
	job.ended = true
	s.inputSeq++
	inputID := fmt.Sprintf("%s-ji%d", s.opts.SessionID, s.inputSeq)
	handle := job.handle
	tool := job.tool
	runID := job.runID
	partial := job.partialText
	s.mu.Unlock()

	text := "the session closed while the job was running"
	if partial != "" {
		text = partial + "\n" + text
	}
	s.write(context.Background(), &JobEnded{
		RecordMeta: NewRecordMeta("job.ended"),
		SessionID:  s.opts.SessionID,
		Job:        job.id,
		Handle:     handle,
		Tool:       tool,
		RunID:      runID,
		Outcome:    JobOutcomeCancelled,
		Cause:      CauseSessionClosed,
		InputID:    inputID,
		Text:       text,
		EndedAt:    nowRFC3339(),
	})
	s.events.emit(EventJobEnded, map[string]any{
		"job":      job.id,
		"handle":   handle,
		"tool":     tool,
		"outcome":  string(JobOutcomeCancelled),
		"cause":    string(CauseSessionClosed),
		"input_id": inputID,
	})
}

// jobSummary describes one job, for the jobs tool.
type jobSummary struct {
	// Job is the job's identifier.
	Job string `json:"job"`
	// Handle is the handle of the tool call the job came from.
	Handle string `json:"handle"`
	// Tool is the name of the tool the job runs.
	Tool string `json:"tool"`
	// Summary describes the call.
	Summary string `json:"summary,omitempty"`
	// Status is "running" or "held".
	Status string `json:"status"`
	// ElapsedMS is how long the job has been running, in milliseconds.
	ElapsedMS int64 `json:"elapsed_ms"`
}

// jobSummaries returns the running and the held jobs.
//
// The list is the model-facing view of the session's jobs.
func (s *Session) jobSummaries() []jobSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []jobSummary
	listed := map[string]bool{}
	for _, id := range s.jobOrder {
		job, ok := s.jobs[id]
		if !ok || job.ended {
			// A delivered job is neither running nor held.
			continue
		}
		listed[job.id] = true
		out = append(out, jobSummary{
			Job:       job.id,
			Handle:    job.handle,
			Tool:      job.tool,
			Summary:   job.summary,
			Status:    "running",
			ElapsedMS: time.Since(job.startedAt).Milliseconds(),
		})
	}
	for _, item := range s.jobQueue {
		if listed[item.result.job] {
			continue
		}
		listed[item.result.job] = true
		summary := ""
		if job, ok := s.jobs[item.result.job]; ok {
			summary = job.summary
		}
		out = append(out, jobSummary{
			Job:     item.result.job,
			Handle:  item.result.handle,
			Tool:    item.result.tool,
			Summary: summary,
			Status:  "held",
		})
	}
	return out
}

// backgroundArg returns the background flag a call asked for, and the
// arguments without it.
//
// The flag is a reserved argument: the core validates it and removes it
// before the tool's own decoder sees the arguments. A value that is not
// a boolean is a recoverable argument error.
func backgroundArg(raw json.RawMessage) (bool, json.RawMessage, error) {
	if len(raw) == 0 {
		return false, raw, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, raw, nil
	}
	value, ok := obj[BackgroundArgName]
	if !ok {
		return false, raw, nil
	}
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return false, raw, InvalidArgsf("the %q argument must be true or false", BackgroundArgName)
	}
	flag, ok := decoded.(bool)
	if !ok {
		return false, raw, InvalidArgsf("the %q argument must be true or false", BackgroundArgName)
	}
	delete(obj, BackgroundArgName)
	stripped, err := json.Marshal(obj)
	if err != nil {
		return false, raw, nil
	}
	return flag, stripped, nil
}

// schemaDefinesBackground reports that a tool's own schema defines the
// reserved background argument.
//
// A schema that defines it cannot be registered: the core would not know
// whether the model meant the tool's argument or the core's.
func schemaDefinesBackground(schema json.RawMessage) bool {
	if len(schema) == 0 {
		return false
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return false
	}
	props, ok := parsed["properties"]
	if !ok {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(props, &fields); err != nil {
		return false
	}
	_, ok = fields[BackgroundArgName]
	return ok
}

// withBackgroundSchema adds the optional background property to a
// tool's schema.
//
// The property is added to the schema the model sees, so the model knows
// it may ask for the call to run in the background.
func withBackgroundSchema(schema json.RawMessage) json.RawMessage {
	parsed := map[string]any{}
	if len(schema) > 0 {
		if err := json.Unmarshal(schema, &parsed); err != nil {
			return schema
		}
	}
	props, _ := parsed["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props[BackgroundArgName] = map[string]any{
		"type":        "boolean",
		"description": "Run the call in the background; its result arrives later as a message.",
	}
	parsed["properties"] = props
	out, err := json.Marshal(parsed)
	if err != nil {
		return schema
	}
	return out
}

// jobToolNames are the names of the tools the core registers for the
// model.
const (
	// JobsToolName is the name of the tool that lists the jobs.
	JobsToolName = "jobs"
	// CancelJobToolName is the name of the tool that stops a job.
	CancelJobToolName = "cancel_job"
)

// JobTool is one of the core's own job tools.
//
// The tools are read-only: they only look at the session's jobs, and the
// one that cancels a job stops work this session started.
//
// A consumer does not build one: the core registers the tools when a
// linked tool may run in the background. The type is exported so a
// prompt builder can ask the tool for its section.
type JobTool struct {
	// session is the session the tool belongs to.
	session *Session
	// name is the tool's name.
	name string
}

// Spec describes the tool.
func (t *JobTool) Spec() ToolSpec {
	switch t.name {
	case CancelJobToolName:
		return ToolSpec{
			Name:        CancelJobToolName,
			Description: "Stop one background job. Its partial output is delivered as its result.",
			Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "job": {"type": "string", "description": "The job's identifier, e.g. \"j3\"."}
  },
  "required": ["job"]
}`),
			Class: ClassReadOnly,
		}
	default:
		return ToolSpec{
			Name:        JobsToolName,
			Description: "List the background jobs that are running, and the results that are held.",
			Schema: json.RawMessage(`{
  "type": "object",
  "properties": {}
}`),
			Class: ClassReadOnly,
		}
	}
}

// Prepare decodes the call's arguments.
func (t *JobTool) Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error) {
	if t.name == CancelJobToolName {
		var a struct {
			Job string `json:"job"`
		}
		if err := decodeStrictJSON(raw, &a); err != nil {
			return nil, InvalidArgsf("%s: %v", t.name, err)
		}
		if a.Job == "" {
			return nil, InvalidArgsf("%s: the job is required", t.name)
		}
		return &jobPrepared{job: a.Job}, nil
	}
	return &jobPrepared{}, nil
}

// Call runs the tool.
func (t *JobTool) Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error) {
	if t.name == CancelJobToolName {
		p, ok := in.(*jobPrepared)
		if !ok {
			return ToolResult{}, InvalidArgsf("%s: the call was not prepared", t.name)
		}
		err := t.session.cancelJob(p.job)
		if err != nil {
			// The job is unknown or already finished. The failure is
			// recoverable: the model sees the message and carries on.
			return ToolResult{}, err
		}
		return ToolResult{Text: fmt.Sprintf("The job %s was cancelled.", p.job)}, nil
	}
	summaries := t.session.jobSummaries()
	if len(summaries) == 0 {
		return ToolResult{Text: "There are no background jobs."}, nil
	}
	var b strings.Builder
	for _, s := range summaries {
		fmt.Fprintf(&b, "%s · %s · %s · %s · %dms\n", s.Job, s.Tool, s.Status, s.Summary, s.ElapsedMS)
	}
	return ToolResult{Text: b.String()}, nil
}

// jobPrepared is a prepared job-tool call.
type jobPrepared struct {
	// job is the job the call names.
	job string
}

// Summary describes the call.
func (p *jobPrepared) Summary() string { return p.job }

// PromptSection renders the section that tells the model about the
// background jobs.
//
// The section is the model's guidance, and it names the two tools the
// core registers for it. The cancel_job tool has no section of its
// own: the jobs tool's section names both.
func (t *JobTool) PromptSection(env prompt.Env) string {
	if t.name == CancelJobToolName {
		return ""
	}
	var b strings.Builder
	b.WriteString("Background jobs:\n")
	b.WriteString("- A tool that says it may run in the background takes a \"background\": true argument. The call returns at once with a job handle, and the job's result arrives later as a message.\n")
	b.WriteString("- Use the jobs tool to see the jobs that are running, and cancel_job to stop one. There is no wait_job: ending your turn is how you wait.\n")
	b.WriteString("- A job keeps running when the turn ends and when the run is cancelled, so a long command belongs in one.\n")
	return b.String()
}

// decodeStrictJSON decodes one JSON value and nothing else.
func decodeStrictJSON(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	return nil
}

// jobPayload renders the job results a woken run carries, for the
// run.started event.
func jobPayload(results []jobResult) []map[string]any {
	if len(results) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"job":     r.job,
			"handle":  r.handle,
			"tool":    r.tool,
			"outcome": string(jobOutcomeToCallOutcome(r.outcome)),
		})
	}
	return out
}
