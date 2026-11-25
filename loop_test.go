package poo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

func TestARunThatEndsOnItsOwnSettlesEndTurn(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "all done"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if res.Turns != 1 {
		t.Errorf("the run made %d turns, want 1", res.Turns)
	}

	names := sink.Names()
	// The system message, the user's prompt, the model's answer and the
	// settle record.
	want := []string{"run.started", "system_prompt", "message", "message", "message", "run.settled"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the records are %v, want %v", names, want)
	}
}

func TestARunThatCallsAToolExecutesIt(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				CallID:    "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
	if len(tool.Calls()) != 1 {
		t.Fatalf("the tool ran %d times, want 1", len(tool.Calls()))
	}
	if !strings.Contains(tool.Calls()[0], "hello") {
		t.Errorf("the tool received %q, want the call's arguments", tool.Calls()[0])
	}

	// The tool call and its result are both recorded.
	if sink.count("tool.call") != 1 {
		t.Errorf("the log holds %d tool.call records, want 1", sink.count("tool.call"))
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

func TestTheSecondRequestCarriesTheToolResult(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				CallID:    "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	})
	tool := &echoTool{result: "the echoed text"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	second := reqs[1].Messages
	// system, user, assistant (with its tool call), tool result
	if len(second) != 4 {
		t.Fatalf("the second request carried %d messages, want 4", len(second))
	}
	last := second[3]
	if last.Role != poo.RoleTool {
		t.Errorf("the last message's role is %q, want tool", last.Role)
	}
	if want := poo.HandleLine("r1", "echo") + "\n" + "the echoed text"; last.Content[0].Text != want {
		t.Errorf("the last message holds %q, want %q", last.Content[0].Text, want)
	}
	// The result carries the provider's call id, because that is the id
	// the provider matches it against.
	if last.Content[0].ToolCallID != "call_1" {
		t.Errorf("the result's tool call id is %q, want call_1", last.Content[0].ToolCallID)
	}
	// The assistant's tool call is in the history, so the provider can
	// match the result to it.
	assistant := second[2]
	if len(assistant.ToolCalls()) != 1 {
		t.Fatalf("the assistant message holds %d tool calls, want 1", len(assistant.ToolCalls()))
	}
	if assistant.ToolCalls()[0].ToolCallID != "call_1" {
		t.Errorf("the tool call's id is %q, want call_1", assistant.ToolCalls()[0].ToolCallID)
	}
}

func TestTheMaxTurnsCeilingSettlesMaxTurns(t *testing.T) {
	// Every response asks for another tool call, so the run only stops
	// at its ceiling.
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{
			CallID:    "call_1",
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"again"}`),
		}},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.MaxTurns = 3

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonMaxTurns {
		t.Errorf("the run settled %q, want max_turns", res.Reason)
	}
	if res.Turns != 3 {
		t.Errorf("the run made %d turns, want 3", res.Turns)
	}
	if len(tool.Calls()) != 3 {
		t.Errorf("the tool ran %d times, want 3", len(tool.Calls()))
	}
	// The turn ceiling is a candidate of its own.
	if res.Detail.Source != "max_turns" {
		t.Errorf("the settle source is %q, want max_turns", res.Detail.Source)
	}
}

func TestACancelledContextSettlesCancelled(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(ctx, "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
}

func TestAProviderErrorSettlesProviderError(t *testing.T) {
	tr := scripted([]fakeResponse{{
		err: &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the endpoint is down",
			Status:  500,
		},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if res.Detail.Provider == nil {
		t.Fatal("the settle detail has no provider information")
	}
	if res.Detail.Provider.Kind != string(poo.ProviderKindServer) {
		t.Errorf("the provider kind is %q, want server", res.Detail.Provider.Kind)
	}
}

func TestAContextLengthErrorSettlesContextExhausted(t *testing.T) {
	tr := scripted([]fakeResponse{{
		err: &poo.ProviderError{
			Kind:    poo.ProviderKindContextLength,
			Message: "the prompt is too long",
			Status:  400,
		},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
}

func TestATruncationRetryGivesTheModelAnotherTurn(t *testing.T) {
	// A turn the output limit cut short carries nothing the run can use. With
	// retries the run asks the model to say less and opens another turn, so a
	// model that overruns once can still finish the work.
	tr := scripted([]fakeResponse{
		{text: "a truncated ans", finish: poo.FinishLength},
		{text: "done"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.TruncationRetries = 1
	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn once the retry answered", res.Reason)
	}
	if n := len(tr.Requests()); n != 2 {
		t.Errorf("the run made %d requests, want 2: the truncated turn and its retry", n)
	}
}

func TestATruncationRetryStopsAtItsBound(t *testing.T) {
	// A model that keeps overrunning its own limit settles output_truncated
	// once the retries are spent, rather than looping forever.
	tr := scripted([]fakeResponse{
		{text: "a truncated ans", finish: poo.FinishLength},
		{text: "another truncat", finish: poo.FinishLength},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.TruncationRetries = 1
	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonOutputTruncated {
		t.Errorf("the run settled %q, want output_truncated after the retries", res.Reason)
	}
	// The settle is the retry's bound, not the natural stop a single cut
	// turn reaches.
	if res.Detail.Source != "truncated_turns" {
		t.Errorf("the settle source is %q, want truncated_turns", res.Detail.Source)
	}
}

// TestATruncatedTurnThatRanItsCallsKeepsGoing covers a truncated turn that
// carried a complete call. The call runs, so the turn continues to the gate. The
// run neither spends a retry on it nor tells the model its work was lost. Two
// such turns in a row used to settle output_truncated on the second.
func TestATruncatedTurnThatRanItsCallsKeepsGoing(t *testing.T) {
	call := poo.ToolCall{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{call}, finish: poo.FinishLength},
		{calls: []poo.ToolCall{call}, finish: poo.FinishLength},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.TruncationRetries = 1
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn: the calls ran", res.Reason, res.Detail.Source)
	}
	if n := len(tr.Requests()); n != 3 {
		t.Errorf("the run made %d requests, want 3: the truncated turns with calls ran and continued", n)
	}
	for i, req := range tr.Requests()[1:] {
		for _, msg := range req.Messages {
			if strings.Contains(msg.Text(), "reached the output limit") {
				t.Errorf("request %d tells the model %q, want no note about a response whose calls ran",
					i+1, msg.Text())
			}
		}
	}
}

// TestATruncationRetryStartsOverAfterAUsableTurn covers the count's reset. A
// turn the run could use ends the run of truncated turns, so a later
// truncation gets the session's retries again.
func TestATruncationRetryStartsOverAfterAUsableTurn(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "a truncated ans", finish: poo.FinishLength},
		{text: "done"},
		{text: "another truncat", finish: poo.FinishLength},
		{text: "done again"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.TruncationRetries = 1
	// The usable turn is a natural stop, so the settle hook sends the run
	// back to the model once. The second truncation is what the reset is
	// read from.
	opts.BeforeSettle = []poo.BeforeSettleHook{&continuingBeforeSettle{stopAfter: 1}}
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn: the second truncation got its retry",
			res.Reason, res.Detail.Source)
	}
	if n := len(tr.Requests()); n != 4 {
		t.Errorf("the run made %d requests, want 4: the usable turn reset the truncated count", n)
	}
}

// TestATruncatedTurnThatIsNotRetriedEndsTheRunOfUnusableTurns covers the reset
// with the retry off. The truncated turn carried usable text and the run was
// not going to retry it, so it ends the run of unusable turns. The empty turn
// before it and the two after it are then two runs, not three in a row.
func TestATruncatedTurnThatIsNotRetriedEndsTheRunOfUnusableTurns(t *testing.T) {
	tr := scripted([]fakeResponse{
		// One empty turn, so there is a run of unusable turns to carry.
		{finish: poo.FinishToolCalls},
		// The truncated turn carries usable text and is not retried: it is
		// a usable turn for the counts.
		{text: "a truncated ans", finish: poo.FinishLength},
		// The settle hook sends the run back once, so the counts are read
		// after the truncated turn.
		{finish: poo.FinishToolCalls},
		{finish: poo.FinishToolCalls},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{&continuingBeforeSettle{stopAfter: 1}}
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonInvalidTurns {
		t.Fatalf("the run settled %q (%s), want invalid_turns", res.Reason, res.Detail.Source)
	}
	if res.Detail.Source != "empty_turns" {
		t.Errorf("the settle source is %q, want empty_turns", res.Detail.Source)
	}
	if n := len(tr.Requests()); n != 4 {
		t.Errorf("the run made %d requests, want 4: the truncated turn reset the count", n)
	}
}

// TestATruncationRetryCountsOnlyTurnsInARow covers the count's reset against
// another unusable turn. The empty turn between the two truncations ends the
// run the settle names, so the second truncation gets its retry instead of
// spending a bound the log would then describe as "in a row".
func TestATruncationRetryCountsOnlyTurnsInARow(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "a truncated ans", finish: poo.FinishLength},
		// An empty turn is unusable for another reason, so it breaks the run
		// of truncated turns.
		{finish: poo.FinishToolCalls},
		{text: "another truncat", finish: poo.FinishLength},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.TruncationRetries = 1
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn: the second truncation started a new run",
			res.Reason, res.Detail.Source)
	}
	if n := len(tr.Requests()); n != 4 {
		t.Errorf("the run made %d requests, want 4: the empty turn ended the run of truncations", n)
	}
}

// TestATruncatedTurnKeepsItsParseFailureNote covers a response the provider cut
// at the output limit that also carries a failure the parser reported. The
// model wrote a call as text on the turn it overran. The parse failure is the
// note the model can act on, so it is the one the turn carries. The turn counts
// against the unusable bound either way.
func TestATruncatedTurnKeepsItsParseFailureNote(t *testing.T) {
	for _, retries := range []int{0, 1} {
		t.Run(fmt.Sprintf("retries=%d", retries), func(t *testing.T) {
			const note = "the call did not parse"
			tr := &itemScriptTransport{scripts: [][]poo.StreamItem{
				{
					{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: `echo {"text":"hi"}`}},
					{Kind: "error", Error: &poo.StreamError{Message: note, Recoverable: true}},
					{Kind: "finish", FinishReason: poo.FinishLength},
				},
				{
					{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "done"}},
					{Kind: "finish", FinishReason: poo.FinishStop},
				},
			}}
			sink := &recordingSink{}
			opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
			opts.TruncationRetries = retries
			s := newTestSession(t, opts)

			res := runSession(t, s, "do the thing")
			if res.Reason != poo.ReasonEndTurn {
				t.Fatalf("the run settled %q (%s), want end_turn: the parse failure is what the model hears",
					res.Reason, res.Detail.Source)
			}
			if n := tr.Requests(); n != 2 {
				t.Fatalf("the run made %d requests, want 2: the failed turn and the answer", n)
			}
			// The model is told about the parse failure, and not to say less:
			// the retry note would replace the diagnosis it can act on.
			told := tr.RequestText(1)
			if !strings.Contains(told, note) {
				t.Errorf("the next request carries %q, want the parse failure %q", told, note)
			}
			if strings.Contains(told, "reached the output limit") {
				t.Errorf("the next request carries the truncation retry's note as well: %q", told)
			}
		})
	}
}

// itemScriptTransport answers each request with the next script of stream
// items.
//
// A response the core reads can carry the provider's own stop reason and a
// failure the parser reported. No one provider's shape produces both, so the
// scripts let a test write one.
type itemScriptTransport struct {
	// scripts are the stream items each request is answered with.
	scripts [][]poo.StreamItem
	mu      sync.Mutex
	// served counts the requests the transport answered.
	served int
	// requests are the requests the transport answered, in order.
	requests []poo.ChatRequest
}

// Stream answers the request with the next script.
func (t *itemScriptTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.mu.Lock()
	t.requests = append(t.requests, req)
	n := t.served
	t.served++
	t.mu.Unlock()
	rendered := poo.Rendered{API: poo.APIChatCompletions}
	if n >= len(t.scripts) {
		return nil, rendered, pootest.ErrScriptExhausted
	}
	return &fakeReader{events: t.scripts[n]}, rendered, nil
}

// Fork is not used by these tests.
func (t *itemScriptTransport) Fork(context.Context, poo.Rendered, []poo.Message, int, poo.ForkOptions) (poo.StreamReader, error) {
	return nil, errNoFork
}

// Requests returns how many requests the transport answered.
func (t *itemScriptTransport) Requests() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

// RequestText returns the text of every message the nth request carried.
func (t *itemScriptTransport) RequestText(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n >= len(t.requests) {
		return ""
	}
	var b strings.Builder
	for _, msg := range t.requests[n].Messages {
		b.WriteString(msg.Text())
		b.WriteString("\n")
	}
	return b.String()
}

func TestAFinishReasonOfLengthReachesBeforeSettle(t *testing.T) {
	// A truncated response is a natural stop, and the
	// `output_truncated` reason is what it settles as.
	tr := scripted([]fakeResponse{{
		text:   "a truncated ans",
		finish: poo.FinishLength,
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonOutputTruncated {
		t.Errorf("the run settled %q, want output_truncated", res.Reason)
	}
}

// unlimitedTransport renders every request without an output limit, the
// way a transport for an endpoint that takes none does.
type unlimitedTransport struct {
	*pootest.Transport
}

func (t unlimitedTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	reader, rendered, err := t.Transport.Stream(ctx, req)
	rendered.MaxTokens = 0
	return reader, rendered, err
}

func TestATruncatedRunSaysWhichLimitItHit(t *testing.T) {
	settingsModel := map[string]string{"model_id": poo.LayerSettings}
	discoveredModel := map[string]string{"model_id": "discovery"}
	tests := []struct {
		name  string
		setup func(m *poo.ModelInfo)
		// unlimited makes the transport render the request with no
		// output limit.
		unlimited bool
		// source is the settle's source.
		source string
		// want is the text the settle carries, built from the max_tokens
		// the transport rendered.
		want func(sent int) string
	}{
		{
			name:   "the default for a settings model that declares no max_output",
			setup:  func(m *poo.ModelInfo) { m.Source = settingsModel },
			source: "output_truncated:default",
			want: func(sent int) string {
				return fmt.Sprintf("the response stopped at max_tokens %d, poo's default for a model with no max_output; set models.glm-flash.max_output to raise it", sent)
			},
		},
		{
			name:   "the default for a model the settings do not declare",
			setup:  func(m *poo.ModelInfo) { m.Source = discoveredModel },
			source: "output_truncated:default",
			want: func(sent int) string {
				return fmt.Sprintf("the response stopped at max_tokens %d, poo's default for a model with no max_output; declare the model in settings with its endpoint, model_id and max_output to raise it", sent)
			},
		},
		{
			name: "the model's own max_output",
			setup: func(m *poo.ModelInfo) {
				m.Source = settingsModel
				m.MaxOutput = 512
			},
			source: "output_truncated:model",
			want: func(sent int) string {
				return "the response stopped at max_tokens 512, the model's max_output"
			},
		},
		{
			name: "the room the context window leaves",
			setup: func(m *poo.ModelInfo) {
				m.ContextLimit = 4096
			},
			source: "output_truncated:context",
			want: func(sent int) string {
				used := 4096 - sent - poo.OutputReserve
				return fmt.Sprintf("the response stopped at max_tokens %d, all the room the context window had left (about %d of 4096 tokens in use)", sent, used)
			},
		},
		{
			name:      "an endpoint that takes no output limit",
			setup:     func(m *poo.ModelInfo) { m.Source = settingsModel },
			unlimited: true,
			source:    "output_truncated:endpoint",
			want: func(sent int) string {
				return "the response stopped on the endpoint's own output limit; poo sends this endpoint none"
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "a truncated ans", finish: poo.FinishLength}})
			opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
			if tc.unlimited {
				opts.Transport = unlimitedTransport{tr}
			}
			opts.Model.Name = "glm-flash"
			opts.Model.ContextLimit = 262144
			opts.Model.MaxOutput = 0
			tc.setup(&opts.Model)

			s := newTestSession(t, opts)
			res := runSession(t, s, "a short prompt")
			if res.Reason != poo.ReasonOutputTruncated {
				t.Fatalf("the run settled %q, want output_truncated", res.Reason)
			}
			if res.Detail.Source != tc.source {
				t.Errorf("the settle source is %q, want %q", res.Detail.Source, tc.source)
			}
			// The transport's own rendering is the limit the request
			// carried: the explanation names what went out.
			sent := tr.Rendered()[0].MaxTokens
			if want := tc.want(sent); res.Detail.Text != want {
				t.Errorf("the settle text is\n  %q\nwant\n  %q", res.Detail.Text, want)
			}
		})
	}
}

func TestNoOutputRoomSettlesContextExhaustedWithoutSending(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	// A context limit this small leaves less than 256 tokens of room
	// once the prompt is counted.
	opts.Model.ContextLimit = 300

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
	if len(tr.Requests()) != 0 {
		t.Errorf("the transport got %d requests, want none", len(tr.Requests()))
	}
}

func TestTheMaxTokensIsComputedFromTheContext(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Model.ContextLimit = 4096
	opts.Model.MaxOutput = 0

	s := newTestSession(t, opts)
	runSession(t, s, "a short prompt")
	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	mt := reqs[0].MaxTokens
	// The room is the limit minus the estimated input minus 256, and the
	// model's own ceiling is 8192 for a window this small. The estimate
	// covers the history at request time: the raw system message ("the system
	// prompt") is sent verbatim, the user message is "a short prompt",
	// and the estimate counts 3.5 chars per token, so 41*10/35 = 11.
	estimate := (len("system") + len("the system prompt") + len("user") + len("a short prompt")) * 10 / 35
	want := 4096 - estimate - 256
	if mt != want {
		t.Errorf("max_tokens is %d, want %d (context limit minus the %d-token estimate minus the 256-token reserve)", mt, want, estimate)
	}
}

func TestTheMaxOutputCapsTheMaxTokens(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Model.ContextLimit = 262144
	opts.Model.MaxOutput = 512

	s := newTestSession(t, opts)
	runSession(t, s, "a short prompt")
	if got := tr.Requests()[0].MaxTokens; got != 512 {
		t.Errorf("max_tokens is %d, want 512", got)
	}
}

func TestAModelWithNoCeilingGetsAnOutputLimitThatScalesWithItsWindow(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Model.ContextLimit = 262144
	opts.Model.MaxOutput = 0

	s := newTestSession(t, opts)
	runSession(t, s, "a short prompt")
	if got := tr.Requests()[0].MaxTokens; got != 32768 {
		t.Errorf("max_tokens is %d, want 32768", got)
	}
}

func TestAnUnknownToolIsARecoverableError(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				CallID:    "call_1",
				Name:      "nope",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{text: "finished"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

func TestTheSessionMovesThroughTheLoopStates(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				CallID:    "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if s.State() != poo.StateIdle {
		t.Errorf("a new session is in %q, want idle", s.State())
	}
	runSession(t, s, "do the thing")
	if s.State() != poo.StateSettled {
		t.Errorf("a finished session is in %q, want settled", s.State())
	}
}

func TestTheContextRunningOutMidRunSettlesContextExhausted(t *testing.T) {
	// The model asks for a call whose result is large, so the next
	// request has no room left.
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{
			CallID:    "call_1",
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"hello"}`),
		}},
	}})
	tool := &echoTool{result: string(make([]byte, 20000))}
	sink := &recordingSink{}

	opts := testOptions(t, tr, tool, sink)
	opts.Model.ContextLimit = 2000

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
	// The first request went out, and the second did not.
	if len(tr.Requests()) != 1 {
		t.Errorf("the transport got %d requests, want 1", len(tr.Requests()))
	}
}

