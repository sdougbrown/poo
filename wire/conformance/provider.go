package conformance

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// provider is the fake OpenAI-compatible endpoint.
//
// The endpoint answers the requests the binary makes with the turns it
// was given, in order, and it records the requests, so a flow can read
// what the model was asked.
//
// The endpoint serves the chat completions path and the systemone
// path: a binary with a decider model asks the second one.
type provider struct {
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

// maxRepeats is how many times the provider serves its last turn
// before it answers plainly.
//
// A flow that scripts a turn the model keeps asking for would
// otherwise run forever: the provider repeats the turn, and the model
// asks for it again.
const maxRepeats = 3

// newProvider starts an endpoint that serves the turns.
func newProvider(t interface{ Cleanup(func()) }, turns []ScriptedAnswer) *provider {
	p := &provider{turns: turns}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	return p
}

// serve answers one request.
func (p *provider) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)

	if strings.HasSuffix(r.URL.Path, "/systemone") {
		p.serveSystemone(w)
		return
	}

	p.mu.Lock()
	p.requests = append(p.requests, decoded)
	turn := p.next()
	p.mu.Unlock()
	turn.serve(w)
}

// next returns the turn the next request gets.
//
// The last turn is served again when the requests run out, so a flow
// that does not script every request still gets an answer.
func (p *provider) next() ScriptedAnswer {
	if len(p.turns) == 0 {
		return ScriptedAnswer{Text: "the answer"}
	}
	if p.index < len(p.turns) {
		turn := p.turns[p.index]
		p.index++
		return turn
	}
	p.repeats++
	if p.repeats > maxRepeats {
		return ScriptedAnswer{Text: "the answer"}
	}
	return p.turns[len(p.turns)-1]
}

// serveSystemone answers a systemone request.
//
// The decider's answer is a plain refusal: the suite does not test the
// decision model, and a binary that asks one gets an answer it can
// read.
func (p *provider) serveSystemone(w http.ResponseWriter) {
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

// requests returns the bodies the endpoint received.
func (p *provider) requestBodies() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]map[string]any, 0, len(p.requests))
	out = append(out, p.requests...)
	return out
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
