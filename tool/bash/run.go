package bash

import (
	"context"
	"errors"
	"time"

	"github.com/sdougbrown/poo"
)

// result is what a command produced.
type result struct {
	// returncode is the command's exit status, nil when the process was
	// killed by a signal.
	returncode *int
	// timedOut reports that the command hit its timeout.
	timedOut bool
	// cancelled reports that the run's context ended the command.
	cancelled bool
	// output is the captured output, truncated to the capture cap.
	output string
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
	if env.Job == "" {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	buf := &cappedBuffer{limit: CaptureCap}
	proc, err := executor.Start(runCtx, Command{
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
		res.returncode = &code
	}
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.timedOut = true
	case errors.Is(runCtx.Err(), context.Canceled):
		res.cancelled = true
	}
	return res, nil
}
