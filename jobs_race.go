package poo

import "time"

// raceWaitBudget bounds the job-race waiter's wait. It is its own
// constant: the record-write budget is the finalization's, and a test
// that tunes one should not move the other.
const raceWaitBudget = 5 * time.Second

// The seams the job races are tested at.
//
// A race is tested at a boundary, and a boundary is reached by a hook
// the test controls. The hooks are the seams the jobs stage's races
// need: one before the run enters the waiting state, one at the
// finalization's input check, and one before the commit point.
//
// The hooks are part of the core's surface on purpose: a consumer that
// wants to test a race of its own needs the same seams.

// JobRaceHooks are the seams the job races are tested at.
//
// A hook is called at a named boundary, and it blocks the run until it
// returns. The waiter it is given blocks until a job result is queued,
// so a test can place a result exactly at the boundary and know that it
// arrived.
type JobRaceHooks struct {
	// BeforeWaiting is called after the turn-end drain and before the
	// run enters the waiting state.
	BeforeWaiting func(wait WaitForResult)
	// BeforeFinalizeCheck is called at the finalization's input check,
	// before the queues are looked at.
	BeforeFinalizeCheck func(wait WaitForResult)
	// BeforeCommit is called after the finalization's cleanup and
	// before the commit point.
	BeforeCommit func(wait WaitForResult)
	// BeforeWake is called after a wake has taken the session's
	// reservation and before it starts its run.
	BeforeWake func()
	// BeforeRunStart is called after a run has taken the session's
	// reservation and before the run is installed.
	BeforeRunStart func()
}

// WaitForResult blocks until a job result is queued, and reports that
// one was.
//
// The wait is bounded, so a test that never releases its job fails
// instead of hanging.
type WaitForResult func() bool

// SetJobRaceHooks installs the race hooks and returns a function that
// removes them.
//
// The hooks are the seams the job races are tested at. A session
// without them never calls anything, so the core's own path does not
// change.
func (s *Session) SetJobRaceHooks(hooks *JobRaceHooks) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.raceHooks
	s.raceHooks = hooks
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.raceHooks = prev
	}
}

// raceHook runs the named race hook, if the session has one.
//
// The caller must not hold the session's lock.
func (s *Session) raceHook(name string) {
	s.mu.Lock()
	hooks := s.raceHooks
	s.mu.Unlock()
	if hooks == nil {
		return
	}
	switch name {
	case "waiting":
		if hooks.BeforeWaiting != nil {
			hooks.BeforeWaiting(s.waitForJobResult)
		}
	case "finalize":
		if hooks.BeforeFinalizeCheck != nil {
			hooks.BeforeFinalizeCheck(s.waitForJobResult)
		}
	case "commit":
		if hooks.BeforeCommit != nil {
			hooks.BeforeCommit(s.waitForJobResult)
		}
	case "wake":
		if hooks.BeforeWake != nil {
			hooks.BeforeWake()
		}
	case "start":
		if hooks.BeforeRunStart != nil {
			hooks.BeforeRunStart()
		}
	}
}

// waitForJobResult blocks until a job result is queued.
//
// The signal is buffered, so a result that was queued before the wait
// began is seen at once.
func (s *Session) waitForJobResult() bool {
	s.mu.Lock()
	ch := s.jobQueued
	s.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	case <-time.After(raceWaitBudget):
		return false
	}
}

// WaitForJobResult blocks until a job result is queued, and reports that
// one was.
//
// The wait is bounded, so a test that never finishes its job fails
// instead of hanging.
func (s *Session) WaitForJobResult() bool {
	return s.waitForJobResult()
}

// QueuedJobResults returns how many job results are queued and waiting
// for their delivery.
func (s *Session) QueuedJobResults() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobQueue)
}
