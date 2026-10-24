package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheExecutorIsPassedToTheRun covers the plan's eval isolation: the
// run's commands go through the executor the harness names.
//
// The executor is a layer under the sandbox: the sandbox wraps the whole
// poo process, and the executor wraps the commands the model runs.
func TestTheExecutorIsPassedToTheRun(t *testing.T) {
	requireBwrap(t)
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	poo := fakePoo(t, `
args="$*"
log=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log) log="$2"; shift 2 ;;
    --working-dir) dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "$args" > "$(dirname "$log")/args.txt"
`+logWritingPoo)

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(t.TempDir(), "work"),
		PooBinary: poo,
		Sandbox:   SandboxNone,
		Executor:  ExecutorBwrap,
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	res := report.Results[0]
	if !res.Success {
		t.Fatalf("the run did not succeed: %+v", res)
	}
	args, err := os.ReadFile(filepath.Join(filepath.Dir(res.LogPath), "args.txt"))
	if err != nil {
		t.Fatalf("reading the run's arguments: %v", err)
	}
	if !strings.Contains(string(args), "--executor bwrap") {
		t.Errorf("the run's arguments are %q, want the executor named", strings.TrimSpace(string(args)))
	}
	if report.Executor != ExecutorBwrap {
		t.Errorf("the report's executor is %q, want %q", report.Executor, ExecutorBwrap)
	}
}

// TestAnUnknownExecutorIsRefused covers the fail-closed rule: a name
// the binary does not link is an error, not a fallback to another
// executor.
func TestAnUnknownExecutorIsRefused(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       t.TempDir(),
		RunsDir:   filepath.Join(t.TempDir(), "work"),
		PooBinary: fakePoo(t, logWritingPoo),
		Sandbox:   SandboxNone,
		Executor:  "no-such-executor",
	})
	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("the run went ahead with an executor that does not exist")
	}
	if !strings.Contains(err.Error(), "no-such-executor") {
		t.Errorf("the failure is %v, want it to name the executor", err)
	}
}
