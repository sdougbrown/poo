package codex_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/codex"
)

// echoTool is the tool the session test's model calls.
type echoTool struct{}

// Spec describes the tool.
func (e *echoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (e *echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{}, nil
}

// Call answers with the argument.
func (e *echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "echoed"}, nil
}

// echoPrepared is a prepared echo call.
type echoPrepared struct{}

// Summary describes the call.
func (p echoPrepared) Summary() string { return "echo" }

// TestASessionRunsOnTheTransport covers the transport in the loop it is
// built for: a turn that calls a tool and a turn that answers. The second
// request carries the tool result as the item the endpoint matches
// against the call it made.
func TestASessionRunsOnTheTransport(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"echo"}}`)+
			frame(`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"text\":\"ping\"}"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":30,"output_tokens":5}}}`),
		frame(`{"type":"response.output_text.delta","output_index":0,"delta":"the answer"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":40,"output_tokens":3}}}`),
	)
	session, err := poo.New(sessionOptions(t, srv.URL, nil))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if result.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", result.Reason)
	}
	if result.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", result.Turns)
	}
	if n := srv.count(); n != 2 {
		t.Fatalf("the endpoint received %d requests, want the tool turn and the answer", n)
	}

	first := srv.request(t, 0)
	if got := field[string](t, first, "instructions"); got != "the prompt" {
		t.Errorf("the instructions are %q, want the session's prompt", got)
	}
	tools := field[[]map[string]any](t, first, "tools")
	if len(tools) != 1 || tools[0]["name"] != "echo" {
		t.Errorf("the request offers %v, want the echo tool", tools)
	}

	second := srv.request(t, 1)
	input := field[[]map[string]any](t, second, "input")
	if len(input) != 3 {
		t.Fatalf("the second request carries %d items, want the call and its result: %s", len(input), second.body)
	}
	expectJSON(t, input[1],
		`{"type":"function_call","call_id":"call_1","name":"echo","arguments":"{\"text\":\"ping\"}"}`)
	output, ok := input[2]["output"].(string)
	if !ok || input[2]["type"] != "function_call_output" || input[2]["call_id"] != "call_1" {
		t.Fatalf("the tool result item is %v, want the call's output", input[2])
	}
	if !strings.Contains(output, "echoed") {
		t.Errorf("the tool result is %q, want the tool's text", output)
	}
}

// reasoningTurn is the stream of a turn that thought, encrypted the
// thought and then called a tool.
const reasoningTurn = `data: {"type":"response.output_item.done","output_index":0,"item":` + reasoningItem + "}\n\n" +
	`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"echo"}}` + "\n\n" +
	`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"text\":\"ping\"}"}` + "\n\n" +
	`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":30,"output_tokens":5}}}` + "\n\n" + done

// sessionOptions builds the options a Codex session test runs with.
func sessionOptions(t *testing.T, endpoint string, sink poo.RecordSink) poo.Options {
	t.Helper()
	return poo.Options{
		Transport: newTransport(endpoint),
		Tools:     map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder: []string{"echo"},
		Sink:      sink,
		SessionID: "s1",
		Model: poo.ModelInfo{
			ModelID:      "gpt-6.1-sol",
			API:          poo.APIResponses,
			ContextLimit: 32768,
			MaxOutput:    4096,
		},
		SystemPrompt: "the prompt",
		WorkingDir:   t.TempDir(),
	}
}

// TestASessionCarriesTheReasoningItemIntoTheNextRequest covers the loop a
// storeless endpoint needs. The encrypted reasoning the first response returned
// is recorded with the assistant message. The next request carries it back
// ahead of the call it reasoned about.
func TestASessionCarriesTheReasoningItemIntoTheNextRequest(t *testing.T) {
	srv := newServer(t, reasoningTurn, completedStream)
	session, err := poo.New(sessionOptions(t, srv.URL, nil))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if n := srv.count(); n != 2 {
		t.Fatalf("the endpoint received %d requests, want the tool turn and the answer", n)
	}

	second := srv.request(t, 1)
	if got := field[bool](t, second, "store"); got {
		t.Error("the second request stores the response, want a storeless request")
	}
	input := field[[]map[string]any](t, second, "input")
	if len(input) != 4 {
		t.Fatalf("the second request carries %d items, want the call, its result and the reasoning item: %s",
			len(input), second.body)
	}
	expectJSON(t, input[1], reasoningItem)
	expectJSON(t, input[2],
		`{"type":"function_call","call_id":"call_1","name":"echo","arguments":"{\"text\":\"ping\"}"}`)
}

