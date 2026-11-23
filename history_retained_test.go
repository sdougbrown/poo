package poo_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The retained history: the read side of the projection, which serves
// the turns the store holds even after a compaction replaced them.

// readSink is a test sink that also reads its records back, so the
// session hands the tools the history the records hold.
type readSink struct {
	*pootest.Sink
}

// Records returns the sink's records.
func (r readSink) ReadRecords(context.Context) ([]poo.Record, error) {
	return r.Sink.Records(), nil
}

// msg is a recorded message in one run's turn.
func msg(runID string, turn int, role poo.Role, text string) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMeta("message"),
		RunID:      runID,
		Turn:       turn,
		Role:       role,
		Content:    []poo.ContentBlock{{Kind: poo.ContentText, Text: text}},
	}
}

// result is a recorded tool result in one run's turn.
func result(runID string, turn int, handle, text string) *poo.ToolResultRecord {
	return &poo.ToolResultRecord{
		RecordMeta: poo.NewRecordMeta("tool_result"),
		RunID:      runID,
		Turn:       turn,
		Handle:     handle,
		Text:       text,
	}
}

// TestTheHistoryServesTheTurnsTheRecordsRetain covers the read side's
// whole job: the turns are the session's own numbers, the text is what
// was said, and a compaction record is a line rather than a cut.
func TestTheHistoryServesTheTurnsTheRecordsRetain(t *testing.T) {
	records := []poo.Record{
		&poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run_started"), RunID: "r1"},
		msg("r1", 1, poo.RoleUser, "do the thing"),
		msg("r1", 1, poo.RoleAssistant, "reading the file"),
		&poo.ToolCallRecord{RecordMeta: poo.NewRecordMeta("tool_call"), RunID: "r1", Turn: 1, Handle: "r1", Tool: "read"},
		result("r1", 1, "r1", "package main\n\nfunc main() {}"),
		&poo.RunSettledRecord{RecordMeta: poo.NewRecordMeta("run_settled"), RunID: "r1", Turns: 1},
		&poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run_started"), RunID: "r2"},
		msg("r2", 1, poo.RoleAssistant, "the answer"),
		&poo.RunSettledRecord{RecordMeta: poo.NewRecordMeta("run_settled"), RunID: "r2", Turns: 1},
	}
	h := poo.NewHistory(records)

	turns, err := h.Turns()
	if err != nil {
		t.Fatalf("reading the turns: %v", err)
	}
	if len(turns) != 2 || turns[0] != 1 || turns[1] != 2 {
		t.Fatalf("the history holds the turns %v, want 1 and 2", turns)
	}

	text, err := h.Turn(1)
	if err != nil {
		t.Fatalf("reading turn 1: %v", err)
	}
	for _, want := range []string{"[user] do the thing", "[assistant] reading the file", "[call r1 read]", "[result r1] package main", "func main() {}"} {
		if !strings.Contains(text, want) {
			t.Errorf("turn 1 is %q, want it to carry %q", text, want)
		}
	}

	// The second run's turns continue the session's numbering.
	text, err = h.Turn(2)
	if err != nil {
		t.Fatalf("reading turn 2: %v", err)
	}
	for _, want := range []string{"[assistant] the answer"} {
		if !strings.Contains(text, want) {
			t.Errorf("turn 2 is %q, want it to carry %q", text, want)
		}
	}

	// A turn the session never had is empty.
	if text, _ := h.Turn(9); text != "" {
		t.Errorf("turn 9 is %q, want empty", text)
	}
}

// TestTheHistorySearchCoversTheRetainedLines covers the search: it
// matches the retained lines with a regular expression, and names the
// turn each match belongs to.
func TestTheHistorySearchCoversTheRetainedLines(t *testing.T) {
	records := []poo.Record{
		msg("r1", 1, poo.RoleUser, "fix the panic in main.go"),
		result("r1", 1, "r1", "the stack trace names resolve()"),
		msg("r1", 2, poo.RoleAssistant, "the panic is fixed"),
	}
	matches, err := poo.NewHistory(records).Search("panic|resolve")
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("the search matched %d lines, want 3", len(matches))
	}
	if matches[0].Turn != 1 || !strings.Contains(matches[0].Text, "main.go") {
		t.Errorf("the first match is %+v, want the turn 1 user line", matches[0])
	}
	if matches[1].Turn != 1 || !strings.Contains(matches[1].Text, "resolve()") {
		t.Errorf("the second match is %+v, want the turn 1 result line", matches[1])
	}
	if matches[2].Turn != 2 || !strings.Contains(matches[2].Text, "panic is fixed") {
		t.Errorf("the third match is %+v, want the turn 2 assistant line", matches[2])
	}
	if _, err := poo.NewHistory(records).Search("("); err == nil {
		t.Error("an invalid pattern was accepted")
	}
}

