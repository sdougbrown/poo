package poo_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// outputLimitRejection is the 400 a hosted API sends when max_tokens is
// above its cap.
func outputLimitRejection(limit int) error {
	return &poo.ProviderError{
		Kind:        poo.ProviderKindOutputLimit,
		Message:     "max_tokens is too large",
		Status:      400,
		OutputLimit: limit,
	}
}

func outputLimitEvents(s *poo.Session) []poo.Event {
	var out []poo.Event
	for _, ev := range s.Events() {
		if ev.Name == poo.EventModelOutputLimit {
			out = append(out, ev)
		}
	}
	return out
}

func requestedTokens(tr interface{ Requests() []poo.ChatRequest }) []int {
	var out []int
	for _, r := range tr.Requests() {
		out = append(out, r.MaxTokens)
	}
	return out
}

// TestAnEndpointCapLearnedFromARejectionServesTheRetryAndTheNextTurn covers
// a model that declares no max_output: the request that the endpoint
// rejects for its max_tokens is sent once more under the cap the endpoint
// named, the session says what it learned, and the later requests keep the
// cap.
func TestAnEndpointCapLearnedFromARejectionServesTheRetryAndTheNextTurn(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{text: "first"},
		{text: "second"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.Name = "big"
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "one"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if res := runSession(t, s, "two"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the second run settled %q, want end_turn", res.Reason)
	}

	want := []int{32768, 16384, 16384}
	if got := requestedTokens(tr); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the requests carried max_tokens %v, want %v", got, want)
	}
	events := outputLimitEvents(s)
	if len(events) != 1 {
		t.Fatalf("the session announced %d output limits, want 1", len(events))
	}
	payload := events[0].Payload
	if payload["limit"] != 16384 {
		t.Errorf("the event names the limit %v, want 16384", payload["limit"])
	}
	if setting, _ := payload["setting"].(string); setting != "models.big.max_output" {
		t.Errorf("the event names the setting %q, want models.big.max_output", setting)
	}
	if text, _ := payload["message"].(string); !strings.Contains(text, "16384") || !strings.Contains(text, "models.big.max_output") {
		t.Errorf("the event's message %q names neither the cap nor the setting", text)
	}
}

// TestARejectionThatStatesNoCapRetriesUnderTheOlderDefault covers a
// rejection whose message names no number.
func TestARejectionThatStatesNoCapRetriesUnderTheOlderDefault(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(0)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "one"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	want := []int{32768, 8192}
	if got := requestedTokens(tr); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the requests carried max_tokens %v, want %v", got, want)
	}
}

// TestADeclaredMaxOutputIsNotRetried covers the user's word: a model
// that declares its limit is never second-guessed, and the run settles as
// any rejected request does.
func TestADeclaredMaxOutputIsNotRetried(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(1024)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	opts.Model.MaxOutput = 4096
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
	if got := len(outputLimitEvents(s)); got != 0 {
		t.Errorf("the session announced %d output limits, want 0", got)
	}
}

// TestASecondRejectionSettlesTheRun covers the bound: the request is
// retried once, and a cap that is still refused ends the run.
func TestASecondRejectionSettlesTheRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{err: outputLimitRejection(8192)},
		{text: "never"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the transport got %d requests, want 2", got)
	}
}

// TestACapNoLowerThanTheRequestIsNotRetried covers a rejection a retry
// cannot help: the request already asked for no more than the cap that
// the rejection names or that poo falls back to.
func TestACapNoLowerThanTheRequestIsNotRetried(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(0)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 32768
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
}
