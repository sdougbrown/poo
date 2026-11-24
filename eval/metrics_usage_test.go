package eval

import "testing"

// The cost of a compaction is its own number.
//
// A request a compaction made is a usage record with the compaction's
// origin. Its tokens are part of what the run spent, and they are also
// the figure a strategy comparison reads, so they are counted twice: once
// in the run's totals and once as compaction.

// compactionUsageRecord builds the usage record of a request a compaction
// made.
func compactionUsageRecord(turn, input, output int, ttft float64, cache *int) string {
	cacheField := ""
	if cache != nil {
		cacheField = `,"cache_read_tokens":` + itoa(*cache)
	}
	return `{"record":"usage","seq":1,"ts":"2026-10-20T00:00:00Z","origin":"compaction","run_id":"r","turn":` +
		itoa(turn) + `,"request":0,"input_tokens":` + itoa(input) +
		`,"output_tokens":` + itoa(output) + cacheField + `,"ttft_ms":` + ftoa(ttft) + `}`
}

// TestTheCompactionTokensAreCounted covers the split: the totals hold
// every request, and the compaction's share is its own.
func TestTheCompactionTokensAreCounted(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		usageRecord(1, 1, 100, 10, 5, nil),
		compactionUsageRecord(1, 900, 90, 50, nil),
		usageRecord(2, 2, 200, 20, 6, nil),
		settledRecord("end_turn", 2),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.InputTokens != 1200 || m.OutputTokens != 120 {
		t.Errorf("the totals are %d in and %d out, want every request's 1200 and 120",
			m.InputTokens, m.OutputTokens)
	}
	if m.CompactionInputTokens != 900 || m.CompactionOutputTokens != 90 {
		t.Errorf("the compaction's share is %d in and %d out, want 900 and 90",
			m.CompactionInputTokens, m.CompactionOutputTokens)
	}
}

// TestACompactionIsNotATurnToTheTTFT covers what the time to the first
// token measures: a compaction is slower than a turn and is not one, so
// its time stays out of the mean and the trend.
func TestACompactionIsNotATurnToTheTTFT(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		usageRecord(1, 1, 100, 10, 4, nil),
		compactionUsageRecord(1, 900, 90, 80, nil),
		usageRecord(2, 2, 200, 20, 6, nil),
		settledRecord("end_turn", 2),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.TTFTMS != 5 {
		t.Errorf("the mean time to the first token is %v, want the turns' 5", m.TTFTMS)
	}
	if m.TTFTTrend != 2 {
		t.Errorf("the trend is %v, want the turns' 2 ms per turn", m.TTFTTrend)
	}
}

// TestARunWithoutCompactionHasNoCompactionTokens covers the other side: a
// log with only turns reports no compaction spend at all.
func TestARunWithoutCompactionHasNoCompactionTokens(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		usageRecord(1, 1, 100, 10, 5, nil),
		settledRecord("end_turn", 1),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})
	if m.CompactionInputTokens != 0 || m.CompactionOutputTokens != 0 {
		t.Errorf("the compaction's share is %d in and %d out, want none",
			m.CompactionInputTokens, m.CompactionOutputTokens)
	}
}

// TestATornLogCountsTurnsWithoutTheCompactions covers the fallback: a log
// whose run was killed has no settle record, so the turns come from the
// usage records, and a request a compaction made is not one of them.
func TestATornLogCountsTurnsWithoutTheCompactions(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		usageRecord(1, 1, 100, 10, 5, nil),
		compactionUsageRecord(1, 900, 90, 50, nil),
		usageRecord(2, 2, 200, 20, 6, nil),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	if got := log.Turns(); got != 2 {
		t.Errorf("the log counts %d turns, want the run's 2: the compaction is not one", got)
	}
	if got := ComputeMetrics(log, Diff{}).Turns; got != 2 {
		t.Errorf("the metrics count %d turns, want 2", got)
	}
}

