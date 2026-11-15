package codex_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/codex"
)

// The request side: what the endpoint receives.

// completed is the stream of a response that finished its answer.
const completed = `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":5,"output_tokens":2}}}`
const completedStream = `data: ` + completed + "\n\n" + done

// expectJSON compares a decoded value with the JSON a test expects.
func expectJSON(t *testing.T, got any, want string) {
	t.Helper()
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("the expected JSON does not decode: %v", err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("the value does not encode: %v", err)
	}
	var gotValue any
	if err := json.Unmarshal(raw, &gotValue); err != nil {
		t.Fatalf("the value does not decode: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("the request carries\n%s\nwant\n%s", raw, want)
	}
}

// reasoningItem is a reasoning item a history may carry.
const reasoningItem = `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"thinking"}],"encrypted_content":"opaque"}`

// TestTheRequestCarriesTheHistoryAsResponsesItems covers the message
// conversion. The system message is the instructions. An assistant message is
// a message item plus one item per call, and a tool result is an item of its
// own.
func TestTheRequestCarriesTheHistoryAsResponsesItems(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.SessionID = "s1" })

	provider := poo.ProviderBlock{API: poo.APIResponses, Type: "reasoning", Data: json.RawMessage(reasoningItem)}
	history := []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "you are terse"),
		poo.NewTextMessage(poo.RoleUser, "list the files"),
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{
			{Kind: poo.ContentThinking, Text: "thinking", Provider: &provider},
			{Kind: poo.ContentText, Text: "sure"},
			{Kind: poo.ContentToolCall, ToolCallID: "call_1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
		}},
		poo.ToolResultMessage("call_1", "bash", "a.txt", false, nil),
	}

	reader, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  history,
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if rendered.API != poo.APIResponses {
		t.Errorf("the snapshot belongs to the %q API, want the Responses API", rendered.API)
	}
	if rendered.Messages != len(history) {
		t.Errorf("the snapshot says %d messages, want %d", rendered.Messages, len(history))
	}

	req := srv.request(t, 0)
	if req.path != "/codex/responses" {
		t.Errorf("the request went to %q, want the Codex responses path", req.path)
	}
	if got := field[string](t, req, "instructions"); got != "you are terse" {
		t.Errorf("the instructions are %q, want the system message", got)
	}

	input := field[[]map[string]any](t, req, "input")
	want := []string{
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"list the files"}]}`,
		reasoningItem,
		`{"type":"message","role":"assistant","id":"msg_poo_0","status":"completed","content":[{"type":"output_text","text":"sure","annotations":[]}]}`,
		`{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"ls\"}"}`,
		`{"type":"function_call_output","call_id":"call_1","output":"a.txt"}`,
	}
	if len(input) != len(want) {
		t.Fatalf("the request carries %d input items, want %d: %s", len(input), len(want), req.body)
	}
	for i, item := range input {
		expectJSON(t, item, want[i])
	}
}

// TestTheRequestCarriesTheFieldsTheEndpointExpects covers the request's
// own fields: no stored response, a stream and the reasoning summary
// asked for.
func TestTheRequestCarriesTheFieldsTheEndpointExpects(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.SessionID = "s1"
		o.ModelInfo = poo.ModelInfo{
			ModelID:   "gpt-6.1-sol",
			Reasoning: poo.ReasoningControl{Mode: poo.ReasoningEffort},
		}
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 2048,
		Reasoning: "high",
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	req := srv.request(t, 0)
	for name, want := range map[string]string{
		"model":               `"gpt-6.1-sol"`,
		"store":               `false`,
		"stream":              `true`,
		"parallel_tool_calls": `true`,
		"tool_choice":         `"auto"`,
		"prompt_cache_key":    `"s1"`,
		"include":             `["reasoning.encrypted_content"]`,
		"text":                `{"verbosity":"low"}`,
		"reasoning":           `{"effort":"high","summary":"auto"}`,
	} {
		raw, ok := req.fields[name]
		if !ok {
			t.Errorf("the request carries no %q field: %s", name, req.body)
			continue
		}
		expectJSON(t, json.RawMessage(raw), want)
	}
}

// TestTheReasoningSettingFollowsTheModelControl covers the effort the
// request asks for and the level that spells off.
func TestTheReasoningSettingFollowsTheModelControl(t *testing.T) {
	cases := []struct {
		name    string
		setting string
		want    string
	}{
		{name: "a level", setting: "medium", want: `{"effort":"medium","summary":"auto"}`},
		{name: "off", setting: "off", want: `{"effort":"none"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, completedStream)
			tr := newTransport(srv.URL, func(o *codex.Options) {
				o.ModelInfo = poo.ModelInfo{
					ModelID:   "gpt-6.1-sol",
					Reasoning: poo.ReasoningControl{Mode: poo.ReasoningEffort},
				}
			})
			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
				Model:     "gpt-6.1-sol",
				Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
				MaxTokens: 64,
				Reasoning: c.setting,
			})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()
			read(t, reader)
			req := srv.request(t, 0)
			expectJSON(t, json.RawMessage(req.fields["reasoning"]), c.want)
		})
	}
}

