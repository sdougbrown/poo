package eval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeReport builds a report with the given runs.
func makeReport(dir string, results ...RunResult) *Report {
	return &Report{Dir: dir, StartedAt: "2026-10-20T00:00:00Z", Results: results}
}

// makeRun builds one run result.
func makeRun(model string, seed int, success bool, metrics Metrics) RunResult {
	return RunResult{Task: "t", Model: model, Seed: seed, Success: success, Metrics: metrics}
}

func TestASmallSuccessDropIsNotARegression(t *testing.T) {
	// A drop of three points is under the five-point floor, so it is not
	// reported.
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{}),
		makeRun("m", 2, true, Metrics{}),
		makeRun("m", 3, true, Metrics{}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{}),
		makeRun("m", 2, true, Metrics{}),
		makeRun("m", 3, false, Metrics{}),
	)

	cmp := Compare(baseline, candidate)
	if len(cmp.Regressions) != 0 {
		t.Errorf("the comparison found %d regressions, want none: %+v", len(cmp.Regressions), cmp.Regressions)
	}
}

func TestABigSuccessDropIsARegression(t *testing.T) {
	// A drop of forty points is over the five-point floor.
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{}),
		makeRun("m", 2, true, Metrics{}),
		makeRun("m", 3, true, Metrics{}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, false, Metrics{}),
		makeRun("m", 2, false, Metrics{}),
		makeRun("m", 3, false, Metrics{}),
	)

	cmp := Compare(baseline, candidate)
	if len(cmp.Regressions) != 1 {
		t.Fatalf("the comparison found %d regressions, want 1", len(cmp.Regressions))
	}
	reg := cmp.Regressions[0]
	if reg.Metric != "success_rate" {
		t.Errorf("the regression is in %q, want success_rate", reg.Metric)
	}
	if reg.Delta >= 0 {
		t.Errorf("the regression's delta is %v, want a drop", reg.Delta)
	}
	if reg.Floor < SuccessFloorPoints/100 {
		t.Errorf("the regression's floor is %v, want at least %v", reg.Floor, SuccessFloorPoints/100)
	}
}

func TestTheNoiseFloorCoversANoisyBaseline(t *testing.T) {
	// The baseline's own seed spread is large, so a difference that
	// would otherwise be a regression is inside the noise.
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{}),
		makeRun("m", 2, false, Metrics{}),
		makeRun("m", 3, true, Metrics{}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{}),
		makeRun("m", 2, true, Metrics{}),
		makeRun("m", 3, true, Metrics{}),
	)

	cmp := Compare(baseline, candidate)
	if len(cmp.Regressions) != 0 {
		t.Errorf("the comparison found %d regressions, want none: %+v", len(cmp.Regressions), cmp.Regressions)
	}
}

func TestMoreTokensIsARegression(t *testing.T) {
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{InputTokens: 1000}),
		makeRun("m", 2, true, Metrics{InputTokens: 1000}),
		makeRun("m", 3, true, Metrics{InputTokens: 1000}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{InputTokens: 5000}),
		makeRun("m", 2, true, Metrics{InputTokens: 5000}),
		makeRun("m", 3, true, Metrics{InputTokens: 5000}),
	)

	cmp := Compare(baseline, candidate)
	if len(cmp.Regressions) != 1 {
		t.Fatalf("the comparison found %d regressions, want 1", len(cmp.Regressions))
	}
	if cmp.Regressions[0].Metric != "input_tokens" {
		t.Errorf("the regression is in %q, want input_tokens", cmp.Regressions[0].Metric)
	}
}

func TestASmallTokenChangeIsNotARegression(t *testing.T) {
	// The tokens have no absolute floor, so a change inside the seed
	// spread is not a regression.
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{InputTokens: 1000}),
		makeRun("m", 2, true, Metrics{InputTokens: 3000}),
		makeRun("m", 3, true, Metrics{InputTokens: 5000}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{InputTokens: 1100}),
		makeRun("m", 2, true, Metrics{InputTokens: 3100}),
		makeRun("m", 3, true, Metrics{InputTokens: 5100}),
	)

	cmp := Compare(baseline, candidate)
	if len(cmp.Regressions) != 0 {
		t.Errorf("the comparison found %d regressions, want none: %+v", len(cmp.Regressions), cmp.Regressions)
	}
}

