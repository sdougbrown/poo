package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAWrittenReportHoldsNoAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	report := &Report{
		Dir: dir,
		Results: []RunResult{{
			LogPath: filepath.Join(dir, "runs", "run-1", "t", "m", "seed1", "session.jsonl"),
		}},
	}
	if err := WriteReport(dir, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	for _, name := range []string{"report.json", "report.md", "summary.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if strings.Contains(string(data), dir) {
			t.Errorf("%s holds the absolute path %s", name, dir)
		}
	}
	if !filepath.IsAbs(report.Results[0].LogPath) {
		t.Error("writing the report changed the caller's log path")
	}
}

func TestARelativeReportDirStillWritesRelativePaths(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	dir := filepath.Join("results", "r")
	report := &Report{
		Dir: filepath.Join(base, dir),
		Results: []RunResult{{
			LogPath: filepath.Join(base, dir, "runs", "run-1", "t", "m", "seed1", "session.jsonl"),
		}},
	}
	if err := WriteReport(dir, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if strings.Contains(string(data), base) {
		t.Errorf("report.md holds the absolute path %s", base)
	}
}

func TestTheCheckOutputNamesNoWorkdir(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	check := "#!/usr/bin/env bash\necho \"failed in $(pwd)/a.txt\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(task.Dir, "check.sh"), []byte(check), 0o755); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	workRoot := filepath.Join(t.TempDir(), "work")
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  workRoot,
		PooBinary: fakePoo(t, logWritingPoo),
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	got := report.Results[0].CheckOutput
	if strings.Contains(got, workRoot) {
		t.Errorf("the check output names the workdir: %q", got)
	}
	if !strings.Contains(got, "$WORKDIR/a.txt") {
		t.Errorf("the check output is %q, want the workdir written as $WORKDIR", got)
	}
}
