package openai_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The request.
//
// The plan: the request carries the output ceiling, the sampling
// parameters, the reasoning parameter and the tools. The ceiling is
// always computed, the tools are always in a stable order, and a model
// that takes no temperature never gets one.

// TestTheMaxTokensAreComputed covers the ceiling: a request that
// arrives without one gets the model's own ceiling, and never the
// endpoint's default.
func TestTheMaxTokensAreComputed(t *testing.T) {
	cases := []struct {
		name string
		info poo.ModelInfo
		want float64
	}{
		{
			name: "a model with a ceiling of its own",
			info: poo.ModelInfo{MaxOutput: 4096},
			want: 4096,
		},
		{
			name: "a model with no ceiling of its own",
			info: poo.ModelInfo{},
			want: poo.DefaultMaxOutput,
		},
		{
			name: "a model with a context window that is smaller",
			info: poo.ModelInfo{MaxOutput: 4096, ContextLimit: 1024},
			want: 1024 - 256,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
			tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ModelInfo: c.info})

			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()
			read(t, reader)

			got, ok := srv.requests[0]["max_tokens"].(float64)
			if !ok {
				t.Fatalf("the request has no max_tokens: %v", srv.requests[0])
			}
			if got != c.want {
				t.Errorf("max_tokens is %v, want %v", got, c.want)
			}
		})
	}
}

// TestTheSamplingOverridesReachTheBody covers the sampling: the
// descriptor's defaults and the configured overrides both reach the
// request.
func TestTheSamplingOverridesReachTheBody(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		Endpoint:  srv.URL,
		Model:     "m",
		ModelInfo: poo.ModelInfo{Sampling: poo.Sampling{Temperature: ptr(0.3), TopP: ptr(0.9)}},
		Sampling: poo.Sampling{
			Temperature: ptr(0.7),
			TopK:        ptr(40.0),
			MinP:        ptr(0.05),
		},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	body := srv.requests[0]
	if body["temperature"] != 0.7 {
		t.Errorf("the temperature is %v, want the override", body["temperature"])
	}
	if body["top_p"] != 0.9 {
		t.Errorf("the top_p is %v, want the descriptor's own", body["top_p"])
	}
	if body["top_k"] != float64(40) {
		t.Errorf("the top_k is %v, want the override", body["top_k"])
	}
	if body["min_p"] != 0.05 {
		t.Errorf("the min_p is %v, want the override", body["min_p"])
	}
}

// TestAFixedTemperatureModelNeverGetsOne covers the plan's rule: a
// model that takes no temperature never gets one, whatever the
// settings ask for.
func TestAFixedTemperatureModelNeverGetsOne(t *testing.T) {
	for _, control := range []poo.TempMode{poo.TempFixed, poo.TempUnsupported} {
		srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
		tr := openai.New(openai.Options{
			Endpoint:  srv.URL,
			Model:     "m",
			ModelInfo: poo.ModelInfo{Temperature: control},
			Sampling:  poo.Sampling{Temperature: ptr(0.7)},
		})

		reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
		if err != nil {
			t.Fatalf("starting the request: %v", err)
		}
		defer reader.Close()
		read(t, reader)

		if _, sent := srv.requests[0]["temperature"]; sent {
			t.Errorf("the request sent a temperature: %v", srv.requests[0])
		}
	}
}

// TestTheReasoningParameterFollowsTheMode covers the plan's mapping: an
// effort is an effort level, a budget is a number of thinking tokens,
// and a toggle is on or off.
func TestTheReasoningParameterFollowsTheMode(t *testing.T) {
	cases := []struct {
		name     string
		mode     poo.ReasoningMode
		param    string
		setting  string
		wantKey  string
		wantPath []string
		wantVal  any
	}{
		{
			name:    "an effort is an effort level",
			mode:    poo.ReasoningEffort,
			setting: "high",
			wantKey: "reasoning_effort",
			wantVal: "high",
		},
		{
			name:     "a budget is a number of thinking tokens",
			mode:     poo.ReasoningBudget,
			setting:  "2048",
			wantKey:  "thinking",
			wantPath: []string{"budget_tokens"},
			wantVal:  float64(2048),
		},
		{
			name:     "a toggle is a chat template argument",
			mode:     poo.ReasoningToggle,
			setting:  "low",
			wantKey:  "chat_template_kwargs",
			wantPath: []string{"enable_thinking"},
			wantVal:  true,
		},
		{
			name:     "the descriptor names the parameter",
			mode:     poo.ReasoningEffort,
			param:    "reasoning.effort",
			setting:  "medium",
			wantKey:  "reasoning",
			wantPath: []string{"effort"},
			wantVal:  "medium",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
			tr := openai.New(openai.Options{
				Endpoint: srv.URL,
				Model:    "m",
				ModelInfo: poo.ModelInfo{Reasoning: poo.ReasoningControl{
					Mode:  c.mode,
					Param: c.param,
				}},
				Reasoning: c.setting,
			})

			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()
			read(t, reader)

			value, ok := srv.requests[0][c.wantKey]
			if !ok {
				t.Fatalf("the request has no %s: %v", c.wantKey, srv.requests[0])
			}
			for _, step := range c.wantPath {
				nested, ok := value.(map[string]any)
				if !ok {
					t.Fatalf("the %s is %v, want an object", c.wantKey, value)
				}
				value, ok = nested[step]
				if !ok {
					t.Fatalf("the %s has no %s: %v", c.wantKey, step, nested)
				}
			}
			if value != c.wantVal {
				t.Errorf("the reasoning parameter is %v, want %v", value, c.wantVal)
			}
		})
	}
}

