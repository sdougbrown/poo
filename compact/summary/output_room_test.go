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
// compactions it made and how many requests the model got.
func runWithBigTurns(t *testing.T, model poo.ModelInfo, turns int, chars int) (compactions, requests int) {
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
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        model,
		SystemPrompt: "the system prompt",
		Compactor:    summary.New(summary.Options{}),
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
	return compactions, len(tr.Requests())
}

// TestTheRoomForTheOutputLimitTriggersASummaryBeforeTheRatio covers a
// 131072-token window with the 32768-token default: three turns of
// 120000 characters put the input near 79% of the window, under the 85%
// ratio but with less room left than the output limit.
func TestTheRoomForTheOutputLimitTriggersASummaryBeforeTheRatio(t *testing.T) {
	compactions, _ := runWithBigTurns(t, poo.ModelInfo{ContextLimit: 131072}, 3, 120000)
	if compactions != 1 {
		t.Errorf("the run made %d compactions, want 1", compactions)
	}
}

// TestAnOutputLimitThatFitsUnderTheRatioLeavesTheTriggerAlone covers the
// same history for a model that declares a 4096-token output limit: the
// room stays above the limit until the ratio, so nothing compacts.
func TestAnOutputLimitThatFitsUnderTheRatioLeavesTheTriggerAlone(t *testing.T) {
	compactions, _ := runWithBigTurns(t, poo.ModelInfo{ContextLimit: 131072, MaxOutput: 4096}, 3, 120000)
	if compactions != 0 {
		t.Errorf("the run made %d compactions, want 0", compactions)
	}
}

// TestASmallWindowDoesNotSummarizeEveryTurn covers a 16384-token window
// with the 8192-token default: the tail and the summary alone leave less
// room than the output limit, so a summary cannot give the room back and
// the room trigger stays quiet while the run grows through the short zone.
func TestASmallWindowDoesNotSummarizeEveryTurn(t *testing.T) {
	compactions, requests := runWithBigTurns(t, poo.ModelInfo{ContextLimit: 16384}, 7, 5000)
	if compactions != 0 {
		t.Errorf("the run made %d compactions, want 0", compactions)
	}
	if requests != 8 {
		t.Errorf("the model got %d requests, want 8 (seven turns and the answer)", requests)
	}
}
