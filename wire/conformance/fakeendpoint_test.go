package conformance_test

// The test runs the fake endpoint the way a wire client's test does:
// it starts the endpoint, sends its own requests with a plain HTTP
// client, and reads back what the endpoint received. The endpoint is
// the suite's own, so a client's fake and the suite's never drift.

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/wire/conformance"
)

// TestFakeEndpointServesScriptedTurns starts the endpoint, runs a
// scripted text turn and a tool-call turn through it, and reads back
// the requests it received.
func TestFakeEndpointServesScriptedTurns(t *testing.T) {
	endpoint := conformance.NewFakeEndpoint(
		conformance.ScriptedAnswer{Text: "hello from the fake"},
		conformance.ScriptedAnswer{ToolCalls: []conformance.ToolCall{
			{Name: "bash", Arguments: map[string]any{"command": "echo hi"}},
		}},
	)
	defer endpoint.Close()

	ask := func(text string) map[string]any {
		return map[string]any{
			"model":  "conformance-model",
			"stream": true,
			"messages": []map[string]any{
				{"role": "user", "content": text},
			},
		}
	}

	text := post(t, endpoint.URL()+"/v1/chat/completions", ask("say hello"))
	if text.text != "hello from the fake" {
		t.Errorf("the answer is %q, want %q", text.text, "hello from the fake")
	}
	if text.finish != "stop" {
		t.Errorf("the answer finished %q, want stop", text.finish)
	}

	call := post(t, endpoint.URL()+"/v1/chat/completions", ask("run the tool"))
	if call.finish != "tool_calls" {
		t.Errorf("the answer finished %q, want tool_calls", call.finish)
	}
	if len(call.calls) != 1 {
		t.Fatalf("the answer made %d tool calls, want 1", len(call.calls))
	}
	if call.calls[0].Name != "bash" {
		t.Errorf("the tool call names %q, want bash", call.calls[0].Name)
	}
	if call.calls[0].Arguments["command"] != "echo hi" {
		t.Errorf("the tool call's arguments are %v, want the command", call.calls[0].Arguments)
	}

	bodies := endpoint.RequestBodies()
	if len(bodies) != 2 {
		t.Fatalf("the endpoint received %d bodies, want 2", len(bodies))
	}
	if bodies[0]["model"] != "conformance-model" {
		t.Errorf("the first request names the model %v, want conformance-model", bodies[0]["model"])
	}
}

// TestFakeEndpointExhaustsItsTurns scripts one turn, sends six
// requests, and checks the turn is answered once, repeated maxRepeats
// more times, and then the plain fallback answer serves the rest.
func TestFakeEndpointExhaustsItsTurns(t *testing.T) {
	endpoint := conformance.NewFakeEndpoint(conformance.ScriptedAnswer{Text: "the scripted turn"})
	defer endpoint.Close()

	ask := map[string]any{
		"model":  "conformance-model",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "answer me"},
		},
	}
	for i := 0; i < 6; i++ {
		got := post(t, endpoint.URL()+"/v1/chat/completions", ask)
		want := "the scripted turn"
		if i > 3 {
			want = "the answer"
		}
		if got.text != want {
			t.Errorf("request %d was answered %q, want %q", i+1, got.text, want)
		}
	}
}

