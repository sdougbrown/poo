package codex_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/codex"
)

// The response side: what the stream reports.

// readUntilFailure drains a stream that fails.
func readUntilFailure(t *testing.T, reader poo.StreamReader) ([]poo.StreamItem, error) {
	t.Helper()
	var events []poo.StreamItem
	for {
		ev, err := reader.Next()
		if errors.Is(err, io.EOF) {
			t.Fatal("the stream ended cleanly, want a failure")
		}
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
}

// TestTheStreamReadsThinkingTextAndUsage covers the deltas: a reasoning
// summary, an answer, and the accounting the terminal event carried.
func TestTheStreamReadsThinkingTextAndUsage(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.created","response":{"status":"in_progress"}}`)+
			frame(`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`)+
			frame(`{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"weigh"}`)+
			frame(`{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"ing"}`)+
			frame(`{"type":"response.reasoning_summary_part.done","output_index":0}`)+
			frame(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","summary":[{"type":"summary_text","text":"weighing"}],"encrypted_content":"opaque"}}`)+
			frame(`{"type":"response.output_item.added","output_index":1,"item":{"type":"message"}}`)+
			frame(`{"type":"response.output_text.delta","output_index":1,"delta":"Hel"}`)+
			frame(`{"type":"response.output_text.delta","output_index":1,"delta":"lo"}`)+
			frame(`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","content":[{"type":"output_text","text":"Hello"}]}}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":100,"output_tokens":7,"input_tokens_details":{"cached_tokens":40}}}}`)+
			done,
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	if got := thinkingOf(events); got != "weighing\n\n" {
		t.Errorf("the thinking is %q, want the summary and the break between its parts", got)
	}
	if got := textOf(events); got != "Hello" {
		t.Errorf("the answer is %q, want Hello", got)
	}
	if got := finishOf(t, events); got != poo.FinishStop {
		t.Errorf("the stream finished with %q, want stop", got)
	}
	usage := usageOf(t, events)
	if usage.InputTokens != 100 {
		t.Errorf("the input count is %d, want the prompt the endpoint counted", usage.InputTokens)
	}
	if usage.OutputTokens != 7 {
		t.Errorf("the output count is %d, want 7", usage.OutputTokens)
	}
	if usage.CacheReadTokens == nil || *usage.CacheReadTokens != 40 {
		t.Errorf("the cache read count is %v, want 40", usage.CacheReadTokens)
	}
	for _, ev := range events {
		if ev.Attempt != 1 {
			t.Errorf("an event says attempt %d, want 1", ev.Attempt)
		}
	}
}

// TestAToolCallArrivesAssembled covers the fragments. The call is named by its
// first fragment, and its arguments are the fragments in order. A turn that
// called a tool finishes as one.
func TestAToolCallArrivesAssembled(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call_1","name":"bash"}}`)+
			frame(`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"comm"}`)+
			frame(`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"and\":\"ls\"}"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":4}}}`),
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	calls := callsOf(events)
	if len(calls) != 3 {
		t.Fatalf("the stream reported %d call fragments, want the announcement and the two deltas", len(calls))
	}
	if calls[0].ID != "call_1" || calls[0].Name != "bash" {
		t.Errorf("the first fragment names %q (%q), want the call the endpoint sent", calls[0].Name, calls[0].ID)
	}
	if calls[0].Origin != poo.CallOriginWire {
		t.Errorf("the call's origin is %q, want wire", calls[0].Origin)
	}
	assembled := ""
	for _, call := range calls {
		if call.Index != 2 {
			t.Errorf("a fragment is at index %d, want the output index", call.Index)
		}
		assembled += string(call.Arguments)
	}
	var arguments map[string]string
	if err := json.Unmarshal([]byte(assembled), &arguments); err != nil {
		t.Fatalf("the fragments assemble to %q, which is not JSON: %v", assembled, err)
	}
	if arguments["command"] != "ls" {
		t.Errorf("the assembled call runs %q, want ls", arguments["command"])
	}
	if got := finishOf(t, events); got != poo.FinishToolCalls {
		t.Errorf("the stream finished with %q, want tool_calls", got)
	}
}

// TestAnItemThatArrivesWholeIsRead covers a server that sends the
// finished item without its deltas.
func TestAnItemThatArrivesWholeIsRead(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","summary":[{"type":"summary_text","text":"a thought"}]}}`)+
			frame(`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","content":[{"type":"output_text","text":"the answer"}]}}`)+
			frame(`{"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","call_id":"call_2","name":"bash"}}`)+
			frame(`{"type":"response.completed","response":{"status":"completed"}}`),
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	if got := thinkingOf(events); got != "a thought" {
		t.Errorf("the thinking is %q, want the item's summary", got)
	}
	if got := textOf(events); got != "the answer" {
		t.Errorf("the answer is %q, want the item's text", got)
	}
	calls := callsOf(events)
	if len(calls) != 2 {
		t.Fatalf("the stream reported %d call fragments, want the call and its empty arguments", len(calls))
	}
	if calls[0].Name != "bash" || calls[0].ID != "call_2" {
		t.Errorf("the call is %q (%q), want the item's own", calls[0].Name, calls[0].ID)
	}
	if string(calls[1].Arguments) != "{}" {
		t.Errorf("the call's arguments are %q, want an empty object", calls[1].Arguments)
	}
}

// TestTheReasoningItemTravelsBackVerbatim covers both directions: the
// response's reasoning item is reported as an opaque block, and a
// history that carries it replays it unchanged.
func TestTheReasoningItemTravelsBackVerbatim(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.done","output_index":0,"item":`+reasoningItem+`}`)+
			frame(`{"type":"response.completed","response":{"status":"completed"}}`),
		completedStream,
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	reader.Close()

	reporter, ok := reader.(poo.ProviderBlockReporter)
	if !ok {
		t.Fatalf("the reader is not a provider block reporter, so the reasoning cannot be kept")
	}
	blocks := reporter.ReportProviderBlocks()
	if len(blocks) != 1 {
		t.Fatalf("the reader reports %d reasoning blocks, want one", len(blocks))
	}
	block := blocks[0]
	if block.API != poo.APIResponses || block.Type != "reasoning" {
		t.Errorf("the block is a %q %q block, want a Responses reasoning block", block.API, block.Type)
	}
	var got, want any
	_ = json.Unmarshal(block.Data, &got)
	_ = json.Unmarshal([]byte(reasoningItem), &want)
	if canonical(t, got) != canonical(t, want) {
		t.Errorf("the reported payload is %s, want the item verbatim %s", block.Data, reasoningItem)
	}

	// The next request carries the block beside its thinking, and the
	// endpoint receives the item it sent.
	history := []poo.Message{
		poo.NewTextMessage(poo.RoleUser, "hi"),
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{
			{Kind: poo.ContentThinking, Text: "weighing", Provider: &block},
			{Kind: poo.ContentText, Text: "there"},
		}},
		poo.NewTextMessage(poo.RoleUser, "carry on"),
	}
	next, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  history,
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the next request: %v", err)
	}
	defer next.Close()
	read(t, next)

	input := field[[]map[string]any](t, srv.request(t, 1), "input")
	if len(input) != 4 {
		t.Fatalf("the next request carries %d items, want the reasoning item between the two exchanges: %s",
			len(input), srv.request(t, 1).body)
	}
	expectJSON(t, input[1], reasoningItem)
}

