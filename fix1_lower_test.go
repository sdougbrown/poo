package poo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestTheParseErrorLoopIsBounded covers the guard a recoverable
// response note needs: a model that keeps reporting the same failure
// must not keep the run asking forever.
//
// The endpoint never stops writing a call that does not parse. The
// note is still recorded, the model gets another turn, and the run
// stops after the consecutive-note budget instead of growing the
// history without bound.
func TestTheParseErrorLoopIsBounded(t *testing.T) {
	srv, requests := unparseableCallEndpoint(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := requests(); got != 3 {
		t.Errorf("the endpoint got %d requests, want the three-note budget", got)
	}
}

// fix1NamedTool is a tool whose spec name the test picks.
type fix1NamedTool struct {
	name string
}

// Spec describes the tool.
func (t *fix1NamedTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:   t.name,
		Schema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

// Prepare accepts anything.
func (t *fix1NamedTool) Prepare(json.RawMessage, poo.PrepareEnv) (poo.Prepared, error) {
	return fix1NamedPrepared{}, nil
}

// Call returns an empty result.
func (t *fix1NamedTool) Call(context.Context, poo.Prepared, poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{}, nil
}

// fix1NamedPrepared is an empty prepared call.
type fix1NamedPrepared struct{}

// Summary describes the call.
func (fix1NamedPrepared) Summary() string { return "" }

// TestTwoSessionsSendTheSameToolOrder covers the plan's stable tool
// order: two sessions that registered the same tools in different
// orders send the same array.
func TestTwoSessionsSendTheSameToolOrder(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	names := []string{"read", "bash", "grep"}
	for _, order := range [][]string{names, {names[2], names[1], names[0]}} {
		tools := map[string]poo.Tool{}
		for _, name := range order {
			tools[name] = &fix1NamedTool{name: name}
		}
		tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "native"})
		s := newTestSession(t, poo.Options{
			Transport:    tr,
			Tools:        tools,
			ToolOrder:    order,
			Sink:         &recordingSink{},
			SessionID:    "s1",
			Model:        poo.ModelInfo{ModelID: "m", ContextLimit: 32768},
			SystemPrompt: "the system prompt",
			WorkingDir:   t.TempDir(),
		})
		runSession(t, s, "hi")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("the endpoint got %d requests, want 2", len(bodies))
	}
	var first, second struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &first); err != nil {
		t.Fatalf("decoding the first body: %v", err)
	}
	if err := json.Unmarshal([]byte(bodies[1]), &second); err != nil {
		t.Fatalf("decoding the second body: %v", err)
	}
	if string(first.Tools) != string(second.Tools) {
		t.Errorf("the tool arrays differ:\n%s\n%s", first.Tools, second.Tools)
	}
}

// TestTheMessageDeltaNamesTheAttempt covers the streaming event after a
// retry: the delta names the attempt that produced it, not the request.
func TestTheMessageDeltaNamesTheAttempt(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"the answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Millisecond},
	})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))
	runSession(t, s, "hi")

	deltas := 0
	for _, ev := range s.Events() {
		if ev.Name != poo.EventMessageDelta {
			continue
		}
		deltas++
		if ev.Payload["attempt"] != 2 {
			t.Errorf("the delta names the attempt %v, want the second one", ev.Payload["attempt"])
		}
	}
	if deltas == 0 {
		t.Fatal("the run emitted no delta")
	}
}
