package poo_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/tool/bash"
)

// askCapture answers every ask and keeps what it was asked about.
type askCapture struct {
	mu   sync.Mutex
	asks []perm.Ask
}

func (*askCapture) Available() bool       { return true }
func (*askCapture) Budget() time.Duration { return time.Minute }

func (a *askCapture) Ask(_ context.Context, ask perm.Ask) (perm.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asks = append(a.asks, ask)
	return perm.DecisionAllow, nil
}

func (a *askCapture) captured() []perm.Ask {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]perm.Ask(nil), a.asks...)
}

// turnCapture keeps the turn summaries the view held at each turn end.
type turnCapture struct {
	mu    sync.Mutex
	turns [][]poo.TurnSummary
}

func (c *turnCapture) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns = append(c.turns, ctx.HistoryView.Turns())
	return poo.TurnDecision{}, nil
}

func (c *turnCapture) captured() [][]poo.TurnSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]poo.TurnSummary(nil), c.turns...)
}

// TestABashDescriptionIsTheEventAndTranscriptSummaryNotTheAsk covers the
// model's own words about a command it has not run: the event stream and the
// transcript may show them, and a permission ask must not. The description
// here downplays what the command does, which is exactly the misleading case
// a permission decision must not rest on.
func TestABashDescriptionIsTheEventAndTranscriptSummaryNotTheAsk(t *testing.T) {
	const command = `printf 'wrote' > victim.txt`
	const description = "list the working directory"

	asker := &askCapture{}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	turns := &turnCapture{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "bash", Arguments: []byte(`{"command":` + jsonQuote(command) + `,"description":` + jsonQuote(description) + `}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{"bash": bash.New()},
		ToolOrder:  []string{"bash"},
		Sink:       sink,
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
		AfterTurn:  []poo.AfterTurnHook{turns},
	}
	s := newTestSession(t, opts, poo.WithPolicy(policy))
	if res := runSession(t, s, "start"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}

	// The permission ask keeps the command, for both a human and a decision
	// model: the summary and the evidence never carry the description.
	asks := asker.captured()
	if len(asks) != 1 {
		t.Fatalf("the policy asked %d time(s), want 1", len(asks))
	}
	if asks[0].Summary != command {
		t.Errorf("the permission ask showed %q, want the command", asks[0].Summary)
	}
	var evidence map[string]string
	if err := json.Unmarshal(asks[0].Arguments, &evidence); err != nil {
		t.Fatalf("the ask's evidence is not an object: %v", err)
	}
	if len(evidence) != 1 || evidence["command"] != command {
		t.Errorf("the ask's evidence is %s, want the command alone", asks[0].Arguments)
	}
	// The ask the session records says the same thing a client shows.
	var logged *poo.PermissionAskedRecord
	for _, rec := range sink.Records() {
		if r, ok := rec.(*poo.PermissionAskedRecord); ok {
			logged = r
		}
	}
	if logged == nil {
		t.Fatal("the log holds no permission.asked record")
	}
	if logged.Summary != command {
		t.Errorf("the logged ask shows %q, want the command", logged.Summary)
	}

	// The event stream carries the description as the call's summary.
	var event poo.Event
	for _, ev := range s.Events() {
		if ev.Name == poo.EventToolCall {
			event = ev
			break
		}
	}
	if event.Name != poo.EventToolCall {
		t.Fatal("the run emitted no tool.call event")
	}
	if got, _ := event.Payload["summary"].(string); got != description {
		t.Errorf("the event's summary is %q, want the description", got)
	}

	// The transcript is the session's log and its history projection: both
	// show the description as the call's summary.
	call := firstCallRecord(t, sink.Records())
	if call.Summary != description {
		t.Errorf("the call record's summary is %q, want the description", call.Summary)
	}
	page := poo.ProjectHistory(sink.Records(), 0, 0)
	var item poo.HistoryItem
	for _, it := range page.Items {
		if it.Type == poo.HistoryToolCall {
			item = it
			break
		}
	}
	if item.Type != poo.HistoryToolCall {
		t.Fatal("the history projection holds no tool call")
	}
	if item.Summary != description {
		t.Errorf("the transcript item's summary is %q, want the description", item.Summary)
	}

	// The turn summary a hook reads has the same summary.
	found := false
	for _, snapshot := range turns.captured() {
		for _, turn := range snapshot {
			for _, call := range turn.Calls {
				if call.Tool != "bash" {
					continue
				}
				found = true
				if call.Summary != description {
					t.Errorf("the turn summary is %q, want the description", call.Summary)
				}
			}
		}
	}
	if !found {
		t.Error("the turn summary holds no bash call")
	}
}

// jsonQuote renders a Go string as a JSON string.
func jsonQuote(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(quoted)
}

// firstCallRecord returns the first tool.call record the sink kept.
func firstCallRecord(t *testing.T, records []poo.Record) *poo.ToolCallRecord {
	t.Helper()
	for _, rec := range records {
		if call, ok := rec.(*poo.ToolCallRecord); ok {
			return call
		}
	}
	t.Fatal("the log holds no tool.call record")
	return nil
}

// callRecordFor returns the tool.call record with the given handle.
func callRecordFor(t *testing.T, records []poo.Record, handle string) *poo.ToolCallRecord {
	t.Helper()
	for _, rec := range records {
		if call, ok := rec.(*poo.ToolCallRecord); ok && call.Handle == handle {
			return call
		}
	}
	t.Fatalf("the log holds no tool.call record for %s", handle)
	return nil
}

// TestAnInterruptKeepsASkippedCallsSummary covers the batch's skip path: a
// call that never starts still gets a record whose summary is the plan's
// summary, so the record, the history and the live turn summary agree.
func TestAnInterruptKeepsASkippedCallsSummary(t *testing.T) {
	const first = `sleep 30`
	const second = `printf second`
	const description = "the second call's summary"

	turns := &turnCapture{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{CallID: "c1", Name: "bash", Arguments: []byte(`{"command":` + jsonQuote(first) + `}`)},
			{CallID: "c2", Name: "bash", Arguments: []byte(`{"command":` + jsonQuote(second) + `,"description":` + jsonQuote(description) + `}`)},
		}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{"bash": bash.New()},
		ToolOrder:  []string{"bash"},
		Sink:       sink,
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
		AfterTurn:  []poo.AfterTurnHook{turns},
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The first call holds the batch open, so the interrupt arrives while
	// the second call has not started.
	waitFor(t, "the first call to start", func() bool {
		for _, ev := range s.Events() {
			if ev.Name == poo.EventToolCall && ev.Payload["handle"] == "r1" {
				return true
			}
		}
		return false
	})
	if _, err := s.Send("stop", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	call := callRecordFor(t, sink.Records(), "r2")
	if call.Summary != description {
		t.Errorf("the skipped call's record summary is %q, want the description", call.Summary)
	}
	firstCall := callRecordFor(t, sink.Records(), "r1")
	if firstCall.Summary != first {
		t.Errorf("the first call's record summary is %q, want the command", firstCall.Summary)
	}
	// The second call really was skipped, so its record came from the
	// skip path rather than its own dispatch.
	skipped := false
	for _, rec := range sink.Records() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Handle == "r2" && r.Outcome == poo.OutcomeSkipped {
			skipped = true
		}
	}
	if !skipped {
		t.Fatal("the second call has no skipped result")
	}
	inHistory := false
	for _, item := range poo.ProjectHistory(sink.Records(), 0, 0).Items {
		if item.Type != poo.HistoryToolCall || item.Handle != "r2" {
			continue
		}
		inHistory = true
		if item.Summary != description {
			t.Errorf("the skipped call's history summary is %q, want the description", item.Summary)
		}
	}
	if !inHistory {
		t.Fatal("the history holds no skipped call")
	}
	inTurn := false
	for _, snapshot := range turns.captured() {
		for _, turn := range snapshot {
			for _, call := range turn.Calls {
				if call.Handle != "r2" {
					continue
				}
				inTurn = true
				if call.Summary != description {
					t.Errorf("the live turn summary is %q, want the description", call.Summary)
				}
			}
		}
	}
	if !inTurn {
		t.Fatal("the live turn summary holds no skipped call")
	}
}

// TestAToolCallEventOmitsAnAbsentSummary covers an event whose call has no
// summary: the key is absent, as the record's optional field is.
func TestAToolCallEventOmitsAnAbsentSummary(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "missing", Arguments: []byte(`{}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	runSession(t, s, "start")

	found := false
	for _, ev := range s.Events() {
		if ev.Name != poo.EventToolCall {
			continue
		}
		found = true
		if summary, ok := ev.Payload["summary"]; ok {
			t.Errorf("the event carries the summary %v, want the key absent", summary)
		}
	}
	if !found {
		t.Fatal("the run emitted no tool.call event")
	}
	call := firstCallRecord(t, sink.Records())
	if call.Summary != "" {
		t.Errorf("the record's summary is %q, want it absent", call.Summary)
	}
}