// TestTheTerminalOutputAloneCarriesTheReasoningItem covers a response that
// never reports its reasoning item as an item of its own. The terminal output
// is the only place the item arrives, encrypted payload included.
func TestTheTerminalOutputAloneCarriesTheReasoningItem(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_text.delta","output_index":1,"delta":"the answer"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","output":[`+reasoningItem+
				`,{"type":"message","role":"assistant","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"the answer"}]}]}}`),
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	reader.Close()

	reporter, ok := reader.(poo.ProviderBlockReporter)
	if !ok {
		t.Fatalf("the reader is not a provider block reporter, so the reasoning cannot be kept")
	}
	blocks := reporter.ReportProviderBlocks()
	if len(blocks) != 1 {
		t.Fatalf("the reader reports %d reasoning blocks, want the terminal response's one", len(blocks))
	}
	var got, want any
	_ = json.Unmarshal(blocks[0].Data, &got)
	_ = json.Unmarshal([]byte(reasoningItem), &want)
	if canonical(t, got) != canonical(t, want) {
		t.Errorf("the reported payload is %s, want the terminal item verbatim %s", blocks[0].Data, reasoningItem)
	}
}

// TestATerminalEventNamesTheFailure covers the two failures a stream can
// report: an error event and a failed response.
func TestATerminalEventNamesTheFailure(t *testing.T) {
	cases := []struct {
		name     string
		stream   string
		wantKind poo.ProviderKind
		wantCode string
	}{
		{
			name:     "an error event that names the plan",
			stream:   frame(`{"type":"error","code":"usage_limit_reached","message":"out of budget"}`),
			wantKind: poo.ProviderKindUsageLimit,
			wantCode: "usage_limit_reached",
		},
		{
			name:     "an error event that names the failure in its own body",
			stream:   frame(`{"type":"error","error":{"code":"rate_limit_exceeded","message":"slow down"}}`),
			wantKind: poo.ProviderKindRateLimited,
			wantCode: "rate_limit_exceeded",
		},
		{
			name:     "an error event that names nothing",
			stream:   frame(`{"type":"error"}`),
			wantKind: poo.ProviderKindServer,
		},
		{
			name:     "a failed response",
			stream:   frame(`{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"the backend broke"}}}`),
			wantKind: poo.ProviderKindServer,
			wantCode: "server_error",
		},
		{
			name:     "a cancelled response",
			stream:   frame(`{"type":"response.failed","response":{"status":"cancelled"}}`),
			wantKind: poo.ProviderKindServer,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newServer(t, c.stream)
			tr := newTransport(srv.URL, func(o *codex.Options) { o.MaxAttempts = 1 })

			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
				Model:     "gpt-6.1-sol",
				Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
				MaxTokens: 64,
			})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()
			events, err := readUntilFailure(t, reader)

			var perr *poo.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("the stream failed with %v, want a provider failure", err)
			}
			if perr.Kind != c.wantKind {
				t.Errorf("the failure is %q, want %q", perr.Kind, c.wantKind)
			}
			if c.wantCode != "" && perr.Code != c.wantCode {
				t.Errorf("the failure's code is %q, want %q", perr.Code, c.wantCode)
			}
			if usageOf(t, events) == nil {
				t.Error("the stream reported no usage before its failure")
			}
		})
	}
}

