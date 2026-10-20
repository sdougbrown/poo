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
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/tool/bash"
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
  --max-turns <n>        The turn ceiling; 0 means unlimited
  --max-output <n>       The model's output-token ceiling
  --log <path>           The session log path
  --working-dir <dir>    The directory the tools work in (default: the current one)
  --timeout <dur>        The wall-clock ceiling for the run, e.g. 5m
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
	logPath      string
	workingDir   string
	timeout      time.Duration
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
	fs.StringVar(&o.logPath, "log", "", "The session log path")
	fs.StringVar(&o.workingDir, "working-dir", "", "The directory the tools work in")
	fs.DurationVar(&o.timeout, "timeout", 0, "The wall-clock ceiling for the run")

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
		return 1
	}
	defer log.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	transport := openai.New(openai.Config{
		BaseURL:   o.baseURL,
		Model:     o.modelID,
		APIKey:    apiKey,
		ToolsMode: o.toolsMode,
	})

	bashTool := bash.New()
	readTool := read.New()

	settings := poo.ToolSettings{
		BashTimeoutSeconds: 120,
		ObservationCapChars: bash.ObservationCap(o.contextLimit),
		ReadOutputCapChars:  read.OutputCap(o.contextLimit),
	}

	sessionID := newSessionID()
	runID := newSessionID()

	if err := log.Append(ctx, &poo.SessionCreated{
		RecordMeta:  poo.NewRecordMeta("session.created"),
		Format:      poo.FormatVersion,
		SessionID:   sessionID,
		Mode:        "logged",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		WorkingDir:  absWorkDir,
		Model:       poo.SessionModel{ID: o.modelID, BaseURL: o.baseURL, MaxOutput: o.maxOutput},
		Tools: []poo.SessionTool{
			{Name: "bash", Class: poo.ClassSideEffecting},
			{Name: "read", Class: poo.ClassReadOnly},
		},
		ContextLimit: o.contextLimit,
		ToolsMode:    o.toolsMode,
		Build:        poo.BuildInfo{Name: "poo", Version: version},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
		return 1
	}

	r := poo.NewRun(poo.Options{
		Transport: transport,
		Tools: map[string]poo.Tool{
			"bash": bashTool,
			"read": readTool,
		},
		ToolOrder:    []string{"bash", "read"},
		Sink:         log,
		SessionID:    sessionID,
		RunID:        runID,
		ModelID:      o.modelID,
		ContextLimit: o.contextLimit,
		MaxOutput:    o.maxOutput,
		MaxTurns:     o.maxTurns,
		SystemPrompt: systemPrompt(o.modelID),
		WorkingDir:   absWorkDir,
		Env:          childEnv,
		Settings:     settings,
		ToolsMode:    o.toolsMode,
	})

	res, err := r.Execute(ctx, strings.Join(fs.Args(), " "))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// A cancellation that arrived before the settle record was
			// written still settles the run.
			res = poo.Result{Reason: poo.ReasonCancelled, Detail: poo.SettleDetail{Source: "cancel:signal"}}
		} else {
			fmt.Fprintf(os.Stderr, "poo run: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(os.Stderr, "poo: settled %s after %d turn(s); log: %s\n", res.Reason, res.Turns, logPath)
	return poo.ExitCode(res.Reason)
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
