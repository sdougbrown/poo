package poo

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrCallNotBackgroundable means the named call cannot be moved to a job.
var ErrCallNotBackgroundable = errors.New("poo: the call cannot move to the background")

type liveCall struct {
	plan     callPlan
	turn     int
	activity context.Context
	ctx      context.Context
	cancel   context.CancelCauseFunc
	stopRun  func() bool
	done     chan struct{}
	transfer chan *jobState
	receipt  chan struct{}
	// jobID is the id the job takes when the call moves to one. It is
	// reserved when the call goes live, so the env the tool holds names
	// the job the call may become from the start: the env rides into the
	// tool's Call once and cannot be revisited when the move happens.
	jobID string
	// The session lock orders completion and the reservation against cancellation.
	finished bool
	moving   bool
	result   ToolResult
	err      error
}

func (c *liveCall) canMove() bool {
	return !c.finished && !c.moving && c.activity.Err() == nil && c.ctx.Err() == nil
}

// BackgroundCall moves a running foreground call into a session job.
func (s *Session) BackgroundCall(runID, handle string) (string, error) {
	s.mu.Lock()
	if s.closed || s.broken {
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return "", ErrSessionClosed
		}
		return "", ErrSessionBroken
	}
	run := s.run
	if run == nil || run.id != runID || run.finalizing || run.committed {
		s.mu.Unlock()
		return "", ErrNotRunning
	}
	issued := run.pendingCalls[handle]
	if issued == nil || issued.live == nil {
		s.mu.Unlock()
		return "", ErrCallNotBackgroundable
	}
	live := issued.live
	s.mu.Unlock()
	p := live.plan
	id, ok, winding, refusal := s.startJob(run, live.turn, handle, p.call.Name, p.prepared, p.summary, p.call.Arguments, p.stripped, p.shorthand, live)
	if winding {
		return "", ErrNotRunning
	}
	if !ok {
		s.mu.Lock()
		closed, broken := s.closed, s.broken
		s.mu.Unlock()
		if closed {
			return "", ErrSessionClosed
		}
		if broken {
			return "", ErrSessionBroken
		}
		if refusal == ErrCallNotBackgroundable.Error() {
			return "", ErrCallNotBackgroundable
		}
		if refusal != "" {
			return "", fmt.Errorf("%w: %s", ErrCallNotBackgroundable, refusal)
		}
		return "", ErrBusy
	}
	<-live.receipt
	return id, nil
}

func (s *Session) runLiveCall(owner *callOwner, turn int, plan callPlan, tool Tool, env ToolEnv, activity context.Context) (ToolResult, error, bool) {
	ctx, cancel := context.WithCancelCause(s.jobCtx)
	c := &liveCall{
		plan: plan, turn: turn, activity: activity, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), transfer: make(chan *jobState, 1), receipt: make(chan struct{}),
	}
	s.mu.Lock()
	if activity.Err() != nil {
		cancel(context.Cause(activity))
	}
	// The job's id is reserved here, under the lock, so a call that
	// never moves skips the number and one that moves takes the id the
	// tool's env already carries.
	s.jobSeq++
	c.jobID = fmt.Sprintf("j%d", s.jobSeq)
	c.stopRun = context.AfterFunc(activity, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !c.moving {
			cancel(context.Cause(activity))
		}
	})
	if issued := owner.pendingCalls[plan.handle]; issued != nil {
		issued.live = c
	}
	s.mu.Unlock()
	// The call runs as the job's own from the start: the env names the
	// job it may become, so a tool that keys on the job id — the per-call
	// timeout, say — sees it, and the deadline is the session's job
	// timeout, not the per-call one. The timer stops when the call's
	// ownership resolves: a move hands the deadline to the job, which
	// arms its own; a call that ends in the run stops it here.
	stopDeadline := s.armJobTimeout(cancel)
	env.JobID = c.jobID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.err = fmt.Errorf("%w: panic: %v", ErrFatal, r)
			}
			s.mu.Lock()
			c.finished = true
			s.mu.Unlock()
			close(c.done)
		}()
		c.result, c.err = tool.Call(ctx, plan.prepared, env)
	}()

	var job *jobState
	select {
	case job = <-c.transfer:
	case <-c.done:
		s.mu.Lock()
		moving := c.moving
		s.mu.Unlock()
		if moving {
			job = <-c.transfer
		}
	}
	if job != nil {
		stopDeadline()
		s.recordJobReceipt(owner, turn, plan.handle, plan.call, job.id, plan.shorthand)
		close(c.receipt)
		return ToolResult{}, nil, true
	}
	<-c.done
	stopDeadline()
	c.stopRun()
	cancel(nil)
	return c.result, c.err, false
}

func (s *Session) finishLiveJob(job *jobState, c *liveCall) {
	defer c.cancel(nil)
	defer s.armJobTimeout(c.cancel)()
	<-c.done
	<-c.receipt
	s.completeJobCall(job, c.ctx, c.result, c.err)
}

// armJobTimeout stops a job when the job's timeout fires: the single
// owner of the bound, it reads the same knob for a job that started
// normally and one a live call moved into, and the cancel carries the
// deadline cause, so the completion classifies the job as timed out.
// The returned stop releases the timer.
func (s *Session) armJobTimeout(cancel context.CancelCauseFunc) (stop func()) {
	timer := time.AfterFunc(s.jobTimeout(), func() { cancel(context.DeadlineExceeded) })
	return func() { timer.Stop() }
}
