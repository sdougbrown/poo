package handoff_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/handoff"
	"github.com/sdougbrown/poo/pootest"
)

// The handoff strategy: the model writes a note for whoever continues
// the task, and the run ends with it.

// echoTool is a tool that answers with a fixed text.
//
// The strategy needs a turn that did something, so the gate is reached
// again after the first request.
type echoTool struct{}

// Spec describes the tool.
func (t *echoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (t *echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{}, nil
}

// Call answers.
func (t *echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "echoed"}, nil
}

// echoPrepared is a prepared echo call.
type echoPrepared struct{}

// Summary describes the call.
func (p echoPrepared) Summary() string { return "the call" }

// session builds a session that runs the handoff strategy.
func session(t *testing.T, tr *pootest.Transport, opts handoff.Options) (*poo.Session, *pootest.Sink) {
	t.Helper()
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 4096},
		SystemPrompt: "the system prompt",
		Compactor:    handoff.New(opts),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, sink
}

// script returns a transport that makes two calls and then answers.
//
// The calls make the run reach the gate again, so the strategy is asked
// at every gate.
func script() *pootest.Transport {
	return pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "the answer"},
	)
}

// longScript returns a transport whose first answer fills the window.
//
// The answer is long enough that the view is over the hard ratio once
// the turn is done.
func longScript() *pootest.Transport {
	return pootest.NewTransport(
		pootest.Turn{
			Text:  strings.Repeat("x", 4000),
			Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}},
		},
		pootest.Turn{Text: "the answer"},
	)
}

// TestTheForkSendsTheMainPrefixPlusTheInstruction covers the plan's
// rule: the fork is byte-identical to the last main request, apart
// from the instruction it appends.
func TestTheForkSendsTheMainPrefixPlusTheInstruction(t *testing.T) {
	tr := script()
	tr.SetForkTurns(pootest.Turn{Text: "the handoff note"})
	s, _ := session(t, tr, handoff.Options{Ratio: 0.001})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}

	forks := tr.Forks()
	if len(forks) != 1 {
		t.Fatalf("the transport got %d forks, want 1", len(forks))
	}
	// The fork's prefix is the last main request, byte for byte.
	rendered := tr.Rendered()
	if len(rendered) == 0 {
		t.Fatal("the transport holds no rendered request")
	}
	last := rendered[len(rendered)-1]
	if string(forks[0].Prev.Body) != string(last.Body) {
		t.Errorf("the fork's prefix is %q, want the main request's %q", forks[0].Prev.Body, last.Body)
	}
	// The instruction is what the fork appended.
	if len(forks[0].Appended) != 1 {
		t.Fatalf("the fork appended %d messages, want 1", len(forks[0].Appended))
	}
	if forks[0].Appended[0].Text() != handoff.Instruction {
		t.Errorf("the fork appended %q, want the instruction", forks[0].Appended[0].Text())
	}
	if forks[0].MaxTokens != handoff.DefaultMaxTokens {
		t.Errorf("the fork asked for %d tokens, want %d", forks[0].MaxTokens, handoff.DefaultMaxTokens)
	}
}

// TestTheReplyBecomesTheHandoff covers the plan's rule: the reply's
// text is the handoff, and a tool call in the reply is ignored.
func TestTheReplyBecomesTheHandoff(t *testing.T) {
	tr := script()
	tr.SetForkTurns(pootest.Turn{
		Text:  "the handoff note",
		Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}},
	})
	s, sink := session(t, tr, handoff.Options{Ratio: 0.001})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Detail.Handoff != "the handoff note" {
		t.Errorf("the settle detail's handoff is %q, want the note", res.Detail.Handoff)
	}
	if res.Detail.Source != "compactor:handoff" {
		t.Errorf("the settle detail's source is %q, want the compactor's", res.Detail.Source)
	}
	// The handoff is recorded.
	found := false
	for _, rec := range sink.Records() {
		if h, ok := rec.(*poo.HandoffRecord); ok {
			found = true
			if h.Handoff != "the handoff note" {
				t.Errorf("the handoff record holds %q, want the note", h.Handoff)
			}
			if h.Strategy != "handoff" {
				t.Errorf("the handoff record's strategy is %q, want handoff", h.Strategy)
			}
		}
	}
	if !found {
		t.Error("the log holds no handoff record")
	}
}

// TestAnEmptyReplyContinuesTheRun covers the plan's rule: an empty
// reply is a compactor failure, and below the hard limit the run
// continues and the strategy retries at the next gate.
func TestAnEmptyReplyContinuesTheRun(t *testing.T) {
	tr := script()
	tr.SetForkTurns(pootest.Turn{Text: ""})
	s, _ := session(t, tr, handoff.Options{Ratio: 0.001})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: the run continues below the hard limit", res.Reason)
	}
	if got := len(tr.Forks()); got < 2 {
		t.Errorf("the transport got %d forks, want the strategy to retry at the next gate", got)
	}
}