// TestACompactionsCacheReadsAreCounted covers the cache total's boundary:
// a compaction's cached prompt tokens are prompt tokens, so they are in
// the total, and the record that adds them is the same one the turn
// count skips.
func TestACompactionsCacheReadsAreCounted(t *testing.T) {
	cached := 500
	log, err := ParseLog(writeLog(t,
		usageRecord(1, 1, 100, 10, 5, &cached),
		compactionUsageRecord(1, 900, 90, 50, &cached),
		settledRecord("end_turn", 1),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.CacheReadTokens == nil || *m.CacheReadTokens != 1000 {
		t.Errorf("the cached prompt tokens are %v, want the turns' and the compaction's 1000", m.CacheReadTokens)
	}
}

// TestTheAggregateCarriesTheCompactionShare covers where the number is
// read: a comparison aggregates runs, and the compaction's share of the
// spend reaches the aggregate and the table it prints.
func TestTheAggregateCarriesTheCompactionShare(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		usageRecord(1, 1, 100, 10, 5, nil),
		compactionUsageRecord(1, 900, 90, 50, nil),
		settledRecord("end_turn", 1),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	runs := []CellResult{{Metrics: ComputeMetrics(log, Diff{}), Success: true}}
	agg := aggregate(runs)

	if agg.CompactionInputTokens != 900 || agg.CompactionOutputTokens != 90 {
		t.Errorf("the aggregate's compaction share is %v in and %v out, want 900 and 90",
			agg.CompactionInputTokens, agg.CompactionOutputTokens)
	}
	names := map[string]bool{}
	for _, m := range comparedMetrics() {
		names[m.name] = true
	}
	for _, want := range []string{"compaction_input_tokens", "compaction_output_tokens"} {
		if !names[want] {
			t.Errorf("the comparison has no %s metric", want)
		}
	}
}

// TestTheComparisonSeesACompactionRegression covers the rows' effect: a
// candidate whose only difference is what compacting cost is one
// regression, and it is the compaction's own metric that names it.
func TestTheComparisonSeesACompactionRegression(t *testing.T) {
	baseline := comparisonReport("b",
		comparisonRun("m", 1, true, 1000, 500),
		comparisonRun("m", 2, true, 1000, 500))
	candidate := comparisonReport("c",
		comparisonRun("m", 1, true, 1000, 500),
		comparisonRun("m", 2, true, 1000, 500))
	for i := range candidate.Results {
		candidate.Results[i].Metrics.CompactionInputTokens = 900
	}

	cmp := Compare(baseline, candidate)
	if len(cmp.Regressions) != 1 {
		t.Fatalf("the comparison found %d regressions, want the compaction's 1: %+v",
			len(cmp.Regressions), cmp.Regressions)
	}
	if got := cmp.Regressions[0].Metric; got != "compaction_input_tokens" {
		t.Errorf("the regression names %q, want compaction_input_tokens", got)
	}
}

// compactionRecord builds a compaction record naming the strategy that
// produced it.
func compactionRecord(strategy string) string {
	return `{"record":"compaction","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,` +
		`"compaction":{"strategy":"` + strategy + `"}}`
}

// TestTheCompactionsCountAndNameTheirStrategies covers the count and
// the strategies the run compacted under: a strategy's fallback shows
// up as a second compaction under its own name.
func TestTheCompactionsCountAndNameTheirStrategies(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		callRecord(1, "history", `{"turns":[1]}`),
		compactionRecord("pointer"),
		compactionRecord("mine"),
		usageRecord(2, 1, 100, 10, 5, nil),
		settledRecord("end_turn", 2),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.Compactions != 2 {
		t.Errorf("the run compacted %d times, want 2", m.Compactions)
	}
	if len(m.CompactionStrategies) != 2 || m.CompactionStrategies[0] != "pointer" || m.CompactionStrategies[1] != "mine" {
		t.Errorf("the strategies are %v, want pointer then mine", m.CompactionStrategies)
	}
}

// TestTheHistoryCallsCountOnlyAfterACompaction covers the recovery
// metric: a history call before any compaction is the tool's ordinary
// use, and only the calls after the first compaction are the run
// reading back what the queries missed.
func TestTheHistoryCallsCountOnlyAfterACompaction(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		callRecord(1, "history", `{"turns":[1]}`),
		compactionRecord("pointer"),
		callRecord(2, "history", `{"turn":3}`),
		callRecord(2, "read", `{"path":"a.go"}`),
		callRecord(3, "history", `{"turn":2}`),
		settledRecord("end_turn", 3),
	))
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.HistoryCallsAfterCompaction != 2 {
		t.Errorf("the run made %d history calls after the compaction, want 2",
			m.HistoryCallsAfterCompaction)
	}
}
