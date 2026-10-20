// Command poo-eval runs the eval harness.
//
// The harness runs poo against a task set, measures what happened, and
// writes a report. It also compares two reports, replays a recorded
// session, and judges a set of transcripts.
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

	"github.com/sdougbrown/poo/eval"
)

const usage = `poo-eval runs the poo eval harness.

Usage:
  poo-eval run    [flags]
  poo-eval compare <baseline> <candidate> [flags]
  poo-eval replay <session.jsonl> [flags]
  poo-eval judge  <results-dir> [flags]

Run flags:
  --models <names>     Comma-separated model names; empty means the whole catalog
  --tasks <terms>      Comma-separated task ids or tags; empty means the whole set
  --seeds <n>          How many times each task runs against each model (default 3)
  --parallel <n>       How many runs may be in flight at once (default 1, at most 2 per endpoint)
  --max-turns <n>      The turn ceiling passed to poo (default 50)
  --out <dir>          Where the report goes (default: evals/results/<date>-<git sha>)
  --models-file <path> The model catalog (default: evals/models.yaml)
  --tasks-dir <path>   The task set (default: evals/tasks)
  --poo <path>         The poo binary to run (default: poo)
  --work-root DIR      Where the workdirs are made (default: the user cache directory)
  --keep-workdirs      Leave the workdirs behind after the run
  --force              Clear the out directory's runs before the run

Compare flags:
  --out <dir>          Where the comparison goes (default: the candidate's directory)

Replay flags:
  --context-limit <n>  The context window the replay uses (default: the recording's)
  --max-turns <n>      The turn ceiling the replay uses (default: the recording's)

Judge flags:
  --base-url <url>     The judge's endpoint (default: http://localhost:4000/v1)
  --model <id>         The judge's model (default: deepseek-flash)
  --api-key-env <var>  The environment variable holding the judge's API key
  --out <dir>          Where the verdicts go (default: <results-dir>/judgments)
  --max-chars <n>      The size a transcript may have before it is summarized (default: 60000)
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
	case "compare":
		return runCompare(args[1:])
	case "replay":
		return runReplay(args[1:])
	case "judge":
		return runJudge(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "poo-eval: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// repoRoot returns the repository's root, the directory that holds the
// evals directory.
//
// The harness looks for evals/models.yaml and evals/tasks relative to
// the repository, so a run from anywhere in the tree finds them.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "evals", "models.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("poo-eval: cannot find the evals directory; run from inside the repository")
		}
		dir = parent
	}
}

// runOptions are the flags of `poo-eval run`.
type runOptions struct {
	models     string
	tasks      string
	seeds      int
	parallel   int
	maxTurns   int
	out        string
	modelsFile string
	tasksDir   string
	pooBinary  string
	workRoot   string
	keepWork   bool
	force      bool
}

// runRun executes `poo-eval run`.
func runRun(args []string) int {
	fs := flag.NewFlagSet("poo-eval run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var o runOptions
	fs.StringVar(&o.models, "models", "", "The model names to run")
	fs.StringVar(&o.tasks, "tasks", "", "The task ids or tags to run")
	fs.IntVar(&o.seeds, "seeds", 0, "How many times each task runs against each model")
	fs.IntVar(&o.parallel, "parallel", 0, "How many runs may be in flight at once")
	fs.IntVar(&o.maxTurns, "max-turns", 0, "The turn ceiling passed to poo")
	fs.StringVar(&o.out, "out", "", "Where the report goes")
	fs.StringVar(&o.modelsFile, "models-file", "", "The model catalog")
	fs.StringVar(&o.tasksDir, "tasks-dir", "", "The task set")
	fs.StringVar(&o.pooBinary, "poo", "", "The poo binary to run")
	fs.StringVar(&o.workRoot, "work-root", "", "Where the workdirs are made (default: the user cache directory)")
	fs.BoolVar(&o.keepWork, "keep-workdirs", false, "Leave the workdirs behind")
	fs.BoolVar(&o.force, "force", false, "Clear the out directory's runs before the run")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 2
	}
	if o.modelsFile == "" {
		o.modelsFile = filepath.Join(root, "evals", "models.yaml")
	}
	if o.tasksDir == "" {
		o.tasksDir = filepath.Join(root, "evals", "tasks")
	}
	if o.out == "" {
		o.out = filepath.Join(root, "evals", "results", defaultRunName(root))
	}
	if o.pooBinary == "" {
		o.pooBinary = pooBinaryPath()
	}

	models, err := eval.LoadModels(o.modelsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}
	// The whole catalog is passed to the runner, not just the models the
	// run uses: the key variables every model names are removed from
	// every environment the harness builds.
	catalog := models
	models = eval.SelectModels(models, splitList(o.models))
	if len(models) == 0 {
		fmt.Fprintf(os.Stderr, "poo-eval: no model matches %q\n", o.models)
		return 2
	}

	tasks, err := eval.LoadTasks(o.tasksDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}
	tasks = eval.SelectTasks(tasks, splitList(o.tasks))
	if len(tasks) == 0 {
		fmt.Fprintf(os.Stderr, "poo-eval: no task matches %q\n", o.tasks)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The workdirs are scratch, removed when the run ends. They live
	// outside the repository, so the task set is not an ancestor of
	// them.
	workRoot := o.workRoot
	if workRoot == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
			return 1
		}
		workRoot = filepath.Join(cache, "poo-eval", "work")
	}
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}

	runner := eval.NewRunner(eval.RunOptions{
		Models:       models,
		Catalog:      catalog,
		Tasks:        tasks,
		TaskRoot:     o.tasksDir,
		Seeds:        o.seeds,
		Parallel:     o.parallel,
		MaxTurns:     o.maxTurns,
		Out:          o.out,
		WorkRoot:     workRoot,
		PooBinary:    o.pooBinary,
		KeepWorkdirs: o.keepWork,
		Force:        o.force,
	})

	fmt.Fprintf(os.Stderr, "poo-eval: %d model(s) x %d task(s) x %d seed(s) -> %s\n",
		len(models), len(tasks), runner.Seeds(), o.out)

	report, err := runner.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}
	if err := eval.WriteReport(o.out, report); err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}

	summary := eval.Summarize(report)
	fmt.Fprintf(os.Stderr, "poo-eval: success %.1f%% over %d run(s); report: %s\n",
		summary.Overall.SuccessRate*100, len(report.Results), filepath.Join(o.out, "report.md"))
	return 0
}

// runCompare executes `poo-eval compare`.
func runCompare(args []string) int {
	fs := flag.NewFlagSet("poo-eval compare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var out string
	fs.StringVar(&out, "out", "", "Where the comparison goes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintf(os.Stderr, "poo-eval compare: a baseline and a candidate are needed\n\n%s", usage)
		return 2
	}

	baseline, err := eval.LoadReport(rest[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}
	candidate, err := eval.LoadReport(rest[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}

	cmp := eval.Compare(baseline, candidate)
	if out == "" {
		out = filepath.Join(rest[1], "comparison")
	}
	if err := eval.WriteComparison(out, cmp); err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "poo-eval: %d regression(s), %d improvement(s); comparison: %s\n",
		len(cmp.Regressions), len(cmp.Improvements), filepath.Join(out, "comparison.md"))
	if len(cmp.Regressions) > 0 {
		// A regression is a signal for the stage's author, not a broken
		// command, so the exit code stays zero.
		return 0
	}
	return 0
}

// runReplay executes `poo-eval replay`.
func runReplay(args []string) int {
	fs := flag.NewFlagSet("poo-eval replay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var contextLimit, maxTurns int
	fs.IntVar(&contextLimit, "context-limit", 0, "The context window the replay uses")
	fs.IntVar(&maxTurns, "max-turns", 0, "The turn ceiling the replay uses")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintf(os.Stderr, "poo-eval replay: a session log is needed\n\n%s", usage)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := eval.Replay(rest[0], eval.ReplayOptions{
		Context:      ctx,
		WorkingDir:   mustGetwd(),
		ContextLimit: contextLimit,
		MaxTurns:     maxTurns,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}

	fmt.Printf("recorded calls: %d\n", len(result.Recorded))
	fmt.Printf("replayed calls:  %d\n", len(result.Replayed))
	if result.Match {
		fmt.Printf("match: the tool-call sequence is the same\n")
		return 0
	}
	fmt.Printf("match: the tool-call sequence differs\n")
	for _, d := range result.Differences {
		fmt.Printf("  %s\n", d)
	}
	return 1
}

// runJudge executes `poo-eval judge`.
func runJudge(args []string) int {
	fs := flag.NewFlagSet("poo-eval judge", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var opts eval.JudgeOptions
	fs.StringVar(&opts.BaseURL, "base-url", eval.DefaultJudgeBaseURL, "The judge's endpoint")
	fs.StringVar(&opts.Model, "model", eval.DefaultJudgeModel, "The judge's model")
	fs.StringVar(&opts.APIKeyEnv, "api-key-env", "", "The environment variable holding the judge's API key")
	fs.StringVar(&opts.OutDir, "out", "", "Where the verdicts go")
	fs.IntVar(&opts.MaxChars, "max-chars", 0, "The size a transcript may have before it is summarized")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintf(os.Stderr, "poo-eval judge: a results directory is needed\n\n%s", usage)
		return 2
	}
	opts.ResultsDir = rest[0]

	judge, err := eval.NewJudge(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := judge.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-eval: %v\n", err)
		return 1
	}

	var flagged int
	for _, v := range result.Verdicts {
		if v.NeedsOwner {
			flagged++
		}
	}
	fmt.Fprintf(os.Stderr, "poo-eval: judged %d transcript(s), %d need the owner; verdicts: %s\n",
		len(result.Verdicts), flagged, filepath.Join(result.OutDir, "summary.md"))
	return 0
}

// defaultRunName returns the name a run's report directory gets.
//
// The name carries the date, the commit and the time, so two runs on
// the same day and the same commit do not collide. A run into a
// directory that already holds a run's output is refused, and the
// default name never is one.
func defaultRunName(root string) string {
	sha := gitSHA(root)
	stamp := time.Now().UTC().Format("2006-01-02-150405")
	if sha == "" {
		return stamp
	}
	return stamp + "-" + sha
}

// gitSHA returns the repository's short commit, or the empty string.
func gitSHA(root string) string {
	return eval.GitSHA(root)
}

// pooBinaryPath returns the poo binary the harness runs.
//
// The binary next to poo-eval is used when there is one, so a built
// binary finds its sibling.
func pooBinaryPath() string {
	self, err := os.Executable()
	if err != nil {
		return "poo"
	}
	sibling := filepath.Join(filepath.Dir(self), "poo")
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return sibling
	}
	return "poo"
}

// splitList splits a comma-separated list into its parts.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// mustGetwd returns the working directory, or the empty string.
func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}