// TestATokenBudgetIsRefused covers a model whose reasoning is not an
// effort: the endpoint has no field for it, so the request never goes
// out.
func TestATokenBudgetIsRefused(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.ModelInfo = poo.ModelInfo{
			ModelID:   "gpt-6.1-sol",
			Reasoning: poo.ReasoningControl{Mode: poo.ReasoningBudget},
		}
	})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
		Reasoning: "2000",
	})
	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindInvalidRequest {
		t.Fatalf("the request failed with %v, want an invalid-request failure", err)
	}
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none", n)
	}
}

// TestToolsAreOfferedInNameOrder covers the stable order and the shape
// of a tool the request offers.
func TestToolsAreOfferedInNameOrder(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)
	tools := []poo.ToolSpec{
		{Name: "zebra", Description: "last", Schema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`)},
		{Name: "alpha", Description: "first"},
	}
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		Tools:     tools,
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	offered := field[[]map[string]any](t, srv.request(t, 0), "tools")
	want := []string{
		`{"type":"function","name":"alpha","description":"first","parameters":{"type":"object","properties":{}},"strict":false}`,
		`{"type":"function","name":"zebra","description":"last","parameters":{"type":"object","properties":{"a":{"type":"string"}}},"strict":false}`,
	}
	if len(offered) != len(want) {
		t.Fatalf("the request offers %d tools, want %d", len(offered), len(want))
	}
	for i, tool := range offered {
		expectJSON(t, tool, want[i])
	}
}

// TestAMidConversationSystemMessageIsADeveloperItem covers the
// instruction the history carries after its leading system message.
func TestAMidConversationSystemMessageIsADeveloperItem(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)
	history := []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "the prompt"),
		poo.NewTextMessage(poo.RoleUser, "hi"),
		poo.NewTextMessage(poo.RoleSystem, "a later instruction"),
	}
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  history,
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	req := srv.request(t, 0)
	if got := field[string](t, req, "instructions"); got != "the prompt" {
		t.Errorf("the instructions are %q, want the leading system message", got)
	}
	input := field[[]map[string]any](t, req, "input")
	expectJSON(t, input[len(input)-1],
		`{"role":"developer","content":[{"type":"input_text","text":"a later instruction"}]}`)
}

// TestTheCredentialIsResolvedForEveryRequest covers the authentication:
// the resolver is asked per attempt, and the headers carry what it
// answered.
func TestTheCredentialIsResolvedForEveryRequest(t *testing.T) {
	srv := newServer(t, completedStream, completedStream)
	tokens := []string{"first-token", "second-token"}
	asked := 0
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.SessionID = "s1"
		o.Originator = "poo-test"
		o.Credentials = func(context.Context) (codex.Credential, error) {
			token := tokens[asked]
			asked++
			return codex.Credential{Token: token, AccountID: "acct-1"}, nil
		}
	})

	for request := 0; request < 2; request++ {
		reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
			Model:     "gpt-6.1-sol",
			Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
			MaxTokens: 64,
		})
		if err != nil {
			t.Fatalf("starting request %d: %v", request, err)
		}
		read(t, reader)
		reader.Close()

		header := srv.request(t, request).header
		for name, want := range map[string]string{
			"Authorization":       "Bearer " + tokens[request],
			"Chatgpt-Account-Id":  "acct-1",
			"Openai-Beta":         "responses=experimental",
			"Originator":          "poo-test",
			"Session-Id":          "s1",
			"X-Client-Request-Id": "s1",
			"Accept":              "text/event-stream",
			"Content-Type":        "application/json",
		} {
			if got := header.Get(name); got != want {
				t.Errorf("request %d carries %s %q, want %q", request, name, got, want)
			}
		}
	}
	if asked != 2 {
		t.Errorf("the resolver was asked %d times, want once per request", asked)
	}
}

// TestTheAccountIsReadFromTheToken covers a resolver that holds only the
// token: the account rides in the token's claims.
func TestTheAccountIsReadFromTheToken(t *testing.T) {
	srv := newServer(t, completedStream)
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-9"}}`))
	token := "header." + claims + ".signature"
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Credentials = credential(token, "")
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if got := srv.request(t, 0).header.Get("Chatgpt-Account-Id"); got != "acct-9" {
		t.Errorf("the account header is %q, want the token's claim", got)
	}
}

