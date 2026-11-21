package poo_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/native"
	"github.com/sdougbrown/poo/compact/summary"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/openai"
)

// Turn growth: the session's measured per-turn input growth, added to the fit
// rule when the setting asks for it.

// TestTheTurnGrowthMovesTheFitRuleByItsOwnTokens covers the budget rule: a
// turn's growth counts as prompt on both sides of the rule, so the trigger
// fires exactly that many tokens earlier.
func TestTheTurnGrowthMovesTheFitRuleByItsOwnTokens(t *testing.T) {
	const need, growth = 4096, 700
	for _, test := range []struct {
		name   string
		window int
		cap    int
		// line is the largest input that still fits: one token more is
		// short.
		line int
	}{
		{"the window side", 40000, 0, 40000 - need - poo.OutputReserve},
		{"the cap side", 400000, 200000, 200000 - poo.OutputReserve},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := poo.ContextBudget{
				ContextLimit:   test.window,
				InputLimit:     test.cap,
				EstimatedInput: test.line,
				OutputLimit:    need,
				Turns:          2,
			}
			if budget.RoomShort(need) {
				t.Errorf("the fit rule fired at %d input tokens, one under its line", test.line)
			}
			budget.EstimatedInput++
			if !budget.RoomShort(need) {
				t.Errorf("the fit rule declined at %d input tokens, on its line", test.line+1)
			}

			grown := budget
			grown.TurnGrowth = growth
			grown.EstimatedInput = test.line - growth + 1
			if !grown.RoomShort(need) {
				t.Errorf("the fit rule declined %d tokens earlier with a turn growth of %d", growth, growth)
			}
			grown.EstimatedInput--
			if grown.RoomShort(need) {
				t.Errorf("the fit rule fired %d tokens earlier with a turn growth of %d, want %d",
					growth+1, growth, growth)
			}
		})
	}
}

// The script the session-level turn-growth test runs: every turn's report adds
// growthReportStep tokens to the one before it.
const (
	// growthReportBase is the first turn's reported input.
	growthReportBase = 4000
	// growthReportStep is how many tokens each turn adds.
	growthReportStep = 2000
	// growthTurns is how many turns the script runs: enough that the fit
	// rule crosses its line mid-run.
	growthTurns = 32
	// growthOutputLimit is the output need the run's model declares.
	growthOutputLimit = 4096
	// growthContextLimit is the run's window.
	growthContextLimit = 40000
)

