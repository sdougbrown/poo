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
// The command's exit status is the shell's. A command that dies from a
// signal has no exit status, and the executor reports it as one: the
// sandbox runs the command under a small helper that reports the wait
// status on a side channel, so a signal death is not confused with an
// exit of 128 plus the signal.
package bwrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	pooexec "github.com/sdougbrown/poo/exec"
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

// The file descriptors the sandbox's helper and bubblewrap report on.
const (
	// statusFD is the descriptor the helper writes the command's wait
	// status to.
	statusFD = 3
	// jsonStatusFD is the descriptor bubblewrap writes its status to.
	jsonStatusFD = 4
	// parentFD is the descriptor the helper watches for the executor's
	// death.
	parentFD = 5
	// statusFDEnv names the descriptor for the helper.
	statusFDEnv = "POO_BWRAP_STATUS_FD"
	// parentFDEnv names the parent-death descriptor for the helper.
	parentFDEnv = "POO_BWRAP_PARENT_FD"
	// helperArg is the first argument the helper is invoked with.
	helperArg = "__poo_bwrap_helper__"
	// helperPath is where the helper is bound inside the sandbox.
	helperPath = "/run/poo-bwrap/helper"
	// resolveBind is the resolver's directory, mounted read-only when
	// the host has one.
	resolveBind = "/run/systemd/resolve"
)

// DefaultROBinds are the host paths the sandbox mounts read-only.
//
// The list is the plan's: the system's own directories, and nothing
// else. $HOME is not among them, so a command cannot read the
// operator's files unless the settings name them.
var DefaultROBinds = []string{"/usr", "/bin", "/lib", "/lib64", "/etc"}

// Masker expands the protected paths to mask.
//
// The executor asks it about every directory the sandbox mounts, not
// only the working directory: a path the operator protects inside a
// read-only or read-write bind has to be hidden as well. The paths it
// returns are absolute; the executor masks the ones a bind reaches.
//
// A session's protected-path matcher satisfies it.
type Masker = pooexec.Masker

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

	// masksMu guards the expansion cache. The binds do not change, so
	// a bind's expansion is computed once and reused by every command.
	masksMu   sync.Mutex
	maskCache map[string][]string
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
	if err := probe(binary, opts); err != nil {
		return nil, &UnavailableError{
			Detail: "bubblewrap cannot create the sandbox's namespaces",
			Err:    err,
		}
	}
	return &Executor{binary: binary, opts: opts, maskCache: map[string][]string{}}, nil
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
// The output of stdout and stderr goes to out as one stream, unless the
// command names a writer for stderr: then the two streams are captured
// apart. A context that has already ended starts nothing: Start returns
// its error and no sandbox.
func (e *Executor) Start(ctx context.Context, c pooexec.Command, out io.Writer) (pooexec.Process, error) {
	if len(c.Argv) == 0 {
		return nil, errors.New("bwrap: the command has no arguments")
	}
	// The sandbox is a process of its own, so a context that is over
	// before it starts would still let a fast command run first.
	if err := ctx.Err(); err != nil {
		return nil, err
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
	// The helper's descriptor is added to the command's environment; a
	// nil environment means the caller's own.
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = append(append([]string(nil), env...),
		fmt.Sprintf("%s=%d", statusFDEnv, statusFD),
		fmt.Sprintf("%s=%d", parentFDEnv, parentFD))
	// The sandbox runs in its own process group, so the whole group can
	// be signalled at once. The parent-death signal ties bubblewrap to
	// poo: --die-with-parent ties the sandbox to bubblewrap, so a poo
	// that dies without running a teardown takes the sandbox with it
	// instead of leaving it to init.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	statusR, statusW, err := os.Pipe()
	if err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	jsonR, jsonW, err := os.Pipe()
	if err != nil {
		pr.Close()
		pw.Close()
		statusR.Close()
		statusW.Close()
		return nil, err
	}
	// The parent-death pipe is the sandbox's tie to poo. The helper holds
	// the read end and poo the write end, so the kernel tells the helper
	// when poo ends, whatever ends it.
	parentR, parentW, err := os.Pipe()
	if err != nil {
		pr.Close()
		pw.Close()
		statusR.Close()
		statusW.Close()
		jsonR.Close()
		jsonW.Close()
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
			statusR.Close()
			statusW.Close()
			jsonR.Close()
			jsonW.Close()
			parentR.Close()
			parentW.Close()
			return nil, err
		}
		cmd.Stderr = errW
	}
	cmd.ExtraFiles = []*os.File{statusW, jsonW, parentR}

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		if errW != nil {
			errPr.Close()
			errW.Close()
		}
		statusR.Close()
		statusW.Close()
		jsonR.Close()
		jsonW.Close()
		parentR.Close()
		parentW.Close()
		return nil, err
	}
	// The parent's copies of the write ends are closed, so the pipes
	// reach EOF once every writer is gone.
	pw.Close()
	if errW != nil {
		errW.Close()
	}
	statusW.Close()
	jsonW.Close()
	parentR.Close()

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

	done := make(chan struct{})
	waited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		close(done)
		waited <- err
	}()

	p := &process{
		cmd:        cmd,
		pipes:      pipes,
		statusR:    statusR,
		jsonR:      jsonR,
		parentW:    parentW,
		waited:     waited,
		drains:     drains,
		done:       done,
		finished:   make(chan struct{}),
		statusDone: make(chan struct{}),
		childDone:  make(chan struct{}),
	}
	go p.readStatus()
	go p.readJSONStatus()
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
func (e *Executor) args(c pooexec.Command) ([]string, error) {
	helper, err := helperExecutable()
	if err != nil {
		return nil, err
	}
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
		// The helper is the sandbox's init, so the namespace lives
		// exactly as long as the helper does. Bubblewrap's own init is
		// a process between the sandbox and the command, and a parent
		// that dies while it starts up leaves it behind.
		"--as-pid-1",
		// A fresh /proc, /dev and /tmp: the command sees its own
		// processes and nobody else's.
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		// The helper reports on its own descriptors.
		"--json-status-fd", fmt.Sprintf("%d", jsonStatusFD),
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
	args = append(args, "--ro-bind-try", resolveBind, resolveBind)
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
	// The helper is mounted after the masks, so a protected path cannot
	// hide it. Only the helper is exposed: the rest of its directory is
	// the sandbox's own.
	args = append(args, "--ro-bind", helper, helperPath)
	args = append(args, "--chdir", c.Dir)
	args = append(args, "--", helperPath, helperArg)
	args = append(args, c.Argv...)
	return args, nil
}

