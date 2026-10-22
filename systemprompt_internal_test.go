package poo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo/prompt"
)

// The prompt a run carries is rendered once, at the start of the run,
// and it never changes while the run goes on. The next run may render
// another one.
//
// The tests reach into the session's builder, because the change
// between the runs is the before-run hooks' business, and the hooks
// have not landed yet.

// promptTransport is a transport that serves scripted responses.
type promptTransport struct {
	mu sync.Mutex
	// requests holds the requests the transport received.
	requests []ChatRequest
	// turns holds the responses, in order.
	turns [][]StreamEvent
	// index is the next response to serve.
	index int
}

// Stream serves the next scripted response.
func (t *promptTransport) Stream(ctx context.Context, req ChatRequest) (StreamReader, Rendered, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = append(t.requests, req)
	if t.index >= len(t.turns) {
		return nil, Rendered{}, errors.New("the script holds no response")
	}
	events := t.turns[t.index]
	t.index++
	return &promptReader{events: events}, Rendered{API: APIChatCompletions, Body: []byte("{}")}, nil
}

// Fork refuses to fork: the tests never compact.
func (t *promptTransport) Fork(ctx context.Context, prev Rendered, appended []Message, maxTokens int) (StreamReader, error) {
	return nil, errors.New("the transport cannot fork")
}

// requests returns the requests the transport received.
func (t *promptTransport) received() []ChatRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]ChatRequest(nil), t.requests...)
}

// promptReader is the read side of one scripted response.
type promptReader struct {
	events []StreamEvent
	index  int
}

// Next returns the next piece of the response.
func (r *promptReader) Next() (StreamEvent, error) {
	if r.index >= len(r.events) {
		return StreamEvent{}, io.EOF
	}
	event := r.events[r.index]
	r.index++
	return event, nil
}

// Close releases the response.
func (r *promptReader) Close() error { return nil }

// textTurn returns a response that ends the turn with a text.
func textTurn(text string) []StreamEvent {
	return []StreamEvent{
		{Kind: "delta", Delta: &Delta{Kind: "text", Text: text}},
		{Kind: "finish", FinishReason: FinishStop},
	}
}

// toolCallTurn returns a response that calls a tool.
func toolCallTurn(name string) []StreamEvent {
	return []StreamEvent{
		{Kind: "tool_call", ToolCall: &ToolCallDelta{
			Index:     0,
			ID:        "call_1",
			Name:      name,
			Arguments: json.RawMessage(`{}`),
			Complete:  true,
		}},
		{Kind: "finish", FinishReason: FinishToolCalls},
	}
}

// systemText returns the system message a request carries.
//
// The history holds the system prompt of every run the session made, so
// the last one is the run's own.
func systemText(req ChatRequest) string {
	text := ""
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			text = msg.Text()
		}
	}
	return text
}

// promptSession builds a session with a scripted transport.
func promptSession(t *testing.T, tr *promptTransport) *Session {
	t.Helper()
	return promptSessionWithTool(t, tr, nil)
}

// promptSessionWithTool builds a session with a scripted transport and
// a tool.
func promptSessionWithTool(t *testing.T, tr *promptTransport, tool Tool) *Session {
	t.Helper()
	opts := Options{
		Transport:  tr,
		SessionID:  "s1",
		Model:      ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
	}
	if tool != nil {
		opts.Tools = map[string]Tool{tool.Spec().Name: tool}
		opts.ToolOrder = []string{tool.Spec().Name}
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// blockingTool is a tool that waits until the test releases it.
type blockingTool struct {
	// started is closed when the tool starts.
	started chan struct{}
	// release carries the signal that lets the tool finish.
	release chan struct{}
	// once guards the close of started.
	once sync.Once
}

// newBlockingTool returns a tool that holds the run until it is
// released.
func newBlockingTool() *blockingTool {
	return &blockingTool{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// Spec describes the tool.
func (t *blockingTool) Spec() ToolSpec {
	return ToolSpec{Name: "block", Description: "Hold the run until the test releases it."}
}

// Prepare decodes the arguments.
func (t *blockingTool) Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error) {
	return blockingPrepared{}, nil
}

// Call holds the run until the test releases it.
func (t *blockingTool) Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error) {
	t.once.Do(func() { close(t.started) })
	select {
	case <-t.release:
		return ToolResult{Text: "the tool's result"}, nil
	case <-ctx.Done():
		return ToolResult{}, ctx.Err()
	}
}

// blockingPrepared is a prepared blocking tool call.
type blockingPrepared struct{}

// Summary describes the call.
func (p blockingPrepared) Summary() string { return "the call" }

// TestThePromptIsRenderedOnceForTheRun covers the rule that the prompt
// is rendered at the start of the run and frozen for it: a change to
// the session's prompt in the middle of the run does not reach the
// run's requests.
func TestThePromptIsRenderedOnceForTheRun(t *testing.T) {
	tool := newBlockingTool()
	tr := &promptTransport{turns: [][]StreamEvent{
		toolCallTurn(tool.Spec().Name),
		textTurn("the answer"),
	}}
	s := promptSessionWithTool(t, tr, tool)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The tool holds the run, so the prompt may change while the run
	// goes on.
	<-tool.started
	s.applyPromptEdit(func(sections prompt.Sections) {
		sections.Set("base", "the other base")
	})
	tool.release <- struct{}{}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	reqs := tr.received()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	first := systemText(reqs[0])
	if first == "" {
		t.Fatal("the first request carries no system prompt")
	}
	if second := systemText(reqs[1]); second != first {
		t.Errorf("the second request carries the prompt %q, want the run's prompt %q", second, first)
	}
	if strings.Contains(first, "the other base") {
		t.Errorf("the run's prompt is %q, want the prompt it rendered", first)
	}
}

// TestThePromptChangesBetweenRuns covers the rule that the prompt may
// change between the runs: the next run renders the session's prompt
// again, and it is the changed one.
func TestThePromptChangesBetweenRuns(t *testing.T) {
	tr := &promptTransport{turns: [][]StreamEvent{textTurn("the answer"), textTurn("the second answer")}}
	s := promptSession(t, tr)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	first := systemText(tr.received()[0])

	// The session's prompt changes between the runs.
	s.applyPromptEdit(func(sections prompt.Sections) {
		sections.Set("base", "the other base")
	})

	if _, err := s.Prompt(context.Background(), "do the other thing"); err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the second run: %v", err)
	}

	reqs := tr.received()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	second := systemText(reqs[1])
	if second == first {
		t.Errorf("the second run carries the first run's prompt: %q", second)
	}
	if !strings.Contains(second, "the other base") {
		t.Errorf("the second run's prompt is %q, want the changed base", second)
	}
}
