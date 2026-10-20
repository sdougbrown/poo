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
	// KillGrace is how long a killed process group has to finish on its
	// own before it is killed outright. The plan's process rule is
	// SIGTERM, wait two seconds, then SIGKILL.
	KillGrace = 2 * time.Second
	// CheckTimeout bounds one check script. The check runs on its own
	// clock, not the run's, so a run that used its whole budget does
	// not starve the check.
	CheckTimeout = 120 * time.Second
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
	// Force clears the out directory's runs directory before the run.
	// A run into a directory that already holds a run's output is
	// refused without it.
	Force bool
	// Catalog is every model the run set knows about, whether or not
	// this run uses it. The key variables the catalog names are removed
	// from every environment the harness builds, so a run against one
	// model never carries another model's credential.
	Catalog []Model
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
	// keyEnvs are the environment variables the catalog names as key
	// variables. They are removed from every environment the harness
	// builds, except the one the running model needs.
	keyEnvs []string
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
	// The binary's path is made absolute here: the run executes it in
	// the workdir, so a relative path would be resolved against the
	// workdir rather than against the caller's directory.
	if opts.PooBinary != "" {
		if abs, err := filepath.Abs(opts.PooBinary); err == nil {
			opts.PooBinary = abs
		}
	}
	// The out directory and the work root are made absolute here, not
	// in the run: poo resolves a relative --log against its own working
	// directory, and the harness opens the log against its own.
	if opts.Out != "" {
		if abs, err := filepath.Abs(opts.Out); err == nil {
			opts.Out = abs
		}
	}
	if opts.WorkRoot != "" {
		if abs, err := filepath.Abs(opts.WorkRoot); err == nil {
			opts.WorkRoot = abs
		}
	}
	// The key variables the catalog names are known before the run, so
	// every environment the harness builds can be scrubbed of them.
	// The catalog the caller names holds every model the run set knows
	// about; the models the run uses are added to it, so a run made
	// without a catalog still scrubs its own model's key.
	catalog := append([]Model(nil), opts.Catalog...)
	catalog = append(catalog, opts.Models...)
	return &Runner{opts: opts, keyEnvs: keyEnvs(catalog)}
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
//
// The out directory holds one run's output. A run into a directory that
// already holds a run's output is refused: the logs of the earlier run
// are there, and a run that reused one of them would report the earlier
// run's metrics. The --force option clears the runs directory first.
//
// The runs themselves live in a directory of this invocation's own, so
// two invocations never write over each other's logs.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	if err := r.prepareOut(); err != nil {
		return nil, err
	}
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
	// The runs live in a directory of this invocation's own, so a
	// rerun never lands on the previous run's log.
	runsDir, err := r.newRunDir()
	if err != nil {
		return nil, err
	}
	// The workdirs live under a directory of this invocation's own, so
	// two invocations that share a work root do not share a workdir.
	workRoot, err := r.newWorkRoot()
	if err != nil {
		return nil, err
	}
	if !r.opts.KeepWorkdirs {
		defer os.RemoveAll(workRoot)
	}

	jobs := r.jobs()
	results := r.execute(ctx, jobs, runsDir, workRoot)

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

// prepareOut makes the out directory ready for the run.
//
// The runs directory holds the logs. A directory that already holds a
// run's output is refused, because the earlier run's log is there and a
// run that landed on it would report the earlier run's metrics. The
// --force option clears the directory instead.
func (r *Runner) prepareOut() error {
	runsDir := filepath.Join(r.opts.Out, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("eval: reading %s: %w", runsDir, err)
	}
	if len(entries) == 0 {
		return nil
	}
	if !r.opts.Force {
		return fmt.Errorf("eval: %s already holds a run's output; use --force to clear it", runsDir)
	}
	if err := os.RemoveAll(runsDir); err != nil {
		return fmt.Errorf("eval: clearing %s: %w", runsDir, err)
	}
	return nil
}

