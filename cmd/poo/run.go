package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/provider/catalog"
	"github.com/sdougbrown/poo/session"
)

// runOptions are the flags of `poo run`.
type runOptions struct {
	sharedFlags
	endpointFlags
	// permissionFallback is the decision when the asker chain is
	// exhausted.
	permissionFallback string
	// reasoningEffort is the reasoning_effort the run sends.
	reasoningEffort string
	// maxTurns is the turn limit.
	maxTurns int
	// maxOutput is the model's output-token limit.
	maxOutput int
	// contextStrategy is the compaction strategy the run pins, when
	// the flag names one.
	contextStrategy string
	// contextKeepRecent is the verbatim tail the compaction keeps,
	// when the flag names one.
	contextKeepRecent int
	// logPath is the session log path.
	logPath string
	// workingDir is the directory the tools work in.
	workingDir string
	// executor is the executor the bash tool runs its commands
	// through.
	executor string
	// projectPrompt includes the working directory's project
	// instructions in the prompt.
	projectPrompt bool
	// timeout is the wall-clock limit for the run.
	timeout time.Duration
	// noWaitJobs exits at the first settle, and the running jobs stop
	// through the session's close.
	noWaitJobs bool
}

// runFlagSpecs declares the flags of `poo run`: the endpoint, then the
// run's own, in the order the help prints them.
func runFlagSpecs() []clicontract.Flag {
	specs := append(endpointFlagSpecs(),
		clicontract.Flag{Name: "permission-fallback", Kind: clicontract.String, Usage: "The decision when the asker chain is exhausted: allow or deny"},
		clicontract.Flag{Name: "reasoning-effort", Kind: clicontract.String, Usage: "The reasoning_effort to send, for models that think only when asked"},
		clicontract.Flag{Name: "max-turns", Kind: clicontract.Int, Usage: "The turn limit"},
		clicontract.Flag{Name: "max-output", Kind: clicontract.Int, Usage: "The model's output-token limit"},
		clicontract.Flag{Name: "context-strategy", Kind: clicontract.String, Usage: "The compaction strategy: summary, mine, pointer, native or handoff"},
		clicontract.Flag{Name: "context-keep-recent", Kind: clicontract.Int, Usage: "The verbatim tail the compaction keeps, in tokens"},
		clicontract.Flag{Name: "log", Kind: clicontract.String, Usage: "The session log path"},
	)
	specs = append(specs, toolFlagSpecs()...)
	specs = append(specs, projectPromptFlagSpecs()...)
	return append(specs,
		clicontract.Flag{Name: "timeout", Kind: clicontract.Duration, Usage: "The wall-clock limit for the run, e.g. 5m"},
		clicontract.Flag{Name: "no-wait-jobs", Kind: clicontract.Bool, Usage: "Exit at the first settle; the running jobs stop through the session's close"},
	)
}

// bind reads the run's flags out of a parsed invocation.
func (o *runOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.endpointFlags.bind(inv)
	o.permissionFallback = inv.Flags["permission-fallback"]
	o.reasoningEffort = inv.Flags["reasoning-effort"]
	o.maxTurns = inv.Ints["max-turns"]
	o.maxOutput = inv.Ints["max-output"]
	o.contextStrategy = inv.Flags["context-strategy"]
	o.contextKeepRecent = inv.Ints["context-keep-recent"]
	o.logPath = inv.Flags["log"]
	o.workingDir = inv.Flags["working-dir"]
	o.executor = inv.Flags["executor"]
	o.projectPrompt = inv.Bools["project-prompt"]
	o.timeout = inv.Durations["timeout"]
	o.noWaitJobs = inv.Bools["no-wait-jobs"]
}

