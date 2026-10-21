package poo

import "context"

// The activity is the work a run is doing at one moment: the provider
// request, the tool batch, the compaction, or the turn-end hooks.
//
// An interrupt ends the activity and nothing else: the run carries on,
// and the interrupt is never a settle candidate.

// startActivity gives the run a fresh activity context.
//
// The activity ends when the interrupt arrives, when the run is
// finalized, or when the activity itself is over.
func (s *Session) startActivity(run *runState) (context.Context, context.CancelCauseFunc) {
	ctx, cancel := context.WithCancelCause(run.ctx)
	s.mu.Lock()
	run.activityCtx = ctx
	run.activityCancel = cancel
	s.mu.Unlock()
	return ctx, cancel
}

// endActivity ends the run's current activity.
//
// The cause is nil, so the activity's end is not a cancellation the
// core has to explain.
func (s *Session) endActivity(run *runState) {
	s.mu.Lock()
	cancel := run.activityCancel
	run.activityCtx = nil
	run.activityCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel(nil)
	}
}

// activityContext returns the context the run's current activity runs
// under.
//
// An activity that has not started yet runs under the run's own
// context.
func (s *Session) activityContext(run *runState) context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.activityCtx != nil {
		return run.activityCtx
	}
	return run.ctx
}

// activityContextFor returns the context the run's current activity runs
// under.
func (s *Session) activityContextFor(run *runState) context.Context {
	return s.activityContext(run)
}

// addInterrupt records that an interrupt arrived.
//
// Several interrupts that arrive during one abort coalesce into one
// abort, and all their messages are delivered.
func (s *Session) addInterrupt(run *runState, inputID string) {
	s.mu.Lock()
	run.interruptPending = true
	run.pendingInterruptIDs = append(run.pendingInterruptIDs, inputID)
	cancel := run.activityCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel(errInterrupted)
	}
}

// takeInterrupt takes the pending interrupt's input ids.
//
// The ids are the inputs whose messages aborted the activity, and they
// are the ones the turn's interrupted event names.
func (s *Session) takeInterrupt(run *runState) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := run.pendingInterruptIDs
	run.pendingInterruptIDs = nil
	return ids
}

// interruptPending reports that an interrupt is waiting to be acted on.
func (s *Session) interruptPending(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return run.interruptPending
}

// wasInterrupted reports that the run's current activity was cut short
// by an interrupt.
func (s *Session) wasInterrupted(run *runState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return run.interruptPending
}

// clearInterrupt forgets the pending interrupt.
func (s *Session) clearInterrupt(run *runState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run.interruptPending = false
	run.pendingInterruptIDs = nil
}
