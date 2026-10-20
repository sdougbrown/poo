package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Defaults for the runner.
const (
	// DefaultSeeds is the number of times a task runs against a model.
	DefaultSeeds = 3
	// DefaultMaxTurns is the turn ceiling the runner passes to poo.
	DefaultMaxTurns = 50
	// DefaultTimeoutS is the wall-clock ceiling for one run, in seconds.
	DefaultTimeoutS = 300
	// MaxParallelPerEndpoint is the most runs that may share an
	// endpoint at the same time. Two models on one endpoint share its
	// capacity, so the runner does not pile on.
	MaxParallelPerEndpoint = 2
	// DefaultPooBinary is the binary the runner executes when the
	// caller names none.
	DefaultPooBinary = "poo"
)

// RunOptions configure one harness run.
type RunOptions struct {
	// Models is the catalog to run against.
	Models []Model
	// Tasks is the task set to run.
	Tasks []Task
	// TaskRoot is the directory the task set lives in. A fixture repo
	// is resolved relative to it.
	TaskRoot string
	// Seeds is the number of times each task runs against each model.
	Seeds int
	// Parallel is the most runs that may be in flight at the same time.
	Parallel int
	// MaxTurns is the turn ceiling passed to poo.
	MaxTurns int
	// Out is the directory the report and the logs go to.
	Out string
	// WorkRoot is the directory the workdirs are created under. It must
	// be on the same filesystem as the report, and it is never /tmp.
	WorkRoot string
	// PoоBinary is the poo executable the runs use.
	PooBinary string
	// KeepWorkdirs leaves the workdirs behind after the run.
	KeepWorkdirs bool
}

// RunResult is the outcome of one task x model x seed.
type RunResult struct {
	// Task, Model and Seed identify the run.
	Task  string `json:"task"`
	Model string `json:"model"`
	Seed  int    `json:"seed"`
	// Success reports whether check.sh exited zero.
	Success bool `json:"success"`
	// CheckExit is the exit code check.sh returned.
	CheckExit int `json:"check_exit"`
	// RunExit is the exit code the poo process returned.
	RunExit int `json:"run_exit"`
	// SettleReason is why the run settled, as the log says.
	SettleReason string `json:"settle_reason"`
	// Metrics is what the harness measured.
	Metrics Metrics `json:"metrics"`
	// LogPath is the session log.
	LogPath string `json:"log_path"`
	// CheckOutput is the tail of check.sh's output.
	CheckOutput string `json:"check_output,omitempty"`
	// DiffOutput is the diff the run left behind.
	DiffOutput string `json:"diff_output,omitempty"`
	// Error is set when the run could not be made at all: the binary
	// was missing, the task could not be checked out, the run timed out.
	Error string `json:"error,omitempty"`
	// DurationMS is the wall time of the whole run, harness included.
	DurationMS int64 `json:"duration_ms"`
}

// Report is the result of a harness run.
type Report struct {
	// StartedAt and FinishedAt bound the run.
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	// GitSHA is the commit the harness ran against.
	GitSHA string `json:"git_sha"`
	// Dir is where the report was written.
	Dir string `json:"dir"`
	// Results are the runs, in a stable order.
	Results []RunResult `json:"results"`
}

// Runner runs the harness.
type Runner struct {
	opts RunOptions
}

// NewRunner returns a runner with the options filled in.
func NewRunner(opts RunOptions) *Runner {
	if opts.Seeds <= 0 {
		opts.Seeds = DefaultSeeds
	}
	if opts.Parallel <= 0 {
		opts.Parallel = 1
	}
	if opts.MaxTurns <= 0 {
		opts.MaxTurns = DefaultMaxTurns
	}
	if opts.PooBinary == "" {
		opts.PooBinary = DefaultPooBinary
	}
	return &Runner{opts: opts}
}

// Seeds returns the number of times each task runs against each model.
func (r *Runner) Seeds() int { return r.opts.Seeds }

// Parallel returns the most runs that may be in flight at once.
func (r *Runner) Parallel() int { return r.opts.Parallel }

