package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEndpoint is a scripted chat endpoint.
type fakeEndpoint struct {
	*httptest.Server
	// bodies holds the raw SSE bodies the endpoint sends, one per
	// request. The last one repeats.
	bodies []string
	// requests holds the request bodies the endpoint received.
	requests []map[string]any
}

// newFakeEndpoint starts an endpoint that answers with a body.
func newFakeEndpoint(t *testing.T, body string) *fakeEndpoint {
	t.Helper()
	return newFakeEndpointSequence(t, []string{body})
}

// newFakeEndpointSequence starts an endpoint that answers with one body
// per request.
func newFakeEndpointSequence(t *testing.T, bodies []string) *fakeEndpoint {
	t.Helper()
	e := &fakeEndpoint{bodies: bodies}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		e.requests = append(e.requests, decoded)

		i := len(e.requests) - 1
		if i >= len(e.bodies) {
			i = len(e.bodies) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, e.bodies[i])
	}))
	t.Cleanup(e.Close)
	return e
}

// readLog reads the records a log holds.
func readLog(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	defer f.Close()

	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("the log holds a line that is not JSON: %q", line)
		}
		out = append(out, rec)
	}
	return out
}

// names returns the record names, in order.
func names(recs []map[string]any) []string {
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec["record"].(string))
	}
	return out
}

func TestARunWritesItsLogAndSettles(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: " + `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	recs := readLog(t, logPath)
	got := names(recs)
	want := []string{"session.created", "run.started", "message", "message", "message", "usage", "run.settled"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the records are %v, want %v", got, want)
	}

	// The first record names the format.
	if recs[0]["format"] != "poo-session-v0" {
		t.Errorf("the format is %v, want poo-session-v0", recs[0]["format"])
	}
	// The last record settles the run.
	settled := recs[len(recs)-1]
	if settled["reason"] != "end_turn" {
		t.Errorf("the settle reason is %v, want end_turn", settled["reason"])
	}
	if settled["turns"].(float64) != 1 {
		t.Errorf("the run made %v turns, want 1", settled["turns"])
	}
	// The usage record holds the token accounting.
	var usage map[string]any
	for _, rec := range recs {
		if rec["record"] == "usage" {
			usage = rec
		}
	}
	if usage == nil {
		t.Fatal("the log holds no usage record")
	}
	if usage["input_tokens"].(float64) != 10 {
		t.Errorf("the input tokens are %v, want 10", usage["input_tokens"])
	}
	if usage["output_tokens"].(float64) != 3 {
		t.Errorf("the output tokens are %v, want 3", usage["output_tokens"])
	}
	if _, ok := usage["ttft_ms"]; !ok {
		t.Error("the usage record has no ttft_ms")
	}
}

func TestARunThatCallsABashToolRunsIt(t *testing.T) {
	// The model asks for a tool call, then answers.
	toolCall := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo hello-from-bash\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	answer := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"the command printed hello-from-bash"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpointSequence(t, []string{toolCall, answer})

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"run the command",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	recs := readLog(t, logPath)
	var call, result map[string]any
	for _, rec := range recs {
		switch rec["record"] {
		case "tool.call":
			call = rec
		case "tool.result":
			result = rec
		}
	}
	if call == nil {
		t.Fatal("the log holds no tool.call record")
	}
	if result == nil {
		t.Fatal("the log holds no tool.result record")
	}
	if call["tool"] != "bash" {
		t.Errorf("the call is %v, want bash", call["tool"])
	}
	args, _ := call["arguments"].(map[string]any)
	if args["command"] != "echo hello-from-bash" {
		t.Errorf("the arguments are %v, want the full arguments", args["arguments"])
	}
	if result["outcome"] != "ok" {
		t.Errorf("the outcome is %v, want ok", result["outcome"])
	}
	text, _ := result["text"].(string)
	if !strings.Contains(text, "hello-from-bash") {
		t.Errorf("the result is %q, want the command's output", text)
	}

	// The second request carries the tool's output back to the model.
	if len(endpoint.requests) != 2 {
		t.Fatalf("the endpoint got %d requests, want 2", len(endpoint.requests))
	}
	messages, _ := endpoint.requests[1]["messages"].([]any)
	last, _ := messages[len(messages)-1].(map[string]any)
	if last["role"] != "tool" {
		t.Errorf("the last message's role is %v, want tool", last["role"])
	}
}

func TestTheMaxTurnsCeilingSetsTheExitCode(t *testing.T) {
	// Every answer asks for another tool call, so the run only stops at
	// its ceiling.
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"true\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, _ := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--max-turns", "2",
		"--working-dir", dir,
		"--log", logPath,
		"loop forever",
	)
	if code != 11 {
		t.Fatalf("the exit code is %d, want 11", code)
	}

	recs := readLog(t, logPath)
	settled := recs[len(recs)-1]
	if settled["reason"] != "max_turns" {
		t.Errorf("the settle reason is %v, want max_turns", settled["reason"])
	}
	if settled["turns"].(float64) != 2 {
		t.Errorf("the run made %v turns, want 2", settled["turns"])
	}
}

func TestAContextLengthErrorSetsTheExitCode(t *testing.T) {
	endpoint := newFakeEndpoint(t, `{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, _ := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"a long prompt",
	)
	if code != 13 {
		t.Fatalf("the exit code is %d, want 13", code)
	}

	recs := readLog(t, logPath)
	settled := recs[len(recs)-1]
	if settled["reason"] != "context_exhausted" {
		t.Errorf("the settle reason is %v, want context_exhausted", settled["reason"])
	}
	detail, _ := settled["detail"].(map[string]any)
	provider, _ := detail["provider"].(map[string]any)
	if provider["kind"] != "context_length" {
		t.Errorf("the provider kind is %v, want context_length", provider["kind"])
	}
}

func TestTheDefaultLogPathIsUsedWhenNoneIsGiven(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "poo-session-") && strings.HasSuffix(e.Name(), ".jsonl") {
			found = true
		}
	}
	if !found {
		t.Errorf("the working directory holds %d entries, want a session log", len(entries))
	}
}

func TestTheAPIKeyIsScrubbedFromTheChildEnvironment(t *testing.T) {
	// The model asks the shell to print the variable the CLI was told to
	// scrub. The value must not reach the child.
	toolCall := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo key=[$POO_TEST_KEY]\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	answer := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpointSequence(t, []string{toolCall, answer})

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	t.Setenv("POO_TEST_KEY", "super-secret-value")
	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--api-key-env", "POO_TEST_KEY",
		"--working-dir", dir,
		"--log", logPath,
		"print the key",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	recs := readLog(t, logPath)
	for _, rec := range recs {
		if rec["record"] != "tool.result" {
			continue
		}
		text, _ := rec["text"].(string)
		if strings.Contains(text, "super-secret-value") {
			t.Errorf("the scrubbed variable reached the child: %q", text)
		}
		if !strings.Contains(text, "key=[]") {
			t.Errorf("the scrubbed variable is not empty in the child: %q", text)
		}
		return
	}
	t.Fatal("the log holds no tool.result record")
}