// TestAnIncompleteResponseIsATruncationOrAFailure covers the two ways a
// response stops short: on its output limit, and for a reason the run
// cannot treat as an answer.
func TestAnIncompleteResponseIsATruncationOrAFailure(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":10,"output_tokens":99}}}`),
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 99,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)
	if got := finishOf(t, events); got != poo.FinishLength {
		t.Errorf("the stream finished with %q, want length", got)
	}
	if usage := usageOf(t, events); usage.OutputTokens != 99 {
		t.Errorf("the output count is %d, want 99", usage.OutputTokens)
	}

	blocked := newServer(t,
		frame(`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"content_filter"}}}`),
	)
	failing := newTransport(blocked.URL, func(o *codex.Options) { o.MaxAttempts = 1 })
	blockedReader, _, err := failing.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 99,
	})
	if err != nil {
		t.Fatalf("starting the second request: %v", err)
	}
	defer blockedReader.Close()
	_, err = readUntilFailure(t, blockedReader)
	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Code != "content_filter" {
		t.Fatalf("the stream failed with %v, want the reason the response gave", err)
	}
}

// TestAStreamThatEndsEarlyIsAFailure covers a body that runs out: after a
// delta the request cannot be repeated, and before one it is retried
// within the budget.
func TestAStreamThatEndsEarlyIsAFailure(t *testing.T) {
	partial := newServer(t, frame(`{"type":"response.output_text.delta","output_index":0,"delta":"half an ans"}`))
	tr := newTransport(partial.URL, func(o *codex.Options) { o.MaxAttempts = 3 })

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	_, failure := readUntilFailure(t, reader)
	var perr *poo.ProviderError
	if !errors.As(failure, &perr) || perr.Kind != poo.ProviderKindInterruptedStream {
		t.Fatalf("the stream failed with %v, want an interrupted stream", failure)
	}
	if n := partial.count(); n != 1 {
		t.Errorf("the endpoint received %d requests, want none after the answer had started", n)
	}

	silent := newServer(t, frame(`{"type":"response.created","response":{"status":"in_progress"}}`))
	failing := newTransport(silent.URL, func(o *codex.Options) {
		o.MaxAttempts = 3
		o.Backoff = []time.Duration{time.Millisecond}
	})
	retrying, _, err := failing.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the second request: %v", err)
	}
	defer retrying.Close()
	_, failure = readUntilFailure(t, retrying)
	if !errors.As(failure, &perr) || perr.Kind != poo.ProviderKindServer {
		t.Fatalf("the stream failed with %v, want a server failure", failure)
	}
	if n := silent.count(); n != 3 {
		t.Errorf("the endpoint received %d requests, want the whole retry budget", n)
	}
}

