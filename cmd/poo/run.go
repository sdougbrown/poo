package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
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
	"github.com/sdougbrown/poo/session"
)

// runOptions are the flags of `poo run`.
type runOptions struct {
	sharedFlags
	// permissionFallback is the decision when the asker chain is
	// exhausted.
	permissionFallback string
	// baseURL is the endpoint's base URL.
	baseURL string
	// modelID is the model identifier.
	modelID string
	// contextLimit is the model's context window in tokens.
	contextLimit int
	// apiKeyEnv is the environment variable holding the API key.
	apiKeyEnv string
	// toolsMode is how the run asks for tool calls.
	toolsMode string
	// reasoningEffort is the reasoning_effort the run sends.
	reasoningEffort string
	// maxTurns is the turn ceiling.
	maxTurns int
	// maxOutput is the model's output-token ceiling.
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
	// timeout is the wall-clock ceiling for the run.
	timeout time.Duration
	// noWaitJobs exits at the first settle, and the running jobs stop
	// through the session's close.
	noWaitJobs bool
}

// register adds the run flags to a flag set.
func (o *runOptions) register(fs *flag.FlagSet) {
	o.sharedFlags.register(fs)
	fs.StringVar(&o.permissionFallback, "permission-fallback", "", "The decision when the asker chain is exhausted: allow or deny")
	fs.StringVar(&o.baseURL, "base-url", "", "The endpoint's base URL")
	fs.StringVar(&o.modelID, "model-id", "", "The model identifier")
	fs.IntVar(&o.contextLimit, "context-limit", 0, "The model's context window in tokens")
	fs.StringVar(&o.apiKeyEnv, "api-key-env", "", "The environment variable holding the API key")
	fs.StringVar(&o.toolsMode, "tools-mode", "", "native, text or auto")
	fs.StringVar(&o.reasoningEffort, "reasoning-effort", "", "The reasoning_effort to send, for models that think only when asked")
	fs.IntVar(&o.maxTurns, "max-turns", 0, "The turn ceiling")
	fs.IntVar(&o.maxOutput, "max-output", 0, "The model's output-token ceiling")
	fs.StringVar(&o.logPath, "log", "", "The session log path")
	fs.StringVar(&o.workingDir, "working-dir", "", "The directory the tools work in")
	fs.StringVar(&o.executor, "executor", "", "The executor the bash tool runs its commands through")
	fs.BoolVar(&o.projectPrompt, "project-prompt", false, "Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt")
	fs.DurationVar(&o.timeout, "timeout", 0, "The wall-clock ceiling for the run")
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
	if settings.Roles[config.RoleChat] == "" && o.baseURL == "" {
		fmt.Fprintf(os.Stderr, "poo run: --base-url, --model-id and --context-limit are required, or a settings file that names a chat model\n")
		return 2
	}

	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      o.overrides(),
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	// The log records the models without their credentials: a URL may
	// carry a key, and the log is a file a consumer reads.
	chatModel := loggedModel(model)

	sessionID := newSessionID()
	created := &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    sessionID,
		Mode:         runtime.SessionMode,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		WorkingDir:   workDir,
		Model:        poo.SessionModel{ID: model.ModelID, BaseURL: loggedBaseURL(model.Endpoint), MaxOutput: model.MaxOutput},
		Tools:        sessionTools(runtime),
		ContextLimit: model.ContextLimit,
		ToolsMode:    string(model.Tools.Mode),
		// The metadata says what the session ran with: the profile it
		// was built from, and the models it talked to.
		Profile: runtime.Profile.Name,
		Roles:   poo.ModelRoles{Chat: &chatModel},

		ReasoningEffort: o.reasoningEffort,
		Build:           poo.BuildInfo{Name: "poo", Version: version},
	}
	if deciderModel, _, err := runtime.Decider(); err == nil {
		deciderModel = loggedModel(deciderModel)
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

	sess, err := poo.New(runtime.Options, poo.WithPolicy(runtime.Policy))
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}
	defer sess.Close()

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
	// across the runs: the first non-zero code wins.
	return combinedExitCode(sess.SettleCodes())
}