// growthScript is the scripted transport's turns: every turn but the last asks
// for a tool and reports more input than the turn before it.
func growthScript() []fakeResponse {
	out := make([]fakeResponse, 0, growthTurns)
	for i := 0; i < growthTurns; i++ {
		out = append(out, fakeResponse{
			calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"t"}`)}},
			usage: &poo.UsageReport{InputTokens: growthReportBase + i*growthReportStep, OutputTokens: 10},
		})
	}
	return append(out, fakeResponse{text: "the answer"})
}

// growthRun is one run of the growth script, as the probe saw it.
type growthRun struct {
	// growth is the turn growth of every budget the compactor was given.
	growth []int
	// acted is the call number of every gate the compactor compacted at.
	acted []int
}

// runGrowthScript runs the growth script with the setting on or off, and
// returns what the compactor saw.
func runGrowthScript(t *testing.T, turnGrowth bool) growthRun {
	t.Helper()
	probe := &growthProbe{inner: roomCompactor{}}
	opts := testOptions(t, scripted(growthScript()), &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.ContextLimit = growthContextLimit
	opts.Model.MaxOutput = growthOutputLimit
	opts.Compactor = probe
	opts.TurnGrowth = turnGrowth
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	return growthRun{growth: probe.growth, acted: probe.acted}
}

// TestTurnGrowthCompactsOneReportEarlier covers the session rule: with the
// setting on, a run whose turns each add the same number of tokens compacts
// one report earlier than the same run with the setting off.
func TestTurnGrowthCompactsOneReportEarlier(t *testing.T) {
	off := runGrowthScript(t, false)
	on := runGrowthScript(t, true)

	if len(off.acted) == 0 || len(on.acted) == 0 {
		t.Fatalf("the run compacted %d times off and %d times on, want at least once each",
			len(off.acted), len(on.acted))
	}
	if want := off.acted[0] - 1; on.acted[0] != want {
		t.Errorf("the run compacted at gate %d with the setting on, want %d: one report earlier than the %d the setting off uses",
			on.acted[0], want, off.acted[0])
	}

	// The setting off leaves the budget alone.
	for i, growth := range off.growth {
		if growth != 0 {
			t.Errorf("the budget at gate %d carries a turn growth of %d with the setting off, want 0", i+1, growth)
		}
	}

	// Before a difference exists the growth is the observation cap in
	// tokens, and once two reports have landed it is the measured step.
	if len(on.growth) < 3 {
		t.Fatalf("the compactor was given %d budgets, want at least 3", len(on.growth))
	}
	if want := int(math.Ceil(30000 / poo.CharsPerToken)); on.growth[0] != want {
		t.Errorf("the first budget carries a turn growth of %d, want the observation cap of %d tokens", on.growth[0], want)
	}
	// The second gate has one report, so the cap still stands in: only the
	// report after it gives the tracker a difference.
	for i, growth := range on.growth {
		if i < 2 {
			continue
		}
		if growth != growthReportStep {
			t.Errorf("the budget at gate %d carries a turn growth of %d, want the measured %d",
				i+1, growth, growthReportStep)
		}
	}
}

// liveTurnsOf returns the run of turns the view still holds, the leading
// removed turns skipped.
func liveTurnsOf(turns []poo.TurnSummary) []poo.TurnSummary {
	var live []poo.TurnSummary
	for _, turn := range turns {
		if turn.Removed {
			if len(live) > 0 {
				break
			}
			continue
		}
		live = append(live, turn)
	}
	return live
}

// growthProbe wraps a compactor and records what every budget it was given
// carried, and which gates it compacted at.
type growthProbe struct {
	// inner is the strategy the run compacts with.
	inner poo.Compactor
	// growth is the turn growth of every budget, in call order.
	growth []int
	// compacted is the compacted input of every budget, in call order.
	compacted []int
	// acted is the call number of every gate the inner strategy said yes at.
	acted []int
}

// ShouldCompact records the budget and asks the strategy it wraps.
func (p *growthProbe) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	p.growth = append(p.growth, budget.TurnGrowth)
	p.compacted = append(p.compacted, budget.CompactedInput)
	act := p.inner.ShouldCompact(view, budget)
	if act {
		p.acted = append(p.acted, len(p.growth))
	}
	return act
}

// Compact asks the strategy it wraps.
func (p *growthProbe) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return p.inner.Compact(ctx, in)
}

// TestAModelSwitchStartsTheTurnGrowthOver covers the switch: the new model's
// tokenizer counts the same view differently, so its first report is a baseline
// rather than growth measured by the model before it.
func TestAModelSwitchStartsTheTurnGrowthOver(t *testing.T) {
	probe := &growthProbe{inner: roomCompactor{}}
	opts := testOptions(t, scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 4000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 6000, OutputTokens: 5}},
		{text: "the first answer"},
		// The new model's first report is far above the old model's last one.
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 20000, OutputTokens: 5}},
		{text: "the second answer"},
	}), &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.ContextLimit = 40000
	opts.Model.MaxOutput = 4096
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = true
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the first run settled %q, want end_turn", res.Reason)
	}
	if len(probe.growth) < 3 || probe.growth[2] != 2000 {
		t.Fatalf("the first run's turn growth is %v, want a measured 2000 at its third gate", probe.growth)
	}

	if _, err := s.SetModel(poo.ModelInfo{
		Name: "next", ModelID: "test-model", ContextLimit: 40000,
		Ops: []poo.Op{poo.OpChat, poo.OpCompact},
	}); err != nil {
		t.Fatalf("switching models: %v", err)
	}
	if res := runSession(t, s, "do it again"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the second run settled %q, want end_turn", res.Reason)
	}
	if len(probe.growth) < 5 {
		t.Fatalf("the compactor was given %d budgets, want the second run's two", len(probe.growth))
	}
	// The second run's first gate still carries what the first model measured:
	// a switch starts the baseline over, not the differences.
	if got := probe.growth[3]; got != 2000 {
		t.Errorf("the first gate after the switch carries a turn growth of %d, want the measured 2000", got)
	}
	// The new model's first report is 14000 above the old model's last one. It
	// is the new baseline, so it records no difference.
	if got := probe.growth[4]; got != 2000 {
		t.Errorf("the gate after the new model's first report carries a turn growth of %d, want 2000: the switch starts the baseline over", got)
	}
}

// TestTheFirstTurnAfterACompactionIsTheNewBaseline covers the tracker's reset
// when a compaction installs its view. The report that follows a compaction is
// above the last one before it, and it is a baseline: the view the compaction
// left is what the run grows from, not the input it replaced.
func TestTheFirstTurnAfterACompactionIsTheNewBaseline(t *testing.T) {
	probe := &growthProbe{inner: roomCompactor{}}
	opts := testOptions(t, scripted([]fakeResponse{
		// The first two turns report the same input, so the tracker records no
		// difference and the growth stays the observation cap.
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		// The report after the compaction is above the one before it.
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 31000, OutputTokens: 5}},
		{text: "the answer"},
	}), &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.ContextLimit = 34000
	opts.Model.MaxOutput = 4096
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = true
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if len(probe.acted) == 0 || probe.acted[0] != 3 {
		t.Fatalf("the run compacted at the gates %v, want the third", probe.acted)
	}
	if len(probe.growth) < 4 {
		t.Fatalf("the compactor was given %d budgets, want the gate after the compaction", len(probe.growth))
	}
	// The growth at the gate after the compaction is the same as at the gate
	// of it: no difference was recorded in between. A tracker that measured
	// across the compaction would have recorded the 1000 tokens the report
	// grew by.
	if got := probe.growth[3]; got != probe.growth[2] {
		t.Errorf("the gate after the compaction carries a turn growth of %d, want the %d the gate of it carried",
			got, probe.growth[2])
	}
}

// runNativeGrowthScript runs the growth script with turn growth on or off and a
// native compactor, and returns what the compactor saw.
func runNativeGrowthScript(t *testing.T, turnGrowth bool) growthRun {
	t.Helper()
	tr := scripted(growthScript())
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	// The compaction reports the input its own request carried. The report is
	// the strategy's, not the view's, so it is no baseline either.
	tr.SetWindowUsage(&poo.UsageReport{InputTokens: 100, OutputTokens: 5})
	probe := &growthProbe{inner: native.New(native.Options{})}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.Ops = []poo.Op{poo.OpChat, poo.OpCompact}
	opts.Model.ContextLimit = growthContextLimit
	opts.Model.MaxOutput = growthOutputLimit
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = turnGrowth
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")
	return growthRun{growth: probe.growth, acted: probe.acted}
}

// TestANativeCompactionIsNotTheTurnGrowthsBaseline covers the native strategy
// with turn growth on. The compaction's own usage report is not the view's
// input, so the turn after it measures from the view the compaction left, and
// the strategy compacts one report earlier than it does with growth off.
func TestANativeCompactionIsNotTheTurnGrowthsBaseline(t *testing.T) {
	off := runNativeGrowthScript(t, false)
	on := runNativeGrowthScript(t, true)

	if len(off.acted) == 0 || len(on.acted) == 0 {
		t.Fatalf("the run compacted %d times off and %d times on, want at least once each",
			len(off.acted), len(on.acted))
	}
	if want := off.acted[0] - 1; on.acted[0] != want {
		t.Errorf("the run compacted at gate %d with the setting on, want %d: one report earlier than the %d the setting off uses",
			on.acted[0], want, off.acted[0])
	}
	// The gate after the compaction still carries the measured step. A tracker
	// that took the compaction's own 100 tokens as its baseline would measure
	// the tens of thousands between them as one turn's growth.
	if len(on.growth) <= on.acted[0] {
		t.Fatalf("the compactor was given %d budgets, want the gate after the compaction at %d", len(on.growth), on.acted[0])
	}
	if got := on.growth[on.acted[0]]; got != growthReportStep {
		t.Errorf("the gate after the compaction carries a turn growth of %d, want the measured %d", got, growthReportStep)
	}
}

// TestAFailedNativeCompactionsReportIsNotTheBaseline covers the report a
// compaction that installs no view leaves. The request was billed and it
// reported the input it carried, but the view did not move, so the turn after
// it measures from the view's last report: the compaction's own tens of tokens
// are not what the run grew by.
func TestAFailedNativeCompactionsReportIsNotTheBaseline(t *testing.T) {
	// Every turn after the second reports growthStep more than the one before
	// it, so the step is what the gates after the failed compaction measure.
	const growthStep = 2000
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000 + growthStep, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000 + 2*growthStep, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindowError(errors.New("the endpoint refused the window"))
	tr.SetWindowBilled(true)
	tr.SetWindowUsage(&poo.UsageReport{InputTokens: 50, OutputTokens: 5})
	probe := &growthProbe{inner: native.New(native.Options{})}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.Ops = []poo.Op{poo.OpChat, poo.OpCompact}
	opts.Model.ContextLimit = 34000
	opts.Model.MaxOutput = 4096
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = true
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")
	if len(probe.acted) == 0 {
		t.Fatal("the run never asked for a window, want the first turn to fail its compaction")
	}
	if len(probe.growth) < 4 {
		t.Fatalf("the compactor was given %d budgets, want the gate after the failed compaction and the one after it", len(probe.growth))
	}
	// The two turns before the compaction reported the same input, so the
	// gates up to it carry the observation cap. The gate after the compaction
	// measures the step between the reports. A tracker that took the
	// compaction's 50 tokens as its baseline would report the whole 30000 as
	// one turn's growth.
	if got := probe.growth[3]; got != growthStep {
		t.Errorf("the gate after the failed compaction carries a turn growth of %d, want the measured step of %d", got, growthStep)
	}
}

// wireAnswer is one scripted provider answer on the wire: the tool the model
// calls or the prose it says, and the usage the endpoint reports.
type wireAnswer struct {
	// call is the tool the model calls, empty for an answer in prose.
	call string
	// text is the prose the model answers with.
	text string
	// input and output are the tokens the endpoint bills the request.
	input, output int
	// noUsage reports that the answer's stream carries no usage chunk, so the
	// session hears no report for the request.
	noUsage bool
	// fail reports that the answer breaks after its usage, so the transport
	// tries the request again.
	fail bool
}

// events renders the answer as the wire's server-sent events.
func (a wireAnswer) events() string {
	out := ""
	if a.call != "" {
		out += `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"` +
			a.call + `","arguments":"{\"text\":\"t\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"
	}
	if a.text != "" {
		out += `data: {"choices":[{"index":0,"delta":{"content":"` + a.text + `"},"finish_reason":"stop"}]}` + "\n\n"
	}
	if !a.noUsage {
		out += fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{}}],"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`+"\n\n",
			a.input, a.output)
	}
	if a.fail {
		out += `data: {"error":{"message":"the endpoint failed","type":"server_error","code":"internal_error"}}` + "\n\n"
	}
	return out + "data: [DONE]\n\n"
}