// TestAFailureBeforeTheFirstDeltaIsRetried covers the retry: the request
// is asked again, and every attempt is reported. The wait the endpoint
// asked for is not the plan's own backoff, so the attempt's delay says
// which one the request waited.
func TestAFailureBeforeTheFirstDeltaIsRetried(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After-Ms", "25")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"the backend broke"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, completedStream)
	}))
	defer srv.Close()

	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.MaxAttempts = 2
		o.Backoff = []time.Duration{time.Millisecond}
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
	events := read(t, reader)

	if attempts != 2 {
		t.Errorf("the endpoint received %d requests, want the retry", attempts)
	}
	if len(reported) != 1 {
		t.Fatalf("the request reported %d attempts, want the one that failed", len(reported))
	}
	if reported[0].Number != 1 || !reported[0].Retried {
		t.Errorf("the attempt is numbered %d and retried=%v, want attempt 1 that was retried",
			reported[0].Number, reported[0].Retried)
	}
	if reported[0].Delay != 25*time.Millisecond {
		t.Errorf("the attempt waits %v, want the endpoint's Retry-After of 25ms", reported[0].Delay)
	}
	var perr *poo.ProviderError
	if !errors.As(reported[0].Err, &perr) || perr.Kind != poo.ProviderKindServer {
		t.Errorf("the attempt failed with %v, want a server failure", reported[0].Err)
	}
	for _, ev := range events {
		if ev.Attempt != 2 {
			t.Errorf("an event says attempt %d, want the attempt that answered", ev.Attempt)
		}
	}
}

// TestARateLimitCarriesTheWaitItAskedFor covers the failure the endpoint
// answers before any stream: the status names the kind, and the wait it
// asked for travels with it.
func TestARateLimitCarriesTheWaitItAskedFor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":"rate_limit_exceeded","message":"slow down","resets_at":1893456000}}`)
	}))
	defer srv.Close()

	tr := newTransport(srv.URL, func(o *codex.Options) { o.MaxAttempts = 1 })
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the request failed with %v, want a provider failure", err)
	}
	if perr.Kind != poo.ProviderKindRateLimited {
		t.Errorf("the failure is %q, want a rate limit", perr.Kind)
	}
	if perr.RetryAfter != 7*time.Second {
		t.Errorf("the failure asks for a wait of %v, want 7s", perr.RetryAfter)
	}
	if perr.ResetsAt.Unix() != 1893456000 {
		t.Errorf("the failure resets at %v, want the body's own reset time", perr.ResetsAt)
	}
}

