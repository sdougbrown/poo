package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestABarePooNameIsFoundOnThePath(t *testing.T) {
	// A bare name is looked up on PATH, not resolved against the
	// caller's directory.
	bin := t.TempDir()
	src := fakePoo(t, logWritingPoo)
	if err := os.Rename(src, filepath.Join(bin, "poo")); err != nil {
		t.Fatalf("placing the fake poo: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cwd := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(out, "work"),
		PooBinary: "poo",
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if !report.Results[0].Success {
		t.Errorf("the run did not succeed: %+v", report.Results[0])
	}
}

func TestAPooBinaryThatCannotBeFoundFailsTheRun(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(out, "work"),
		PooBinary: "no-such-poo",
	})
	report, err := runner.Run(context.Background())
	if err == nil {
		t.Fatalf("the run went ahead with a missing binary: %+v", report)
	}
	if !strings.Contains(err.Error(), "no-such-poo") {
		t.Errorf("the error does not name the binary: %v", err)
	}
}

func TestAWorkRootOnTheTaskSetsPathIsRefused(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	for name, runsDir := range map[string]string{
		"inside the task set": filepath.Join(taskRoot, "work"),
		"the task set itself": taskRoot,
		"above the task set":  filepath.Dir(taskRoot),
	} {
		runner := NewRunner(RunOptions{
			Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
			Tasks:     []Task{task},
			TaskRoot:  taskRoot,
			Seeds:     1,
			Out:       t.TempDir(),
			RunsDir:   runsDir,
			PooBinary: fakePoo(t, logWritingPoo),
		})
		if _, err := runner.Run(context.Background()); err == nil {
			t.Errorf("a work root %s was accepted", name)
		}
	}
}

func TestAMissingLogStillListsTheUnexpectedFiles(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	poo := fakePoo(t, `while [ "$#" -gt 0 ]; do case "$1" in --working-dir) d="$2"; shift 2 ;; *) shift ;; esac; done
echo changed > "$d/a.txt"
echo stray > "$d/stray.txt"
`)
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(out, "work"),
		PooBinary: poo,
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	got := report.Results[0].Metrics.UnexpectedFiles
	if len(got) != 1 || got[0] != "stray.txt" {
		t.Errorf("unexpected files = %v, want [stray.txt]", got)
	}
}

func TestAFileUnderTestsIsAlwaysUnexpected(t *testing.T) {
	for _, expected := range [][]string{{"**"}, {"tests/**"}} {
		got := unexpectedFiles(Task{ExpectedPaths: expected}, []string{"tests/test_a.py", "pkg/tests/test_b.py"})
		if len(got) != 2 {
			t.Errorf("expected paths %v: unexpected = %v, want both test files", expected, got)
		}
	}
	allowed := unexpectedFiles(Task{ExpectedPaths: []string{"**"}, AllowTestEdits: true}, []string{"tests/test_a.py"})
	if len(allowed) != 0 {
		t.Errorf("a task that allows test edits still flagged %v", allowed)
	}
}

func TestAWorkRootInsideTheTaskSetsRepositoryIsRefused(t *testing.T) {
	for name, marker := range map[string]func(repo string) error{
		"go.mod": func(repo string) error {
			return os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/r\n"), 0o644)
		},
		".git dir": func(repo string) error { return os.Mkdir(filepath.Join(repo, ".git"), 0o755) },
		".git file": func(repo string) error {
			return os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: elsewhere\n"), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			if err := marker(repo); err != nil {
				t.Fatal(err)
			}
			refusesWorkRootIn(t, repo)
		})
	}
}

func refusesWorkRootIn(t *testing.T, repo string) {
	t.Helper()
	runner := NewRunner(RunOptions{
		TaskRoot: filepath.Join(repo, "evals", "tasks"),
		RunsDir:  filepath.Join(repo, ".cache", "eval-work"),
	})
	err := runner.checkRunsDir()
	if err == nil || !strings.Contains(err.Error(), "inside the task set's repository") {
		t.Errorf("a work root inside the task set's repository gave %v, want a refusal", err)
	}
}

func TestARunWithAWorkRootInsideTheRepositoryIsRefused(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	repo := filepath.Dir(fixtureRoot)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskRoot := filepath.Join(repo, "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       t.TempDir(),
		RunsDir:   filepath.Join(repo, ".cache", "eval-work"),
		PooBinary: fakePoo(t, logWritingPoo),
	})
	_, err := runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "inside the task set's repository") {
		t.Errorf("Run returned %v, want the repository refusal", err)
	}
}