// wireTransport serves the answers in order and tries a failed request once
// more, so an attempt marked fail is retried the way a real one is.
func wireTransport(t *testing.T, answers []wireAnswer) *openai.Transport {
	t.Helper()
	var mu sync.Mutex
	served := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		n := served
		served++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n >= len(answers) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, answers[n].events())
	}))
	t.Cleanup(srv.Close)
	return openai.New(openai.Options{
		Endpoint:  srv.URL,
		Model:     "m",
		ToolsMode: "native",
		Backoff:   []time.Duration{time.Millisecond},
	})
}

// TestARetriedRequestDoesNotFeedTheTurnGrowth covers the retry: a request the
// transport tried more than once reports the sum of its attempts' input, and
// that sum is no turn's growth. The tracker starts its baseline over instead.
func TestARetriedRequestDoesNotFeedTheTurnGrowth(t *testing.T) {
	const (
		first = 4000
		step  = 2000
	)
	// The retried request's prompt, reported by the attempt that failed and
	// again by the one that answered.
	const prompt = first + 2*step
	probe := &growthProbe{inner: pootest.NewCompactor(false, poo.Compaction{})}
	opts := testOptions(t, wireTransport(t, []wireAnswer{
		{call: "echo", input: first, output: 5},
		{call: "echo", input: first + step, output: 5},
		// The third turn's request fails before its first delta and is
		// tried again, so its report is the prompt twice over.
		{input: prompt, output: 5, fail: true},
		{call: "echo", input: prompt, output: 5},
		{call: "echo", input: first + 3*step, output: 5},
		{text: "the answer"},
	}), &echoTool{result: "echoed"}, &recordingSink{})
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = true
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if len(probe.growth) < 5 {
		t.Fatalf("the compactor was given %d budgets, want the five the run's turns made", len(probe.growth))
	}
	// The gate of the turn before the retry carries the step it measured.
	if got := probe.growth[2]; got != step {
		t.Fatalf("the gate before the retried request carries a turn growth of %d, want the measured %d", got, step)
	}
	// The retried report is not a turn, so it records no difference: every
	// gate from there on carries the step measured before it.
	for i := 3; i < len(probe.growth); i++ {
		if got := probe.growth[i]; got != step {
			t.Errorf("the budget at gate %d carries a turn growth of %d, want the measured %d: a retried report is no turn",
				i+1, got, step)
		}
	}
}

