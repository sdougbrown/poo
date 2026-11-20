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

// TestACompactionsOwnReportIsNotATurn covers the origin the session leaves
// out: the input a compaction's own request reported is the strategy's, not
// the view's, so it neither measures a difference nor replaces the baseline.
func TestACompactionsOwnReportIsNotATurn(t *testing.T) {
	s := &Session{}
	s.opts.TurnGrowth = true
	s.growth.observe(100)
	s.growth.observe(200)
	if got := s.growth.growth(1); got != 100 {
		t.Fatalf("the turn growth is %d, want 100 before the compaction", got)
	}

	// The compaction's request reported far more input than the view holds.
	s.noteTurnInput(RecordMeta{Record: "usage", Origin: OriginCompaction}, &UsageReport{InputTokens: 5000})
	if got := s.growth.growth(1); got != 100 {
		t.Errorf("the turn growth is %d, want 100: a compaction's report measures nothing", got)
	}

	// The next turn measures against the view's last report, not against the
	// compaction's.
	s.noteTurnInput(RecordMeta{Record: "usage"}, &UsageReport{InputTokens: 400})
	if got := s.growth.growth(1); got != 200 {
		t.Errorf("the turn growth is %d, want the 200 the turn after the compaction added", got)
	}

	// A compaction that reports nothing leaves the tracker as it found it, so
	// the turn after it still measures against the turn before it.
	s.noteTurnInput(RecordMeta{Record: "usage", Origin: OriginCompaction}, nil)
	s.noteTurnInput(RecordMeta{Record: "usage"}, &UsageReport{InputTokens: 800})
	if got := s.growth.growth(1); got != 400 {
		t.Errorf("the turn growth is %d, want the 400 the turn after the silent compaction added", got)
	}
}

// TestATurnThatReportsNoInputStartsTheBaselineOver covers a turn whose usage
// says nothing about the view. A difference must never span two turns, so the
// report after such a turn is a baseline rather than growth measured across the
// gap.
func TestATurnThatReportsNoInputStartsTheBaselineOver(t *testing.T) {
	for _, test := range []struct {
		name  string
		usage *UsageReport
	}{
		{"no usage at all", nil},
		{"no input tokens", &UsageReport{InputTokens: 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Session{}
			s.opts.TurnGrowth = true
			// The first two turns measure a difference of 2000.
			s.noteTurnInput(RecordMeta{Record: "usage"}, &UsageReport{InputTokens: 1000})
			s.noteTurnInput(RecordMeta{Record: "usage"}, &UsageReport{InputTokens: 3000})
			if got := s.growth.growth(1); got != 2000 {
				t.Fatalf("the turn growth is %d, want 2000 before the silent turn", got)
			}

			s.noteTurnInput(RecordMeta{Record: "usage"}, test.usage)
			s.noteTurnInput(RecordMeta{Record: "usage"}, &UsageReport{InputTokens: 9000})
			if got := s.growth.growth(1); got != 2000 {
				t.Errorf("the turn growth is %d, want the earlier 2000: the input across the silent turn is no difference", got)
			}
			// The report after the silent turn is the baseline, so the next turn
			// measures against it.
			s.noteTurnInput(RecordMeta{Record: "usage"}, &UsageReport{InputTokens: 12000})
			if got := s.growth.growth(1); got != 3000 {
				t.Errorf("the turn growth is %d, want the 3000 the turn after the new baseline added", got)
			}
		})
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
