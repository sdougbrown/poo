package poo

import (
	"context"
	"time"
)

// The waiting for jobs: a one-shot client waits while a run is active, a
// job is running, or a queued result is eligible to wake a run.

// JobsRunning reports the jobs that are still running.
func (s *Session) JobsRunning() []string {
	return s.runningJobIDs()
}

// JobInfo describes one of the session's jobs.
type JobInfo struct {
	// Job is the job's identifier, e.g. "j3".
	Job string
	// Handle is the handle of the tool call the job came from.
	Handle string
	// Tool is the name of the tool the job runs.
	Tool string
	// RunID identifies the run that started the job.
	RunID string
	// StartedAt is when the job started, in RFC 3339.
	StartedAt string
	// Status is "running" or "held".
	//
	// A held job is a finished job whose result waits for the next
	// run: the work is done, and the model has not read it yet.
	Status string
}

// The statuses a job is reported with.
const (
	// JobRunning means the job is still running.
	JobRunning = "running"
	// JobHeld means the job's result is waiting for the next run.
	JobHeld = "held"
)

// Jobs returns the session's jobs.
//
// The running jobs come first, in start order, and the held results
// follow them.
func (s *Session) Jobs() []JobInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobInfo, 0, len(s.jobOrder)+len(s.jobQueue))
	for _, id := range s.jobOrder {
		job, ok := s.jobs[id]
		if !ok || job.ended {
			continue
		}
		out = append(out, JobInfo{
			Job:       job.id,
			Handle:    job.handle,
			Tool:      job.tool,
			RunID:     job.runID,
			StartedAt: job.startedAt.UTC().Format(time.RFC3339Nano),
			Status:    JobRunning,
		})
	}
	for _, item := range s.jobQueue {
		if !item.held {
			continue
		}
		out = append(out, JobInfo{
			Job:    item.result.job,
			Handle: item.result.handle,
			Tool:   item.result.tool,
			RunID:  item.result.runID,
			Status: JobHeld,
		})
	}
	return out
}

// HeldJobs reports the job results that are held.
//
// A held result waits for the next client prompt: it is not eligible to
// wake a run, so a one-shot client may exit with these in hand.
func (s *Session) HeldJobs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, item := range s.jobQueue {
		if item.held {
			out = append(out, item.result.job)
		}
	}
	return out
}

// WaitJobs waits until no job is running and no queued result is
// eligible to wake a run.
//
// A held result does not keep the wait alive: it waits for the next
// client prompt, so a one-shot client may go on without it.
//
// The wait ends when the session's context ends, and the call reports
// whether the jobs are done.
func (s *Session) WaitJobs(ctx context.Context) error {
	for {
		s.mu.Lock()
		running := s.runningJobCountLocked()
		// A run that is open is work in flight, including a woken run
		// whose job has already ended. A completion being recorded and
		// a reserved wake are work too: their handoff into the queue or
		// the active run has not happened yet.
		active := s.run != nil || s.reserved || s.jobCompletions > 0
		// A result that is queued while a run is open is delivered to
		// that run, so it does not keep the wait alive. Only a result
		// that is waiting for a wake does.
		pending := 0
		if !active {
			for _, item := range s.jobQueue {
				if !item.held {
					pending++
				}
			}
		}
		closed := s.closed
		broken := s.broken
		s.mu.Unlock()

		if closed {
			return ErrSessionClosed
		}
		if broken {
			// A broken session cannot record a wake or deliver a
			// result: waiting for one would never end.
			return ErrSessionBroken
		}
		if running == 0 && pending == 0 && !active {
			return nil
		}
		// The wait is short: the core does the work, and the client
		// only has to come back and look.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// SettleCodes returns the exit codes of the runs that have settled.
//
// The codes are the ones the settle-reason table gives, and they are
// combined across the woken runs: the first non-zero code wins, and a
// session whose every run succeeded has no code at all.
func (s *Session) SettleCodes() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.settleCodes...)
}
