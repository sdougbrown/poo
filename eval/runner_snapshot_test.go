package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// editingPoo is a fake poo that records the content it starts from, then
// edits the file at the absolute path it was built with, then does the
// task's work in its own workdir.
func editingPoo(t *testing.T, record, target string) string {
	t.Helper()
	return fakePoo(t, `while [ "$#" -gt 0 ]; do case "$1" in --working-dir) d="$2"; shift 2 ;; *) shift ;; esac; done
cat "$d/a.txt" >> '`+record+`'
echo hacked >> '`+target+`'
echo fixed > "$d/a.txt"
`)
}

func TestARunThatEditsTheSourceIsInvalid(t *testing.T) {
	for name, target := range map[string]func(fixtureRoot, taskDir string) string{
		"fixture":  func(fixtureRoot, _ string) string { return filepath.Join(fixtureRoot, "fix-a", "a.txt") },
		"task dir": func(_, taskDir string) string { return filepath.Join(taskDir, "check.sh") },
	} {
		t.Run(name, func(t *testing.T) {
			fixtureRoot := fakeFixture(t, "fix-a", "old\n")
			taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
			task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
			out := t.TempDir()
			runner := NewRunner(RunOptions{
				Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
				Tasks:     []Task{task},
				TaskRoot:  taskRoot,
				Seeds:     1,
				Out:       out,
				WorkRoot:  filepath.Join(out, "work"),
				PooBinary: editingPoo(t, filepath.Join(t.TempDir(), "rec"), target(fixtureRoot, task.Dir)),
			})
			report, err := runner.Run(context.Background())
			if err != nil {
				t.Fatalf("running the harness: %v", err)
			}
			got := report.Results[0]
			if got.Success {
				t.Errorf("a run that edited the source succeeded: %+v", got)
			}
			if !strings.Contains(got.Error, "source fixture changed") {
				t.Errorf("error = %q, want it to say the source changed", got.Error)
			}
		})
	}
}

func TestALaterRunStartsFromTheSnapshotNotTheEditedSource(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	record := filepath.Join(t.TempDir(), "rec")
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     2,
		Parallel:  1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: editingPoo(t, record, filepath.Join(fixtureRoot, "fix-a", "a.txt")),
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "old\nold\n" {
		t.Errorf("the runs started from %q, want both from the original content", got)
	}
}

func TestARunThatEditsTheSnapshotIsInvalid(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	out := t.TempDir()
	// The snapshot sits beside the workdirs, under the invocation's work root.
	poisoner := fakePoo(t, `while [ "$#" -gt 0 ]; do case "$1" in --working-dir) d="$2"; shift 2 ;; *) shift ;; esac; done
s="$d"
while [ ! -d "$s/fixtures/fix-a" ]; do s=$(dirname "$s"); done
echo poisoned >> "$s/fixtures/fix-a/a.txt"
echo fixed > "$d/a.txt"
`)
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     2,
		Parallel:  1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: poisoner,
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	for _, got := range report.Results {
		if !got.SourceChanged || got.Success {
			t.Errorf("seed %d edited the snapshot and was not flagged: %+v", got.Seed, got)
		}
	}
}

func TestAReportOfARunThatEditedItsSourceHoldsNoAbsolutePaths(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: editingPoo(t, filepath.Join(t.TempDir(), "rec"), filepath.Join(fixtureRoot, "fix-a", "a.txt")),
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if !strings.Contains(report.Results[0].Error, "fixtures/fix-a") {
		t.Errorf("error = %q, want it to name fixtures/fix-a", report.Results[0].Error)
	}
	if err := WriteReport(out, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	for _, name := range []string{"report.json", "report.md", "summary.json"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, root := range []string{filepath.Dir(fixtureRoot), out} {
			if strings.Contains(string(data), root) {
				t.Errorf("%s holds the absolute path %s", name, root)
			}
		}
	}
}