// Run executes the harness.
//
// Every task runs against every model, once per seed. The runs happen in
// parallel, up to the parallel limit, and no more than two of them share
// an endpoint at the same time.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	report := &Report{
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Dir:       r.opts.Out,
		GitSHA:    gitSHA(),
	}
	if err := os.MkdirAll(r.opts.Out, 0o755); err != nil {
		return nil, fmt.Errorf("eval: creating %s: %w", r.opts.Out, err)
	}
	if err := os.MkdirAll(r.opts.WorkRoot, 0o755); err != nil {
		return nil, fmt.Errorf("eval: creating %s: %w", r.opts.WorkRoot, err)
	}

	jobs := r.jobs()
	results := r.execute(ctx, jobs)

	report.Results = results
	sort.SliceStable(report.Results, func(i, j int) bool {
		a, b := report.Results[i], report.Results[j]
		if a.Task != b.Task {
			return a.Task < b.Task
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Seed < b.Seed
	})
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return report, nil
}

// job is one run to make.
type job struct {
	task  Task
	model Model
	seed  int
}

// jobs returns the runs the harness makes.
func (r *Runner) jobs() []job {
	var out []job
	for _, t := range r.opts.Tasks {
		for _, m := range r.opts.Models {
			for seed := 1; seed <= r.opts.Seeds; seed++ {
				out = append(out, job{task: t, model: m, seed: seed})
			}
		}
	}
	return out
}

// execute makes the runs.
//
// The parallel limit is global, and each endpoint has its own smaller
// limit, so two models on one endpoint do not both use it at once.
func (r *Runner) execute(ctx context.Context, jobs []job) []RunResult {
	results := make([]RunResult, len(jobs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, r.opts.Parallel)
	endpointSem := map[string]chan struct{}{}

	for i, j := range jobs {
		endpoint := j.model.Endpoint()
		semEP, ok := endpointSem[endpoint]
		if !ok {
			semEP = make(chan struct{}, MaxParallelPerEndpoint)
			endpointSem[endpoint] = semEP
		}

		wg.Add(1)
		go func(i int, j job, semEP chan struct{}) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			semEP <- struct{}{}
			defer func() { <-semEP }()

			results[i] = r.runOne(ctx, j)
		}(i, j, semEP)
	}
	wg.Wait()
	return results
}

// runOne makes one run: check out, run poo, run the check.
func (r *Runner) runOne(ctx context.Context, j job) RunResult {
	started := time.Now()
	result := RunResult{Task: j.task.ID, Model: j.model.Name, Seed: j.seed}

	// The report holds the log and the check's output. The workdir is
	// scratch, and lives under the work root, which is never /tmp.
	runDir := filepath.Join(r.opts.Out, "runs", j.task.ID, j.model.Name, "seed"+strconv.Itoa(j.seed))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		result.Error = fmt.Sprintf("creating %s: %v", runDir, err)
		return result
	}

	workRoot := filepath.Join(r.opts.WorkRoot, j.task.ID, j.model.Name, "seed"+strconv.Itoa(j.seed))
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		result.Error = fmt.Sprintf("creating %s: %v", workRoot, err)
		return result
	}
	workdir := filepath.Join(workRoot, "work")
	pristine := filepath.Join(workRoot, "pristine")
	if !r.opts.KeepWorkdirs {
		// The run's own scratch goes. The directories above it do not:
		// another seed of the same model may be running in one of them
		// at the same time, and removing it would pull the workdir out
		// from under that run.
		defer os.RemoveAll(workRoot)
	}

	workdir, err := r.checkout(ctx, j.task, workdir)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// The pristine tree, so the diff can be measured against it.
	if err := copyTree(workdir, pristine); err != nil {
		result.Error = fmt.Sprintf("copying the pristine tree: %v", err)
		return result
	}

	logPath := filepath.Join(runDir, "session.jsonl")
	result.LogPath = logPath
	runCtx, cancel := context.WithTimeout(ctx, r.timeout(j.task))
	defer cancel()

	pooExit, runErr := r.runPoo(runCtx, j, workdir, logPath)
	result.RunExit = pooExit
	if runErr != nil {
		result.Error = runErr.Error()
	}

	// The diff, measured against the pristine tree. It is measured
	// before the check runs, so the check can be told what the run
	// changed.
	diff, diffOut, diffErr := measureDiff(workdir, pristine)
	if diffErr != nil && result.Error == "" {
		result.Error = diffErr.Error()
	}
	result.DiffOutput = tail(diffOut, 8000)

	// The check runs in the workdir, whatever the run did.
	checkExit, checkOut, checkErr := r.runCheck(runCtx, j.task, workdir, logPath, diff.Files)
	result.CheckExit = checkExit
	result.CheckOutput = tail(checkOut, 4000)
	result.Success = checkExit == 0 && checkErr == nil
	if checkErr != nil && result.Error == "" {
		result.Error = checkErr.Error()
	}

	result.Metrics = r.metrics(j, logPath, diff)

	result.DurationMS = time.Since(started).Milliseconds()
	return result
}