// newRunDir creates the directory this invocation's runs live in.
//
// The directory carries a name of its own, so a rerun never lands on the
// previous run's log.
func (r *Runner) newRunDir() (string, error) {
	name, err := randomName()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(r.opts.Out, "runs", "run-"+name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("eval: creating %s: %w", dir, err)
	}
	return dir, nil
}

// newWorkRoot creates the directory this invocation's workdirs live in.
//
// The directory carries a name of its own, so two invocations that share
// a work root do not share a workdir.
func (r *Runner) newWorkRoot() (string, error) {
	name, err := randomName()
	if err != nil {
		return "", err
	}
	root := filepath.Join(r.opts.WorkRoot, "run-"+name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("eval: creating %s: %w", root, err)
	}
	return root, nil
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
func (r *Runner) execute(ctx context.Context, jobs []job, runsDir, workRoot string) []RunResult {
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

			results[i] = r.runOne(ctx, j, runsDir, workRoot)
		}(i, j, semEP)
	}
	wg.Wait()
	return results
}

// runOne makes one run: check out, run poo, run the check.
func (r *Runner) runOne(ctx context.Context, j job, runsDir, workRoot string) RunResult {
	started := time.Now()
	result := RunResult{Task: j.task.ID, Model: j.model.Name, Seed: j.seed}

	// The report holds the log and the check's output. The workdir is
	// scratch, and lives under the work root, which is never /tmp.
	runDir := filepath.Join(runsDir, j.task.ID, j.model.Name, "seed"+strconv.Itoa(j.seed))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		result.Error = fmt.Sprintf("creating %s: %v", runDir, err)
		return result
	}

	seedRoot := filepath.Join(workRoot, j.task.ID, j.model.Name, "seed"+strconv.Itoa(j.seed))
	if err := os.MkdirAll(seedRoot, 0o755); err != nil {
		result.Error = fmt.Sprintf("creating %s: %v", seedRoot, err)
		return result
	}
	workdir := filepath.Join(seedRoot, "work")
	pristine := filepath.Join(seedRoot, "pristine")
	if !r.opts.KeepWorkdirs {
		// The run's own scratch goes. The directories above it do not:
		// another seed of the same model may be running in one of them
		// at the same time, and removing it would pull the workdir out
		// from under that run.
		defer os.RemoveAll(seedRoot)
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

	// The fixture's own tests are put back before the check runs, so a
	// model that rewrote them cannot make the check pass. A change to
	// a test file is still in the diff, so it is reported as an
	// unexpected change.
	if !j.task.AllowTestEdits {
		if err := restoreTests(pristine, workdir); err != nil {
			if result.Error == "" {
				result.Error = fmt.Sprintf("restoring the fixture's tests: %v", err)
			}
		}
	}

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

	// A git repo: "<url>@<sha>". The sha is the part after the last
	// "@", so an SSH url ("git@host:repo") keeps its own "@".
	url, sha, ok := splitRepoSHA(t.Repo)
	if !ok {
		return "", fmt.Errorf("task %q: the git repo %q names no sha", t.ID, t.Repo)
	}
	args := []string{"clone", "--quiet"}
	args = append(args, url, workdir)
	cmd := exec.CommandContext(ctx, "git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cloning %s: %v: %s", url, err, strings.TrimSpace(out.String()))
	}
	if sha != "" {
		// The clone is shallow, so the sha may not be in it. The sha is
		// fetched on its own when it is missing.
		if !r.gitHasSHA(ctx, workdir, sha) {
			fetch := exec.CommandContext(ctx, "git", "-C", workdir, "fetch", "--quiet", "--depth", "1", "origin", sha)
			var fo bytes.Buffer
			fetch.Stdout = &fo
			fetch.Stderr = &fo
			if err := fetch.Run(); err != nil {
				return "", fmt.Errorf("fetching %s: %v: %s", sha, err, strings.TrimSpace(fo.String()))
			}
		}
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

// splitRepoSHA splits a task's repo into its url and its pinned sha.
//
// The sha is the part after the last "@", so an SSH url ("git@host:org
// /repo") keeps the "@" it owns.
func splitRepoSHA(repo string) (url, sha string, ok bool) {
	idx := strings.LastIndex(repo, "@")
	if idx < 0 {
		return repo, "", false
	}
	url = repo[:idx]
	sha = repo[idx+1:]
	if url == "" || sha == "" {
		return repo, "", false
	}
	return url, sha, true
}

// gitHasSHA reports whether a clone holds a commit.
func (r *Runner) gitHasSHA(ctx context.Context, dir, sha string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "cat-file", "-e", sha+"^{commit}")
	return cmd.Run() == nil
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
		// poo is given the run's own deadline, so it can settle by
		// itself and write its settle record rather than being killed
		// mid-write.
		"--timeout", r.timeout(j.task).String(),
	}
	if j.model.APIKeyEnv != "" {
		args = append(args, "--api-key-env", j.model.APIKeyEnv)
	}
	args = append(args, j.task.Prompt)

	cmd := exec.CommandContext(ctx, r.opts.PooBinary, args...)
	cmd.Dir = workdir
	// The run inherits the environment, so the key variable is there for
	// the binary to read. Every other key variable the catalog names is
	// gone, so a run against a model with no key never carries another
	// model's credential.
	cmd.Env = r.pooEnv(j.model)
	// The python the model runs must not leave bytecode behind, and the
	// check's own python must not either.
	cmd.Env = append(cmd.Env, "PYTHONDONTWRITEBYTECODE=1")
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
			// A process the harness killed reports a negative exit
			// code, and there is no error to report beyond that: the
			// run's own context says why it was killed.
			if exit >= 0 {
				err = nil
			} else {
				err = fmt.Errorf("the process was killed by signal %d", -exit)
			}
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
// The group gets a SIGTERM first and a SIGKILL after a two-second
// grace, so a process that needs a moment to write its settle record
// still has the time to write it.
func killGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	// A negative pid names the process group.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		return err
	}
	time.Sleep(KillGrace)
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
//
// The check runs on its own clock, not the run's. A run that used its
// whole budget would otherwise leave the check no time at all, and the
// check would fail with a cancellation even though the model finished.
//
// The check's environment holds no key variable: its output goes into
// the report, and a check that prints its environment must not print a
// credential.
func (r *Runner) runCheck(ctx context.Context, t Task, workdir, logPath string, changed []string) (int, string, error) {
	checkCtx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, "/bin/bash", filepath.Join(t.Dir, "check.sh"))
	cmd.Dir = workdir
	cmd.Env = r.checkEnv(
		"POO_CHECK=1",
		"POO_WORKDIR="+workdir,
		"POO_SESSION_LOG="+logPath,
		"POO_CHANGED_FILES="+strings.Join(changed, "\n"),
		// The check's own python must not leave bytecode in the
		// workdir, or the diff would show files the model never
		// touched.
		"PYTHONDONTWRITEBYTECODE=1",
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
//
// A change to a test file is always unexpected, unless the task's own
// prompt asks the run to change the tests. The fixture's tests are the
// task's own, and a run that rewrote them changed the measuring
// instrument rather than the thing under test.
func unexpectedFiles(t Task, changed []string) []string {
	if len(changed) == 0 {
		return nil
	}
	var out []string
	for _, f := range changed {
		if (!t.AllowTestEdits && isTestFile(f)) || !matchesAny(t.ExpectedPaths, f) {
			out = append(out, f)
		}
	}
	return out
}

// isTestFile reports whether a changed path is a test file.
func isTestFile(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p != TestsDir {
			continue
		}
		// The last component is the file's name, so a path under a
		// tests directory is a test file.
		if i == len(parts)-1 {
			return true
		}
	}
	return false
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