// TestAnUnresolvableCredentialFailsBeforeTheRequest covers the two ways
// a credential is missing: no resolver, and a token no account can be
// read from.
func TestAnUnresolvableCredentialFailsBeforeTheRequest(t *testing.T) {
	cases := []struct {
		name        string
		credentials func(context.Context) (codex.Credential, error)
		wantKind    poo.ProviderKind
	}{
		{
			name:        "no resolver",
			credentials: nil,
			wantKind:    poo.ProviderKindInvalidRequest,
		},
		{
			name: "the resolver fails",
			credentials: func(context.Context) (codex.Credential, error) {
				return codex.Credential{}, errors.New("the keychain is locked")
			},
			wantKind: poo.ProviderKindAuthFailed,
		},
		{
			name: "the token names no account",
			credentials: func(context.Context) (codex.Credential, error) {
				return codex.Credential{Token: "not-a-jwt"}, nil
			},
			wantKind: poo.ProviderKindAuthFailed,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, completedStream)
			tr := newTransport(srv.URL, func(o *codex.Options) { o.Credentials = c.credentials })

			_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
				Model:     "gpt-6.1-sol",
				Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
				MaxTokens: 64,
			})
			var perr *poo.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("the request failed with %v, want a provider failure", err)
			}
			if perr.Kind != c.wantKind {
				t.Errorf("the failure is %q, want %q", perr.Kind, c.wantKind)
			}
			if n := srv.count(); n != 0 {
				t.Errorf("the endpoint received %d requests, want none", n)
			}
		})
	}
}