// TestARetriedReportAfterACompactionLeavesTheGuardUnarmed covers the other
// seam the retry touches: a compaction's first report is what the native
// strategy's guard measures. A request that was retried reports the sum of its
// attempts, so it leaves the guard unarmed and the next report arms it.
func TestARetriedReportAfterACompactionLeavesTheGuardUnarmed(t *testing.T) {
	const (
		// prompt is the input the retried request's prompt carries, and the
		// transport reports it once for each of its attempts.
		prompt = 12000
		next   = 18000
	)
	probe := &growthProbe{inner: roomCompactor{}}
	sink := &recordingSink{}
	opts := testOptions(t, wireTransport(t, []wireAnswer{
		{call: "echo", input: 20000, output: 5},
		{call: "echo", input: 30000, output: 5},
		// The first request after the compaction is tried twice, so its
		// report is the prompt twice over.
		{input: prompt, output: 5, fail: true},
		{call: "echo", input: prompt, output: 5},
		{call: "echo", input: next, output: 5},
		{text: "the answer"},
	}), &echoTool{result: "echoed"}, sink)
	opts.Model.ContextLimit = 34000
	opts.Model.MaxOutput = 4096
	opts.Compactor = probe
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if len(probe.acted) == 0 || probe.acted[0] != 3 {
		t.Fatalf("the run compacted at the gates %v, want the third: the one before the retried request", probe.acted)
	}
	if len(probe.compacted) < 5 {
		t.Fatalf("the compactor was given %d budgets, want the five the run's turns made", len(probe.compacted))
	}
	// The retried report says nothing about what the view the compaction left
	// cost, so the guard is still unarmed at the gate after it.
	if got := probe.compacted[3]; got != 0 {
		t.Errorf("the gate after the retried report carries a compacted input of %d, want 0: a retried report is no measurement", got)
	}
	// The next report is the view's own, and the guard arms on it.
	if got := probe.compacted[4]; got != next {
		t.Errorf("the gate after the next report carries a compacted input of %d, want %d", got, next)
	}

	// A session resumed from these records arms the guard the same way. The
	// retried report is no measurement there either, so the restore takes the
	// next one and not the attempts' sum.
	restoredProbe := &growthProbe{inner: roomCompactor{}}
	restoredOpts := restoreOptions(t, wireTransport(t, []wireAnswer{{text: "the answer"}}), &recordingSink{})
	restoredOpts.Compactor = restoredProbe
	restored, _, err := poo.Restore(restoredOpts, append([]poo.Record{header("s1")}, sink.allRecords()...))
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	if res := runSession(t, restored, "carry on"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the restored run settled %q, want end_turn", res.Reason)
	}
	if len(restoredProbe.compacted) == 0 {
		t.Fatal("the restored compactor was given no budget")
	}
	live := probe.compacted[len(probe.compacted)-1]
	if got := restoredProbe.compacted[0]; got != live {
		t.Errorf("the restored budget carries a compacted input of %d, want the live guard's %d", got, live)
	}
}