// TestAnEmptyReplyAtTheHardLimitSettles covers the plan's rule: at or
// above the hard limit a compactor that failed settles the run as
// context_exhausted, and there is no handoff.
func TestAnEmptyReplyAtTheHardLimitSettles(t *testing.T) {
	tr := longScript()
	tr.SetForkTurns(pootest.Turn{Text: ""})
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 2000},
		HardRatio:    0.5,
		SystemPrompt: "the system prompt",
		Compactor:    handoff.New(handoff.Options{Ratio: 0.001}),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "compaction_failed" {
		t.Errorf("the settle detail's source is %q, want compaction_failed", res.Detail.Source)
	}
	if res.Detail.Handoff != "" {
		t.Errorf("the settle detail holds the handoff %q, want none", res.Detail.Handoff)
	}
}

// TestTheForkIsNotATurnAndItsUsageCounts covers the plan's rule: the
// fork's usage counts toward the run's token ceiling, and the fork is
// not a turn.
func TestTheForkIsNotATurnAndItsUsageCounts(t *testing.T) {
	tr := script()
	tr.SetForkTurns(pootest.Turn{Text: "the handoff note", InputTokens: 100, OutputTokens: 100})
	s, _ := session(t, tr, handoff.Options{Ratio: 0.001})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The fork is not a turn: the run made one request.
	if res.Turns != 1 {
		t.Errorf("the run made %d turns, want 1: the fork is not a turn", res.Turns)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
}

// TestTheStrategyWaitsForARequest covers the plan's rule: the strategy
// acts only after the run has asked the model something.
func TestTheStrategyWaitsForARequest(t *testing.T) {
	tr := script()
	tr.SetForkTurns(pootest.Turn{Text: "the handoff note"})
	s, _ := session(t, tr, handoff.Options{Ratio: 0.001})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The first gate holds no turn, so the strategy did not act there.
	// The second gate holds one, and the strategy acts.
	if got := len(tr.Forks()); got != 1 {
		t.Errorf("the transport got %d forks, want 1", got)
	}
}

// TestTheForkFailureSettlesAtTheHardLimit covers the plan's rule: a
// fork that failed is a compactor failure.
func TestTheForkFailureSettlesAtTheHardLimit(t *testing.T) {
	tr := longScript()
	// The transport holds no forked response, so the fork fails.
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 2000},
		HardRatio:    0.5,
		SystemPrompt: "the system prompt",
		Compactor:    handoff.New(handoff.Options{Ratio: 0.001}),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if !strings.Contains(res.Detail.Text, "script") {
		t.Errorf("the settle detail's text is %q, want the failure", res.Detail.Text)
	}
}

// forkCompactor is a compactor that forks and then carries on.
//
// It stands in for a strategy that asks the model something and keeps
// the run going, so the fork's usage is counted against the run.
type forkCompactor struct {
	// forks counts the forks the compactor asked for.
	forks int
}

// ShouldCompact says yes once the run has asked the model something.
func (c *forkCompactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	return budget.Turns >= 1
}

// Compact forks the last request and replaces the turn.
func (c *forkCompactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	msg := poo.NewTextMessage(poo.RoleUser, "the instruction")
	if _, _, err := fork(ctx, []poo.Message{msg}, 128); err != nil {
		return poo.Compaction{}, err
	}
	c.forks++
	return poo.Compaction{
		Strategy: "forking",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: 1, Last: 1},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
		},
	}, nil
}

// TestTheForkUsageCountsTowardTheRunBudget covers the plan's rule: the
// fork's usage counts toward MaxRunTokens, and the fork is not a turn.
func TestTheForkUsageCountsTowardTheRunBudget(t *testing.T) {
	tr := script()
	tr.SetForkTurns(pootest.Turn{Text: "the fork's answer", InputTokens: 60, OutputTokens: 60})
	compactor := &forkCompactor{}
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport: tr,
		Tools:     map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder: []string{"echo"},
		Sink:      sink,
		SessionID: "s1",
		Model:     poo.ModelInfo{ModelID: "test-model", ContextLimit: 4096},
		// The fork alone spends the ceiling.
		MaxRunTokens: 100,
		SystemPrompt: "the system prompt",
		Compactor:    compactor,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if compactor.forks != 1 {
		t.Errorf("the compactor forked %d time(s), want 1", compactor.forks)
	}
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Fatalf("the run settled %q, want budget_exhausted: the fork's usage counts", res.Reason)
	}
	if res.Detail.Source != "max_run_tokens" {
		t.Errorf("the settle detail's source is %q, want max_run_tokens", res.Detail.Source)
	}
	// The fork is not a turn: the run made two requests.
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2: the fork is not a turn", res.Turns)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the transport got %d requests, want 2", got)
	}
}