// TestFakeEndpointServesSystemone asks the systemone path and checks
// the answer names itself and carries no decisions.
func TestFakeEndpointServesSystemone(t *testing.T) {
	endpoint := conformance.NewFakeEndpoint(conformance.ScriptedAnswer{Text: "the answer"})
	defer endpoint.Close()

	resp, err := http.Post(endpoint.URL()+"/v1/systemone", "application/json",
		strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("calling the endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the endpoint answered %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("the answer's content type is %q, want application/json", got)
	}
	var body struct {
		Object  string         `json:"object"`
		Answers map[string]any `json:"answers"`
		Usage   map[string]any `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	if body.Object != "systemone.response" {
		t.Errorf("the answer's object is %q, want systemone.response", body.Object)
	}
	if len(body.Answers) != 0 {
		t.Errorf("the answer carries %d answers, want none", len(body.Answers))
	}
}

// TestFakeEndpointWriteSettingsExtraOverridesTheDocument checks that a
// key the caller passes in extra wins over the document's own value.
func TestFakeEndpointWriteSettingsExtraOverridesTheDocument(t *testing.T) {
	endpoint := conformance.NewFakeEndpoint(conformance.ScriptedAnswer{Text: "the answer"})
	defer endpoint.Close()

	path := filepath.Join(t.TempDir(), "settings.json")
	extra := map[string]any{"roles": map[string]any{"chat": "other-model"}}
	if err := endpoint.WriteSettings(path, "conformance-model", 8192, extra); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the settings: %v", err)
	}
	var document struct {
		Roles map[string]string `json:"roles"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	if document.Roles["chat"] != "other-model" {
		t.Errorf("the chat role is %q, want other-model", document.Roles["chat"])
	}
}

// TestFakeEndpointWriteSettingsPointsABinaryAtTheEndpoint checks the
// settings document the helper writes: the endpoint, the model, its
// context limit and the role point at the running fake.
func TestFakeEndpointWriteSettingsPointsABinaryAtTheEndpoint(t *testing.T) {
	endpoint := conformance.NewFakeEndpoint(conformance.ScriptedAnswer{Text: "the answer"})
	defer endpoint.Close()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := endpoint.WriteSettings(path, "conformance-model", 8192, nil); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the settings: %v", err)
	}
	var document struct {
		Endpoints map[string]struct{ Endpoint string } `json:"endpoints"`
		Models    map[string]struct {
			Endpoint     string `json:"endpoint"`
			ModelID      string `json:"model_id"`
			ContextLimit int    `json:"context_limit"`
		} `json:"models"`
		Roles map[string]string `json:"roles"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	if document.Endpoints["fake"].Endpoint != endpoint.URL()+"/v1" {
		t.Errorf("the settings name the endpoint %q, want %q",
			document.Endpoints["fake"].Endpoint, endpoint.URL()+"/v1")
	}
	if document.Models["conformance-model"].ModelID != "conformance-model" {
		t.Errorf("the settings name the model id %q, want conformance-model",
			document.Models["conformance-model"].ModelID)
	}
	if document.Models["conformance-model"].ContextLimit != 8192 {
		t.Errorf("the model's context limit is %d, want 8192",
			document.Models["conformance-model"].ContextLimit)
	}
	if document.Roles["chat"] != "conformance-model" {
		t.Errorf("the chat role is %q, want conformance-model", document.Roles["chat"])
	}
}

// answer is what one scripted turn's stream said.
type answer struct {
	// text is the content the deltas carried.
	text string
	// calls are the tool calls the deltas carried.
	calls []conformance.ToolCall
	// finish is the last chunk's finish reason.
	finish string
}

// post sends one chat completions request with a plain HTTP client and
// reads the stream back.
func post(t *testing.T, url string, body map[string]any) answer {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("calling the endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the endpoint answered %d, want 200", resp.StatusCode)
	}

	var out answer
	toolArguments := map[int]map[string]any{}
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		line := strings.TrimPrefix(scan.Text(), "data: ")
		if !strings.HasPrefix(scan.Text(), "data: ") || line == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int `json:"index"`
						Function struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			t.Fatalf("decoding a chunk: %v", err)
		}
		for _, choice := range chunk.Choices {
			out.text += choice.Delta.Content
			for _, raw := range choice.Delta.ToolCalls {
				if raw.Function.Name != "" {
					for len(out.calls) <= raw.Index {
						out.calls = append(out.calls, conformance.ToolCall{})
					}
					out.calls[raw.Index].Name = raw.Function.Name
				}
				if len(raw.Function.Arguments) > 0 {
					var encoded string
					if err := json.Unmarshal(raw.Function.Arguments, &encoded); err != nil {
						t.Fatalf("decoding the tool call's arguments: %v", err)
					}
					arguments := toolArguments[raw.Index]
					if arguments == nil {
						arguments = map[string]any{}
					}
					if err := json.Unmarshal([]byte(encoded), &arguments); err != nil {
						t.Fatalf("decoding the tool call's arguments: %v", err)
					}
					toolArguments[raw.Index] = arguments
					out.calls[raw.Index].Arguments = arguments
				}
			}
			if choice.FinishReason != "" {
				out.finish = choice.FinishReason
			}
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	return out
}
