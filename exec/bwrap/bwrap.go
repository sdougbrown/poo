// Package bwrap implements the bubblewrap executor for the bash tool.
//
// The executor runs a command in a bubblewrap sandbox: a namespace of
// its own, with a filesystem of its own and, unless the network is
// cut, the host's network.
//
// The sandbox is not a security boundary against hostile code. It keeps
// a model's commands inside the task: the kernel is the host's, and
// with the network on, the network is the host's too. docs/sandbox.md
// says what it contains and what it does not.
//
// The exit status is the one bubblewrap reports: the command's own
// status, or 128 plus the signal when the command died from one. A
// local command that dies from a signal has no status at all, so the
// two executors differ there: the sandbox names the number where the
// local executor names nothing.
package bwrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sdougbrown/poo/tool/bash"
)

// The names and defaults of the executor.
const (
	// Name is the executor's name, as the settings name it.
	Name = "bwrap"
	// CodeUnavailable is the code an unavailable executor reports.
	CodeUnavailable = "executor_unavailable"
	// ProbeTimeout bounds the check that the sandbox can be created.
	ProbeTimeout = 30 * time.Second
	// KillGrace is how long the sandbox has between SIGTERM and
	// SIGKILL.
	KillGrace = 2 * time.Second
)

// DefaultROBinds are the host paths the sandbox mounts read-only.
//
// The list is the plan's: the system's own directories, and nothing
// else. $HOME is not among them, so a command cannot read the
// operator's files unless the settings name them.
var DefaultROBinds = []string{"/usr", "/bin", "/lib", "/lib64", "/etc"}

// Masker expands the protected paths to mask.
//
// The paths it returns are absolute and under the root the executor
// gives it. A session's protected-path matcher satisfies it.
type Masker = bash.Masker

// Options configure the executor.
type Options struct {
	// Binary is the bwrap executable. Empty means the one on PATH.
	Binary string
	// DisableNetwork gives the sandbox its own empty network
	// namespace instead of the host's. The network is shared unless
	// this is set.
	DisableNetwork bool
	// ROBinds are the host paths mounted read-only. The default list
	// is used when it is empty.
	ROBinds []string
	// RWBinds are the host paths mounted read-write, besides the
	// command's working directory.
	RWBinds []string
	// Masker expands the protected paths to mask. Nil masks nothing.
	Masker Masker
}

// Executor runs a command in a bubblewrap sandbox.
type Executor struct {
	binary string
	opts   Options
}

// New returns a bwrap executor.
//
// The construction fails when the binary is missing or when the
// namespaces cannot be created: an executor that cannot run is
// reported, never replaced with a weaker one. A session that asked for
// this executor must not run its commands unconfined instead.
func New(opts Options) (*Executor, error) {
	binary := opts.Binary
	if binary == "" {
		found, err := exec.LookPath("bwrap")
		if err != nil {
			return nil, &UnavailableError{
				Detail: "bubblewrap is not installed",
				Err:    err,
			}
		}
		binary = found
	}
	if err := probe(binary); err != nil {
		return nil, &UnavailableError{
			Detail: "bubblewrap cannot create the sandbox's namespaces",
			Err:    err,
		}
	}
	return &Executor{binary: binary, opts: opts}, nil
}

// Name is the executor's name.
func (e *Executor) Name() string { return Name }

// Start runs the command in a sandbox.
//
// The command sees the read-only system directories, its working
// directory read-write, and the extra read-write binds. Nothing else of
// the filesystem is there, and $HOME is not mounted unless a bind names
// it.
//
// The output of stdout and stderr goes to out as one stream.
func (e *Executor) Start(ctx context.Context, c bash.Command, out io.Writer) (bash.Process, error) {
	if len(c.Argv) == 0 {
		return nil, errors.New("bwrap: the command has no arguments")
	}
	// An empty directory means the caller's own, and the sandbox binds
	// it under its own name.
	if c.Dir == "" {
		dir, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("bwrap: the working directory: %w", err)
		}
		c.Dir = dir
	}
	args, err := e.args(c)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(e.binary, args...)
	cmd.Dir = c.Dir
	// A nil Stdin makes the sandbox read from the null device.
	cmd.Stdin = nil
	cmd.Env = c.Env
	// The sandbox runs in its own process group, so the whole group can
	// be signalled at once.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	// The parent's copy of the write end is closed, so the pipe reaches
	// EOF once every writer is gone.
	pw.Close()

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(out, pr)
	}()

	done := make(chan struct{})
	waited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		close(done)
		waited <- err
	}()

	p := &process{
		cmd:      cmd,
		pr:       pr,
		waited:   waited,
		drained:  drained,
		done:     done,
		finished: make(chan struct{}),
	}
	go func() {
		// The context ends the sandbox: a cancelled run must not
		// leave one behind.
		select {
		case <-ctx.Done():
			p.Kill()
		case <-p.finished:
		}
	}()
	return p, nil
}

