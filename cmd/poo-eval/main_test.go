package main

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/sdougbrown/poo/eval"
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
	// The temporary root is no repository, so the commit part may be
	// missing; the date and time never are.
	name := filepath.Base(out)
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{6}(-[0-9a-f]{7,})?$`).MatchString(name) {
		t.Errorf("the run's directory name %q is not <date>-<time>[-<sha>]", name)
	}
}

func TestCompareOfTwoNamedReportsNeedsNoRepository(t *testing.T) {
	baseline, candidate := t.TempDir(), t.TempDir()
	for _, dir := range []string{baseline, candidate} {
		if err := eval.WriteReport(dir, &eval.Report{}); err != nil {
			t.Fatalf("writing a report: %v", err)
		}
	}
	t.Chdir(t.TempDir())
	if code := runCompare([]string{baseline, candidate}); code != 0 {
		t.Fatalf("compare of two named reports outside a repository exited %d, want 0", code)
	}
}
