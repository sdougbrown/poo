package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheJudgeReadsTheDirectoryTheRunnerNames(t *testing.T) {
	j := job{task: Task{ID: "t"}, model: Model{Name: "m"}, seed: 7}
	runsDir := filepath.Join(t.TempDir(), runsDirName)
	// The runner writes each invocation's runs under a run-<id>
	// directory of its own.
	task, model, seed, ok := parseRunDir(runsDir, jobDir(filepath.Join(runsDir, "run-abc"), j))
	if !ok || task != "t" || model != "m" || seed != 7 {
		t.Errorf("parsed %q %q %d %v, want t m 7 true", task, model, seed, ok)
	}
}

func TestARunsDirectoryWithoutATranscriptIsAnError(t *testing.T) {
	resultsDir := t.TempDir()
	stray := filepath.Join(resultsDir, runsDirName, "t", "m", "attempt1")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "session.jsonl"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	judge, err := NewJudge(JudgeOptions{ResultsDir: resultsDir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := judge.findTranscripts(); err == nil {
		t.Error("the judge found nothing in a runs directory that holds files, and said nothing")
	}
}

func TestARunWhoseSeedsAllTimedOutLeavesNothingToJudge(t *testing.T) {
	// The runner makes each seed's directory before poo writes a log, so
	// a run in which every seed timed out leaves a runs directory with
	// no transcript. That is the harness's own output, not a layout
	// mismatch, and the judge has nothing to judge.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	task.TimeoutS = 1
	binary := fakePoo(t, "sleep 30\n")

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     2,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})
	report, err := runner.Run(t.Context())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if err := WriteReport(out, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}

	judge, err := NewJudge(JudgeOptions{ResultsDir: out})
	if err != nil {
		t.Fatal(err)
	}
	found, err := judge.findTranscripts()
	if err != nil {
		t.Fatalf("the judge refused a run whose seeds all timed out: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("the judge found %d transcripts, want none", len(found))
	}
}

func TestTheLayoutErrorNamesTheRunDirectoryLevel(t *testing.T) {
	resultsDir := t.TempDir()
	stray := filepath.Join(resultsDir, runsDirName, "run-abc", "t", "m", "attempt1")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "session.jsonl"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	judge, err := NewJudge(JudgeOptions{ResultsDir: resultsDir})
	if err != nil {
		t.Fatal(err)
	}
	_, err = judge.findTranscripts()
	if err == nil || !strings.Contains(err.Error(), "run-<id>") {
		t.Errorf("the error is %v, want it to name the run-<id> level", err)
	}
}