// args returns the bubblewrap arguments for a command.
func (e *Executor) args(c bash.Command) ([]string, error) {
	args := []string{
		// Every namespace the sandbox can have, so the command cannot
		// see the host's processes, its cgroups or its host name.
		"--unshare-all",
		// The sandbox ends when the process that started it ends, so
		// a crash does not leave a sandbox behind.
		"--die-with-parent",
		// A session of its own, so the command's processes are not in
		// the caller's process group.
		"--new-session",
		// A fresh /proc, /dev and /tmp: the command sees its own
		// processes and nobody else's.
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	if !e.opts.DisableNetwork {
		// The network is the host's, unless the settings cut it.
		args = append(args, "--share-net")
	}
	for _, path := range e.roBinds() {
		args = append(args, "--ro-bind-try", path, path)
	}
	// The resolver's directory, when the host's resolver keeps its
	// configuration there: /etc/resolv.conf is often a link into it,
	// and a name that does not resolve is a broken network.
	args = append(args, "--ro-bind-try", "/run/systemd/resolve", "/run/systemd/resolve")
	// The working directory is the one place the command may write.
	args = append(args, "--bind", c.Dir, c.Dir)
	for _, path := range e.opts.RWBinds {
		args = append(args, "--bind", path, path)
	}
	masks, err := e.masks(c.Dir)
	if err != nil {
		return nil, err
	}
	for _, mask := range masks {
		args = append(args, mask...)
	}
	args = append(args, "--chdir", c.Dir)
	args = append(args, "--")
	args = append(args, c.Argv...)
	return args, nil
}

// roBinds returns the host paths the sandbox mounts read-only.
func (e *Executor) roBinds() []string {
	if len(e.opts.ROBinds) > 0 {
		return e.opts.ROBinds
	}
	return DefaultROBinds
}

// masks returns the arguments that hide the protected paths.
//
// A protected path inside a bound directory is hidden: a directory
// behind an empty tmpfs, a file behind an empty device. A path outside
// every bound directory is not in the sandbox at all, so there is
// nothing to hide.
func (e *Executor) masks(root string) ([][]string, error) {
	if e.opts.Masker == nil {
		return nil, nil
	}
	paths, err := e.opts.Masker.Expand(root)
	if err != nil {
		return nil, fmt.Errorf("bwrap: expanding the protected paths: %w", err)
	}
	paths = append([]string(nil), paths...)
	// The shortest path first, so a directory is hidden before the
	// files inside it, and a file inside a hidden directory is not
	// masked again.
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) < len(paths[j]) })

	var out [][]string
	var hidden []string
	for _, path := range paths {
		if path == "" || !e.bound(path, root) {
			continue
		}
		if underAny(hidden, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			// A path that is not there cannot be read either.
			continue
		}
		if info.IsDir() {
			out = append(out, []string{"--tmpfs", path})
			hidden = append(hidden, path)
			continue
		}
		out = append(out, []string{"--dev-bind", "/dev/null", path})
	}
	return out, nil
}

// bound reports whether a path is inside one of the directories the
// sandbox mounts.
func (e *Executor) bound(path, root string) bool {
	if root != "" && under(root, path) {
		return true
	}
	for _, dir := range e.opts.RWBinds {
		if dir != "" && under(dir, path) {
			return true
		}
	}
	return false
}

// under reports whether a path is inside a directory.
func under(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// underAny reports whether a path is inside one of the directories.
func underAny(dirs []string, path string) bool {
	for _, dir := range dirs {
		if under(dir, path) {
			return true
		}
	}
	return false
}

// probe checks that the binary can create the namespaces the sandbox
// needs.
//
// The check runs a trivial command in a sandbox with the same binds the
// executor uses. A host that does not allow unprivileged user
// namespaces fails here, and the executor is never handed out.
func probe(binary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	args := []string{"--unshare-all"}
	for _, dir := range DefaultROBinds {
		args = append(args, "--ro-bind-try", dir, dir)
	}
	args = append(args, "--", "/bin/true")
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// process is a sandbox that is running.
type process struct {
	cmd     *exec.Cmd
	pr      *os.File
	waited  chan error
	drained chan struct{}
	// done is closed when the sandbox's own process has been reaped.
	done chan struct{}
	// finished is closed when the wait is over, so the context watcher
	// stops.
	finished chan struct{}
	// waitOnce keeps the teardown to one run.
	waitOnce sync.Once

	mu sync.Mutex
	// exited reports that the sandbox has ended. A kill after that
	// would signal a process that may belong to another one by now.
	exited bool
	code   int
	err    error
}

// Wait waits for the sandbox to end and for everything in it to die.
//
// The exit code is the command's: bubblewrap passes the command's
// status on. A command that was killed has none, and its code is
// negative.
func (p *process) Wait() (int, error) {
	p.waitOnce.Do(func() {
		err := <-p.waited

		p.mu.Lock()
		p.code = exitCode(p.cmd)
		p.err = err
		p.exited = true
		p.mu.Unlock()

		// A descendant may still hold the pipe open. The wait is
		// bounded, so the call cannot hang on it.
		select {
		case <-p.drained:
		case <-time.After(KillGrace):
			p.pr.Close()
			<-p.drained
		}
		p.pr.Close()
		close(p.finished)
	})

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, p.err
}

// Kill stops the sandbox.
//
// The sandbox's processes are the sandbox's init's children, and the
// kernel ends a namespace when its init dies: killing bubblewrap ends
// the namespace, and everything in it with it. A descendant that left
// the process group dies with the namespace, which is the escape the
// local executor cannot close.
func (p *process) Kill() {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited {
		return
	}
	// The sandbox's init gets SIGTERM first, and the namespace goes
	// with it.
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(KillGrace)
	for time.Now().Before(deadline) {
		if !p.alive() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = p.cmd.Process.Signal(syscall.SIGKILL)
}

// alive reports whether the sandbox's own process is still there.
func (p *process) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
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

// UnavailableError is the error for an executor that cannot run.
//
// The code is the same for every cause: the executor is unavailable,
// whether the binary is missing or the namespaces cannot be created.
type UnavailableError struct {
	// Detail says what went wrong.
	Detail string
	// Err is the underlying error, if there is one.
	Err error
}

func (e *UnavailableError) Error() string {
	if e.Err == nil {
		return CodeUnavailable + ": " + e.Detail
	}
	return CodeUnavailable + ": " + e.Detail + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *UnavailableError) Unwrap() error { return e.Err }

// Code is the machine-readable name of the failure.
func (e *UnavailableError) Code() string { return CodeUnavailable }
