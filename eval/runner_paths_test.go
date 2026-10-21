package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The runner's own layout rules.
//
// A run writes into a directory the caller names, and the names are
// turned into absolute paths before anything is created: poo resolves a
// relative --log against its own working directory, and the harness
// opens the log against its own.

func TestTheRunnerMakesItsPathsAbsolute(t *testing.T) {
	// A relative --out must not send the log somewhere the harness
	// cannot read.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	// The out directory is relative, and the harness runs from a
	// directory that is not the one the path is relative to.
	base := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("changing the working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       "relout",
		WorkRoot:  filepath.Join(base, "work"),
		PooBinary: fakePoo(t, logWritingPoo),
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	r := report.Results[0]
	if !filepath.IsAbs(r.LogPath) {
		t.Errorf("the log path is %q, want an absolute path", r.LogPath)
	}
	if _, err := os.Stat(r.LogPath); err != nil {
		t.Errorf("the log is not where the report says it is: %v", err)
	}
	if r.Metrics.SettleReason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", r.Metrics.SettleReason)
	}
}

func TestASecondRunDoesNotReadTheFirstRunsLog(t *testing.T) {
	// A second run into the same out directory is refused, so it can
	// never report the first run's metrics. With --force it starts
	// clean, and its log is not the first run's log.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	newRunner := func(force bool) *Runner {
		return NewRunner(RunOptions{
			Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
			Tasks:     []Task{task},
			TaskRoot:  taskRoot,
			Seeds:     1,
			Out:       out,
			WorkRoot:  filepath.Join(out, "work"),
			PooBinary: fakePoo(t, logWritingPoo),
			Force:     force,
		})
	}

	first, err := newRunner(false).Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	firstLog := first.Results[0].LogPath
	if _, err := os.Stat(firstLog); err != nil {
		t.Fatalf("the first run kept no log: %v", err)
	}

	// The same out directory, without --force: refused.
	if _, err := newRunner(false).Run(context.Background()); err == nil {
		t.Error("the second run into the same directory was accepted, want a refusal")
	}

	// With --force the run starts clean.
	second, err := newRunner(true).Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness with --force: %v", err)
	}
	secondLog := second.Results[0].LogPath
	if secondLog == firstLog {
		t.Errorf("the second run reused the first run's log: %s", secondLog)
	}
	if _, err := os.Stat(secondLog); err != nil {
		t.Errorf("the second run kept no log: %v", err)
	}
	if second.Results[0].Metrics.SettleReason != "end_turn" {
		t.Errorf("the second run settled %q, want end_turn", second.Results[0].Metrics.SettleReason)
	}
}

func TestTheRunnerRefusesANonEmptyOutDirectory(t *testing.T) {
	// A run into a directory that already holds a run's output is
	// refused, so a rerun cannot read the previous run's log.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	runs := filepath.Join(out, "runs")
	if err := os.MkdirAll(runs, 0o755); err != nil {
		t.Fatalf("making the runs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runs, "stale.txt"), []byte("old run\n"), 0o644); err != nil {
		t.Fatalf("writing the stale file: %v", err)
	}

	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: fakePoo(t, logWritingPoo),
	})

	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("the run into a non-empty directory was accepted, want a refusal")
	}
	// The stale file is still there: the refusal must not clear it.
	if _, err := os.Stat(filepath.Join(runs, "stale.txt")); err != nil {
		t.Errorf("the refusal removed the stale file: %v", err)
	}
}

func TestTheRunnerClearsTheRunsDirectoryWithForce(t *testing.T) {
	// --force clears the runs directory first, so a rerun starts clean.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	runs := filepath.Join(out, "runs")
	if err := os.MkdirAll(runs, 0o755); err != nil {
		t.Fatalf("making the runs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runs, "stale.txt"), []byte("old run\n"), 0o644); err != nil {
		t.Fatalf("writing the stale file: %v", err)
	}

	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: fakePoo(t, logWritingPoo),
		Force:     true,
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if !report.Results[0].Success {
		t.Errorf("the forced run did not pass: %+v", report.Results[0])
	}
	if _, err := os.Stat(filepath.Join(runs, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("the stale file is still there, want it cleared: %v", err)
	}
}

func TestThePooBinaryPathIsMadeAbsolute(t *testing.T) {
	// The binary runs in the workdir, so a relative path would be
	// resolved against the workdir rather than against the caller's
	// directory.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	base := t.TempDir()
	binary := fakePoo(t, logWritingPoo)
	relative := filepath.Join(base, "poo")
	if err := os.Rename(binary, relative); err != nil {
		t.Fatalf("moving the binary: %v", err)
	}

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: relative,
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if report.Results[0].Error != "" {
		t.Errorf("the run failed: %s", report.Results[0].Error)
	}
	if report.Results[0].Metrics.SettleReason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", report.Results[0].Metrics.SettleReason)
	}
}

func TestTwoRunsDoNotShareAWorkRoot(t *testing.T) {
	// Two harness invocations must not share a work root, or one
	// removes the other's workdir.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	workRoot := filepath.Join(t.TempDir(), "work")
	marker := filepath.Join(t.TempDir(), "seen.txt")
	binary := fakePoo(t, `
dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --working-dir) dir="$2"; shift 2 ;;
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
sleep 0.5
if [ -f "$dir/a.txt" ]; then
  echo present >> "`+marker+`"
else
  echo gone >> "`+marker+`"
fi
`+logWritingPoo)

	run := func(out string) *Report {
		runner := NewRunner(RunOptions{
			Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
			Tasks:     []Task{task},
			TaskRoot:  taskRoot,
			Seeds:     1,
			Out:       out,
			WorkRoot:  workRoot,
			PooBinary: binary,
		})
		report, err := runner.Run(context.Background())
		if err != nil {
			t.Fatalf("running the harness: %v", err)
		}
		return report
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); run(filepath.Join(t.TempDir(), "a")) }()
	go func() { defer wg.Done(); run(filepath.Join(t.TempDir(), "b")) }()
	wg.Wait()

	seen, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading the marker: %v", err)
	}
	if strings.Contains(string(seen), "gone") {
		t.Errorf("a run found its workdir gone:\n%s", seen)
	}
}