// TestCancellingTheRunEndsTheStream covers the cancellation: the run's
// context ends the stream with its own failure, not a network one.
func TestCancellingTheRunEndsTheStream(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frame(`{"type":"response.output_text.delta","output_index":0,"delta":"half"}`))
		w.(http.Flusher).Flush()
		close(blocked)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := newTransport(srv.URL)
	reader, _, err := tr.Stream(ctx, poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	first, err := reader.Next()
	if err != nil {
		t.Fatalf("reading the first event: %v", err)
	}
	if first.Kind != "delta" {
		t.Fatalf("the first event is %q, want the delta", first.Kind)
	}

	<-blocked
	cancel()

	if _, err := readUntilFailure(t, reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the run's cancellation", err)
	}
}

// TestAnUnknownEventIsSkipped covers a payload the transport cannot read:
// it is not worth failing the run over, and the answer after it still
// arrives.
func TestAnUnknownEventIsSkipped(t *testing.T) {
	srv := newServer(t,
		frame(`not json at all`)+
			frame(`{"type":"a.type.from.the.future","delta":"ignored"}`)+
			frame(`{"type":"response.output_text.delta","output_index":0,"delta":"the answer"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed"}}`),
	)
	tr := newTransport(srv.URL)
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)
	if got := textOf(events); !strings.Contains(got, "the answer") {
		t.Errorf("the answer is %q, want the event after the unknown ones", got)
	}
}

// TestAFrameTheTransportCannotReadDoesNotPoisonTheStream covers a server that
// separates its events with a single newline. A frame that is not JSON is
// dropped rather than carried. The events after it still arrive, and the stream
// still names its end.
func TestAFrameTheTransportCannotReadDoesNotPoisonTheStream(t *testing.T) {
	srv := newServer(t,
		// Every event on its own line, and no blank line between them.
		"data: not json at all\n"+
			`data: {"type":"response.output_text.delta","output_index":0,"delta":"the answer"}`+"\n"+
			`data: {"type":"response.completed","response":{"status":"completed"}}`+"\n",
	)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.MaxAttempts = 1 })
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	if got := textOf(events); got != "the answer" {
		t.Errorf("the answer is %q, want the event after the unreadable frame", got)
	}
	if got := finishOf(t, events); got != poo.FinishStop {
		t.Errorf("the stream finished with %q, want stop", got)
	}
	if n := srv.count(); n != 1 {
		t.Errorf("the endpoint received %d requests, want the one that answered", n)
	}
}

// TestAMultiLineFrameIsJoinedWithoutABlankLine covers an event whose payload is
// spread over several data lines with no blank line between the events. The
// lines are joined, because another line may still complete a payload that has
// not ended.
func TestAMultiLineFrameIsJoinedWithoutABlankLine(t *testing.T) {
	srv := newServer(t,
		`data: {"type":"response.output_text.delta",`+"\n"+
			`data: "output_index":0,"delta":"split"}`+"\n"+
			`data: {"type":"response.completed","response":{"status":"completed"}}`+"\n",
	)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.MaxAttempts = 1 })
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	if got := textOf(events); got != "split" {
		t.Errorf("the answer is %q, want the payload the two lines hold", got)
	}
	if got := finishOf(t, events); got != poo.FinishStop {
		t.Errorf("the stream finished with %q, want stop", got)
	}
}