// calibrationProbe records the estimator's state at every gate.
type calibrationProbe struct {
	// inner is the strategy the run compacts with.
	inner poo.Compactor
	// charsPerToken is the budget's calibration at every gate, in call order.
	charsPerToken []float64
	// estimated is the budget's estimated input at every gate, in call order.
	estimated []int
}

// ShouldCompact records the budget's calibration and asks the strategy it
// wraps.
func (p *calibrationProbe) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	p.charsPerToken = append(p.charsPerToken, budget.CharsPerToken)
	p.estimated = append(p.estimated, budget.EstimatedInput)
	return p.inner.ShouldCompact(view, budget)
}

// Compact asks the strategy it wraps.
func (p *calibrationProbe) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return p.inner.Compact(ctx, in)
}

// calibrationScript is the answers the calibration test serves: four turns, and
// the third request tried twice when retry is set.
//
// A retried report is no report, so without the retry the third answer carries
// no usage. That run is the calibration a retried report must leave alone. The
// token counts are hundreds and the tool's result is long, so the ratio lands
// inside its clamp rather than on its floor.
func calibrationScript(retry bool) []wireAnswer {
	const (
		first = 400
		step  = 50
		// prompt is the input the third request carries, reported once by
		// each of its attempts.
		prompt = first + 2*step
	)
	out := []wireAnswer{
		{call: "echo", input: first, output: 5},
		{call: "echo", input: first + step, output: 5},
	}
	if retry {
		out = append(out,
			wireAnswer{input: prompt, output: 5, fail: true},
			wireAnswer{call: "echo", input: prompt, output: 5},
		)
	} else {
		out = append(out, wireAnswer{call: "echo", output: 5, noUsage: true})
	}
	return append(out,
		wireAnswer{call: "echo", input: prompt + step, output: 5},
		wireAnswer{text: "the answer"},
	)
}