// handleRun executes `poo run`.
func handleRun(parent context.Context, inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o runOptions
	o.bind(inv)
	if err := o.validate(inv.Args); err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}

	// The working directory comes first: the settings are discovered
	// from it, so a run pointed at a project reads that project's
	// settings.
	workDir, err := o.workingDirectory()
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}
	settings, err := o.settings(workDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}
	if err := o.apply(settings); err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}
	if settings.Roles[string(catalog.RoleChat)] == "" && o.endpoint == "" {
		fmt.Fprintf(stderr, "poo run: --endpoint, --model-id and --context-limit are required, or a settings file that names a chat model\n")
		return exit{usageExitCode}
	}

	overrides, err := o.overrides()
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}
	overrides.ProjectPrompt = o.projectPrompt
	overrides.PermissionFallback = o.permissionFallback
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      config.EnvOverrides(overrides),
		WorkingDir:     workDir,
		DefaultProfile: defaultProfile,
		UserPromptFile: defaultUserPromptFile(),
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
		// The command has no wire client, so the chain is the decide
		// asker alone, and it is there only when a decider model is
		// configured.
		Askers: []string{"decide"},
		Build:  poo.BuildInfo{Name: "poo", Version: buildVersion().String()},
	})
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}
	if runtime.Warning != "" {
		fmt.Fprintf(stderr, "poo run: warning: %s\n", runtime.Warning)
	}

	// The model the run talks to. The flags name it, and they change
	// what the settings and the catalog resolved.
	model := modelWithFlags(runtime, &o)
	runtime.Options.Model = model

	if err := addFinishTool(runtime); err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}

	transport, err := runtime.Transport(model)
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{operationalExitCode}
	}

	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	baseCtx, stop := context.WithCancel(parent)
	defer stop()
	ctx := baseCtx
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(baseCtx, o.timeout)
		defer cancel()
	}

	// The log records the models without their credentials: a URL may
	// carry a key, and the log is a file a consumer reads.
	chatModel := session.LoggedModel(model)

	sessionID := session.NewID()
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{
			Format:     poo.FormatVersion,
			SessionID:  sessionID,
			Mode:       runtime.SessionMode,
			CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
			WorkingDir: workDir,
			Tools:      runtime.SessionTools(),
			// The metadata says what the session ran with: the profile
			// it was built from, and the models it talked to.
			Profile:         runtime.Profile.Name,
			Roles:           poo.ModelRoles{Chat: &chatModel},
			Build:           poo.BuildInfo{Name: "poo", Version: buildVersion().String()},
			Model:           poo.SessionModel{ModelID: model.ModelID, Endpoint: session.LoggedEndpoint(model.Endpoint), MaxOutput: model.MaxOutput},
			ContextLimit:    model.ContextLimit,
			ToolsMode:       poo.ToolsMode(model.Tools.Mode),
			ReasoningEffort: o.reasoningEffort,
		},
	}
	if deciderModel, _, err := runtime.Decider(); err == nil {
		deciderModel = session.LoggedModel(deciderModel)
		created.Roles.Decider = &deciderModel
	}
	if runtime.Options.SideModel.ModelID != "" {
		sideModel := session.LoggedModel(runtime.Options.SideModel)
		created.Roles.Compaction = &sideModel
	}

	sink, err := openSink(ctx, runtime, created, o.logPath, workDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{usageExitCode}
	}
	defer sink.Close()

	runtime.Options.Transport = transport
	runtime.Options.Sink = sink
	runtime.Options.SessionDir = sink.sessionDir()
	runtime.Options.SessionID = sessionID
	runtime.Options.ReasoningEffort = o.reasoningEffort
	runtime.Options.ToolsMode = string(model.Tools.Mode)
	runtime.Options.Prompt = append(runtime.Options.Prompt, promptMutation(runtime, model))
	runtime.Options.Observers = append(runtime.Options.Observers, warningObserver{w: stderr})

	if deciderModel, decider, err := runtime.Decider(); err == nil {
		runtime.Options.DeciderModel = deciderModel
		runtime.Options.Decider = decider
	}

	sess, code, err := newSession(runtime)
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{code}
	}
	defer sess.Close()

	// The first interrupt cancels the run, and the run settles through
	// the normal path. A second interrupt is the operator saying the
	// finalization is stuck: it stops what it can and ends the process
	// at once.
	finished := make(chan struct{})
	defer close(finished)
	go watchInterrupts(interrupts, stop, sess.Close, finished, exitProcess)

	runID, err := sess.Prompt(ctx, strings.Join(inv.Args, " "))
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{operationalExitCode}
	}

	// The answer and the exit code belong to the run the client asked
	// for, not to a later run that a job result woke.
	res, err := sess.WaitRun(ctx, runID)
	if err != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", err)
		return exit{operationalExitCode}
	}

	fmt.Fprintf(stderr, "poo: settled %s after %d turn(s)\n", res.Reason, res.Turns)

	// The one-shot waits for the jobs, so the work the model put in the
	// background is done before the process goes. The answer is selected
	// after the wait, because a wake may produce the last run. The flag
	// turns the wait off, and the jobs stop through the session's close.
	answer, waitErr := runOutput(ctx, sess, res, o.noWaitJobs)
	if waitErr != nil {
		fmt.Fprintf(stderr, "poo run: %v\n", waitErr)
	}

	// The run's answer goes to stdout: the finish result when the
	// client's run produced one, and the last assistant message
	// otherwise.
	if answer != "" {
		fmt.Fprintln(stdout, answer)
	}

	// The held results do not keep the process alive: they wait for the
	// next load, and the client is told which ones they are.
	if held := sess.HeldJobs(); len(held) > 0 {
		fmt.Fprintf(stderr, "poo: %d job result(s) held for the next run: %s\n", len(held), strings.Join(held, ", "))
	}

	// The exit code comes from the settle-reason table, combined
	// across the runs: the first non-zero code wins. A failed jobs
	// wait is the client's own context - the jobs stop through the
	// close, and the wait error is on stderr - so it does not add a
	// code of its own.
	return exit{combinedExitCode(sess.SettleCodes())}
}

