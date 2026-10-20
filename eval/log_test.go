package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeLog writes a session log and returns its path.
//
// The records are the raw JSON the harness reads, so a test writes the
// log the way poo writes it.
func writeLog(t *testing.T, records ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	var b []byte
	for _, r := range records {
		b = append(b, r...)
		b = append(b, '\n')
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}
	return path
}

// usageRecord builds a usage record.
func usageRecord(turn, request int, input, output int, ttft float64, cache *int) string {
	cacheField := ""
	if cache != nil {
		cacheField = `,"cache_read_tokens":` + itoa(*cache)
	}
	return `{"record":"usage","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":` +
		itoa(turn) + `,"request":` + itoa(request) + `,"input_tokens":` + itoa(input) +
		`,"output_tokens":` + itoa(output) + cacheField + `,"ttft_ms":` + ftoa(ttft) + `}`
}

// callRecord builds a tool call record.
func callRecord(turn int, tool, args string) string {
	return `{"record":"tool.call","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":` +
		itoa(turn) + `,"handle":"r1","tool":"` + tool + `","arguments":` + args + `}`
}

// resultRecord builds a tool result record.
func resultRecord(turn int, tool, outcome string) string {
	return `{"record":"tool.result","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":` +
		itoa(turn) + `,"handle":"r1","tool":"` + tool + `","outcome":"` + outcome + `","text":"out"}`
}

// settledRecord builds a run settle record.
func settledRecord(reason string, turns int) string {
	return `{"record":"run.settled","seq":1,"ts":"2026-10-20T00:00:01Z","run_id":"r","reason":"` +
		reason + `","detail":{"source":"end_turn"},"turns":` + itoa(turns) +
		`,"settled_at":"2026-10-20T00:00:01Z"}`
}