// runCalibrationScript runs one calibration script and returns what the probe
// recorded.
func runCalibrationScript(t *testing.T, answers []wireAnswer) *calibrationProbe {
	t.Helper()
	probe := &calibrationProbe{inner: pootest.NewCompactor(false, poo.Compaction{})}
	opts := testOptions(t, wireTransport(t, answers), &echoTool{result: strings.Repeat("x", 1000)}, &recordingSink{})
	opts.Settings.ObservationCapChars = 4000
	opts.Compactor = probe
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	return probe
}

// TestARetriedReportDoesNotFeedTheTokenCalibration covers the estimator: a
// request the transport tried twice reports the sum of its attempts' input, and
// that sum is no calibration. The ratio and the baseline stay where the last
// unretried report put them. The gate after the retry then estimates the view as
// the run with no report there estimates it.
func TestARetriedReportDoesNotFeedTheTokenCalibration(t *testing.T) {
	reference := runCalibrationScript(t, calibrationScript(false))
	retried := runCalibrationScript(t, calibrationScript(true))

	// The gate before the fourth request is the first that sees the third
	// request's report. When retry is set, that report is the retried one.
	const after = 3
	if len(reference.estimated) <= after || len(retried.estimated) <= after {
		t.Fatalf("the runs were given %d and %d gates, want the gate after the retried report at %d",
			len(reference.estimated), len(retried.estimated), after)
	}
	// Both runs see the same reports before the retried one, so it is the only
	// difference the gate below can carry.
	for i := 0; i < after; i++ {
		if reference.charsPerToken[i] != retried.charsPerToken[i] || reference.estimated[i] != retried.estimated[i] {
			t.Fatalf("the gates before the retried report differ at %d: %v/%d against %v/%d",
				i, reference.charsPerToken[i], reference.estimated[i], retried.charsPerToken[i], retried.estimated[i])
		}
	}
	if got, want := retried.charsPerToken[after], reference.charsPerToken[after]; got != want {
		t.Errorf("the gate after the retried report carries %v characters per token, want the %v the last report set", got, want)
	}
	if got, want := retried.estimated[after], reference.estimated[after]; got != want {
		t.Errorf("the gate after the retried report estimates %d input tokens, want the %d the last report set", got, want)
	}
}

// summarizeAtGate asks for a compaction at one gate and lets the strategy it
// wraps produce it.
type summarizeAtGate struct {
	// inner is the strategy the run compacts with.
	inner poo.Compactor
	// at is the gate, counting from one, the compaction happens at.
	at int
	// gates is how many gates the run has consulted it at.
	gates int
}

// ShouldCompact says yes at the gate the test named.
func (c *summarizeAtGate) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	c.gates++
	return c.gates == c.at
}

// Compact asks the strategy it wraps.
func (c *summarizeAtGate) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return c.inner.Compact(ctx, in)
}

// sideCompactionScript is the answers a summary compaction's side request is
// served: two turns, the summarization's Ask, and two turns after it.
//
// The Ask is retried when retry is set, and its report is then the sum of two
// attempts' input. Without the retry the Ask reports no usage, so the run is the
// calibration a retried report must leave alone.
func sideCompactionScript(retry bool) []wireAnswer {
	out := []wireAnswer{
		{call: "echo", input: 4000, output: 5},
		{call: "echo", input: 4500, output: 5},
	}
	if retry {
		out = append(out,
			wireAnswer{input: 5000, output: 5, fail: true},
			wireAnswer{text: "the summary note", input: 5000, output: 5},
		)
	} else {
		out = append(out, wireAnswer{text: "the summary note", noUsage: true})
	}
	return append(out,
		wireAnswer{call: "echo", input: 6000, output: 5},
		wireAnswer{text: "the answer"},
	)
}

