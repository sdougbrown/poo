package eval

import (
	"testing"
)

// run builds one run result for a comparison.
func comparisonRun(model string, seed int, success bool, input, output int) CellResult {
	return CellResult{
		Model:   model,
		Seed:    seed,
		Success: success,
		Metrics: Metrics{InputTokens: input, OutputTokens: output},
	}
}

// comparisonReport builds a report whose runs all belong to the named
// directory, so Compare can tell the sides apart.
func comparisonReport(dir string, runs ...CellResult) *Report {
	return &Report{Dir: dir, Results: runs}
}

// TestAComparisonClassifiesTheDeltas covers the noise floor: a
// difference is a regression only when it beats the larger side's
// spread and the absolute or relative floor.
func TestAComparisonClassifiesTheDeltas(t *testing.T) {
	cases := []struct {
		name             string
		baseline, cand   *Report
		wantRegressions  int
		wantImprovements int
	}{
		{
			name: "a difference under the noise floor is neither",
			// The baseline's own spread (seeds 0 and 1) puts the floor
			// at 1.0, above the candidate's 0.5 gain.
			baseline: comparisonReport("b",
				comparisonRun("m", 1, false, 100, 50),
				comparisonRun("m", 2, true, 100, 50)),
			cand: comparisonReport("c",
				comparisonRun("m", 1, true, 100, 50),
				comparisonRun("m", 2, true, 100, 50)),
		},
		{
			name: "a success gain above the floor is an improvement",
			baseline: comparisonReport("b",
				comparisonRun("m", 1, false, 100, 50),
				comparisonRun("m", 2, false, 100, 50)),
			cand: comparisonReport("c",
				comparisonRun("m", 1, true, 100, 50),
				comparisonRun("m", 2, true, 100, 50)),
			wantImprovements: 1,
		},
		{
			name: "an input blowup above the relative floor is a regression",
			baseline: comparisonReport("b",
				comparisonRun("m", 1, true, 100, 50),
				comparisonRun("m", 2, true, 100, 50)),
			cand: comparisonReport("c",
				comparisonRun("m", 1, true, 200, 50),
				comparisonRun("m", 2, true, 200, 50)),
			wantRegressions: 1,
		},
		{
			name: "an input drop is an improvement",
			baseline: comparisonReport("b",
				comparisonRun("m", 1, true, 200, 50),
				comparisonRun("m", 2, true, 200, 50)),
			cand: comparisonReport("c",
				comparisonRun("m", 1, true, 100, 50),
				comparisonRun("m", 2, true, 100, 50)),
			wantImprovements: 1,
		},
		{
			name: "a token gain under the relative floor is neither",
			// The relative floor for input is twenty percent of 1000:
			// 80 is under it.
			baseline: comparisonReport("b",
				comparisonRun("m", 1, true, 1000, 500),
				comparisonRun("m", 2, true, 1000, 500)),
			cand: comparisonReport("c",
				comparisonRun("m", 1, true, 1080, 500),
				comparisonRun("m", 2, true, 1080, 500)),
		},
		{
			name: "a token gain above the relative floor is a regression",
			baseline: comparisonReport("b",
				comparisonRun("m", 1, true, 1000, 500),
				comparisonRun("m", 2, true, 1000, 500)),
			cand: comparisonReport("c",
				comparisonRun("m", 1, true, 1300, 500),
				comparisonRun("m", 2, true, 1300, 500)),
			wantRegressions: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmp := Compare(c.baseline, c.cand)
			if got := len(cmp.Regressions); got != c.wantRegressions {
				t.Errorf("the comparison holds %d regressions, want %d: %+v", got, c.wantRegressions, cmp.Regressions)
			}
			if got := len(cmp.Improvements); got != c.wantImprovements {
				t.Errorf("the comparison holds %d improvements, want %d: %+v", got, c.wantImprovements, cmp.Improvements)
			}
		})
	}
}

// TestAComparisonNamesOneSidedModels covers a model that ran on only
// one side: it is named and skipped, never compared against zeros.
func TestAComparisonNamesOneSidedModels(t *testing.T) {
	baseline := comparisonReport("b", comparisonRun("m", 1, true, 100, 50))
	candidate := comparisonReport("c",
		comparisonRun("m", 1, true, 100, 50),
		comparisonRun("solo", 1, false, 100, 50))

	cmp := Compare(baseline, candidate)
	if len(cmp.OneSided) != 1 || cmp.OneSided[0] != "solo" {
		t.Errorf("the one-sided models are %v, want [solo]", cmp.OneSided)
	}
	for _, mc := range cmp.PerModel {
		if mc.Model == "solo" {
			t.Error("the one-sided model was compared against zeros")
		}
	}
	if len(cmp.Regressions) != 0 || len(cmp.Improvements) != 0 {
		t.Errorf("the one-sided model produced a finding: %+v / %+v", cmp.Regressions, cmp.Improvements)
	}
}