// warningObserver prints the session warnings the run itself reports.
//
// The one-shot prints no events, so a warning the session learned while
// it ran would otherwise reach the discard logger and no one else.
type warningObserver struct{ w io.Writer }

// Observe prints the warnings the one-shot cares about.
func (o warningObserver) Observe(ev poo.Event) error {
	if ev.Name != poo.EventModelOutputLimit {
		return nil
	}
	message, _ := ev.Payload["message"].(string)
	if message == "" {
		return nil
	}
	_, err := fmt.Fprintf(o.w, "poo run: warning: %s\n", message)
	return err
}

// The exit code a second interrupt leaves: the one a cancelled run
// leaves.
const secondSignalExitCode = 130

// secondSignalStopWait bounds the teardown a second interrupt gives the
// session before it ends the process.
const secondSignalStopWait = 250 * time.Millisecond

// exitProcess ends the process. A test replaces it to observe the
// second interrupt without ending the test binary.
var exitProcess = os.Exit

// watchInterrupts waits for the operator's interrupts.
//
// The first one cancels the run. A second one stops the session, which
// takes the jobs down with it, and ends the process at once.
func watchInterrupts(interrupts <-chan os.Signal, cancel context.CancelFunc, closeSession func() error, finished <-chan struct{}, exit func(int)) {
	select {
	case <-interrupts:
		cancel()
	case <-finished:
		return
	}
	select {
	case <-interrupts:
		done := make(chan struct{})
		go func() {
			if closeSession != nil {
				_ = closeSession()
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(secondSignalStopWait):
		}
		exit(secondSignalExitCode)
	case <-finished:
	}
}

// modelWithFlags returns the model the run talks to.
//
// The catalog resolves the model the settings name, and the flags
// change what it resolved: the limit and the reasoning control are
// the operator's.
func modelWithFlags(runtime *config.Runtime, o *runOptions) poo.ModelInfo {
	model := runtime.Options.Model
	if o.maxOutput > 0 {
		model.MaxOutput = o.maxOutput
	}
	if o.reasoningEffort != "" {
		model.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: poo.ReasoningPathEffort}
	}
	return model
}