// runSideCompactionScript runs one side-request script and returns what the
// probe recorded.
func runSideCompactionScript(t *testing.T, answers []wireAnswer) (*calibrationProbe, *recordingSink) {
	t.Helper()
	probe := &calibrationProbe{inner: &summarizeAtGate{
		inner: summary.New(summary.Options{KeepRecentTokens: 1}),
		at:    3,
	}}
	sink := &recordingSink{}
	opts := testOptions(t, wireTransport(t, answers), &echoTool{result: strings.Repeat("x", 1000)}, sink)
	opts.Compactor = probe
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	return probe, sink
}

// TestARetriedSideReportDoesNotFeedTheTokenCalibration covers the side request:
// a compaction's Ask goes through the transport too, and a retried one reports
// the sum of its attempts' input. That sum is no calibration, so the ratio and
// the baseline stay where the last unretried report put them, and the record
// says the report was a retried one.
func TestARetriedSideReportDoesNotFeedTheTokenCalibration(t *testing.T) {
	reference, _ := runSideCompactionScript(t, sideCompactionScript(false))
	retried, sink := runSideCompactionScript(t, sideCompactionScript(true))

	// The compaction happens at the third gate, the first that has two turns
	// to summarize, so the fourth gate is the first that sees the Ask's report.
	const after = 3
	if len(reference.estimated) <= after || len(retried.estimated) <= after {
		t.Fatalf("the runs were given %d and %d gates, want the gate after the side request at %d",
			len(reference.estimated), len(retried.estimated), after)
	}
	// Both runs see the same reports before the Ask, so it is the only
	// difference the gates below can carry.
	for i := 0; i <= after; i++ {
		if reference.charsPerToken[i] != retried.charsPerToken[i] || reference.estimated[i] != retried.estimated[i] {
			t.Fatalf("the gates up to the side request differ at %d: %v/%d against %v/%d",
				i, reference.charsPerToken[i], reference.estimated[i], retried.charsPerToken[i], retried.estimated[i])
		}
	}
	for i := after + 1; i < len(retried.estimated); i++ {
		if got, want := retried.charsPerToken[i], reference.charsPerToken[i]; got != want {
			t.Errorf("the gate after the retried side request carries %v characters per token, want the %v the last report set", got, want)
		}
		if got, want := retried.estimated[i], reference.estimated[i]; got != want {
			t.Errorf("the gate after the retried side request estimates %d input tokens, want the %d the last report set", got, want)
		}
	}

	// The side request's own record says the report it carries is a retried
	// one, so a reader of the log knows the sum is not one request's cost.
	var sides []*poo.UsageRecord
	for _, rec := range sink.allRecords() {
		if usage, ok := rec.(*poo.UsageRecord); ok && usage.Origin == poo.OriginCompaction {
			sides = append(sides, usage)
		}
	}
	if len(sides) != 1 {
		t.Fatalf("the run wrote %d usage records for the side request, want 1", len(sides))
	}
	if !sides[0].Retried {
		t.Error("the side request's record does not say its report was retried")
	}
	if got := sides[0].InputTokens; got != 10000 {
		t.Errorf("the side request's record carries %d input tokens, want the 10000 the two attempts summed", got)
	}
}

// roomCompactor compacts when the next answer no longer fits, and replaces
// every live turn but the newest with one message.
//
// It asks the model nothing, so the scripted transport's turns stay the run's
// own and every turn reports the input the script names.
type roomCompactor struct{}

// ShouldCompact reports that the next answer no longer fits and that there is
// an older turn to replace.
func (roomCompactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	return budget.RoomShort(budget.OutputLimit) && len(liveTurnsOf(view.Turns())) >= 2
}

// Compact replaces every live turn but the newest.
func (roomCompactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	live := liveTurnsOf(in.View.Turns())
	if len(live) < 2 {
		return poo.Compaction{}, errors.New("there is nothing older to compact")
	}
	return poo.Compaction{
		Strategy: "cut",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: live[0].TurnIndex, Last: live[len(live)-2].TurnIndex},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
		},
	}, nil
}
