package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/prompt/agentsmd"
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

// register adds the run flags to a flag set.
func (o *runOptions) register(fs *flag.FlagSet) {
	o.sharedFlags.register(fs)
	o.endpointFlags.register(fs)
	fs.StringVar(&o.permissionFallback, "permission-fallback", "", "The decision when the asker chain is exhausted: allow or deny")
	fs.StringVar(&o.reasoningEffort, "reasoning-effort", "", "The reasoning_effort to send, for models that think only when asked")
	fs.IntVar(&o.maxTurns, "max-turns", 0, "The turn limit")
	fs.IntVar(&o.maxOutput, "max-output", 0, "The model's output-token limit")
	fs.StringVar(&o.logPath, "log", "", "The session log path")
	fs.StringVar(&o.workingDir, "working-dir", "", "The directory the tools work in")
	fs.StringVar(&o.executor, "executor", "", "The executor the bash tool runs its commands through")
	fs.BoolVar(&o.projectPrompt, "project-prompt", false, "Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt")
	fs.DurationVar(&o.timeout, "timeout", 0, "The wall-clock limit for the run")
	fs.BoolVar(&o.noWaitJobs, "no-wait-jobs", false, "Exit at the first settle; the running jobs stop through the session's close")
}

// runRun executes `poo run`.
func runRun(args []string) int {
	fs := flag.NewFlagSet("poo run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var o runOptions
	o.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := o.validate(fs.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}

	settings, err := o.settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}
	if err := o.apply(settings); err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}
	workDir, err := o.workingDirectory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}
	if settings.Roles[string(catalog.RoleChat)] == "" && o.endpoint == "" {
		fmt.Fprintf(os.Stderr, "poo run: --endpoint, --model-id and --context-limit are required, or a settings file that names a chat model\n")
		return 2
	}

	overrides, err := o.overrides()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}
	overrides.PermissionFallback = o.permissionFallback
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      overrides,
		WorkingDir:     workDir,
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
		// The command has no wire client, so the chain is the decide
		// asker alone, and it is there only when a decider model is
		// configured.
		Askers: []string{"decide"},
		Build:  poo.BuildInfo{Name: "poo", Version: version},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}
	if runtime.Warning != "" {
		fmt.Fprintf(os.Stderr, "poo run: warning: %s\n", runtime.Warning)
	}

	// The model the run talks to. The flags name it, and they change
	// what the settings and the catalog resolved.
	model := modelWithFlags(runtime, &o)
	runtime.Options.Model = model

	if err := addFinishTool(runtime); err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}

	transport, err := runtime.Transport(model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}

	// The project's instructions are opt-in: the flag asks for them, and
	// the loader reads the working directory only.
	project := ""
	if o.projectPrompt {
		text, err := agentsmd.Load(workDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo run: --project-prompt: %v\n", err)
			return 2
		}
		project = text
	}

	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	baseCtx, stop := context.WithCancel(context.Background())
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
			Build:           poo.BuildInfo{Name: "poo", Version: version},
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

	sink, err := openSink(ctx, runtime, created, o.logPath, workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}
	defer sink.Close()

	runtime.Options.Transport = transport
	runtime.Options.Sink = sink
	runtime.Options.SessionID = sessionID
	runtime.Options.ReasoningEffort = o.reasoningEffort
	runtime.Options.ToolsMode = string(model.Tools.Mode)
	runtime.Options.Prompt = append(runtime.Options.Prompt, promptMutation(runtime, model, project))

	if deciderModel, decider, err := runtime.Decider(); err == nil {
		runtime.Options.DeciderModel = deciderModel
		runtime.Options.Decider = decider
	}

	sess, code, err := newSession(runtime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return code
	}
	defer sess.Close()

	// The first interrupt cancels the run, and the run settles through
	// the normal path. A second interrupt is the operator saying the
	// finalization is stuck: it stops what it can and ends the process
	// at once.
	finished := make(chan struct{})
	defer close(finished)
	go watchInterrupts(interrupts, stop, sess.Close, finished, exitProcess)

	runID, err := sess.Prompt(ctx, strings.Join(fs.Args(), " "))
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}

	// The answer and the exit code belong to the run the client asked
	// for, not to a later run that a job result woke.
	res, err := sess.WaitRun(ctx, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "poo: settled %s after %d turn(s)\n", res.Reason, res.Turns)

	// The one-shot waits for the jobs, so the work the model put in the
	// background is done before the process goes. The answer is selected
	// after the wait, because a wake may produce the last run. The flag
	// turns the wait off, and the jobs stop through the session's close.
	answer, waitErr := runOutput(ctx, sess, res, o.noWaitJobs)
	if waitErr != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", waitErr)
	}

	// The run's answer goes to stdout: the finish result when the
	// client's run produced one, and the last assistant message
	// otherwise.
	if answer != "" {
		fmt.Println(answer)
	}

	// The held results do not keep the process alive: they wait for the
	// next load, and the client is told which ones they are.
	if held := sess.HeldJobs(); len(held) > 0 {
		fmt.Fprintf(os.Stderr, "poo: %d job result(s) held for the next run: %s\n", len(held), strings.Join(held, ", "))
	}

	// The exit code comes from the settle-reason table, combined
	// across the runs: the first non-zero code wins. A failed jobs
	// wait is the client's own context - the jobs stop through the
	// close, and the wait error is on stderr - so it does not add a
	// code of its own.
	return combinedExitCode(sess.SettleCodes())
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
func promptMutation(runtime *config.Runtime, model poo.ModelInfo, project string) func(*prompt.Builder) {
	tools := runtime.ToolSpecs()
	toolsMode := string(model.Tools.Mode)
	return systemPrompt(model.ModelID, tools, toolsMode, project)
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
		return &sink{log: log, path: logPath}, nil
	}
	if runtime.Store == nil {
		return &sink{}, nil
	}
	if _, err := runtime.Store.Create(ctx, created); err != nil {
		return nil, err
	}
	return &sink{store: runtime.Store, path: runtime.Store.Dir()}, nil
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
}

// Append writes one record.
func (s *sink) Append(ctx context.Context, rec poo.Record) error {
	switch {
	case s.log != nil:
		return s.log.Append(ctx, rec)
	case s.store != nil:
		return s.store.Append(ctx, rec)
	default:
		return nil
	}
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

// version is the build's version.
const version = "0.1.0"
