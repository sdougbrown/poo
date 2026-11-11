package poo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// A tool's Prepare sees the environment the run works in: the working
// directory and the protected paths. A tool that resolves a relative path
// against the working directory needs the first, and a tool that refuses
// to read a protected path needs the second.

// recordingPrepareTool records the environment its Prepare received.
type recordingPrepareTool struct {
	echoTool
	// env is the environment the last Prepare received.
	env poo.PrepareEnv
	// prepareCalls counts the Prepare calls.
	prepareCalls int
}

// Prepare records the environment and accepts the call.
func (t *recordingPrepareTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	t.prepareCalls++
	t.env = env
	return echoPrepared{text: string(raw)}, nil
}

// TestPrepareReceivesThePrepareEnv covers the contract: the tool's
// Prepare is given the environment, and the run's working directory
// reaches it.
func TestPrepareReceivesThePrepareEnv(t *testing.T) {
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{CallID: "call_1", Name: "echo", Arguments: json.RawMessage(`{}`)}},
	}})
	sink := &recordingSink{}
	tool := &recordingPrepareTool{}

	opts := testOptions(t, tr, &tool.echoTool, sink)
	opts.Tools["echo"] = tool
	opts.WorkingDir = "/tmp/the-working-dir"
	// The fake transport repeats its last response, so the run needs a
	// turn ceiling: the test is about the first call's environment.
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")
	if tool.prepareCalls != 1 {
		t.Fatalf("Prepare was called %d times, want 1", tool.prepareCalls)
	}
	if tool.env.WorkingDir != "/tmp/the-working-dir" {
		t.Errorf("Prepare's working directory is %q, want %q", tool.env.WorkingDir, "/tmp/the-working-dir")
	}
}

// specNamedTool is a tool whose spec name the test picks.
type specNamedTool struct {
	name string
}

// Spec describes the tool.
func (t *specNamedTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:   t.name,
		Schema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}
}

// Prepare accepts anything.
func (t *specNamedTool) Prepare(json.RawMessage, poo.PrepareEnv) (poo.Prepared, error) {
	return specNamedPrepared{}, nil
}

// Call returns an empty result.
func (t *specNamedTool) Call(context.Context, poo.Prepared, poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{}, nil
}

// specNamedPrepared is an empty prepared call.
type specNamedPrepared struct{}

// Summary describes the call.
func (specNamedPrepared) Summary() string { return "" }

// TestTwoSessionsSendTheSameToolOrder covers the stable tool
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
			tools[name] = &specNamedTool{name: name}
		}
		tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ToolsMode: "native"})
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