// validate checks the flags and the prompt.
func (o *runOptions) validate(rest []string) error {
	if err := o.checkUnmatched(); err != nil {
		return err
	}
	switch o.permissionFallback {
	case "", "allow", "deny":
	default:
		return errors.New("--permission-fallback must be allow or deny")
	}
	if err := o.endpointFlags.validate(); err != nil {
		return err
	}
	if o.maxTurns < 0 {
		return errors.New("--max-turns must not be negative")
	}
	if o.maxOutput < 0 {
		return errors.New("--max-output must not be negative")
	}
	switch o.contextStrategy {
	case "", "summary", "mine", "pointer", "native", "handoff":
	default:
		return errors.New("--context-strategy must be summary, mine, pointer, native or handoff")
	}
	if o.contextKeepRecent < 0 {
		return errors.New("--context-keep-recent must not be negative")
	}
	if len(rest) == 0 {
		return errors.New("a prompt is required")
	}
	return nil
}

// apply turns the flags into settings.
//
// The flags come last: they change what the settings file says. A flag
// that was not given changes nothing.
func (o *runOptions) apply(settings *config.Settings) error {
	o.endpointFlags.apply(settings)
	if o.maxTurns > 0 {
		settings.Limits.MaxTurns = o.maxTurns
	}
	if o.contextStrategy != "" {
		strategy := o.contextStrategy
		settings.Context.Strategy = &strategy
	}
	if o.contextKeepRecent > 0 {
		// The tail is a per-strategy setting, and the flag names one
		// tail for whichever strategy runs: the three block
		// strategies all take it, and the ones without a tail ignore
		// it.
		keep := o.contextKeepRecent
		settings.Context.Summary.KeepRecentTokens = &keep
		settings.Context.Mine.KeepRecentTokens = &keep
		settings.Context.Pointer.KeepRecentTokens = &keep
	}
	// The timeout is the run's deadline, not its budget: the deadline
	// ends the run, and the settings' limits.max_duration_s is the
	// budget the run measures itself against.
	if o.executor != "" {
		executor := o.executor
		settings.Tools.Bash.Executor = &executor
	}
	return nil
}

// workingDirectory returns the directory the tools work in.
//
// The directory has to exist: a typo would otherwise create an empty
// directory and run the task in it.
func (o *runOptions) workingDirectory() (string, error) {
	return workingDirectory(o.workingDir)
}

// newSession constructs the session from the runtime, and returns the
// exit code a construction failure takes. The caller prints the error.
//
// The prompt renders again here, after the command has put its own tool
// and its own sections in the options the build checked. A prompt that
// does not render in that environment is the settings' failure: it
// carries the build's message and takes the build's exit code. Every
// other construction failure is the run's own.
func newSession(runtime *config.Runtime) (*poo.Session, int, error) {
	edits, err := runtime.SessionPrompt(runtime.Options.WorkingDir)
	if err != nil {
		return nil, 2, err
	}
	runtime.Options.Prompt = append(runtime.Options.Prompt, edits...)
	sess, err := poo.New(runtime.Options, poo.WithPolicy(runtime.Policy))
	if err == nil {
		return sess, 0, nil
	}
	if errors.Is(err, poo.ErrPromptRender) {
		return nil, 2, config.PromptRenderError(err)
	}
	return nil, 1, err
}

// promptMutation returns the prompt the command adds.
//
// The command's own sections come after the settings': the flags come
// last, so they change what the settings file said.
func promptMutation(runtime *config.Runtime, model poo.ModelInfo) func(*prompt.Builder) {
	tools := runtime.ToolSpecs()
	toolsMode := string(model.Tools.Mode)
	return systemPrompt(model.ModelID, tools, toolsMode)
}

// openSink opens the session's records.
//
// The records go to the log the caller asked for, or to the store the
// settings name. An ephemeral session writes nothing.
func openSink(ctx context.Context, runtime *config.Runtime, created *poo.SessionCreatedRecord, logPath, workDir string) (*sink, error) {
	if logPath != "" {
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Join(workDir, logPath)
		}
		log, err := session.Open(logPath)
		if err != nil {
			return nil, err
		}
		if err := log.Append(ctx, created); err != nil {
			log.Close()
			return nil, err
		}
		return &sink{log: log, path: logPath, recordDir: filepath.Dir(logPath)}, nil
	}
	if runtime.Store == nil {
		return &sink{}, nil
	}
	if _, err := runtime.Store.Create(ctx, created); err != nil {
		return nil, err
	}
	return &sink{store: runtime.Store, path: runtime.Store.Dir(), recordDir: runtime.Store.Dir()}, nil
}

