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
func compactionUsageRecord(turn, input, output int, ttft float64) string {
	return `{"record":"usage","seq":1,"ts":"2026-10-20T00:00:00Z","origin":"compaction","run_id":"r","turn":` +
		itoa(turn) + `,"request":0,"input_tokens":` + itoa(input) +
		`,"output_tokens":` + itoa(output) + `,"ttft_ms":` + ftoa(ttft) + `}`
}

// TestTheCompactionTokensAreCounted covers the split: the totals hold
// every request, and the compaction's share is its own.
func TestTheCompactionTokensAreCounted(t *testing.T) {
	log, err := ParseLog(writeLog(t,
		usageRecord(1, 1, 100, 10, 5, nil),
		compactionUsageRecord(1, 900, 90, 50),
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
		compactionUsageRecord(1, 900, 90, 80),
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