// itoa formats an int.
func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// ftoa formats a float.
func ftoa(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func TestTheMetricsComeFromTheLog(t *testing.T) {
	cache := 400
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"run.started","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","model_id":"m","max_turns":50}`,
		callRecord(1, "read", `{"path":"a.go"}`),
		resultRecord(1, "read", "ok"),
		usageRecord(1, 1, 1000, 100, 200, &cache),
		callRecord(2, "bash", `{"command":"ls"}`),
		resultRecord(2, "bash", "error"),
		usageRecord(2, 2, 2000, 200, 400, &cache),
		settledRecord("end_turn", 2),
	)

	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{Files: []string{"a.go"}, Lines: 3})

	if m.Turns != 2 {
		t.Errorf("the turns are %d, want 2", m.Turns)
	}
	if m.InputTokens != 3000 || m.OutputTokens != 300 {
		t.Errorf("the tokens are %d in and %d out, want 3000 in and 300 out", m.InputTokens, m.OutputTokens)
	}
	if m.ToolCalls != 2 || m.ToolErrors != 1 {
		t.Errorf("the tool calls are %d with %d errors, want 2 with 1 error", m.ToolCalls, m.ToolErrors)
	}
	if m.ToolErrorRate != 0.5 {
		t.Errorf("the tool error rate is %v, want 0.5", m.ToolErrorRate)
	}
	if m.CacheReadTokens == nil || *m.CacheReadTokens != 800 {
		t.Errorf("the cache read tokens are %v, want 800", m.CacheReadTokens)
	}
	if m.CacheHitRatio == nil {
		t.Fatal("the cache hit ratio is missing")
	}
	if want := 800.0 / 3000.0; *m.CacheHitRatio < want-0.001 || *m.CacheHitRatio > want+0.001 {
		t.Errorf("the cache hit ratio is %v, want about %v", *m.CacheHitRatio, want)
	}
	if m.SettleReason != "end_turn" {
		t.Errorf("the settle reason is %q, want end_turn", m.SettleReason)
	}
	if m.DiffLines != 3 {
		t.Errorf("the diff is %d lines, want 3", m.DiffLines)
	}
}

func TestTheTTFTTrendFollowsThePrefix(t *testing.T) {
	// A run whose first token gets slower and slower has a positive
	// trend, and the trend is reported.
	path := writeLog(t,
		usageRecord(1, 1, 100, 10, 100, nil),
		usageRecord(2, 2, 200, 10, 200, nil),
		usageRecord(3, 3, 300, 10, 300, nil),
		settledRecord("end_turn", 3),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.TTFTTrend <= 0 {
		t.Errorf("the TTFT trend is %v, want a positive slope", m.TTFTTrend)
	}
	if !m.TTFTGrowsWithPrefix {
		t.Error("the run should report that the time to the first token grew with the prefix")
	}

	// A run whose time to the first token is flat does not.
	flat := writeLog(t,
		usageRecord(1, 1, 100, 10, 100, nil),
		usageRecord(2, 2, 200, 10, 100, nil),
		settledRecord("end_turn", 2),
	)
	flatLog, err := ParseLog(flat)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	if ComputeMetrics(flatLog, Diff{}).TTFTGrowsWithPrefix {
		t.Error("a flat time to the first token should not report growth")
	}
}

func TestTheReReadRateCountsARepeatedRange(t *testing.T) {
	path := writeLog(t,
		callRecord(1, "read", `{"path":"a.go","offset":10,"limit":20}`),
		resultRecord(1, "read", "ok"),
		callRecord(2, "read", `{"path":"a.go","offset":10,"limit":20}`),
		resultRecord(2, "read", "ok"),
		callRecord(3, "read", `{"path":"b.go","offset":1,"limit":10}`),
		resultRecord(3, "read", "ok"),
		settledRecord("end_turn", 3),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.Reads != 3 {
		t.Errorf("the run made %d reads, want 3", m.Reads)
	}
	if m.Rereads != 1 {
		t.Errorf("the run made %d re-reads, want 1", m.Rereads)
	}
	if want := 1.0 / 3.0; m.RereadRate < want-0.001 || m.RereadRate > want+0.001 {
		t.Errorf("the re-read rate is %v, want about %v", m.RereadRate, want)
	}
}

func TestTheReReadRateSeesAShellRead(t *testing.T) {
	// A `cat` of the same file twice is a re-read, and a `sed -n` of the
	// same range twice is a re-read.
	path := writeLog(t,
		callRecord(1, "bash", `{"command":"cat src/main.go"}`),
		resultRecord(1, "bash", "ok"),
		callRecord(2, "bash", `{"command":"cat src/main.go"}`),
		resultRecord(2, "bash", "ok"),
		callRecord(3, "bash", `{"command":"sed -n '10,20p' src/other.go"}`),
		resultRecord(3, "bash", "ok"),
		callRecord(4, "bash", `{"command":"sed -n '10,20p' src/other.go"}`),
		resultRecord(4, "bash", "ok"),
		settledRecord("end_turn", 4),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.Reads != 4 {
		t.Errorf("the run made %d reads, want 4", m.Reads)
	}
	if m.Rereads != 2 {
		t.Errorf("the run made %d re-reads, want 2", m.Rereads)
	}
}

func TestAGrepIsNotARead(t *testing.T) {
	// A grep searches; it does not pull a file into the context.
	path := writeLog(t,
		callRecord(1, "bash", `{"command":"grep -rn TODO src/"}`),
		resultRecord(1, "bash", "ok"),
		settledRecord("end_turn", 1),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})
	if m.Reads != 0 {
		t.Errorf("the run made %d reads, want 0", m.Reads)
	}
}

func TestTheTextParseRateCountsUnparsableCalls(t *testing.T) {
	// The log records malformed arguments as a JSON string, so a call
	// whose arguments are a string is a call that did not parse.
	path := writeLog(t,
		callRecord(1, "read", `{"path":"a.go"}`),
		resultRecord(1, "read", "ok"),
		callRecord(2, "read", `"path=a.go"`),
		resultRecord(2, "read", "error"),
		settledRecord("end_turn", 2),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.TextParseFailures != 1 {
		t.Errorf("the run had %d parse failures, want 1", m.TextParseFailures)
	}
	if want := 0.5; m.TextParseRate < want-0.001 || m.TextParseRate > want+0.001 {
		t.Errorf("the parse rate is %v, want %v", m.TextParseRate, want)
	}
}

func TestTheTranscriptKeepsTheTurnsApart(t *testing.T) {
	path := writeLog(t,
		`{"record":"message","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":0,"origin":"user","role":"user","content":[{"kind":"text","text":"do the thing"}]}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls"}}]}`,
		`{"record":"tool.result","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"handle":"r1","tool":"bash","outcome":"ok","text":"a.go"}`,
		`{"record":"message","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[{"kind":"text","text":"done"}]}`,
		settledRecord("end_turn", 2),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}

	segments := log.TranscriptSegments()
	if len(segments) != 3 {
		t.Fatalf("the transcript has %d segments, want 3", len(segments))
	}
	if segments[0].Turn != 0 || segments[1].Turn != 1 || segments[2].Turn != 2 {
		t.Errorf("the segments are in turns %d, %d, %d, want 0, 1, 2",
			segments[0].Turn, segments[1].Turn, segments[2].Turn)
	}
	if !contains(segments[1].Text, "calls bash") {
		t.Errorf("the second segment does not hold the tool call: %q", segments[1].Text)
	}
	if !contains(segments[2].Text, "done") {
		t.Errorf("the third segment does not hold the answer: %q", segments[2].Text)
	}
}

func TestTheFinalMessageIsTheLastOne(t *testing.T) {
	path := writeLog(t,
		`{"record":"message","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":"first"}]}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[{"kind":"text","text":"second"}]}`,
		settledRecord("end_turn", 2),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	if got := log.FinalMessage(); got != "second" {
		t.Errorf("the final message is %q, want %q", got, "second")
	}
}

// contains reports whether a string holds a substring.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		indexOf(haystack, needle) >= 0)
}

// indexOf returns the index of a substring, or -1.
func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