// TestAForkKeepsTheBodyApartFromTheAppendedItems covers the fork: the
// snapshot's body is re-sent, with the appended messages after the last
// item and nothing else changed.
func TestAForkKeepsTheBodyApartFromTheAppendedItems(t *testing.T) {
	srv := newServer(t, completedStream, completedStream)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.SessionID = "s1" })
	history := []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "the prompt"),
		poo.NewTextMessage(poo.RoleUser, "hi"),
	}

	reader, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  history,
		MaxTokens: 512,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	reader.Close()

	forked, err := tr.Fork(context.Background(), rendered, []poo.Message{
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{{Kind: poo.ContentText, Text: "an answer"}}},
		poo.NewTextMessage(poo.RoleSystem, "a later instruction"),
		poo.NewTextMessage(poo.RoleUser, "carry on"),
	}, 99)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer forked.Close()
	read(t, forked)

	first := srv.request(t, 0)
	second := srv.request(t, 1)
	for name, raw := range first.fields {
		if name == "input" {
			continue
		}
		if got := string(second.fields[name]); got != string(raw) {
			t.Errorf("the fork changed the %q field to %s, want %s", name, got, raw)
		}
	}
	for _, name := range []string{"max_output_tokens", "temperature", "top_p"} {
		if _, ok := second.fields[name]; ok {
			t.Errorf("the fork carries %q, want the snapshot's body as it was: %s", name, second.body)
		}
	}

	before := field[[]map[string]any](t, first, "input")
	after := field[[]map[string]any](t, second, "input")
	if len(after) != len(before)+3 {
		t.Fatalf("the fork carries %d input items, want %d: %s", len(after), len(before)+3, second.body)
	}
	for i, item := range before {
		expectJSON(t, after[i], canonical(t, item))
	}
	appended := []string{
		`{"type":"message","role":"assistant","id":"msg_poo_1","status":"completed","content":[{"type":"output_text","text":"an answer","annotations":[]}]}`,
		`{"role":"developer","content":[{"type":"input_text","text":"a later instruction"}]}`,
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"carry on"}]}`,
	}
	for i, want := range appended {
		expectJSON(t, after[len(before)+i], want)
	}
}

// canonical renders a decoded value as the JSON a test can compare.
func canonical(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("the value does not encode: %v", err)
	}
	return string(raw)
}

// TestAForkOfAnotherAPIIsRefused covers the snapshot a fork cannot use.
func TestAForkOfAnotherAPIIsRefused(t *testing.T) {
	tr := newTransport("http://127.0.0.1:1", func(o *codex.Options) { o.Credentials = credential("t", "a") })
	_, err := tr.Fork(context.Background(), poo.Rendered{
		API:  poo.APIChatCompletions,
		Body: []byte(`{"input":[]}`),
	}, nil, 16)
	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindInvalidRequest {
		t.Fatalf("the fork failed with %v, want an invalid-request failure", err)
	}
	if !strings.Contains(perr.Message, string(poo.APIChatCompletions)) {
		t.Errorf("the failure is %q, want it to name the API the snapshot belongs to", perr.Message)
	}
}

// TestASnapshotWithoutABodyIsRefused covers the empty snapshot.
func TestASnapshotWithoutABodyIsRefused(t *testing.T) {
	tr := newTransport("http://127.0.0.1:1", func(o *codex.Options) { o.Credentials = credential("t", "a") })
	_, err := tr.Fork(context.Background(), poo.Rendered{API: poo.APIResponses}, nil, 16)
	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindInvalidRequest {
		t.Fatalf("the fork failed with %v, want an invalid-request failure", err)
	}
}

// TestAnEndpointThatAnswersWithJSONIsAFailure covers a 200 whose body is
// an error rather than a stream.
func TestAnEndpointThatAnswersWithJSONIsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"error":{"code":"usage_limit_reached","message":"out of budget"}}`)
	}))
	defer srv.Close()

	tr := newTransport(srv.URL)
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the request failed with %v, want a provider failure", err)
	}
	if perr.Kind != poo.ProviderKindUsageLimit {
		t.Errorf("the failure is %q, want a usage limit", perr.Kind)
	}
}

