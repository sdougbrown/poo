package summary_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/summary"
	"github.com/sdougbrown/poo/pootest"
)

// The room trigger: a request's max_tokens is clamped to the room the
// window leaves, so the strategy summarizes once the room drops under the
// output limit, and only when a summary would give the room back.

// bigTurn is a tool-calling turn whose text is the given number of
// characters long.
func bigTurn(chars int) pootest.Turn {
	return pootest.Turn{
		Text:  strings.Repeat("x", chars),
		Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"t"}`)}},
	}
}

// runWithBigTurns runs a session whose model has the given window and
// output limit through turns of the given size, and returns how many
// compactions it made, how many requests the model got, and whether the
// run ever saw the output room short.
func runWithBigTurns(t *testing.T, model poo.ModelInfo, turns int, chars int) (compactions, requests int, short bool) {
	t.Helper()
	var script []pootest.Turn
	for i := 0; i < turns; i++ {
		script = append(script, bigTurn(chars))
	}
	summaryTurn := pootest.Turn{Text: "## Goal\nfinish the task"}
	final := pootest.Turn{Text: "the final answer"}
	// Every compaction the strategy makes consumes a scripted summary, and
	// the repeated summaries keep a runaway visible rather than exhausted.
	tr := pootest.NewTransport(append(append(script, summaryTurn), final)...)

	sink := pootest.NewSink()
	model.ModelID = "test-model"
	probe := &roomProbe{inner: summary.New(summary.Options{})}
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        model,
		SystemPrompt: "the system prompt",
		Compactor:    probe,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	for _, rec := range sink.Records() {
		if _, ok := rec.(*poo.CompactionRecord); ok {
			compactions++
		}
	}
	return compactions, len(tr.Requests()), probe.short
}

// roomProbe records whether the strategy ever saw the output room short,
// and otherwise acts for the strategy it wraps.
type roomProbe struct {
	inner poo.Compactor
	short bool
}

func (p *roomProbe) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	if budget.OutputRoomShort() {
		p.short = true
	}
	return p.inner.ShouldCompact(view, budget)
}

func (p *roomProbe) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return p.inner.Compact(ctx, in)
}

// TestTheRoomForTheOutputLimitTriggersASummaryBeforeTheRatio covers a
// 131072-token window with the 32768-token default: three turns of
// 120000 characters put the input near 79% of the window, under the 85%
// ratio but with less room left than the output limit.
func TestTheRoomForTheOutputLimitTriggersASummaryBeforeTheRatio(t *testing.T) {
	compactions, _, _ := runWithBigTurns(t, poo.ModelInfo{ContextLimit: 131072}, 3, 120000)
	if compactions != 1 {
		t.Errorf("the run made %d compactions, want 1", compactions)
	}
}

// TestAnOutputLimitThatFitsUnderTheRatioLeavesTheTriggerAlone covers the
// same history for a model that declares a 4096-token output limit: the
// room stays above the limit until the ratio, so nothing compacts.
func TestAnOutputLimitThatFitsUnderTheRatioLeavesTheTriggerAlone(t *testing.T) {
	compactions, _, _ := runWithBigTurns(t, poo.ModelInfo{ContextLimit: 131072, MaxOutput: 4096}, 3, 120000)
	if compactions != 0 {
		t.Errorf("the run made %d compactions, want 0", compactions)
	}
}

// TestASmallWindowDoesNotSummarizeEveryTurn covers a 16384-token window
// with the 8192-token default: the tail and the summary alone leave less
// room than the output limit, so a summary cannot give the room back and
// the room trigger stays quiet while the run grows through the short zone.
func TestASmallWindowDoesNotSummarizeEveryTurn(t *testing.T) {
	compactions, requests, short := runWithBigTurns(t, poo.ModelInfo{ContextLimit: 16384}, 7, 5000)
	if !short {
		t.Error("the run never entered the short zone, so the test proves nothing")
	}
	if compactions != 0 {
		t.Errorf("the run made %d compactions, want 0", compactions)
	}
	if requests != 8 {
		t.Errorf("the model got %d requests, want 8 (seven turns and the answer)", requests)
	}
}

// TestADeclaredLimitAboveTheWindowDefaultUsesTheDefaultForTheRoom covers
// the limit the room trigger reads: a 200000-token window whose model
// declares 64000 output tokens uses the 32768-token default instead, so a
// 70%-full window is not short and a window with under 32768 tokens of room
// left is. The trigger fires before the 85% ratio, so only the room can be
// what compacts.
func TestADeclaredLimitAboveTheWindowDefaultUsesTheDefaultForTheRoom(t *testing.T) {
	const window = 200000
	limit := min(64000, poo.DefaultMaxOutputFor(window))
	if limit != 32768 {
		t.Fatalf("the room limit is %d, want the 32768-token default", limit)
	}
	view := poo.NewHistoryView([]poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "the system prompt"),
		poo.NewTextMessage(poo.RoleAssistant, strings.Repeat("x", 48000)),
		poo.NewTextMessage(poo.RoleAssistant, "the second answer"),
	}, nil, []poo.TurnSummary{
		{TurnIndex: 1, Start: 1, End: 2},
		{TurnIndex: 2, Start: 2, End: 3},
	})
	c := summary.New(summary.Options{KeepRecentTokens: 1})
	budget := poo.ContextBudget{
		ContextLimit:   window,
		EstimatedInput: window * 70 / 100,
		OutputLimit:    limit,
		Turns:          2,
	}
	if c.ShouldCompact(view, budget) {
		t.Error("the strategy compacted at 70% of the window, before the room was short")
	}
	budget.EstimatedInput = window - poo.OutputReserve - limit + 1
	if !c.ShouldCompact(view, budget) {
		t.Error("the strategy did not compact with under the output limit of room left")
	}
}
