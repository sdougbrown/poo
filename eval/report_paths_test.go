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
		Results: []CellResult{{
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
		Results: []CellResult{{
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
	runsDir := filepath.Join(t.TempDir(), "work")
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   runsDir,
		PooBinary: fakePoo(t, logWritingPoo),
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	got := report.Results[0].CheckOutput
	if strings.Contains(got, runsDir) {
		t.Errorf("the check output names the workdir: %q", got)
	}
	if !strings.Contains(got, "$WORKDIR/a.txt") {
		t.Errorf("the check output is %q, want the workdir written as $WORKDIR", got)
	}
}

func TestACompareOfTwoWrittenReportsNamesBothDirectories(t *testing.T) {
	base, cand := t.TempDir(), t.TempDir()
	for _, dir := range []string{base, cand} {
		if err := WriteReport(dir, &Report{Dir: dir}); err != nil {
			t.Fatalf("writing the report: %v", err)
		}
	}
	data, err := os.ReadFile(filepath.Join(base, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dir": "."`) {
		t.Errorf("report.json should keep a relative dir, got:\n%s", data)
	}
	a, err := LoadReport(base)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadReport(cand)
	if err != nil {
		t.Fatal(err)
	}
	cmp := Compare(a, b)
	if cmp.Baseline != base || cmp.Candidate != cand {
		t.Errorf("the comparison names %q and %q, want %q and %q", cmp.Baseline, cmp.Candidate, base, cand)
	}
}

func TestAWrittenComparisonHoldsNoAbsolutePaths(t *testing.T) {
	base, cand := t.TempDir(), t.TempDir()
	for _, dir := range []string{base, cand} {
		if err := WriteReport(dir, &Report{Dir: dir}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := LoadReport(base)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadReport(cand)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(cand, "comparison")
	if err := WriteComparison(out, Compare(a, b)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"comparison.json", "comparison.md"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{base, cand} {
			if strings.Contains(string(data), dir) {
				t.Errorf("%s holds the absolute path %s", name, dir)
			}
		}
		if !strings.Contains(string(data), "..") {
			t.Errorf("%s names neither report relative to the comparison:\n%s", name, data)
		}
	}
}

func TestTheCheckOutputNamesNoWorkRootEither(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	// The check prints a path above its working dir: the work root.
	check := "#!/usr/bin/env bash\necho \"root: $(dirname \"$(dirname \"$(dirname \"$(dirname \"$(pwd)\")\")\")\")\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(task.Dir, "check.sh"), []byte(check), 0o755); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	runsDir := filepath.Join(t.TempDir(), "work")
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   runsDir,
		PooBinary: fakePoo(t, logWritingPoo),
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	got := report.Results[0].CheckOutput
	if strings.Contains(got, runsDir) || !strings.Contains(got, "$WORKROOT") {
		t.Errorf("the check output is %q, want the work root written as $WORKROOT", got)
	}
}