// sink is where a session's records go.
//
// A sink holds a log file, a store, or nothing at all: an ephemeral
// session writes nothing.
type sink struct {
	// log is the log file the records go to.
	log *session.Log
	// store is the store the records go to.
	store session.Store
	// path is where the records go, for the message the command
	// prints.
	path string
	// recordDir is the directory the records are in, which the session
	// keeps its own files beside. It is empty for a sink that writes
	// nothing.
	recordDir string
	// mu guards records.
	mu sync.Mutex
	// records are the records this process wrote, in order, for the
	// history tool's read side.
	records []poo.Record
}

// recordsSnapshot returns the records this process wrote.
func (s *sink) recordsSnapshot() []poo.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]poo.Record, len(s.records))
	copy(out, s.records)
	return out
}

// sessionDir returns the directory the session's files are in.
func (s *sink) sessionDir() string { return s.recordDir }

// Append writes one record.
func (s *sink) Append(ctx context.Context, rec poo.Record) error {
	// A log or a store owns the records: the write is the only copy.
	// With neither, the fallback owns them itself and keeps them.
	switch {
	case s.log != nil:
		return s.log.Append(ctx, rec)
	case s.store != nil:
		return s.store.Append(ctx, rec)
	default:
		s.keep(rec)
		return nil
	}
}

// keep holds a record in the fallback's own buffer. Nothing else owns
// the records here, so the buffer is the read side's whole supply.
func (s *sink) keep(rec poo.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
}

// Flush pushes the buffered records out.
func (s *sink) Flush(ctx context.Context) error {
	switch {
	case s.log != nil:
		return s.log.Flush(ctx)
	case s.store != nil:
		return s.store.Flush(ctx)
	default:
		return nil
	}
}

// Records reads the session's records back, oldest first.
//
// It is the read side poo.RecordReader asks for: with it, the session's
// history tool reads what the store retained. The log path reads its
// file; the store path reads through the store's open session; with
// neither, the fallback's own buffer is the whole supply.
func (s *sink) ReadRecords(ctx context.Context) ([]poo.Record, error) {
	switch {
	case s.log != nil:
		return s.log.ReadRecords(ctx)
	case s.store != nil:
		// The store is open under this process's authority and holds the
		// whole retained chain, restored sessions included: the read goes
		// through it, so the retention pointer's numbering is the one
		// the session was built with.
		return s.store.ReadRecords(ctx)
	default:
		// Nothing owns the records: the buffer is what there is.
		return s.recordsSnapshot(), nil
	}
}

// Close releases the sink's resources.
func (s *sink) Close() error {
	switch {
	case s.log != nil:
		return s.log.Close()
	case s.store != nil:
		return s.store.Close()
	default:
		return nil
	}
}

// finalAnswer returns the run's answer, as the client asked for it.
//
// The finish result is the typed answer when the run produced one, and
// the last assistant message is the answer otherwise.
func finalAnswer(sess *poo.Session, res poo.Result) string {
	if res.Detail.Result != nil {
		return string(res.Detail.Result)
	}
	return sess.LastAssistantMessage()
}

// runOutput waits for the jobs and then selects the answer to print.
//
// The client run's finish result is fixed by its settle, so it is kept
// across the wait. Without one, the answer is the last run's final
// assistant message, which is only known once the jobs and their wakes
// are done.
func runOutput(ctx context.Context, sess *poo.Session, res poo.Result, noWaitJobs bool) (string, error) {
	if !noWaitJobs {
		if err := sess.WaitJobs(ctx); err != nil {
			// The wait failed, but the run itself may have finished:
			// its answer is already fixed at settle, so it survives
			// the failed wait.
			return finalAnswer(sess, res), err
		}
	}
	return finalAnswer(sess, res), nil
}

// combinedExitCode combines the exit codes of the runs that settled.
//
// The first non-zero code wins, and a session whose every run succeeded
// has the code zero.
func combinedExitCode(codes []int) int {
	for _, code := range codes {
		if code != 0 {
			return code
		}
	}
	return 0
}