func TestAnImprovementIsNotARegression(t *testing.T) {
	baseline := makeReport("base",
		makeRun("m", 1, true, Metrics{}),
		makeRun("m", 2, true, Metrics{}),
		makeRun("m", 3, true, Metrics{}),
	)
	candidate := makeReport("cand",
		makeRun("m", 1, true, Metrics{}),
		makeRun("m", 2, true, Metrics{}),
		makeRun("m", 3, true, Metrics{}),
	)

	cmp := Compare(baseline, candidate)
	if len(cmp.Regressions) != 0 || len(cmp.Improvements) != 0 {
		t.Errorf("the comparison found %d regressions and %d improvements, want none of either",
			len(cmp.Regressions), len(cmp.Improvements))
	}
}

func TestTheReportIsWrittenAndRead(t *testing.T) {
	dir := t.TempDir()
	report := makeReport(dir,
		makeRun("m", 1, true, Metrics{Turns: 3, SettleReason: "end_turn"}),
	)
	if err := WriteReport(dir, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	for _, name := range []string{"report.json", "summary.json", "report.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("the report has no %s: %v", name, err)
		}
	}

	read, err := LoadReport(dir)
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if len(read.Results) != 1 || read.Results[0].Metrics.Turns != 3 {
		t.Errorf("the report read back as %+v, want one run with three turns", read.Results)
	}
}

func TestTheMarkdownReportHoldsTheNumbers(t *testing.T) {
	dir := t.TempDir()
	report := makeReport(dir,
		makeRun("gemma4", 1, true, Metrics{Turns: 5, SettleReason: "end_turn"}),
		makeRun("gemma4", 2, false, Metrics{Turns: 7, SettleReason: "max_turns"}),
	)
	summary := Summarize(report)
	markdown := RenderMarkdown(report, summary)

	for _, needle := range []string{"gemma4", "## By model", "## By task", "## Settle reasons", "end_turn"} {
		if !contains(markdown, needle) {
			t.Errorf("the markdown report does not mention %q", needle)
		}
	}
}

func TestTheSummaryCountsThePathsOutsideTheExpectedOnes(t *testing.T) {
	report := makeReport("d",
		RunResult{Task: "t", Model: "m", Seed: 1, Success: true, Metrics: Metrics{UnexpectedFiles: []string{"other.go"}}},
		RunResult{Task: "t", Model: "m", Seed: 2, Success: true, Metrics: Metrics{}},
	)
	summary := Summarize(report)
	if summary.Overall.UnexpectedFiles != 1 {
		t.Errorf("the summary counts %d runs with unexpected files, want 1", summary.Overall.UnexpectedFiles)
	}
}