// modelWithFlags returns the model the run talks to.
//
// The catalog resolves the model the settings name, and the flags
// change what it resolved: the ceiling and the reasoning control are
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
	switch o.toolsMode {
	case "", "native", "text", "auto":
	default:
		return fmt.Errorf("--tools-mode must be native, text or auto, not %q", o.toolsMode)
	}
	if o.maxTurns < 0 {
		return errors.New("--max-turns must not be negative")
	}
	if o.maxOutput < 0 {
		return errors.New("--max-output must not be negative")
	}
	// The flags that name a model describe one model, so they are all
	// or none of them.
	named := o.baseURL != "" || o.modelID != "" || o.contextLimit > 0 || o.apiKeyEnv != "" || o.toolsMode != ""
	if named {
		switch {
		case o.baseURL == "":
			return errors.New("--base-url is required with --model-id")
		case o.modelID == "":
			return errors.New("--model-id is required with --base-url")
		case o.contextLimit <= 0:
			return errors.New("--context-limit is required and must be positive")
		}
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
	if o.baseURL != "" {
		name := o.modelID
		if settings.Endpoints == nil {
			settings.Endpoints = map[string]config.Endpoint{}
		}
		if settings.Models == nil {
			settings.Models = map[string]config.Model{}
		}
		if settings.Roles == nil {
			settings.Roles = map[string]string{}
		}
		settings.Endpoints[name] = config.Endpoint{
			BaseURL:   o.baseURL,
			APIKeyEnv: o.apiKeyEnv,
		}
		toolsMode := o.toolsMode
		if toolsMode == "" {
			toolsMode = string(poo.ToolsNative)
		}
		settings.Models[name] = config.Model{
			Endpoint:     name,
			ModelID:      o.modelID,
			API:          string(poo.APIChatCompletions),
			ToolsMode:    toolsMode,
			Ops:          []string{string(poo.OpChat)},
			ContextLimit: o.contextLimit,
		}
		settings.Roles[config.RoleChat] = name
	}
	if o.maxTurns > 0 {
		settings.Limits.MaxTurns = o.maxTurns
	}
	if o.timeout > 0 {
		settings.Limits.MaxDurationS = int(o.timeout / time.Second)
	}
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
	dir := o.workingDir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("--working-dir %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--working-dir %s is not a directory", abs)
	}
	return abs, nil
}

// promptMutation returns the prompt the command adds.
//
// The command's own sections come after the settings': the flags come
// last, so they change what the settings file said.
func promptMutation(runtime *config.Runtime, model poo.ModelInfo, project string) func(*prompt.Builder) {
	tools := toolSpecs(runtime)
	toolsMode := string(model.Tools.Mode)
	return systemPrompt(model.ModelID, tools, toolsMode, project)
}

// openSink opens the session's records.
//
// The records go to the log the caller asked for, or to the store the
// settings name. An ephemeral session writes nothing.
func openSink(ctx context.Context, runtime *config.Runtime, created *poo.SessionCreated, logPath, workDir string) (*sink, error) {
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

// sessionTools returns the tools the session's metadata holds.
//
// The session's tool list derives from the same specs: one source for
// the name and the class, so the log cannot drift from the wire.
func sessionTools(runtime *config.Runtime) []poo.SessionTool {
	specs := toolSpecs(runtime)
	tools := make([]poo.SessionTool, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, poo.SessionTool{
			Name:       spec.Name,
			Class:      spec.Class,
			Background: spec.Background,
		})
	}
	return tools
}

// toolSpecs returns the specs of the tools the session offers.
func toolSpecs(runtime *config.Runtime) []poo.ToolSpec {
	specs := make([]poo.ToolSpec, 0, len(runtime.Options.ToolOrder))
	for _, name := range runtime.Options.ToolOrder {
		tool, ok := runtime.Options.Tools[name]
		if !ok || tool == nil {
			continue
		}
		specs = append(specs, tool.Spec())
	}
	return specs
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
			return "", err
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

// loggedModel returns the model the log records.
//
// A URL may carry credentials, and the log is a file a consumer reads,
// so the userinfo is stripped before the model is written.
func loggedModel(model poo.ModelInfo) poo.ModelInfo {
	model.Endpoint = loggedBaseURL(model.Endpoint)
	return model
}

// loggedBaseURL returns the base URL the log records.
//
// A URL may carry credentials, and the log is a file a consumer reads, so
// the userinfo is stripped before it is written.
func loggedBaseURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

// version is the build's version.
const version = "0.1.0"