// helperExecutable returns the binary the sandbox's helper runs.
//
// The helper is the running binary in its helper mode: a separate binary
// would have to be installed beside poo, and a shell cannot tell a signal
// death from an exit of 128 plus the signal.
func helperExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("bwrap: finding the helper's binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("bwrap: resolving the helper's binary %s: %w", path, err)
	}
	return resolved, nil
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
// A path inside a bound directory is hidden: a directory behind an empty
// tmpfs, a file behind an empty device. A path that is a symlink is hidden
// at the path it reaches, because a mount cannot land on a link. A path
// outside every bound directory is not in the sandbox at all, so there is
// nothing to hide.
// The masker is asked about every bind, not only the working directory,
// so a protected path inside a read-only or read-write bind is hidden too.
func (e *Executor) masks(root string) ([][]string, error) {
	if e.opts.Masker == nil {
		return nil, nil
	}
	paths, err := e.expandBinds(root)
	if err != nil {
		return nil, err
	}
	paths = append([]string(nil), paths...)
	// The shortest path first, so a directory is hidden before the
	// files inside it, and a file inside a hidden directory is not
	// masked again.
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) < len(paths[j]) })

	var out [][]string
	var hidden []string
	for _, path := range paths {
		path, ok := e.destination(path, root)
		if !ok || underAny(hidden, path) {
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

// destination returns the path to mask for one masker path, and whether
// the sandbox holds that path.
//
// A mount cannot land on a symlink, so a link is masked at the path it
// reaches: that is the path a command reads through it. A target outside
// every bind is not in the sandbox at all, and needs no mount; a link that
// cannot be resolved is not masked either. A bind's own path is the
// exception: the sandbox creates a directory of that name for the mount,
// so a bind whose host path is a symlink is still masked by name.
func (e *Executor) destination(path, root string) (string, bool) {
	if path == "" {
		return "", false
	}
	if !e.isBind(path, root) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", false
			}
			path = target
		}
	}
	if !e.bound(path, root) {
		return "", false
	}
	return path, true
}

// isBind reports whether a path is one of the directories the sandbox
// mounts.
func (e *Executor) isBind(path, root string) bool {
	if path == root || path == resolveBind {
		return true
	}
	for _, dir := range e.roBinds() {
		if dir == path {
			return true
		}
	}
	return slices.Contains(e.opts.RWBinds, path)
}