// TestAModelThatCannotThinkIsAskedNotTo covers the plan's rule: a model
// whose reasoning cannot be controlled takes no parameter, and a
// setting that asks it to think is a configuration error.
func TestAModelThatCannotThinkIsAskedNotTo(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		Endpoint:  srv.URL,
		Model:     "m",
		ModelInfo: poo.ModelInfo{Reasoning: poo.ReasoningControl{Mode: poo.ReasoningNone}},
		Reasoning: "low",
	})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		t.Fatal("the request succeeded, want the configuration to be refused")
	}
	if !strings.Contains(err.Error(), "does not control its reasoning") {
		t.Errorf("the error is %q, want it to name the reason", err)
	}
}

// TestAModelThatCannotThinkIsAskedNotToSendAnything covers the same
// rule with the setting turned off: the request goes out without a
// reasoning parameter.
func TestAModelThatCannotThinkIsAskedNotToSendAnything(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		Endpoint:  srv.URL,
		Model:     "m",
		ModelInfo: poo.ModelInfo{Reasoning: poo.ReasoningControl{Mode: poo.ReasoningNone}},
		Reasoning: "off",
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	for _, key := range []string{"reasoning_effort", "reasoning_budget", "chat_template_kwargs"} {
		if _, sent := srv.requests[0][key]; sent {
			t.Errorf("the request sent a %s: %v", key, srv.requests[0])
		}
	}
}

// TestAToggleModelIsSwitchedOff covers the plan's rule: a toggle
// model that is asked not to think is switched off explicitly, because
// its template thinks by default.
func TestAToggleModelIsSwitchedOff(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		ModelInfo: poo.ModelInfo{Reasoning: poo.ReasoningControl{
			Mode: poo.ReasoningToggle,
		}},
		Reasoning: "off",
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	kwargs, ok := srv.requests[0]["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("the request has no chat_template_kwargs: %v", srv.requests[0])
	}
	if kwargs["enable_thinking"] != false {
		t.Errorf("the request's enable_thinking is %v, want false", kwargs["enable_thinking"])
	}
}

// TestAModelWithoutAReasoningControlRefusesTheSetting covers the
// plan's rule: the descriptor holds the rule, and a model that names no
// reasoning control is asked for none. A setting that asks it to think
// is a configuration error, and the request never goes out.
func TestAModelWithoutAReasoningControlRefusesTheSetting(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		Endpoint:  srv.URL,
		Model:     "m",
		ModelInfo: poo.ModelInfo{},
		Reasoning: "low",
	})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		t.Fatal("the request succeeded, want the configuration to be refused")
	}
	if len(srv.requests) != 0 {
		t.Errorf("the endpoint got %d requests, want none", len(srv.requests))
	}
}

// TestTheToolsAreSentInAStableOrder covers the plan's rule: the tool
// list is serialized in a stable order, so two sessions that registered
// the same tools in different orders send the same array.
func TestTheToolsAreSentInAStableOrder(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ToolsMode: "native"})

	tools := []poo.ToolSpec{
		{Name: "read", Description: "read a file", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "bash", Description: "run a command", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "grep", Description: "search", Schema: json.RawMessage(`{"type":"object"}`)},
	}
	reversed := []poo.ToolSpec{tools[2], tools[1], tools[0]}

	for _, order := range [][]poo.ToolSpec{tools, reversed} {
		reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16, Tools: order})
		if err != nil {
			t.Fatalf("starting the request: %v", err)
		}
		defer reader.Close()
		read(t, reader)
	}

	if len(srv.requests) != 2 {
		t.Fatalf("the endpoint got %d requests, want 2", len(srv.requests))
	}
	first, err := json.Marshal(srv.requests[0]["tools"])
	if err != nil {
		t.Fatalf("encoding the first tool array: %v", err)
	}
	second, err := json.Marshal(srv.requests[1]["tools"])
	if err != nil {
		t.Fatalf("encoding the second tool array: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("the tool arrays differ:\n%s\n%s", first, second)
	}
	names := []string{}
	for _, tool := range srv.requests[0]["tools"].([]any) {
		fn, _ := tool.(map[string]any)["function"].(map[string]any)
		names = append(names, fn["name"].(string))
	}
	if strings.Join(names, ",") != "bash,grep,read" {
		t.Errorf("the tools are %v, want them sorted by name", names)
	}
}

// ptr returns a pointer to a value.
func ptr[T any](v T) *T {
	return &v
}
