package poo

import "testing"

// The turn-growth tracker: what one turn adds to the prompt, measured from the
// session's own usage reports.

// observeAll feeds the tracker the given reports, in order.
func observeAll(g *growthTracker, reports ...int) {
	for _, input := range reports {
		g.observe(input)
	}
}

// TestTheTurnGrowthIsTheLargestRecentDifference covers the measure: the growth
// is the largest increase between two reports, and the reports that came
// before the recent ones do not count.
func TestTheTurnGrowthIsTheLargestRecentDifference(t *testing.T) {
	g := &growthTracker{}
	// The first report is the baseline, and the three that follow add 50,
	// 250 and 50.
	observeAll(g, 100, 150, 400, 450)
	if got := g.growth(1); got != 250 {
		t.Errorf("the turn growth is %d, want the largest difference 250", got)
	}
	// A fourth difference joins the window, so the 250 is still the
	// largest of the four the tracker keeps.
	observeAll(g, 500)
	if got := g.growth(1); got != 250 {
		t.Errorf("the turn growth is %d, want 250 from the recent window", got)
	}
}

// TestAnOlderLargerDifferenceAgesOut covers the window: the tracker keeps the
// last growthReports differences, so one large turn stops moving the trigger
// once enough turns have followed it.
func TestAnOlderLargerDifferenceAgesOut(t *testing.T) {
	g := &growthTracker{}
	// The first report is the baseline, and the one after it adds 400.
	observeAll(g, 100, 500)
	if got := g.growth(1); got != 400 {
		t.Errorf("the turn growth is %d, want the first turn's 400", got)
	}
	// growthReports more turns of 50 each push the 400 out of the window.
	for i := 1; i <= growthReports; i++ {
		g.observe(500 + 50*i)
	}
	if got := g.growth(1); got != 50 {
		t.Errorf("the turn growth is %d, want 50: the larger difference aged out", got)
	}
}

// TestTheFirstReportAfterACompactionAddsNoDifference covers the reset: a
// compaction drops the input, and the drop is not growth. The differences
// recorded before the compaction stay.
func TestTheFirstReportAfterACompactionAddsNoDifference(t *testing.T) {
	g := &growthTracker{}
	observeAll(g, 100, 300, 500)
	if got := g.growth(1); got != 200 {
		t.Errorf("the turn growth is %d, want 200", got)
	}

	g.resetBaseline()
	// The first report after the compaction is the new baseline, however
	// far the input fell.
	observeAll(g, 40)
	if got := g.growth(1); got != 200 {
		t.Errorf("the turn growth is %d, want the earlier 200: a compaction records no difference", got)
	}
	// The report after it measures against the new baseline.
	observeAll(g, 340)
	if got := g.growth(1); got != 300 {
		t.Errorf("the turn growth is %d, want 300 from the turn after the compaction", got)
	}
}

// TestTheObservationCapStandsInBeforeADifferenceExists covers the start of a
// run: until two reports give the tracker a difference, the growth is the
// observation cap, because one tool result is what a turn can add unseen.
func TestTheObservationCapStandsInBeforeADifferenceExists(t *testing.T) {
	for _, test := range []struct {
		name    string
		reports []int
	}{
		{"no report", nil},
		{"one report", []int{100}},
		{"two reports that do not grow", []int{200, 100}},
	} {
		g := &growthTracker{}
		observeAll(g, test.reports...)
		if got := g.growth(777); got != 777 {
			t.Errorf("%s: the turn growth is %d, want the observation cap 777", test.name, got)
		}
	}
}