// TestAFinishedCallAtATakenIndexIsRead covers an event that names no output
// index, so it lands on the one another item took. The call is still a call,
// and the thinking before it is not lost or overwritten.
func TestAFinishedCallAtATakenIndexIsRead(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`)+
			frame(`{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"think"}`)+
			frame(`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{}"}}`)+
			frame(`{"type":"response.completed","response":{"status":"completed"}}`),
	)
	tr := newTransport(srv.URL)
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	if got := thinkingOf(events); got != "think" {
		t.Errorf("the thinking is %q, want the reasoning the response streamed", got)
	}
	calls := callsOf(events)
	if len(calls) == 0 {
		t.Fatalf("the stream reported no call, want the finished one: %v", events)
	}
	assembled := ""
	for _, call := range calls {
		assembled += string(call.Arguments)
	}
	if calls[0].Name != "bash" || calls[0].ID != "call_1" {
		t.Errorf("the call is %q (%q), want the one the item named", calls[0].Name, calls[0].ID)
	}
	if assembled != "{}" {
		t.Errorf("the call's arguments are %q, want the item's own", assembled)
	}
	if got := finishOf(t, events); got != poo.FinishToolCalls {
		t.Errorf("the stream finished with %q, want tool_calls", got)
	}
}

// TestACompletedEventWithoutAResponseEndsTheStream covers a terminal event the
// endpoint sends without the response object. The stream ended, so the run gets
// its finish and its usage rather than a protocol error.
func TestACompletedEventWithoutAResponseEndsTheStream(t *testing.T) {
	srv := newServer(t, frame(`{"type":"response.completed"}`)+done)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.MaxAttempts = 1 })
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	if got := finishOf(t, events); got != poo.FinishStop {
		t.Errorf("the stream finished with %q, want stop", got)
	}
	if usageOf(t, events) == nil {
		t.Error("the stream reported no usage")
	}
	if n := srv.count(); n != 1 {
		t.Errorf("the endpoint received %d requests, want the one that answered", n)
	}
}

// TestTheTimeoutBoundsTheWaitForTheFirstByte covers an endpoint that sends its
// headers and then nothing. The transport's own bound ends the wait, so the run
// is not held by a body that never starts.
func TestTheTimeoutBoundsTheWaitForTheFirstByte(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// The body starts well after the transport's own bound.
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, completedStream)
	}))
	defer srv.Close()

	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.Timeout = 50 * time.Millisecond
		o.MaxAttempts = 1
	})
	started := time.Now()
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	elapsed := time.Since(started)

	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindNetwork {
		t.Fatalf("the request failed with %v, want a network failure", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("the request ended after %v, want the transport's own bound to end the wait", elapsed)
	}
}

