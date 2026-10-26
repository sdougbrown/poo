package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// The store failure: a refused record settles the run with store_error
// and breaks the session.

// TestARefusedToolResultSettlesStoreError covers the failure table's
// RecordSink row: a refused result record is a store_error candidate,
// and the session is broken.
func TestARefusedToolResultSettlesStoreError(t *testing.T) {
	sink := &failingSink{failRecord: "tool.result"}
	tool := &echoTool{result: "the tool said something"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}
	if _, err := s.Prompt(context.Background(), "again"); !errors.Is(err, poo.ErrSessionBroken) {
		t.Errorf("the broken session returned %v, want a broken session", err)
	}
}

// TestARefusedToolResultMessageSettlesStoreError covers the same row
// for the message that carries the result: the record the model reads
// is a record like any other, and a refused one is terminal.
func TestARefusedToolResultMessageSettlesStoreError(t *testing.T) {
	// The records of a run that calls a tool are: the system message,
	// the prompt, the reply and the result's message. The fourth one
	// is refused.
	sink := &failingSink{failRecord: "message", failAfter: 3}
	tool := &echoTool{result: "the tool said something"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}
}

// TestARefusedContextNoticeSettlesStoreError covers the same row for
// the notice the model gets when the context fills up.
func TestARefusedContextNoticeSettlesStoreError(t *testing.T) {
	// The notice is written when the estimate reaches the notice
	// ratio, so the window is small enough for the first request to
	// reach it, and the prompt is long enough to fill it.
	sink := &failingSink{failRecord: "message", failAfter: 2}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SystemPrompt = strings.Repeat("x", 10000)
	opts.Model = poo.ModelInfo{ModelID: "test-model", ContextLimit: 3400}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}
}

// interruptingTransport streams a reply and then waits for the
// interrupt.
//
// The second request fails: the run has nothing left to say once the
// turn was cut short.
type interruptingTransport struct {
	// started is closed when the first request arrives.
	started chan struct{}
	// once guards the close.
	once sync.Once
	// mu guards calls.
	mu sync.Mutex
	// calls counts the requests.
	calls int
}

// Stream serves the one reply the transport has.
func (t *interruptingTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.mu.Lock()
	t.calls++
	calls := t.calls
	t.mu.Unlock()
	if calls > 1 {
		return nil, poo.Rendered{}, errors.New("the transport has no more replies")
	}
	t.once.Do(func() { close(t.started) })
	return &interruptingReader{ctx: ctx}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented: the transport never forks.
func (t *interruptingTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errors.New("the transport does not fork")
}

// interruptingReader yields a reply and then waits for the context to
// end.
type interruptingReader struct {
	// ctx is the request's context.
	ctx context.Context
	// stage says what the reader hands out next.
	stage int
}

// Next yields the reply and then waits.
func (r *interruptingReader) Next() (poo.StreamItem, error) {
	if r.stage == 0 {
		r.stage++
		return poo.StreamItem{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "half an answer"}}, nil
	}
	<-r.ctx.Done()
	return poo.StreamItem{}, r.ctx.Err()
}

// Close releases the reply.
func (r *interruptingReader) Close() error { return nil }

// TestARefusedPartialMessageSettlesStoreError covers the same row for
// the message an interrupted turn leaves behind.
func TestARefusedPartialMessageSettlesStoreError(t *testing.T) {
	sink := &failingSink{failRecord: "message", failAfter: 2}
	tr := &interruptingTransport{started: make(chan struct{})}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("interrupting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}
}

// TestTheStoreErrorSaysTheStoreRefused covers the settle detail: the
// record says what the store refused.
func TestTheStoreErrorSaysTheStoreRefused(t *testing.T) {
	sink := &failingSink{failRecord: "tool.result"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Fatalf("the run settled %q, want store_error", res.Reason)
	}
	if !strings.Contains(res.Detail.Text, "store") {
		t.Errorf("the settle detail says %q, want the store failure", res.Detail.Text)
	}
	if res.Detail.Source != "store" {
		t.Errorf("the settle detail's source is %q, want store", res.Detail.Source)
	}
}
