// Command poo runs a poo session.
//
// The skeleton ships one command: `poo run`, which takes a prompt, talks to
// an OpenAI-compatible endpoint, and settles with a reason. The exit code
// says why the run ended.
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
	"github.com/sdougbrown/poo/compact/handoff"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/prompt/agentsmd"
	"github.com/sdougbrown/poo/provider/openai"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

const usage = `poo runs a session against an OpenAI-compatible endpoint.

Usage:
  poo run --base-url <url> --model-id <id> --context-limit <n> [flags] "prompt"

Run flags:
  --base-url <url>       The endpoint's base URL, e.g. http://localhost:4000/v1
  --model-id <id>        The model identifier
  --context-limit <n>    The model's context window in tokens (required)
  --api-key-env <var>    The environment variable holding the API key
  --tools-mode <mode>    native (default), text or auto
  --reasoning-effort <e> reasoning_effort to send (e.g. low), for models that think only when asked
  --max-turns <n>        The turn ceiling; 0 means unlimited
  --max-output <n>       The model's output-token ceiling
  --log <path>           The session log path
  --working-dir <dir>    The directory the tools work in (default: the current one)
  --executor <name>      The executor the bash tool runs commands through: local (default) or bwrap
  --project-prompt       Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt
  --timeout <dur>        The wall-clock ceiling for the run, e.g. 5m
  --no-wait-jobs         Exit at the first settle; the running jobs stop through the session's close
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the command and returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "run":
		return runRun(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "poo: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// runOptions are the flags of `poo run`.
type runOptions struct {
	baseURL      string
	modelID      string
	contextLimit int
	apiKeyEnv    string
	toolsMode    string
	maxTurns     int
	maxOutput    int
	// reasoningEffort is sent as the request's reasoning_effort when set.
	reasoningEffort string
	logPath         string
	workingDir      string
	// executor is the executor the bash tool runs its commands
	// through.
	executor string
	// projectPrompt includes the working directory's project
	// instructions in the prompt.
	projectPrompt bool
	timeout       time.Duration
	// noWaitJobs exits at the first settle, and the running jobs stop
	// through the session's close.
	noWaitJobs bool
}

// runRun executes `poo run`.
func runRun(args []string) int {
	fs := flag.NewFlagSet("poo run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var o runOptions
	fs.StringVar(&o.baseURL, "base-url", "", "The endpoint's base URL")
	fs.StringVar(&o.modelID, "model-id", "", "The model identifier")
	fs.IntVar(&o.contextLimit, "context-limit", 0, "The model's context window in tokens")
	fs.StringVar(&o.apiKeyEnv, "api-key-env", "", "The environment variable holding the API key")
	fs.StringVar(&o.toolsMode, "tools-mode", "native", "native, text or auto")
	fs.IntVar(&o.maxTurns, "max-turns", 0, "The turn ceiling")
	fs.IntVar(&o.maxOutput, "max-output", 0, "The model's output-token ceiling")
	fs.StringVar(&o.reasoningEffort, "reasoning-effort", "", "The reasoning_effort to send, for models that think only when asked")
	fs.StringVar(&o.logPath, "log", "", "The session log path")
	fs.StringVar(&o.workingDir, "working-dir", "", "The directory the tools work in")
	fs.StringVar(&o.executor, "executor", "local", "The executor the bash tool runs commands through")
	fs.BoolVar(&o.projectPrompt, "project-prompt", false, "Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt")
	fs.DurationVar(&o.timeout, "timeout", 0, "The wall-clock ceiling for the run")
	fs.BoolVar(&o.noWaitJobs, "no-wait-jobs", false, "Exit at the first settle; the running jobs stop through the session's close")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := o.validate(fs.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}

	workDir := o.workingDir
	if workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
			return 1
		}
		workDir = wd
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}
	// The working directory has to exist: a typo would otherwise create
	// an empty directory and run the task in it.
	info, err := os.Stat(absWorkDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: --working-dir %s: %v\n", absWorkDir, err)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "poo run: --working-dir %s is not a directory\n", absWorkDir)
		return 2
	}

	// The project's instructions are opt-in: the flag asks for them, and
	// the loader reads the working directory only.
	project := ""
	if o.projectPrompt {
		text, err := agentsmd.Load(absWorkDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo run: --project-prompt: %v\n", err)
			return 2
		}
		project = text
	}

	logPath := o.logPath
	if logPath == "" {
		logPath = defaultLogPath(absWorkDir)
	} else if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(absWorkDir, logPath)
	}

	apiKey := ""
	if o.apiKeyEnv != "" {
		apiKey = os.Getenv(o.apiKeyEnv)
	}

	// The child environment drops the API key variable, so a command the
	// model runs cannot read the credential.
	scrub := []string{}
	if o.apiKeyEnv != "" {
		scrub = append(scrub, o.apiKeyEnv)
	}
	childEnv := bash.ChildEnv(os.Environ(), scrub)

	log, err := session.Open(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}
	defer log.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	// The descriptor of the model the run talks to. The transport and
	// the session read it, so the flags name the model in one place.
	model := o.modelInfo()

	transport := openai.New(openai.Config{
		BaseURL:         o.baseURL,
		Model:           o.modelID,
		APIKey:          apiKey,
		ToolsMode:       o.toolsMode,
		ReasoningEffort: o.reasoningEffort,
		Info:            model,
	})

	// The executor is chosen before the session starts: a session that
	// asked for an executor that cannot run fails here, and never runs
	// its commands through another one.
	executor, err := executorRegistry().New(o.executor, bash.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 2
	}

	bashTool := bash.New(bash.WithExecutor(executor))
	readTool := read.New()
	grepTool := grep.New()
	editTool := edit.New()

	// The tools the binary links, and the tools a run offers.
	//
	// The order is the offer order, and it is what a run enables: the
	// grep and the edit tools are linked into the binary, but a run
	// does not offer them until a profile names them.
	tools := map[string]poo.Tool{
		"bash": bashTool,
		"read": readTool,
		"grep": grepTool,
		"edit": editTool,
	}
	toolOrder := []string{"bash", "read"}
	// The code profile allows every unmatched side-effecting call. The
	// command installs no asker chain, so a doom loop that asks falls
	// back, and the load-time warning names that fallback.
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAllow, WorkingDir: absWorkDir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}
	if warning := perm.Warning(policy, policy.Askers()); warning != "" {
		fmt.Fprintf(os.Stderr, "poo run: warning: %s\n", warning)
	}
	toolSpecs := make([]poo.ToolSpec, 0, len(toolOrder))
	// The session's tool list derives from the same specs: one source
	// for the name and class, so the log cannot drift from the wire.
	sessionTools := make([]poo.SessionTool, 0, len(toolOrder))
	for _, name := range toolOrder {
		spec := tools[name].Spec()
		toolSpecs = append(toolSpecs, spec)
		sessionTools = append(sessionTools, poo.SessionTool{Name: spec.Name, Class: spec.Class})
	}

	settings := poo.ToolSettings{
		BashTimeoutSeconds:  int(bash.DefaultTimeout / time.Second),
		ObservationCapChars: bash.ObservationCap(o.contextLimit),
		ReadOutputCapChars:  read.OutputCap(o.contextLimit),
	}

	sessionID := newSessionID()

	if err := log.Append(ctx, &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    sessionID,
		Mode:         "logged",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		WorkingDir:   absWorkDir,
		Model:        poo.SessionModel{ID: o.modelID, BaseURL: loggedBaseURL(o.baseURL), MaxOutput: o.maxOutput},
		Tools:        sessionTools,
		ContextLimit: o.contextLimit,
		ToolsMode:    o.toolsMode,

		ReasoningEffort: o.reasoningEffort,
		Build:           poo.BuildInfo{Name: "poo", Version: version},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}

	sess, err := poo.New(poo.Options{
		Transport:  transport,
		Tools:      tools,
		ToolOrder:  toolOrder,
		Sink:       log,
		SessionID:  sessionID,
		Model:      model,
		MaxTurns:   o.maxTurns,
		WorkingDir: absWorkDir,
		Env:        childEnv,
		Settings:   settings,
		ToolsMode:  o.toolsMode,

		ReasoningEffort: o.reasoningEffort,
		// The plan makes handoff the command's default strategy: a
		// session that reaches the window writes a handoff and settles
		// context_exhausted with the note.
		Compactor: handoff.New(handoff.Options{}),
	}, poo.WithPrompt(systemPrompt(o.modelID, toolSpecs, o.toolsMode, project)), poo.WithPolicy(policy))
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

	fmt.Fprintf(os.Stderr, "poo: settled %s after %d turn(s); log: %s\n", res.Reason, res.Turns, logPath)

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

// modelInfo builds the descriptor of the model the run talks to.
//
// The flags name the model the endpoint serves: the identifier it knows
// it by, the window it has, the ceiling it writes, how the run asks it
// for tool calls and how its reasoning is controlled.
//
// The API is the Chat Completions API, because it is the only one the
// transport speaks.
func (o runOptions) modelInfo() poo.ModelInfo {
	info := poo.ModelInfo{
		ModelID:      o.modelID,
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: o.contextLimit,
		MaxOutput:    o.maxOutput,
		Tools:        poo.ToolCalling{Mode: poo.ToolsMode(o.toolsMode)},
	}
	if o.reasoningEffort != "" {
		// The flag names an effort level, and the effort level is what
		// the model takes.
		info.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: poo.ReasoningPathEffort}
	}
	return info
}

// validate checks the flags and the prompt.
func (o *runOptions) validate(rest []string) error {
	switch {
	case o.baseURL == "":
		return errors.New("--base-url is required")
	case o.modelID == "":
		return errors.New("--model-id is required")
	case o.contextLimit <= 0:
		return errors.New("--context-limit is required and must be positive")
	}
	switch o.toolsMode {
	case "native", "text", "auto":
	default:
		return fmt.Errorf("--tools-mode must be native, text or auto, not %q", o.toolsMode)
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

// defaultLogPath returns the log path a run uses when none is given.
//
// The name carries a timestamp, so two runs in one directory do not
// collide.
func defaultLogPath(dir string) string {
	return session.DefaultPath(dir, time.Now())
}

// version is the build's version.
const version = "0.1.0"