// TestTheToolsModeTheEndpointCannotServeIsRefused covers the mode the
// endpoint has no way to serve. It makes native calls only, so a model that
// asks for the tools in the prompt is a configuration error.
func TestTheToolsModeTheEndpointCannotServeIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		want    string
		refused bool
	}{
		{name: "offered natively", mode: "native", want: "offered"},
		{name: "offered for the model that decides", mode: "auto", want: "offered"},
		{name: "not offered at all", mode: "none"},
		{name: "described in the prompt", mode: "text", refused: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, completedStream)
			tr := newTransport(srv.URL, func(o *codex.Options) { o.ToolsMode = c.mode })
			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
				Model:     "gpt-6.1-sol",
				Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
				Tools:     []poo.ToolSpec{{Name: "echo"}},
				MaxTokens: 64,
			})
			if c.refused {
				var perr *poo.ProviderError
				if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindInvalidRequest {
					t.Fatalf("the request failed with %v, want an invalid-request failure", err)
				}
				if n := srv.count(); n != 0 {
					t.Errorf("the endpoint received %d requests, want none", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()
			read(t, reader)
			req := srv.request(t, 0)
			raw, offered := req.fields["tools"]
			switch {
			case c.want == "offered" && !offered:
				t.Errorf("the request offers no tools in %q mode", c.mode)
			case c.want != "offered" && offered:
				t.Errorf("the request offers %s in %q mode", raw, c.mode)
			}
		})
	}
}

// TestTheCallerHeadersReachTheRequest covers the headers a consumer adds
// beside the ones the transport sets.
func TestTheCallerHeadersReachTheRequest(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Headers = map[string]string{"X-Test": "1"}
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	req := srv.request(t, 0)
	if got := req.header.Get("X-Test"); got != "1" {
		t.Errorf("the request carries X-Test %q, want the caller's header", got)
	}
}

// TestTheEndpointURL covers the base URLs a consumer configures.
func TestTheEndpointURL(t *testing.T) {
	cases := map[string]string{
		"https://chatgpt.com/backend-api":      "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com/backend-api/":     "https://chatgpt.com/backend-api/codex/responses",
		"https://example.test/codex":           "https://example.test/codex/responses",
		"https://example.test/codex/responses": "https://example.test/codex/responses",
		"":                                     codex.DefaultEndpoint + "/codex/responses",
	}
	for base, want := range cases {
		if got := codex.ResponsesURL(base); got != want {
			t.Errorf("the URL of %q is %q, want %q", base, got, want)
		}
	}
}

// TestTheRequestOmitsTheLimitAndSamplingTheEndpointRefuses covers the fields
// the Codex endpoint rejects. It takes no output limit, no temperature and no
// top-p, so neither the caller's limit nor the model's sampling defaults reach
// the body.
func TestTheRequestOmitsTheLimitAndSamplingTheEndpointRefuses(t *testing.T) {
	srv := newServer(t, completedStream)
	temperature := 0.25
	topP := 0.9
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.ModelInfo = poo.ModelInfo{
			ModelID:     "gpt-6.1-sol",
			Temperature: poo.TempFree,
			Sampling:    poo.Sampling{Temperature: &temperature, TopP: &topP},
		}
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	req := srv.request(t, 0)
	for _, name := range []string{"max_output_tokens", "temperature", "top_p"} {
		if raw, ok := req.fields[name]; ok {
			t.Errorf("the request carries %q %s, want the field left out: %s", name, raw, req.body)
		}
	}
}

// TestAnErrorBodyWithLeadingWhitespaceIsAFailure covers an endpoint that
// answers a refused parameter with a 200 and a pretty-printed error. The body
// is an error whatever whitespace it opens with, and its own code is what the
// run reports.
func TestAnErrorBodyWithLeadingWhitespaceIsAFailure(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "\n  {\"error\":{\"message\":\"Unsupported parameter: max_output_tokens\","+
			"\"type\":\"invalid_request_error\",\"code\":\"unsupported_parameter\"}}")
	}))
	defer srv.Close()

	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Backoff = []time.Duration{time.Millisecond, time.Millisecond}
	})
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the request failed with %v, want the provider's own failure", err)
	}
	if perr.Code != "unsupported_parameter" {
		t.Errorf("the failure's code is %q, want the code the endpoint sent", perr.Code)
	}
	if perr.Kind != poo.ProviderKindInvalidRequest {
		t.Errorf("the failure is %q, want a rejected request", perr.Kind)
	}
	if requests != 1 {
		t.Errorf("the endpoint received %d requests, want the one the endpoint answered as refused", requests)
	}
}

