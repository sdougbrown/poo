package bash

import (
	"context"
	"errors"
	"time"

	"github.com/sdougbrown/poo"

	"github.com/sdougbrown/poo/exec"
)

// result is what a command produced.
type result struct {
	// exitCode is the command's exit status, nil when the process was
	// killed by a signal.
	exitCode *int
	// timedOut reports that the command hit its timeout.
	timedOut bool
	// cancelled reports that the run's context ended the command.
	cancelled bool
	// output is the captured output, truncated to the capture cap.
	output string
}

// stopped is the result of a command the context ended before it ran.
//
// There is no output and no return code: the command never wrote
// anything, and the model has to see that the call did not happen.
func stopped(err error) result {
	return result{
		timedOut:  errors.Is(err, context.DeadlineExceeded),
		cancelled: errors.Is(err, context.Canceled),
	}
}

// run executes the command through the tool's executor and collects its
// output.
//
// The command gets a fresh shell, a null stdin and the scrubbed
// environment. Whatever the executor starts is stopped before the call
// returns, so no process outlives the call.
//
// A job runs under the session's own deadline, and the session's
// deadline is the job's timeout: the per-call timeout does not apply to
// a job, or a long command would never finish.
func (t *Tool) run(ctx context.Context, command string, env poo.ToolEnv, timeout time.Duration) (result, error) {
	executor := t.Executor
	if executor == nil {
		executor = NewLocal()
	}

	runCtx := ctx
	if env.JobID == "" {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	buf := NewCappedBuffer(CaptureCap)
	// A context that has already ended runs nothing: a cancelled call
	// must not let the command write its side effects first.
	if err := runCtx.Err(); err != nil {
		return stopped(err), nil
	}
	proc, err := executor.Start(runCtx, exec.Command{
		Argv: []string{"/bin/bash", "-c", command},
		Dir:  env.WorkingDir,
		Env:  ScrubEnv(env.Env, t.Scrub),
	}, buf)
	if err != nil {
		return result{}, err
	}

	code, _ := proc.Wait()

	res := result{output: buf.String()}
	// A command that was killed has no exit code, and the model has to
	// see that it failed.
	if code >= 0 {
		res.exitCode = &code
	}
	// Only a command the executor stopped was stopped by the run's
	// context: one that exited on its own keeps that outcome, however
	// long the teardown of what it left behind takes.
	if proc.Stopped() {
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			res.timedOut = true
		case errors.Is(runCtx.Err(), context.Canceled):
			res.cancelled = true
		}
	}
	return res, nil
}