// timeout returns the wall-clock ceiling for a task's run.
func (r *Runner) timeout(t Task) time.Duration {
	if t.TimeoutS > 0 {
		return time.Duration(t.TimeoutS) * time.Second
	}
	return DefaultTimeoutS * time.Second
}

// checkout prepares the workdir for a run.
//
// A fixture repo is copied; a git repo is cloned at its pinned sha. The
// directory is the one the caller names, which is under the work root,
// never in /tmp.
func (r *Runner) checkout(ctx context.Context, t Task, workdir string) (string, error) {
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", workdir, err)
	}

	if strings.HasPrefix(t.Repo, "fixture:") {
		src, err := t.FixtureDir(r.opts.TaskRoot)
		if err != nil {
			return "", err
		}
		if err := copyTree(src, workdir); err != nil {
			return "", fmt.Errorf("checking out fixture %s: %w", src, err)
		}
		return workdir, nil
	}

	// A git repo: "<url>@<sha>". The clone is shallow, and the sha is
	// checked out when the server allows it.
	url, sha, ok := strings.Cut(t.Repo, "@")
	if !ok {
		return "", fmt.Errorf("task %q: the git repo %q names no sha", t.ID, t.Repo)
	}
	args := []string{"clone", "--quiet"}
	if sha != "" {
		args = append(args, "--depth", "1")
	}
	args = append(args, url, workdir)
	cmd := exec.CommandContext(ctx, "git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cloning %s: %v: %s", url, err, strings.TrimSpace(out.String()))
	}
	if sha != "" {
		checkout := exec.CommandContext(ctx, "git", "-C", workdir, "checkout", "--quiet", sha)
		var co bytes.Buffer
		checkout.Stdout = &co
		checkout.Stderr = &co
		if err := checkout.Run(); err != nil {
			return "", fmt.Errorf("checking out %s: %v: %s", sha, err, strings.TrimSpace(co.String()))
		}
	}
	return workdir, nil
}

// runPoo runs the poo binary against a task.
//
// The binary is the real one: the harness measures what a user gets. The
// API key is passed by name, through --api-key-env, so the harness never
// reads the value and never writes it anywhere.
//
// The child runs in its own process group, and the group is killed when
// the run's context ends. Killing only the process would leave its
// children holding the output pipe, and the harness would wait for a
// run that was already over.
func (r *Runner) runPoo(ctx context.Context, j job, workdir, logPath string) (int, error) {
	args := []string{
		"run",
		"--base-url", j.model.BaseURL,
		"--model-id", j.model.ModelID,
		"--context-limit", strconv.Itoa(j.model.ContextLimit),
		"--tools-mode", j.model.EffectiveToolsMode(),
		"--max-turns", strconv.Itoa(r.opts.MaxTurns),
		"--log", logPath,
		"--working-dir", workdir,
	}
	if j.model.APIKeyEnv != "" {
		args = append(args, "--api-key-env", j.model.APIKeyEnv)
	}
	args = append(args, j.task.Prompt)

	cmd := exec.CommandContext(ctx, r.opts.PooBinary, args...)
	cmd.Dir = workdir
	// The run inherits the environment, so the key variable is there for
	// the binary to read. The binary scrubs it from the tools' child
	// environment itself.
	cmd.Env = os.Environ()
	// The child runs in its own process group, and the group is killed
	// when the run's context ends. Killing only the process would leave
	// its children holding the output pipe, and the harness would wait
	// for a run that was already over.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The cancel function kills the whole group, and the wait delay
	// bounds the time after it: the pipes are closed even if a child
	// holds them.
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
			err = nil
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return exit, fmt.Errorf("the run timed out after %s: %s", r.timeout(j.task), strings.TrimSpace(out.String()))
		}
		return exit, fmt.Errorf("running poo: %v: %s", err, strings.TrimSpace(out.String()))
	}
	return exit, nil
}

// killGroup kills a process group.
//
// The group gets a SIGTERM first and a SIGKILL after a short grace, so
// a child that ignores the first signal still goes away.
func killGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	// A negative pid names the process group.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return syscall.Kill(-pid, syscall.SIGKILL)
}