// TestALongSessionIdentifierIsClamped covers the session the endpoint keeps
// its cache under. The identifier is cut to the length the endpoint accepts,
// in characters rather than bytes, and the headers carry the same cut.
func TestALongSessionIdentifierIsClamped(t *testing.T) {
	srv := newServer(t, completedStream)
	long := strings.Repeat("s", 60) + strings.Repeat("é", 10)
	want := strings.Repeat("s", 60) + strings.Repeat("é", 4)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.SessionID = long })

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	req := srv.request(t, 0)
	if got := field[string](t, req, "prompt_cache_key"); got != want {
		t.Errorf("the prompt cache key is %q, want the first %d characters", got, len([]rune(want)))
	}
	for _, name := range []string{"Session-Id", "X-Client-Request-Id"} {
		if got := req.header.Get(name); got != want {
			t.Errorf("%s is %q, want the same cut", name, got)
		}
	}
}

// TestAForkCarriesAnAppendedAttachmentTheSnapshotRead covers the snapshot a
// fork shapes its appended messages from. The model info the request carried
// is not part of the snapshot, so the modalities the body was shaped for are.
// An appended attachment the model reads survives.
func TestAForkCarriesAnAppendedAttachmentTheSnapshotRead(t *testing.T) {
	srv := newServer(t, completedStream, completedStream)
	tr := newTransport(srv.URL)
	reader, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model: "gpt-6.1-sol",
		ModelInfo: poo.ModelInfo{
			ModelID: "gpt-6.1-sol",
			Inputs:  []poo.Modality{poo.ModalityText, poo.ModalityImage},
		},
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	reader.Close()

	image := poo.Message{Role: poo.RoleUser, Content: []poo.ContentBlock{
		{Kind: poo.ContentImage, MediaType: "image/png", Data: []byte("PNG")},
	}}
	forked, err := tr.Fork(context.Background(), rendered, []poo.Message{image}, 64)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer forked.Close()
	read(t, forked)

	input := field[[]map[string]any](t, srv.request(t, 1), "input")
	appended := input[len(input)-1]
	content, ok := appended["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("the appended item is %v, want the image part", appended)
	}
	part, _ := content[0].(map[string]any)
	if part["type"] != "input_image" || part["image_url"] != "data:image/png;base64,UE5H" {
		t.Errorf("the appended part is %v, want the image the message carries", part)
	}
}

// TestARedirectIsRefused covers the endpoint that answers with a redirect. The
// request carries the conversation and the account, so it goes to the endpoint
// the transport was configured with. It goes nowhere else, whatever client the
// consumer injected.
func TestARedirectIsRefused(t *testing.T) {
	clients := []struct {
		name   string
		client *http.Client
	}{
		{name: "the transport's own client"},
		{
			name: "a client that would follow a redirect",
			client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				return nil
			}},
		},
	}
	for _, c := range clients {
		t.Run(c.name, func(t *testing.T) {
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirected.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, completedStream)
			}))
			defer target.Close()
			var asked atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				http.Redirect(w, r, target.URL+"/codex/responses", http.StatusTemporaryRedirect)
			}))
			defer endpoint.Close()

			tr := newTransport(endpoint.URL, func(o *codex.Options) {
				if c.client != nil {
					o.HTTPClient = c.client
				}
			})
			_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
				Model:     "gpt-6.1-sol",
				Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "a secret to keep")},
				MaxTokens: 64,
			})
			var perr *poo.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("the request failed with %v, want the redirect refused", err)
			}
			if perr.Kind != poo.ProviderKindInvalidRequest {
				t.Errorf("the failure is %q, want a refused request", perr.Kind)
			}
			if !strings.Contains(perr.Message, "redirect") {
				t.Errorf("the failure is %q, want it to name the redirect", perr.Message)
			}
			if perr.Status != http.StatusTemporaryRedirect {
				t.Errorf("the failure reports status %d, want the redirect's %d", perr.Status, http.StatusTemporaryRedirect)
			}
			if n := asked.Load(); n != 1 {
				t.Errorf("the configured endpoint received %d requests, want the one it redirected", n)
			}
			if n := redirected.Load(); n != 0 {
				t.Errorf("the redirect target received %d requests, want none: the conversation and the account stay on the configured endpoint", n)
			}
		})
	}
}

