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
		s.recordJobReceipt(owner, turn, plan.handle, plan.call, job.id, plan.shorthand)
		close(c.receipt)
		return ToolResult{}, nil, true
	}
	<-c.done
	c.stopRun()
	cancel(nil)
	return c.result, c.err, false
}

func (s *Session) finishLiveJob(job *jobState, c *liveCall) {
	defer c.cancel(nil)
	timer := time.AfterFunc(s.jobTimeout(), func() { c.cancel(context.DeadlineExceeded) })
	defer timer.Stop()
	<-c.done
	timer.Stop()
	<-c.receipt
	s.completeJobCall(job, c.ctx, c.result, c.err)
}