// runCheck runs a task's check.sh in the workdir.
//
// The check gets three environment variables, so it can see what the run
// did without the harness having to parse its output:
//
//   - POO_WORKDIR: the workdir's absolute path.
//   - POO_SESSION_LOG: the session log's absolute path. A read-only
//     task greps it for the answer the run recorded.
//   - POO_CHANGED_FILES: the paths the run changed, one per line.
//
// The check gets no network and a small timeout: a check that hangs must
// not hang the harness.
func (r *Runner) runCheck(ctx context.Context, t Task, workdir, logPath string, changed []string) (int, string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, "/bin/bash", filepath.Join(t.Dir, "check.sh"))
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"POO_CHECK=1",
		"POO_WORKDIR="+workdir,
		"POO_SESSION_LOG="+logPath,
		"POO_CHANGED_FILES="+strings.Join(changed, "\n"),
	)
	// The check runs in its own process group, and the group is killed
	// when the check's context ends. A check that leaves a child behind
	// must not hold the harness up.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
		} else {
			return exit, out.String(), fmt.Errorf("running check.sh: %w", err)
		}
	}
	return exit, out.String(), nil
}

// metrics measures a run from its log.
//
// A run whose log could not be read still gets metrics: the ones that do
// not need a log, like the diff.
func (r *Runner) metrics(j job, logPath string, diff Diff) Metrics {
	log, err := ParseLog(logPath)
	if err != nil {
		m := Metrics{SettleReason: "unknown", ChangedFiles: diff.Files, DiffLines: diff.Lines}
		return m
	}
	m := ComputeMetrics(log, diff)
	m.UnexpectedFiles = unexpectedFiles(j.task, diff.Files)
	return m
}

// unexpectedFiles returns the changed paths outside the task's expected
// paths.
//
// A task with no expected paths expects nothing in particular, so every
// change is unexpected: the signal is meant to be read.
func unexpectedFiles(t Task, changed []string) []string {
	if len(changed) == 0 {
		return nil
	}
	if len(t.ExpectedPaths) == 0 {
		return sortedStrings(append([]string(nil), changed...))
	}
	var out []string
	for _, f := range changed {
		if !matchesAny(t.ExpectedPaths, f) {
			out = append(out, f)
		}
	}
	return out
}

// matchesAny reports whether a path matches one of the globs.
func matchesAny(globs []string, path string) bool {
	for _, g := range globs {
		if matchGlob(g, path) {
			return true
		}
	}
	return false
}

// matchGlob reports whether a file path matches a glob.
//
// The glob is matched against the path's slash-separated form, and "**"
// matches any number of path segments, so "src/**" matches a file under
// src at any depth. A glob with no "**" matches the path itself or a
// path under it, so "src/main.go" and "src" both cover the file.
func matchGlob(glob, filePath string) bool {
	glob = filepath.ToSlash(glob)
	filePath = filepath.ToSlash(filePath)
	if glob == "" {
		return false
	}
	if ok, err := path.Match(glob, filePath); err == nil && ok {
		return true
	}
	if !strings.Contains(glob, "**") {
		// A directory glob covers everything under it.
		if strings.HasPrefix(filePath, strings.TrimSuffix(glob, "/")+"/") {
			return true
		}
	}
	return matchDoubleStar(glob, filePath)
}

// matchDoubleStar matches a glob holding "**" against a path.
//
// The glob is split on "**", and the parts around it must match the
// path's head and tail, with anything in between.
func matchDoubleStar(glob, filePath string) bool {
	parts := strings.Split(glob, "**")
	// The first part must match the head of the path.
	head := parts[0]
	if head != "" {
		if !strings.HasPrefix(filePath, head) {
			return false
		}
		filePath = filePath[len(head):]
	}
	// The last part must match the tail of the path.
	tail := parts[len(parts)-1]
	if tail != "" {
		if !strings.HasSuffix(filePath, tail) {
			return false
		}
		filePath = filePath[:len(filePath)-len(tail)]
	}
	// What is left must not cross a path segment, unless the glob leaves
	// it free.
	for _, mid := range parts[1 : len(parts)-1] {
		idx := strings.Index(filePath, mid)
		if idx < 0 {
			return false
		}
		filePath = filePath[idx+len(mid):]
	}
	return true
}

// tail returns the last n characters of a string.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// gitSHA returns the commit the harness ran against, or the empty string
// when the harness runs outside a repository.
func gitSHA() string {
	return GitSHA("")
}

// GitSHA returns a directory's repository's short commit.
//
// The empty directory means the working directory. An empty result means
// the directory is not in a repository.
func GitSHA(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	if dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}