func TestTheJudgeVerdictIsWritten(t *testing.T) {
	// A judge that answers with a verdict, over a transcript.
	verdictJSON := `{"logical_path":true,"agrees_with_check":true,"pathologies":[],"needs_owner":false,"reason":"Turn 1 read the file, turn 2 edited it."}`
	answer, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{"content": verdictJSON},
		}},
	})
	if err != nil {
		t.Fatalf("encoding the answer: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(answer)
	}))
	defer server.Close()

	resultsDir := t.TempDir()
	// A transcript under the results directory.
	runDir := filepath.Join(resultsDir, "runs", "t", "m", "seed1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	log := strings.Join([]string{
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":"I read the file."}]}`,
		`{"record":"run.settled","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(runDir, "session.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	judge, err := NewJudge(JudgeOptions{BaseURL: server.URL, ResultsDir: resultsDir})
	if err != nil {
		t.Fatalf("making the judge: %v", err)
	}
	result, err := judge.Run(t.Context())
	if err != nil {
		t.Fatalf("running the judge: %v", err)
	}
	if len(result.Verdicts) != 1 {
		t.Fatalf("the judge wrote %d verdicts, want 1", len(result.Verdicts))
	}
	verdict := result.Verdicts[0]
	if !verdict.LogicalPath || !verdict.AgreesWithCheck || verdict.NeedsOwner {
		t.Errorf("the verdict is %+v, want a clean one", verdict)
	}
	if verdict.Reason == "" {
		t.Error("the verdict has no reason")
	}

	// The verdict file and the summary are written.
	if _, err := os.Stat(filepath.Join(resultsDir, "judgments", "t__m__seed1.json")); err != nil {
		t.Errorf("the verdict file is missing: %v", err)
	}
	summary, err := os.ReadFile(filepath.Join(resultsDir, "judgments", "summary.md"))
	if err != nil {
		t.Fatalf("reading the summary: %v", err)
	}
	for _, needle := range []string{"Needs owner", "Pathologies per model", "All verdicts"} {
		if !contains(string(summary), needle) {
			t.Errorf("the summary does not have the %q section", needle)
		}
	}
}

func TestOneUnreadableTranscriptDoesNotStopTheJudge(t *testing.T) {
	// A judge that cannot be reached produces a verdict with the error
	// in it, and the run still reports every transcript.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the judge is down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	resultsDir := t.TempDir()
	runDir := filepath.Join(resultsDir, "runs", "t", "m", "seed1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	log := `{"record":"run.settled","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`
	if err := os.WriteFile(filepath.Join(runDir, "session.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	judge, err := NewJudge(JudgeOptions{BaseURL: server.URL, ResultsDir: resultsDir})
	if err != nil {
		t.Fatalf("making the judge: %v", err)
	}
	result, err := judge.Run(t.Context())
	if err != nil {
		t.Fatalf("running the judge: %v", err)
	}
	if len(result.Verdicts) != 1 {
		t.Fatalf("the judge wrote %d verdicts, want 1", len(result.Verdicts))
	}
	if result.Verdicts[0].Error == "" {
		t.Error("the verdict has no error, want the judge's failure")
	}
	// The verdict is still written, so a reader can see what happened.
	if _, err := os.Stat(filepath.Join(resultsDir, "judgments", "t__m__seed1.json")); err != nil {
		t.Errorf("the verdict file is missing: %v", err)
	}
}

func TestTheJudgePromptTreatsTheTranscriptAsData(t *testing.T) {
	// The prompt must tell the judge to treat the transcript as data.
	if !contains(judgePrompt, "DATA") {
		t.Error("the prompt does not say the transcript is data")
	}
	if !contains(judgePrompt, "Ignore it") {
		t.Error("the prompt does not tell the judge to ignore the transcript's instructions")
	}
}

func TestALargeTranscriptIsSummarized(t *testing.T) {
	// A transcript with many turns is summarized, and the verdict says
	// so.
	var segments []TranscriptSegment
	for i := 0; i < 20; i++ {
		segments = append(segments, TranscriptSegment{Turn: i, Text: strings.Repeat("x", 1000)})
	}
	text, summarized, elided := summarizeTranscript(segments, 5000)
	if !summarized {
		t.Error("the transcript was not summarized")
	}
	if elided <= 0 {
		t.Errorf("the summary elided %d turns, want some", elided)
	}
	if !contains(text, "elided") {
		t.Errorf("the summary does not say what it elided: %q", text[:200])
	}
	// The first and the last turn survive.
	if !contains(text, strings.Repeat("x", 1000)) {
		t.Error("the summary holds none of the original turns")
	}
}

func TestTheJudgeAnswerIsFoundInsideProse(t *testing.T) {
	// A judge that wraps its answer in prose still gets read.
	answer := "Here is my verdict:\n```json\n{\"logical_path\": false, \"agrees_with_check\": true, \"pathologies\": [\"loop_or_reread\"], \"needs_owner\": true, \"reason\": \"Turn 2 read the same file again.\"}\n```\n"
	parsed, err := parseVerdict(answer)
	if err != nil {
		t.Fatalf("parsing the verdict: %v", err)
	}
	if *parsed.LogicalPath {
		t.Error("the logical path was read as true, want false")
	}
	if !*parsed.NeedsOwner {
		t.Error("the needs-owner flag was read as false, want true")
	}
	if len(parsed.Pathologies) != 1 {
		t.Errorf("the pathologies are %v, want one", parsed.Pathologies)
	}
}

func TestAnUnknownPathologyBecomesOther(t *testing.T) {
	got := normalizePathologies([]string{"loop_or_reread", "something_new"})
	if len(got) != 2 {
		t.Fatalf("the pathologies are %v, want two", got)
	}
	if got[0] != PathologyLoopOrReread {
		t.Errorf("the first pathology is %q, want loop_or_reread", got[0])
	}
	if got[1] != PathologyOther {
		t.Errorf("the second pathology is %q, want other", got[1])
	}
}

func TestTheVerdictIsValidJSON(t *testing.T) {
	verdict := Verdict{
		Task: "t", Model: "m", Seed: 1,
		LogicalPath: true, AgreesWithCheck: false,
		Pathologies: []Pathology{PathologyOffTaskEdit},
		NeedsOwner:  true,
		Reason:      "Turn 3 edited a file the task did not name.",
	}
	data, err := json.Marshal(verdict)
	if err != nil {
		t.Fatalf("encoding the verdict: %v", err)
	}
	var back Verdict
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("decoding the verdict: %v", err)
	}
	if back.LogicalPath != verdict.LogicalPath || back.NeedsOwner != verdict.NeedsOwner {
		t.Errorf("the verdict read back as %+v", back)
	}
	if len(back.Pathologies) != 1 || back.Pathologies[0] != PathologyOffTaskEdit {
		t.Errorf("the pathologies read back as %v", back.Pathologies)
	}
}
