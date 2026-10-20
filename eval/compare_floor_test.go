package eval

import (
	"testing"
)

// The noise floor keeps a difference that the runs themselves do not
// explain from being called a regression.
//
// A metric that scales with its baseline gets a floor that scales with
// it too: a ten percent change in the token count is not a regression
// when the token count has no noise of its own.

func TestATenPercentTokenChangeIsNotARegression(t *testing.T) {
	// The baseline has no spread at all, so a ten percent increase is
	// inside the floor.
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{InputTokens: 1000}),
		makeRun("m", 2, true, Metrics{InputTokens: 1000}),
		makeRun("m", 3, true, Metrics{InputTokens: 1000}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{InputTokens: 1100}),
		makeRun("m", 2, true, Metrics{InputTokens: 1100}),
		makeRun("m", 3, true, Metrics{InputTokens: 1100}),
	)

	cmp := Compare(baseline, candidate)
	for _, r := range cmp.Regressions {
		if r.Metric == "input_tokens" {
			t.Errorf("a ten percent token change is a regression: %+v", r)
		}
	}
}

func TestALargeTokenChangeIsARegression(t *testing.T) {
	// A twenty-five percent change is over the floor.
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{InputTokens: 1000}),
		makeRun("m", 2, true, Metrics{InputTokens: 1000}),
		makeRun("m", 3, true, Metrics{InputTokens: 1000}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{InputTokens: 1250}),
		makeRun("m", 2, true, Metrics{InputTokens: 1250}),
		makeRun("m", 3, true, Metrics{InputTokens: 1250}),
	)

	cmp := Compare(baseline, candidate)
	found := false
	for _, r := range cmp.Regressions {
		if r.Metric == "input_tokens" {
			found = true
			if r.Floor < 200 {
				t.Errorf("the regression's floor is %v, want at least 200", r.Floor)
			}
		}
	}
	if !found {
		t.Errorf("a twenty-five percent token change is not a regression: %+v", cmp.Regressions)
	}
}

func TestTheOutputTokensHaveTheSameFloor(t *testing.T) {
	// The output tokens scale the same way the input tokens do.
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{OutputTokens: 400}),
		makeRun("m", 2, true, Metrics{OutputTokens: 400}),
		makeRun("m", 3, true, Metrics{OutputTokens: 400}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{OutputTokens: 440}),
		makeRun("m", 2, true, Metrics{OutputTokens: 440}),
		makeRun("m", 3, true, Metrics{OutputTokens: 440}),
	)

	cmp := Compare(baseline, candidate)
	for _, r := range cmp.Regressions {
		if r.Metric == "output_tokens" {
			t.Errorf("a ten percent output-token change is a regression: %+v", r)
		}
	}
}
