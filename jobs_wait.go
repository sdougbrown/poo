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
		pending := 0
		for _, item := range s.jobQueue {
			if !item.held {
				pending++
			}
		}
		closed := s.closed
		s.mu.Unlock()

		if closed {
			return ErrSessionClosed
		}
		if running == 0 && pending == 0 {
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
