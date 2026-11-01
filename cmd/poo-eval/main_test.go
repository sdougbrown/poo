package main

import (
	"path/filepath"
	"testing"
)

func TestACompareWithoutABaselineComparesAgainstTheAcceptedOne(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "evals", "runs", "2026-10-25-120000-abc1234")

	baseline, got, ok := comparePaths(root, []string{candidate})
	if !ok {
		t.Fatal("one argument: comparePaths refused the invocation")
	}
	if want := filepath.Join(root, "evals", "baseline"); baseline != want {
		t.Errorf("the baseline is %s, want %s", baseline, want)
	}
	if got != candidate {
		t.Errorf("the candidate is %s, want %s", got, candidate)
	}
}

func TestACompareWithTwoArgumentsUsesBoth(t *testing.T) {
	root := t.TempDir()
	baselinePath := filepath.Join(root, "old")
	candidatePath := filepath.Join(root, "new")

	baseline, candidate, ok := comparePaths(root, []string{baselinePath, candidatePath})
	if !ok {
		t.Fatal("two arguments: comparePaths refused the invocation")
	}
	if baseline != baselinePath || candidate != candidatePath {
		t.Errorf("comparePaths resolved (%s, %s), want the two arguments", baseline, candidate)
	}
}

func TestACompareWithNoArgumentsIsRefused(t *testing.T) {
	if _, _, ok := comparePaths(t.TempDir(), nil); ok {
		t.Error("comparePaths accepted an empty invocation")
	}
}

func TestARunDefaultsToTheIgnoredRunsDirectory(t *testing.T) {
	out := defaultOutDir(t.TempDir())
	if filepath.Base(filepath.Dir(out)) != "runs" {
		t.Errorf("the default report directory is %s, want it under evals/runs", out)
	}
}

func TestARunDefaultNameCarriesTheDateAndTheSHA(t *testing.T) {
	root := t.TempDir()
	out := defaultOutDir(root)
	if filepath.Dir(filepath.Dir(out)) != filepath.Join(root, "evals") {
		t.Errorf("the default report directory is %s, want it under %s", out, filepath.Join(root, "evals"))
	}
	name := filepath.Base(out)
	if len(name) < len("2006-01-02-150405") {
		t.Errorf("the run's directory name %q is shorter than a date stamp", name)
	}
}
