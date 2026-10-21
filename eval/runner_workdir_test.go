package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The harness's own rules about the workdir and the check.

func TestTheGeneratedBytecodeIsNotInTheDiff(t *testing.T) {
	// A run that runs python leaves __pycache__ behind. Those are
	// generated files, not the model's changes, and they must not show
	// up in the diff.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	binary := fakePoo(t, `
dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --working-dir) dir="$2"; shift 2 ;;
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$dir/pkg/__pycache__"
echo "cached" > "$dir/pkg/__pycache__/mod.cpython-314.pyc"
mkdir -p "$dir/.pytest_cache"
echo "cache" > "$dir/.pytest_cache/CACHEDIR.TAG"
echo fixed > "$dir/a.txt"
`+logWritingPoo)

	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	r := report.Results[0]
	for _, f := range r.Metrics.ChangedFiles {
		if strings.Contains(f, "__pycache__") || strings.HasSuffix(f, ".pyc") || strings.Contains(f, ".pytest_cache") {
			t.Errorf("the diff holds the generated file %q", f)
		}
	}
	if len(r.Metrics.ChangedFiles) != 1 || r.Metrics.ChangedFiles[0] != "a.txt" {
		t.Errorf("the diff holds %v, want a.txt alone", r.Metrics.ChangedFiles)
	}
}

func TestTheCheckGetsItsOwnTime(t *testing.T) {
	// A run that uses nearly all of its timeout must not starve the
	// check: the check runs on its own clock, not the run's.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")

	dir := filepath.Join(taskRoot, "fix-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the task directory: %v", err)
	}
	taskJSON := `{"repo": "fixture:fix-a", "prompt": "do it", "timeout_s": 2, "tags": ["t"], "expected_paths": ["a.txt"]}`
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(taskJSON), 0o644); err != nil {
		t.Fatalf("writing the task: %v", err)
	}
	check := "#!/usr/bin/env bash\nset -uo pipefail\ngrep -q 'fixed' a.txt\n"
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(check), 0o755); err != nil {
		t.Fatalf("writing the check: %v", err)
	}
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("loading the task: %v", err)
	}

	// The run takes most of its two seconds and then changes the file.
	binary := fakePoo(t, `
dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --working-dir) dir="$2"; shift 2 ;;
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
sleep 1.5
echo fixed > "$dir/a.txt"
`+logWritingPoo)

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if !report.Results[0].Success {
		t.Errorf("the check was starved by the run: %+v", report.Results[0])
	}
}

func TestATimedOutRunIsGivenTimeToSettle(t *testing.T) {
	// A poo that traps SIGTERM and writes its settle record a moment
	// later must have its settle record kept: the harness gives the
	// process group two seconds before it kills it.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	task.TimeoutS = 1

	binary := fakePoo(t, `
log=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
trap 'sleep 0.4; cat > "$log" <<JSON
{"record":"run.settled","seq":1,"ts":"2026-10-20T00:00:01Z","run_id":"r","reason":"cancelled","detail":{"source":"cancel:context"},"turns":1}
JSON
exit 0' TERM
sleep 30
`)

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	r := report.Results[0]
	if r.Metrics.SettleReason != "cancelled" {
		t.Errorf("the run settled %q, want cancelled", r.Metrics.SettleReason)
	}
}

func TestTheTimeoutReachesPoo(t *testing.T) {
	// poo is given the run's own deadline, so it can settle by itself
	// rather than being killed.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	capture := filepath.Join(out, "args.txt")
	binary := fakePoo(t, `
args=""
while [ "$#" -gt 0 ]; do
  args="$args $1"
  shift
done
printf '%s' "$args" > "`+capture+`"
`+logWritingPoo)

	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("reading the arguments: %v", err)
	}
	fields := strings.Fields(string(args))
	var passed time.Duration
	for i, f := range fields {
		if f == "--timeout" && i+1 < len(fields) {
			d, err := time.ParseDuration(fields[i+1])
			if err != nil {
				t.Fatalf("the --timeout %q is not a duration: %v", fields[i+1], err)
			}
			passed = d
		}
	}
	if passed != runner.pooTimeout(task) {
		t.Errorf("poo was given --timeout %s, want %s", passed, runner.pooTimeout(task))
	}
	if passed <= 0 || passed >= runner.timeout(task) {
		t.Errorf("poo was given --timeout %s, want it shorter than the harness deadline %s", passed, runner.timeout(task))
	}
}

func TestTheDiffScratchIsNotInTmp(t *testing.T) {
	// The diff's scratch files live under the work root, never in
	// /tmp's shared space.
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("making the work root: %v", err)
	}
	old := filepath.Join(root, "old")
	new := filepath.Join(root, "new")
	if err := os.WriteFile(old, []byte("a\n"), 0o644); err != nil {
		t.Fatalf("writing the old file: %v", err)
	}
	if err := os.WriteFile(new, []byte("b\n"), 0o644); err != nil {
		t.Fatalf("writing the new file: %v", err)
	}

	text, _, err := diffBytesIn(work, "x.txt", []byte("a\n"), []byte("b\n"))
	if err != nil {
		t.Fatalf("diffing: %v", err)
	}
	if !strings.Contains(text, "b") {
		t.Errorf("the diff is %q, want the changed line", text)
	}
}

func TestTheCheckRunsWhenTheRunUsedItsWholeBudget(t *testing.T) {
	// A run that ends exactly at its deadline still gets a check.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	task.TimeoutS = 1

	binary := fakePoo(t, `
dir=""
log=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --working-dir) dir="$2"; shift 2 ;;
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
sleep 0.9
echo fixed > "$dir/a.txt"
`+logWritingPoo)

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if report.Results[0].CheckExit != 0 {
		t.Errorf("the check exited %d, want 0", report.Results[0].CheckExit)
	}
	_ = time.Now
}