// TestARestoredSessionCarriesTheReasoningItemBack covers the record: a
// session rebuilt from another one's records still holds the encrypted
// reasoning, so the request it sends carries it.
func TestARestoredSessionCarriesTheReasoningItemBack(t *testing.T) {
	sink := pootest.NewSink()
	first := newServer(t, reasoningTurn, completedStream)
	session, err := poo.New(sessionOptions(t, first.URL, sink))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}

	resumed := newServer(t, completedStream)
	records := append([]poo.Record{sessionHeader()}, sink.Records()...)
	restored, _, err := poo.Restore(sessionOptions(t, resumed.URL, pootest.NewSink()), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	defer restored.Close()

	if _, err := restored.Prompt(context.Background(), "carry on"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := restored.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}

	if n := resumed.count(); n != 1 {
		t.Fatalf("the endpoint received %d requests, want the restored run's one", n)
	}
	input := field[[]map[string]any](t, resumed.request(t, 0), "input")
	// The restored history is the user's request, the reasoning item, the
	// call and its result, then the prompt the run was given.
	if len(input) != 5 {
		t.Fatalf("the restored request carries %d items, want the reasoning item and the call it reasoned about: %s",
			len(input), resumed.request(t, 0).body)
	}
	expectJSON(t, input[1], reasoningItem)
}

// sessionHeader is the header record a consumer writes for a session it
// starts, with the tools and the model the test's session ran with.
func sessionHeader() *poo.SessionCreatedRecord {
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{
			Format:     poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-01-01T00:00:00Z",
			WorkingDir: "/tmp",
			Roles: poo.ModelRoles{Chat: &poo.ModelInfo{
				Name:         "gpt-6.1-sol",
				ModelID:      "gpt-6.1-sol",
				API:          poo.APIResponses,
				ContextLimit: 32768,
				MaxOutput:    4096,
			}},
			Tools: []poo.ToolInfo{{Name: "echo", Class: poo.ClassReadOnly}},
		},
	}
}

// bareReasoningItem is a reasoning item as response.output_item.done
// reports it: the summary, and no encrypted payload.
const bareReasoningItem = `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"thinking"}]}`

// reasoningTurnTerminalOnly is the stream of a turn that reasoned
// without encrypted payload at its item.done event. The terminal
// response carries the item whole, which is where the endpoint puts the
// encrypted content.
const reasoningTurnTerminalOnly = `data: {"type":"response.output_item.done","output_index":0,"item":` + bareReasoningItem + "}\n\n" +
	`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"echo"}}` + "\n\n" +
	`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"text\":\"ping\"}"}` + "\n\n" +
	`data: {"type":"response.completed","response":{"status":"completed","output":[` + reasoningItem + `,{"type":"function_call","call_id":"call_1","name":"echo","arguments":"{\"text\":\"ping\"}"}],"usage":{"input_tokens":30,"output_tokens":5}}}` + "\n\n" + done

// TestTheTerminalOutputCarriesTheEncryptedReasoning covers the shape the
// endpoint really sends. The reasoning item's encrypted payload arrives in the
// terminal response's output, not in its item.done event. The next request
// carries it back all the same.
func TestTheTerminalOutputCarriesTheEncryptedReasoning(t *testing.T) {
	srv := newServer(t, reasoningTurnTerminalOnly, completedStream)
	session, err := poo.New(sessionOptions(t, srv.URL, nil))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if n := srv.count(); n != 2 {
		t.Fatalf("the endpoint received %d requests, want the tool turn and the answer", n)
	}

	second := srv.request(t, 1)
	input := field[[]map[string]any](t, second, "input")
	if len(input) != 4 {
		t.Fatalf("the second request carries %d items, want the reasoning item, the call and its result: %s",
			len(input), second.body)
	}
	expectJSON(t, input[1], reasoningItem)
}

// TestTheRecordedUsageIsTheEndpointsOwn covers the usage the run records.
// The prompt count is the endpoint's own, and the cached tokens ride beside it,
// so the run's token budget counts what the endpoint counted.
func TestTheRecordedUsageIsTheEndpointsOwn(t *testing.T) {
	sink := pootest.NewSink()
	srv := newServer(t,
		frame(`{"type":"response.output_text.delta","output_index":0,"delta":"the answer"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":`+
				`{"input_tokens":100,"output_tokens":7,"total_tokens":107,`+
				`"input_tokens_details":{"cached_tokens":40}}}}`),
	)
	session, err := poo.New(sessionOptions(t, srv.URL, sink))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()
	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	var usage *poo.UsageRecord
	for _, rec := range sink.Records() {
		if u, ok := rec.(*poo.UsageRecord); ok {
			usage = u
		}
	}
	if usage == nil {
		t.Fatal("the run recorded no usage")
	}
	if usage.InputTokens != 100 {
		t.Errorf("the recorded input count is %d, want the 100 the endpoint reported", usage.InputTokens)
	}
	if usage.OutputTokens != 7 {
		t.Errorf("the recorded output count is %d, want 7", usage.OutputTokens)
	}
	if usage.CacheReadTokens == nil || *usage.CacheReadTokens != 40 {
		t.Errorf("the recorded cache read count is %v, want 40", usage.CacheReadTokens)
	}
}

// failedAttemptStream is the stream of an attempt that reported what it spent
// and then failed, so a retried request has a failed attempt to count.
func failedAttemptStream(input, output int) string {
	return frame(fmt.Sprintf(`{"type":"response.failed","response":{"status":"failed",`+
		`"error":{"code":"rate_limit_exceeded","message":"slow down"},`+
		`"usage":{"input_tokens":%d,"output_tokens":%d}}}`, input, output))
}