// TestATransientCredentialFailureIsRetried covers a resolver that fails the
// way a token store does. The failure it names is a provider failure, so the
// attempt is retried and the request still goes out.
func TestATransientCredentialFailureIsRetried(t *testing.T) {
	srv := newServer(t, completedStream)
	asked := 0
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.MaxAttempts = 2
		o.Backoff = []time.Duration{time.Millisecond}
		o.Credentials = func(context.Context) (codex.Credential, error) {
			asked++
			if asked == 1 {
				return codex.Credential{}, &poo.ProviderError{
					Kind:    poo.ProviderKindNetwork,
					Message: "the token store timed out",
				}
			}
			return codex.Credential{Token: "t", AccountID: "a"}, nil
		}
	})
	var reported []poo.Attempt
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
		OnAttempt: func(a poo.Attempt) { reported = append(reported, a) },
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if asked != 2 {
		t.Errorf("the resolver was asked %d times, want the retry", asked)
	}
	if n := srv.count(); n != 1 {
		t.Errorf("the endpoint received %d requests, want the attempt that resolved a token", n)
	}
	if len(reported) != 1 || !reported[0].Retried {
		t.Fatalf("the request reported %v, want the failed credential attempt", reported)
	}
	var perr *poo.ProviderError
	if !errors.As(reported[0].Err, &perr) || perr.Kind != poo.ProviderKindNetwork {
		t.Errorf("the attempt failed with %v, want the transient failure the resolver named", reported[0].Err)
	}
}

// TestACredentialResolutionThatTheRunEndedIsTheRuns covers a resolver
// whose context ended: the run is stopping, so the failure is the
// context's, not the credential's.
func TestACredentialResolutionThatTheRunEndedIsTheRuns(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Credentials = func(context.Context) (codex.Credential, error) {
			return codex.Credential{}, fmt.Errorf("reading the token: %w", context.Canceled)
		}
	})
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the request failed with %v, want the run's cancellation", err)
	}
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none", n)
	}
}

// TestACredentialDeadlineIsATransientFailure covers a resolver whose own
// refresh timed out while the run is alive. The failure is a network one, so
// the run retries instead of settling on the account.
func TestACredentialDeadlineIsATransientFailure(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.MaxAttempts = 1
		o.Credentials = func(context.Context) (codex.Credential, error) {
			return codex.Credential{}, fmt.Errorf("refreshing the token: %w", context.DeadlineExceeded)
		}
	})
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the request failed with %v, want a transient provider failure", err)
	}
	if perr.Kind != poo.ProviderKindNetwork {
		t.Errorf("the failure is %q, want a network failure", perr.Kind)
	}
	if !perr.Retryable() {
		t.Errorf("the failure is not retryable, want a transient one")
	}
}