func TestTheSettleRecordCarriesTheReason(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")
	records := sink.allRecords()
	last := records[len(records)-1]
	settled, ok := last.(*poo.RunSettledRecord)
	if !ok {
		t.Fatalf("the last record is %T, want a run.settled", last)
	}
	if settled.Reason != poo.ReasonEndTurn {
		t.Errorf("the settle reason is %q, want end_turn", settled.Reason)
	}
	if settled.RunID == "" {
		t.Error("the settle record has no run id")
	}
	if settled.Seq == 0 {
		t.Error("the settle record has no sequence number")
	}
	if settled.TS == "" {
		t.Error("the settle record has no timestamp")
	}
}

func TestMalformedArgumentsAreRecordedAsText(t *testing.T) {
	// The model sent arguments that are not valid JSON. The record
	// still holds valid JSON, so a consumer can decode it without a
	// special case, and the raw text is preserved.
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				CallID:    "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text": broken`),
			}},
		},
		{text: "finished"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")

	var call *poo.ToolCallRecord
	for _, rec := range sink.allRecords() {
		if c, ok := rec.(*poo.ToolCallRecord); ok {
			call = c
		}
	}
	if call == nil {
		t.Fatal("the log holds no tool.call record")
	}
	if !json.Valid(call.Arguments) {
		t.Fatalf("the recorded arguments are not valid JSON: %s", call.Arguments)
	}
	var text string
	if err := json.Unmarshal(call.Arguments, &text); err != nil {
		t.Fatalf("the recorded arguments are not a JSON string: %s", call.Arguments)
	}
	if text != `{"text": broken` {
		t.Errorf("the recorded arguments are %q, want the raw text", text)
	}
}

func TestASessionWithoutATransportIsAConstructionError(t *testing.T) {
	_, err := poo.New(poo.Options{Model: poo.ModelInfo{ContextLimit: 1024}})
	if err == nil {
		t.Fatal("a session with no transport was built")
	}
	var init *poo.ErrSessionInit
	if !asSessionInit(err, &init) {
		t.Fatalf("the error is %T, want an ErrSessionInit", err)
	}
}

// TestASideTransportWithoutItsModelIsAConstructionError covers the
// side model's pairing: a transport with no model to ask, or a model
// with no transport to ask, is a construction error, not a session
// that half-configures its side requests.
func TestASideTransportWithoutItsModelIsAConstructionError(t *testing.T) {
	for _, opts := range []poo.Options{
		{
			Transport:     pootest.NewTransport(),
			Model:         poo.ModelInfo{ModelID: "m", ContextLimit: 1024},
			SideTransport: pootest.NewTransport(),
		},
		{
			Transport: pootest.NewTransport(),
			Model:     poo.ModelInfo{ModelID: "m", ContextLimit: 1024},
			SideModel: poo.ModelInfo{ModelID: "side", ContextLimit: 1024},
		},
	} {
		_, err := poo.New(opts)
		if err == nil {
			t.Fatal("a half-configured side model was built")
		}
		var init *poo.ErrSessionInit
		if !asSessionInit(err, &init) {
			t.Fatalf("the error is %T, want an ErrSessionInit", err)
		}
	}
}

// asSessionInit reports that the error is an ErrSessionInit.
func asSessionInit(err error, target **poo.ErrSessionInit) bool {
	for err != nil {
		if e, ok := err.(*poo.ErrSessionInit); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestTheSessionRefusesASecondPromptWhileARunIsActive(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "hello"},
	})
	// The tool holds the run until the second prompt has been refused, so
	// the run cannot settle first and make room for it.
	started, release := make(chan struct{}), make(chan struct{})
	tool := &echoTool{call: func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-release
		return poo.ToolResult{Text: "echoed"}, nil
	}}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if _, err := s.Prompt(context.Background(), "first"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	_, err := s.Prompt(context.Background(), "second")
	close(release)
	if err == nil {
		t.Fatal("the session accepted a second prompt")
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}