// expandBinds returns the protected paths the sandbox's binds reach.
//
// A bind's expansion is computed once and reused: the binds do not change
// between commands, and walking a large read-only bind for every command
// would cost more than the sandbox itself. The working directory is the
// other root, and a session that changes it gets a new expansion.
func (e *Executor) expandBinds(root string) ([]string, error) {
	roots := make([]string, 0, len(e.roBinds())+len(e.opts.RWBinds)+2)
	roots = append(roots, root)
	roots = append(roots, e.roBinds()...)
	roots = append(roots, e.opts.RWBinds...)
	roots = append(roots, resolveBind)

	var out []string
	for _, dir := range roots {
		if dir == "" {
			continue
		}
		paths, ok := e.cachedMasks(dir)
		if !ok {
			expanded, err := e.opts.Masker.Expand(dir)
			if err != nil {
				return nil, fmt.Errorf("bwrap: expanding the protected paths under %s: %w", dir, err)
			}
			paths = append([]string(nil), expanded...)
			e.cacheMasks(dir, paths)
		}
		out = append(out, paths...)
	}
	return out, nil
}

// cachedMasks returns a bind's expansion, when it has one.
func (e *Executor) cachedMasks(dir string) ([]string, bool) {
	e.masksMu.Lock()
	defer e.masksMu.Unlock()
	paths, ok := e.maskCache[dir]
	return paths, ok
}

// cacheMasks remembers a bind's expansion.
func (e *Executor) cacheMasks(dir string, paths []string) {
	e.masksMu.Lock()
	defer e.masksMu.Unlock()
	e.maskCache[dir] = paths
}