// TestACredentialResolutionCutByTheTransportTimeoutIsRetried covers the
// transport's own bound ending the resolution while the run is alive. The
// resolution did not finish in time, which is a transient failure and not the
// run's cancellation, so another attempt resolves a token.
func TestACredentialResolutionCutByTheTransportTimeoutIsRetried(t *testing.T) {
	srv := newServer(t, completedStream)
	asked := 0
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Timeout = 50 * time.Millisecond
		o.MaxAttempts = 2
		o.Backoff = []time.Duration{time.Millisecond}
		o.Credentials = func(ctx context.Context) (codex.Credential, error) {
			asked++
			if asked == 1 {
				// The token store is slower than the request's bound.
				<-ctx.Done()
				return codex.Credential{}, fmt.Errorf("reading the token: %w", ctx.Err())
			}
			return codex.Credential{Token: "t", AccountID: "a"}, nil
		}
	})
	var reported []poo.Attempt
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
		OnAttempt: func(a poo.Attempt) { reported = append(reported, a) },
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if asked != 2 {
		t.Errorf("the resolver was asked %d times, want the retry", asked)
	}
	if n := srv.count(); n != 1 {
		t.Errorf("the endpoint received %d requests, want the attempt that resolved a token", n)
	}
	if len(reported) != 1 || !reported[0].Retried {
		t.Fatalf("the request reported %v, want the failed attempt and its retry", reported)
	}
	var perr *poo.ProviderError
	if !errors.As(reported[0].Err, &perr) || perr.Kind != poo.ProviderKindNetwork {
		t.Errorf("the attempt failed with %v, want the transient failure the transport's bound causes", reported[0].Err)
	}
	if errors.Is(reported[0].Err, context.Canceled) || errors.Is(reported[0].Err, context.DeadlineExceeded) {
		t.Errorf("the attempt reports %v, want a transient failure rather than the run stopping", reported[0].Err)
	}
}

// TestACancelledRunDuringCredentialResolutionIsTheRuns covers the run's
// own cancellation ending the resolution: the failure is the context's,
// so the run settles as cancelled and no attempt is made.
func TestACancelledRunDuringCredentialResolutionIsTheRuns(t *testing.T) {
	srv := newServer(t, completedStream)
	asked := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Timeout = time.Second
		o.MaxAttempts = 3
		o.Backoff = []time.Duration{time.Millisecond}
		o.Credentials = func(resolveCtx context.Context) (codex.Credential, error) {
			asked++
			cancel()
			<-resolveCtx.Done()
			return codex.Credential{}, fmt.Errorf("reading the token: %w", resolveCtx.Err())
		}
	})
	_, _, err := tr.Stream(ctx, poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the request failed with %v, want the run's cancellation", err)
	}
	if asked != 1 {
		t.Errorf("the resolver was asked %d times, want none after the run ended", asked)
	}
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none", n)
	}
}

// TestATerminalCredentialFailureIsNotRetried covers the verdict of the
// resolver's own failure. A refresh that traded the token and could not store
// the new credential is terminal, and the transport hands it back as it is. The
// transport's bound ending at the same time does not turn it into a failure
// another attempt resolves.
func TestATerminalCredentialFailureIsNotRetried(t *testing.T) {
	srv := newServer(t, completedStream)
	asked := 0
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Timeout = 10 * time.Millisecond
		o.MaxAttempts = 3
		o.Backoff = []time.Duration{time.Millisecond}
		o.Credentials = func(ctx context.Context) (codex.Credential, error) {
			asked++
			<-ctx.Done()
			// The failure an adapter maps piauth's spent refresh onto: the
			// token was traded and not stored, so a retry spends it again.
			return codex.Credential{}, &poo.ProviderError{
				Kind:    poo.ProviderKindAuthFailed,
				Message: "piauth: the refreshed token was not stored",
			}
		}
	})
	var reported []poo.Attempt
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
		OnAttempt: func(a poo.Attempt) { reported = append(reported, a) },
	})
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the request failed with %v, want the failure the resolver named", err)
	}
	if perr.Kind != poo.ProviderKindAuthFailed {
		t.Errorf("the failure is %q, want the auth failure the resolver named", perr.Kind)
	}
	if asked != 1 {
		t.Errorf("the resolver was asked %d times, want the one attempt a terminal failure gets", asked)
	}
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none", n)
	}
	if len(reported) != 1 || reported[0].Retried {
		t.Errorf("the request reported %v, want one attempt that was not retried", reported)
	}
}