// TestTheUsageReportsTheWholePrompt covers the counts poo's usage record
// carries. The prompt count is the endpoint's own. The cached tokens are the
// ones it served from its cache, counted beside it rather than taken out of it.
func TestTheUsageReportsTheWholePrompt(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_text.delta","output_index":0,"delta":"hi"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":`+
				`{"input_tokens":100,"output_tokens":7,"total_tokens":107,`+
				`"input_tokens_details":{"cached_tokens":40,"cache_write_tokens":10}}}}`),
	)
	tr := newTransport(srv.URL)
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	usage := usageOf(t, read(t, reader))

	if usage.InputTokens != 100 {
		t.Errorf("the input count is %d, want the prompt count the endpoint reported", usage.InputTokens)
	}
	if usage.OutputTokens != 7 {
		t.Errorf("the output count is %d, want 7", usage.OutputTokens)
	}
	if usage.CacheReadTokens == nil || *usage.CacheReadTokens != 40 {
		t.Errorf("the cache read count is %v, want the 40 the endpoint served from its cache", usage.CacheReadTokens)
	}
}

// TestAnInterruptedStreamKeepsOnlyReplayableReasoning covers the reasoning a
// cut-short response produced. An item without the payload that lets the
// endpoint take it back is not replayed. The next request would carry an item
// the endpoint cannot match.
func TestAnInterruptedStreamKeepsOnlyReplayableReasoning(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"think"}`)+
			frame(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1",`+
				`"summary":[{"type":"summary_text","text":"think"}]}}`)+
			frame(`{"type":"response.output_text.delta","output_index":1,"delta":"half an ans"}`),
	)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.MaxAttempts = 1 })
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events, failure := readUntilFailure(t, reader)

	var perr *poo.ProviderError
	if !errors.As(failure, &perr) || perr.Kind != poo.ProviderKindInterruptedStream {
		t.Fatalf("the stream failed with %v, want an interrupted stream", failure)
	}
	if got := thinkingOf(events); got != "think" {
		t.Errorf("the thinking is %q, want what the response streamed", got)
	}
	if got := textOf(events); got != "half an ans" {
		t.Errorf("the answer is %q, want what the response streamed", got)
	}
	reporter, ok := reader.(interface{ ReportProviderBlocks() []poo.ProviderBlock })
	if !ok {
		t.Fatal("the reader reports no provider blocks")
	}
	if blocks := reporter.ReportProviderBlocks(); len(blocks) != 0 {
		t.Errorf("the reader reports %d blocks, want none the endpoint could not take back: %s",
			len(blocks), blocks[0].Data)
	}

	// The same cut-short response whose item does carry the payload is
	// replayed: the endpoint can match it.
	replayable := newServer(t,
		frame(`{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"think"}`)+
			frame(`{"type":"response.output_item.done","output_index":0,"item":`+reasoningItem+`}`)+
			frame(`{"type":"response.output_text.delta","output_index":1,"delta":"half an ans"}`),
	)
	keeping := newTransport(replayable.URL, func(o *codex.Options) { o.MaxAttempts = 1 })
	keepingReader, _, err := keeping.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the second request: %v", err)
	}
	defer keepingReader.Close()
	_, _ = readUntilFailure(t, keepingReader)
	if blocks := keepingReader.(interface{ ReportProviderBlocks() []poo.ProviderBlock }).ReportProviderBlocks(); len(blocks) != 1 {
		t.Fatalf("the reader reports %d blocks, want the reasoning item it can replay", len(blocks))
	}
}

// TestTheFinishedCallCarriesOnlyTheArgumentsTheDeltasMissed covers the event
// that repeats a call's whole argument string. The core assembles a call by
// appending, so only the part it has not seen is emitted, and a string that
// disagrees with the fragments is left alone rather than replacing what the
// core already holds.
func TestTheFinishedCallCarriesOnlyTheArgumentsTheDeltasMissed(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call_1","name":"bash"}}`)+
			frame(`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"comm"}`)+
			// The endpoint repeats the whole string: only the tail is new.
			frame(`{"type":"response.function_call_arguments.done","output_index":2,"arguments":"{\"command\":\"ls\"}"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":4}}}`),
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	var arguments string
	complete := false
	for _, ev := range events {
		if ev.ToolCall == nil {
			continue
		}
		arguments += string(ev.ToolCall.Arguments)
		complete = complete || ev.ToolCall.Complete
	}
	if arguments != `{"command":"ls"}` {
		t.Errorf("the call's arguments are %q, want the fragments and the tail once", arguments)
	}
	if !complete {
		t.Error("the call never became complete, so the core cannot run it")
	}
}

// TestAFinishedCallThatDisagreesIsLeftAlone covers the string that is not the
// fragments plus a tail. Replacing what the core holds is not possible, so the
// disagreement is ignored rather than emitted as a second call.
func TestAFinishedCallThatDisagreesIsLeftAlone(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call_1","name":"bash"}}`)+
			frame(`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"command\":\"ls\"}"}`)+
			frame(`{"type":"response.function_call_arguments.done","output_index":2,"arguments":"{\"command\":\"rm -rf /\"}"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":4}}}`),
	)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	events := read(t, reader)

	var arguments string
	for _, ev := range events {
		if ev.ToolCall != nil {
			arguments += string(ev.ToolCall.Arguments)
		}
	}
	if arguments != `{"command":"ls"}` {
		t.Errorf("the call's arguments are %q, want the fragments the core already assembled", arguments)
	}
}