// bound reports whether a path is inside one of the directories the
// sandbox mounts.
func (e *Executor) bound(path, root string) bool {
	dirs := make([]string, 0, len(e.roBinds())+len(e.opts.RWBinds)+2)
	dirs = append(dirs, root)
	dirs = append(dirs, e.roBinds()...)
	dirs = append(dirs, e.opts.RWBinds...)
	dirs = append(dirs, resolveBind)
	for _, dir := range dirs {
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
// needs and mount the bind the executor was configured with.
//
// The check runs a trivial command in a sandbox with the executor's own
// binds. A host that does not allow unprivileged user namespaces fails
// here, and so does a read-write bind that does not exist: the executor is
// never handed out with a bind the sandbox cannot make.
func probe(binary string, opts Options) error {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	args := []string{"--unshare-all"}
	ro := opts.ROBinds
	if len(ro) == 0 {
		ro = DefaultROBinds
	}
	for _, dir := range ro {
		args = append(args, "--ro-bind-try", dir, dir)
	}
	for _, dir := range opts.RWBinds {
		args = append(args, "--bind", dir, dir)
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
	cmd *exec.Cmd
	// pipes are the read ends of the capture, one per stream.
	pipes   []*os.File
	statusR *os.File
	jsonR   *os.File
	// parentW is the write end of the parent-death pipe. Closing it
	// tells the helper that poo is ending.
	parentW *os.File
	waited  chan error
	// drains report that a stream has been read to its end.
	drains []chan struct{}
	// done is closed when the sandbox's own process has been reaped.
	done chan struct{}
	// finished is closed when the wait is over, so the context watcher
	// stops.
	finished chan struct{}
	// statusDone and childDone are closed when the helper's report and
	// bubblewrap's child pid have been read.
	statusDone chan struct{}
	childDone  chan struct{}
	// waitOnce keeps the teardown to one run.
	waitOnce sync.Once

	mu sync.Mutex
	// exited reports that the sandbox has ended. A kill after that
	// would signal a process that may belong to another one by now.
	exited bool
	// stopping reports that a kill is taking the sandbox, rather than
	// the command exiting on its own.
	stopping bool
	// stopped reports that a kill took the command. It is decided when
	// the sandbox's own process is reaped.
	stopped bool
	code    int
	err     error
	// status is what the helper reported about the command's wait.
	status statusReport
	// childPID is bubblewrap's child in the host's pid space: the
	// helper, which is the process a graceful kill signals.
	childPID int
}

// statusReport is the wait status the helper writes.
type statusReport struct {
	// Exit is the command's exit status.
	Exit *int `json:"exit,omitempty"`
	// Signal is the signal that killed the command.
	Signal *int `json:"signal,omitempty"`
}

// jsonStatus is one document of bubblewrap's status stream.
type jsonStatus struct {
	// ChildPID is the host pid of the sandbox's child.
	ChildPID int `json:"child-pid,omitempty"`
}

// readStatus reads the helper's report.
func (p *process) readStatus() {
	defer close(p.statusDone)
	defer p.statusR.Close()
	var report statusReport
	if err := json.NewDecoder(p.statusR).Decode(&report); err == nil {
		p.mu.Lock()
		p.status = report
		p.mu.Unlock()
	}
}

// readJSONStatus reads bubblewrap's status stream.
//
// The first document names the child, which is the process a graceful kill
// signals. The stream is drained to the end so bubblewrap never blocks on
// it.
func (p *process) readJSONStatus() {
	defer close(p.childDone)
	defer p.jsonR.Close()
	dec := json.NewDecoder(p.jsonR)
	for {
		var status jsonStatus
		if err := dec.Decode(&status); err != nil {
			return
		}
		if status.ChildPID > 0 {
			p.mu.Lock()
			p.childPID = status.ChildPID
			p.mu.Unlock()
		}
	}
}

// Wait waits for the sandbox to end and for everything in it to die.
//
// The exit code is the command's. A command that was killed has none, and
// its code is negative, whichever executor ran it: the helper reports the
// signal on a side channel, so an exit of 128 plus the signal is not
// mistaken for a killed process.
func (p *process) Wait() (int, error) {
	p.waitOnce.Do(func() {
		err := <-p.waited

		// The command's own process has ended: whatever the teardown
		// of what it left behind does, this is its outcome.
		p.mu.Lock()
		p.stopped = p.stopping
		p.mu.Unlock()

		// The helper's report arrives before bubblewrap exits, so the
		// wait is bounded and does not hold a finished call for long.
		select {
		case <-p.statusDone:
		case <-time.After(KillGrace):
		}

		p.mu.Lock()
		switch {
		case p.status.Signal != nil:
			// A killed process has no exit code.
			p.code = -1
		case p.status.Exit != nil:
			p.code = *p.status.Exit
		default:
			p.code = exitCode(p.cmd)
		}
		p.err = err
		p.exited = true
		p.mu.Unlock()

		// A descendant may still hold a pipe open. The wait is
		// bounded, so the call cannot hang on it.
		deadline := time.After(KillGrace)
		for _, drained := range p.drains {
			select {
			case <-drained:
			case <-deadline:
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

// Stopped reports whether the command was killed instead of exiting on
// its own.
func (p *process) Stopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// Kill stops the sandbox.
//
// The helper gets SIGTERM, and it gives the command the same graceful
// window the local executor gives: SIGTERM, then SIGKILL after the grace
// period. The helper is the sandbox's init, so the kernel ends the
// namespace when the helper exits; a descendant that left the process
// group dies with the namespace, which is the escape the local executor
// cannot close.
func (p *process) Kill() {
	p.mu.Lock()
	if p.exited {
		p.mu.Unlock()
		return
	}
	p.stopping = true
	child := p.childPID
	p.mu.Unlock()
	if child <= 0 {
		// The child may not have been read yet: a cancellation can
		// arrive as soon as the sandbox starts. Give the status stream
		// a moment, so the command gets the graceful signal too.
		select {
		case <-p.childDone:
		case <-p.done:
		case <-time.After(500 * time.Millisecond):
		}
		p.mu.Lock()
		child = p.childPID
		p.mu.Unlock()
	}
	if child <= 0 {
		// The child is not known yet: signal the sandbox itself, which
		// ends the namespace.
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		deadline := time.Now().Add(KillGrace)
		for time.Now().Before(deadline) {
			if !p.alive() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		_ = p.cmd.Process.Signal(syscall.SIGKILL)
		return
	}

	// The helper forwards SIGTERM to the command and kills it after the
	// grace period. The wait is slightly longer than the grace, so the
	// helper's own escalation can finish.
	_ = syscall.Kill(child, syscall.SIGTERM)
	deadline := time.Now().Add(KillGrace + 250*time.Millisecond)
	for time.Now().Before(deadline) {
		if !p.alive() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(child, syscall.SIGKILL)
	_ = p.cmd.Process.Signal(syscall.SIGKILL)
}

// closePipes closes the ends the wait has finished with.
func (p *process) closePipes() {
	for _, pipe := range p.pipes {
		pipe.Close()
	}
	p.parentW.Close()
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
