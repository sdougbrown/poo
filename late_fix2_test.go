package poo_test

import (
	"context"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// fix2LateAfterTurn blocks until it is released or its activity ends.
type fix2LateAfterTurn struct {
	mu      sync.Mutex
	runs    int
	entered chan struct{}
}

func (h *fix2LateAfterTurn) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	h.mu.Lock()
	h.runs++
	first := h.runs == 1
	h.mu.Unlock()
	if first {
		close(h.entered)
		<-ctx.Done()
		return poo.TurnDecision{Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, "late")}}, nil
	}
	return poo.TurnDecision{}, nil
}

// TestALateAfterTurnDecisionIsLogged covers the rule that a hook
// decision that arrives after its activity ended is logged.
func TestALateAfterTurnDecisionIsLogged(t *testing.T) {
	logger, capture := newCaptureLogger()
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Logger = logger
	h := &fix2LateAfterTurn{entered: make(chan struct{})}
	opts.AfterTurn = []poo.AfterTurnHook{h}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-h.entered
	if _, err := s.Send("stop", poo.Interrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the late hook decision", func() bool {
		return len(capture.find("hook decision after the activity ended")) == 1
	})
	got := capture.find("hook decision after the activity ended")
	if len(got) != 1 {
		t.Fatalf("the logger holds %d late hook decisions, want 1", len(got))
	}
	if got[0].attrs["point"] != "after_turn" {
		t.Errorf("the late decision names point %v, want after_turn", got[0].attrs["point"])
	}
}

// fix2LateResponseTransport serves one response that never finishes.
type fix2LateResponseTransport struct {
	poo.ChatTransport
	started chan struct{}
}

func (tr *fix2LateResponseTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return &fix2PartialReader{ctx: ctx, started: tr.started}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// TestALateProviderResponseIsLoggedOnce covers the plan's rule: a late
// provider response is dropped and logged once, never per delta.
func TestALateProviderResponseIsLoggedOnce(t *testing.T) {
	logger, capture := newCaptureLogger()
	tr := &fix2LateResponseTransport{
		ChatTransport: scripted([]fakeResponse{{text: "done"}}),
		started:       make(chan struct{}),
	}
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Logger = logger
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-tr.started
	s.Cancel("late:response")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the late provider response", func() bool {
		return len(capture.find("provider response after commit point")) == 1
	})
	if got := len(capture.find("provider response after commit point")); got != 1 {
		t.Fatalf("the logger holds %d late provider responses, want 1", got)
	}
}
