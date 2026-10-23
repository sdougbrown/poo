package bash

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Local is the executor that runs a command in poo's own process space.
//
// The command gets a fresh shell, a null stdin and the given
// environment, and it runs in its own process group. stdout and stderr
// share one pipe, so the output arrives interleaved.
//
// Nothing the command starts outlives the call: whatever is still in
// the process group when the command ends is killed before the call
// returns.
//
// A descendant that deliberately leaves the group (setsid, a double
// fork into a new session) escapes the group, and with it the
// teardown. That is the local executor's documented limit; a sandboxed
// executor closes it.
type Local struct{}

// NewLocal returns the local executor.
func NewLocal() *Local { return &Local{} }

// Name is the executor's name.
func (l *Local) Name() string { return "local" }

// Start runs the command in its own process group.
//
// The output of stdout and stderr goes to out as one stream, unless the
// command names a writer for stderr: then the two streams are captured
// apart. The context ends the command: when it is done, the command's
// process group is torn down.
func (l *Local) Start(ctx context.Context, c Command, out io.Writer) (Process, error) {
	if len(c.Argv) == 0 {
		return nil, errors.New("bash: the command has no arguments")
	}
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	// A nil Stdin makes the process read from the null device.
	cmd.Stdin = nil
	cmd.Env = c.Env
	// The command runs in its own process group, so the whole group can
	// be signalled at once.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	var errPr, errW *os.File
	if c.Stderr != nil {
		errPr, errW, err = os.Pipe()
		if err != nil {
			pr.Close()
			pw.Close()
			return nil, err
		}
		cmd.Stderr = errW
	}

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		if errW != nil {
			errPr.Close()
			errW.Close()
		}
		return nil, err
	}
	// The parent's copies of the write ends are closed, so a pipe
	// reaches EOF once every writer is gone.
	pw.Close()
	if errW != nil {
		errW.Close()
	}

	pgid := cmd.Process.Pid

	pipes := []*os.File{pr}
	writers := []io.Writer{out}
	if errPr != nil {
		pipes = append(pipes, errPr)
		writers = append(writers, c.Stderr)
	}
	drains := make([]chan struct{}, 0, len(pipes))
	for i, pipe := range pipes {
		drained := make(chan struct{})
		drains = append(drains, drained)
		go func(pipe *os.File, w io.Writer) {
			defer close(drained)
			_, _ = io.Copy(w, pipe)
		}(pipe, writers[i])
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	p := &localProcess{
		cmd:      cmd,
		pgid:     pgid,
		pipes:    pipes,
		waited:   waited,
		drains:   drains,
		finished: make(chan struct{}),
	}
	go func() {
		// The context ends the command: a cancelled run must not leave
		// a process behind.
		select {
		case <-ctx.Done():
			p.Kill()
		case <-p.finished:
		}
	}()
	return p, nil
}

// localProcess is a command running in its own process group.
type localProcess struct {
	cmd  *exec.Cmd
	pgid int
	// pipes are the read ends of the capture, one per stream.
	pipes  []*os.File
	waited chan error
	// drains report that a stream has been read to its end.
	drains []chan struct{}

	// finished is closed when the wait is over, so the context watcher
	// stops.
	finished chan struct{}
	// waitOnce keeps the teardown to one run.
	waitOnce sync.Once

	mu sync.Mutex
	// exited reports that the command has exited and the group has
	// been torn down. A kill after that would signal a group that may
	// belong to another process by now.
	exited bool
	code   int
	err    error
}

// Wait waits for the command to finish and for everything it left
// behind to die.
//
// The command's exit code is returned. A command that was killed has
// none, and its code is negative.
func (p *localProcess) Wait() (int, error) {
	p.waitOnce.Do(func() {
		err := <-p.waited

		p.mu.Lock()
		p.code = exitCode(p.cmd)
		p.err = err
		p.exited = true
		p.mu.Unlock()

		// Nothing may outlive the call: SIGTERM the group, then
		// SIGKILL it once the grace period is up.
		p.terminateGroup()

		// A descendant may still hold a pipe open. Wait for the
		// drains, then close the pipes so the goroutines cannot hang.
		deadline := time.After(KillGrace)
		for _, drained := range p.drains {
			select {
			case <-drained:
			case <-deadline:
				p.killGroup(syscall.SIGKILL)
				p.closePipes()
				<-drained
			}
		}
		p.closePipes()
		close(p.finished)
	})

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, p.err
}

// closePipes closes the read ends the wait has finished with.
func (p *localProcess) closePipes() {
	for _, pipe := range p.pipes {
		pipe.Close()
	}
}

// Kill stops the command and everything in its process group.
//
// The group gets SIGTERM, then SIGKILL after the grace period, so a
// process that ignores SIGTERM does not survive the call.
func (p *localProcess) Kill() {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited {
		return
	}
	p.terminateGroup()
}

// terminateGroup kills the process group and returns when the group is
// gone.
//
// The group gets SIGTERM, then SIGKILL after the grace period. The wait
// is bounded, so the call cannot hang on a process that ignores
// SIGTERM.
func (p *localProcess) terminateGroup() {
	p.killGroup(syscall.SIGTERM)
	deadline := time.Now().Add(KillGrace)
	for time.Now().Before(deadline) {
		if !p.groupAlive() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.killGroup(syscall.SIGKILL)
	// The kill is not instant, and a caller that checks for survivors
	// right after the call has to find none.
	deadline = time.Now().Add(KillGrace)
	for time.Now().Before(deadline) {
		if !p.groupAlive() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// groupAlive reports whether any process is left in the group.
func (p *localProcess) groupAlive() bool {
	if p.pgid <= 0 {
		return false
	}
	// Signal 0 checks for existence without disturbing the process.
	return syscall.Kill(-p.pgid, syscall.Signal(0)) == nil
}

// killGroup signals a whole process group. A group that is already gone
// is not an error.
func (p *localProcess) killGroup(sig syscall.Signal) {
	if p.pgid <= 0 {
		return
	}
	// A negative pid signals the process group.
	_ = syscall.Kill(-p.pgid, sig)
}

// exitCode returns the command's exit code.
//
// A command that died from a signal has none, and its code is negative.
func exitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}