// TestTheSessionServesItsHistoryToTheTools covers the seam: a session
// whose sink can read its records back hands the tools a history, and
// the history holds what the store retained, compaction included.
func TestTheSessionServesItsHistoryToTheTools(t *testing.T) {
	historyTool := &recordingTool{}
	tr := pootest.NewTransport(
		pootest.Turn{Text: "TURN-ONE " + strings.Repeat("x", 3500), Calls: []pootest.Call{{Name: "probe"}}},
		pootest.Turn{Text: "the final answer"},
	)
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"probe": historyTool},
		ToolOrder:    []string{"probe"},
		Sink:         readSink{Sink: sink},
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 20000},
		SystemPrompt: "the system prompt",
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

	if historyTool.env == nil || historyTool.env.History == nil {
		t.Fatal("the tool's environment holds no history, want the sink's read side")
	}
	turns, err := historyTool.env.History.Turns()
	if err != nil {
		t.Fatalf("reading the turns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("the history holds %d turns, want 2", len(turns))
	}
	text, err := historyTool.env.History.Turn(1)
	if err != nil {
		t.Fatalf("reading the turn: %v", err)
	}
	for _, want := range []string{"TURN-ONE", "[result", "probed"} {
		if !strings.Contains(text, want) {
			t.Errorf("turn 1 is %q, want it to carry %q", text, want)
		}
	}
}

// recordingTool is a tool that keeps the environment it was called
// with, so a test can read the seam the core handed it.
type recordingTool struct {
	env *poo.ToolEnv
}

// Spec describes the tool.
func (t *recordingTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "probe",
		Description: "record the environment",
		Schema:      json.RawMessage(`{"type":"object","additionalProperties":false}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (t *recordingTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return recordingPrepared{}, nil
}

// Call answers and keeps the environment.
func (t *recordingTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	e := env
	t.env = &e
	return poo.ToolResult{Text: "probed"}, nil
}

// recordingPrepared is a prepared probe call.
type recordingPrepared struct{}

// Summary describes the call.
func (p recordingPrepared) Summary() string { return "probe" }

// TestARetainedHistoryKeepsTheCutTurns covers the recovery semantics
// the retention pointer names: a compaction record in the chain removes
// turns from the view, never from the store, so the retained history
// still numbers and holds the turns the compaction cut.
func TestARetainedHistoryKeepsTheCutTurns(t *testing.T) {
	records := []poo.Record{
		header("s1"),
		&poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), RunID: "r1", Turn: 1, Role: poo.RoleUser,
			Content: poo.NewTextMessage(poo.RoleUser, "first").Content},
		&poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), RunID: "r1", Turn: 2, Role: poo.RoleAssistant,
			Content: poo.NewTextMessage(poo.RoleAssistant, "second").Content},
		&poo.CompactionRecord{RecordMeta: poo.NewRecordMeta("compaction"), RunID: "r1",
			Strategy: "mine", Cut: &poo.TurnRange{First: 1, Last: 2}},
		&poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), RunID: "r1", Turn: 3, Role: poo.RoleAssistant,
			Content: poo.NewTextMessage(poo.RoleAssistant, "third").Content},
	}
	h := poo.NewHistory(records)
	turns, err := h.Turns()
	if err != nil {
		t.Fatalf("reading the turns: %v", err)
	}
	if !slices.Equal(turns, []int{1, 2, 3}) {
		t.Errorf("the retained turns are %v, want 1, 2, 3: the compaction cuts the view, never the store", turns)
	}
	text, err := h.Turn(1)
	if err != nil || text == "" {
		t.Errorf("the cut turn 1 is not recoverable: text %q err %v", text, err)
	}
}