// TestTheRecordedUsageCountsEveryAttempt covers the usage a retried request
// records. An attempt that failed before it answered still spent tokens, so the
// record holds what the attempts spent together, however many of them failed.
func TestTheRecordedUsageCountsEveryAttempt(t *testing.T) {
	answer := frame(`{"type":"response.output_text.delta","output_index":0,"delta":"the answer"}`) +
		frame(`{"type":"response.completed","response":{"status":"completed",`+
			`"usage":{"input_tokens":20,"output_tokens":6}}}`)
	cases := []struct {
		name          string
		failures      []string
		input, output int
	}{
		{
			name:     "one failed attempt before the answer",
			failures: []string{failedAttemptStream(10, 4)},
			input:    30,
			output:   10,
		},
		{
			name:     "two failed attempts before the answer",
			failures: []string{failedAttemptStream(10, 4), failedAttemptStream(5, 3)},
			input:    35,
			output:   13,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := pootest.NewSink()
			srv := newServer(t, append(tc.failures, answer)...)
			opts := sessionOptions(t, srv.URL, sink)
			tr := newTransport(srv.URL, func(o *codex.Options) {
				o.MaxAttempts = 3
				o.Backoff = []time.Duration{time.Millisecond, time.Millisecond}
			})
			opts.Transport = tr
			session, err := poo.New(opts)
			if err != nil {
				t.Fatalf("building the session: %v", err)
			}
			defer session.Close()

			if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			result, err := session.Wait(context.Background())
			if err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}
			if result.Reason != poo.ReasonEndTurn {
				t.Errorf("the run settled %q, want end_turn", result.Reason)
			}
			if n := srv.count(); n != len(tc.failures)+1 {
				t.Fatalf("the endpoint received %d requests, want the failed attempts and the one that answered", n)
			}

			var usage *poo.UsageRecord
			records := 0
			for _, rec := range sink.Records() {
				if u, ok := rec.(*poo.UsageRecord); ok {
					usage = u
					records++
				}
			}
			if records != 1 {
				t.Fatalf("the run wrote %d usage records, want one for the request", records)
			}
			if usage.InputTokens != tc.input {
				t.Errorf("the recorded input count is %d, want the %d every attempt spent", usage.InputTokens, tc.input)
			}
			if usage.OutputTokens != tc.output {
				t.Errorf("the recorded output count is %d, want the %d every attempt spent", usage.OutputTokens, tc.output)
			}
		})
	}
}

// truncatedTurn is the stream of a turn the endpoint cut short after the
// reasoning item: the reasoning is complete and replayable, the answer is
// not.
const truncatedTurn = `data: {"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"thinking"}` + "\n\n" +
	`data: {"type":"response.output_item.done","output_index":0,"item":` + reasoningItem + `}` + "\n\n" +
	`data: {"type":"response.output_text.delta","output_index":1,"delta":"half an answer"}` + "\n\n"

// TestARunCutShortKeepsTheReasoningItCanReplay covers the run that died
// mid-stream. The reasoning item the endpoint completed is recorded with the
// partial message the run keeps, and the next request carries it back.
func TestARunCutShortKeepsTheReasoningItCanReplay(t *testing.T) {
	sink := pootest.NewSink()
	srv := newServer(t, truncatedTurn, completedStream)
	session, err := poo.New(sessionOptions(t, srv.URL, sink))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if result.Reason != poo.ReasonProviderError {
		t.Fatalf("the run settled %q, want provider_error", result.Reason)
	}

	// The partial message the run kept carries the reasoning item beside
	// the thinking text it belongs to.
	var partial *poo.MessageRecord
	for _, msg := range sink.Messages() {
		if msg.Role == poo.RoleAssistant {
			partial = msg
		}
	}
	if partial == nil {
		t.Fatal("the run recorded no assistant message")
	}
	if !partial.Partial {
		t.Error("the recorded message is not marked partial, want the cut-short one")
	}
	var companion *poo.ProviderBlock
	for _, block := range partial.Content {
		if block.Provider != nil {
			companion = block.Provider
		}
	}
	if companion == nil {
		t.Fatalf("the recorded message carries no provider block: %v", partial.Content)
	}
	if companion.Type != "reasoning" {
		t.Errorf("the recorded block is a %q block, want the reasoning item", companion.Type)
	}

	if _, err := session.Prompt(context.Background(), "carry on"); err != nil {
		t.Fatalf("starting the next run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the next run: %v", err)
	}
	if n := srv.count(); n != 2 {
		t.Fatalf("the endpoint received %d requests, want the cut-short turn and the answer", n)
	}
	input := field[[]map[string]any](t, srv.request(t, 1), "input")
	var found bool
	for _, item := range input {
		if item["type"] == "reasoning" {
			found = true
			expectJSON(t, item, reasoningItem)
		}
	}
	if !found {
		t.Fatalf("the next request carries no reasoning item: %s", srv.request(t, 1).body)
	}
}
