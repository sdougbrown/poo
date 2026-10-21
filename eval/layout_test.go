package eval

import (
	"os"
	"path/filepath"
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
