package conformance

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"
)

// FakeEndpoint is a scripted OpenAI-compatible endpoint.
//
// A wire client's tests start one, point their binary's settings at it
// with WriteSettings, and read back the requests the binary made with
// RequestBodies. It serves the turns it was given, in order, and it
// serves its last turn again when the turns run out, so a test that
// does not script every request still gets an answer.
//
// The endpoint serves the chat completions path and the systemone
// path: a binary with a decider model asks the second one.
type FakeEndpoint struct {
	// server is the running endpoint.
	server *httptest.Server

	// mu guards the fields below.
	mu sync.Mutex
	// turns are the answers the endpoint serves.
	turns []ScriptedAnswer
	// requests are the bodies the endpoint received, in order.
	requests []map[string]any
	// index is the turn the next request gets.
	index int
	// repeats is how many times the last turn was served again.
	repeats int
}

// maxRepeats is how many times the endpoint serves its last turn
// before it answers plainly.
//
// A test that scripts a turn the model keeps asking for would
// otherwise run forever: the endpoint repeats the turn, and the model
// asks for it again.
const maxRepeats = 3

// NewFakeEndpoint starts an endpoint that serves the turns.
//
// The caller closes the endpoint: a test that skips its cleanup leaves
// the address bound.
func NewFakeEndpoint(turns ...ScriptedAnswer) *FakeEndpoint {
	e := &FakeEndpoint{turns: turns}
	e.server = httptest.NewServer(http.HandlerFunc(e.serve))
	return e
}

// URL returns the base address of the endpoint. The chat completions
// path is URL + "/v1".
func (e *FakeEndpoint) URL() string { return e.server.URL }

// Close shuts the endpoint down.
func (e *FakeEndpoint) Close() { e.server.Close() }

// RequestBodies returns the bodies the endpoint received, in order.
func (e *FakeEndpoint) RequestBodies() []map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]map[string]any, 0, len(e.requests))
	out = append(out, e.requests...)
	return out
}

// WriteSettings writes the settings document that points a poo binary
// at the endpoint to the file at path, and returns the write's error,
// if any. The document names the model on the endpoint and its context
// limit, and the extra settings the caller passes at the top level: a
// key the caller names overrides the document's own.
func (e *FakeEndpoint) WriteSettings(path, model string, contextLimit int, extra map[string]any) error {
	document := map[string]any{
		"version": 1,
		"endpoints": map[string]any{
			"fake": map[string]any{"endpoint": e.server.URL + "/v1"},
		},
		"models": map[string]any{
			model: map[string]any{
				"endpoint":      "fake",
				"model_id":      model,
				"api":           "chat_completions",
				"tools_mode":    "native",
				"ops":           []string{"chat"},
				"context_limit": contextLimit,
			},
		},
		"roles": map[string]any{"chat": model},
	}
	for name, value := range extra {
		document[name] = value
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

// serve answers one request.
func (e *FakeEndpoint) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)

	if strings.HasSuffix(r.URL.Path, "/systemone") {
		e.serveSystemone(w)
		return
	}

	e.mu.Lock()
	e.requests = append(e.requests, decoded)
	turn := e.next()
	e.mu.Unlock()
	turn.serve(w)
}

// next returns the turn the next request gets.
//
// The last turn is served again when the requests run out, so a test
// that does not script every request still gets an answer.
func (e *FakeEndpoint) next() ScriptedAnswer {
	if len(e.turns) == 0 {
		return ScriptedAnswer{Text: "the answer"}
	}
	if e.index < len(e.turns) {
		turn := e.turns[e.index]
		e.index++
		return turn
	}
	e.repeats++
	if e.repeats > maxRepeats {
		return ScriptedAnswer{Text: "the answer"}
	}
	return e.turns[len(e.turns)-1]
}

// serveSystemone answers a systemone request.
//
// The decider's answer is a plain refusal: neither the suite nor a
// client's test exercises the decision model, and a binary that asks
// one gets an answer it can read.
func (e *FakeEndpoint) serveSystemone(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	answer := map[string]any{
		"object":  "systemone.response",
		"id":      "systemone-1",
		"created": 0,
		"answers": map[string]any{},
		"usage":   nil,
	}
	_ = json.NewEncoder(w).Encode(answer)
}

// serve writes the turn's answer.
func (t ScriptedAnswer) serve(w http.ResponseWriter) {
	if t.Delay > 0 {
		time.Sleep(t.Delay)
	}
	if t.Status != 0 && t.Status != http.StatusOK {
		t.serveError(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	write := func(payload string) {
		_, _ = io.WriteString(w, "data: "+payload+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	if len(t.Chunks) > 0 {
		write(`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + quote(t.Chunks[0]) + `}}]}`)
		for _, chunk := range t.Chunks[1:] {
			write(`{"choices":[{"index":0,"delta":{"content":` + quote(chunk) + `}}]}`)
		}
	} else if t.Text != "" || len(t.ToolCalls) == 0 {
		write(`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + quote(t.Text) + `}}]}`)
	}
	for i, call := range t.ToolCalls {
		arguments, err := json.Marshal(call.Arguments)
		if err != nil {
			arguments = []byte("{}")
		}
		payload := fmt.Sprintf(
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":%d,"id":"call_%d","type":"function","function":{"name":%s,"arguments":%s}}]}}]}`,
			i, i+1, quote(call.Name), quote(string(arguments)))
		write(payload)
	}
	finish := "stop"
	if len(t.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	write(`{"choices":[{"index":0,"delta":{},"finish_reason":"` + finish + `"}]}`)
	if t.FailAfterDelta {
		// The stream breaks after the answer: the model has seen part
		// of it, and the failure is not retried.
		write(`{"error":{"message":"the endpoint broke","type":"server_error","code":"internal_error"}}`)
	}
	// The endpoint sends no usage chunk. The core calibrates its
	// estimate with the usage a request reports, and a fake count
	// would move the estimate away from the view the flow sized.
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// serveError writes the turn's failure.
func (t ScriptedAnswer) serveError(w http.ResponseWriter) {
	failure := t.Error
	if failure == nil {
		failure = &ProviderError{Type: "server_error", Code: "internal_error", Message: "the endpoint failed"}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(t.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": failure.Message,
			"type":    failure.Type,
			"code":    failure.Code,
		},
	})
}

// quote returns the text as a JSON string.
func quote(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		return `""`
	}
	return string(encoded)
}
