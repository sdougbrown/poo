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

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/codex"
)

// The compaction side: what the endpoint receives, and the window it answers
// with.

// summaryItem is a message item a compaction response may carry beside the
// compacted state.
func summaryItem(text string) string {
	return `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + text + `"}]}`
}

// compactionStream renders the stream of a compaction response whose output
// carries the items.
func compactionStream(items ...string) string {
	return frame(`{"type":"response.completed","response":{"status":"completed","output":[`+strings.Join(items, ",")+`]}}`) + done
}

// compactView is a history a compaction encodes: the system prompt, a user
// message, a reply and the tool result the last request could not carry.
func compactView() poo.View {
	return poo.NewHistoryView([]poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "you are terse"),
		poo.NewTextMessage(poo.RoleUser, "list the files"),
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{{Kind: poo.ContentText, Text: "sure"}}},
		poo.ToolResultMessage("call_1", "bash", "a.txt", false, nil),
	}, nil, nil)
}

// compact asks a transport's native compaction capability for the window of a
// view. The capability is what the core asks for, so the tests ask for it the
// same way.
func compact(t *testing.T, compactor poo.NativeCompactor, view poo.View) (poo.ProviderWindow, error) {
	t.Helper()
	res, err := compactResult(t, compactor, view)
	return res.Window, err
}

// compactResult asks a transport's native compaction capability for
// everything a compaction produced.
func compactResult(t *testing.T, compactor poo.NativeCompactor, view poo.View) (poo.NativeCompactionResult, error) {
	t.Helper()
	return compactor.Compact(context.Background(), poo.NativeCompaction{
		Model: poo.ModelInfo{ModelID: "gpt-6.1-sol"},
		View:  view,
	})
}

// wantCompactionFailure reports that a compaction whose answer cannot be
// installed failed, and returned no window.
func wantCompactionFailure(t *testing.T, window poo.ProviderWindow, err error) error {
	t.Helper()
	if err == nil {
		t.Fatalf("the compaction returned %+v, want a failure", window)
	}
	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindServer {
		t.Fatalf("the compaction failed with %v, want a server failure", err)
	}
	if len(window.Items) != 0 {
		t.Errorf("the failed compaction returned %+v, want no window", window)
	}
	return err
}

// TestACompactionRequestCarriesTheCurrentViewAndTheTrigger covers the request.
// The view is encoded as the run's history, not the last request's snapshot.
// The trigger follows the view's last item, and the request authenticates the
// way every other request this transport makes does.
func TestACompactionRequestCarriesTheCurrentViewAndTheTrigger(t *testing.T) {
	srv := newServer(t, compactionStream(compactionItem))
	tr := newTransport(srv.URL, func(o *codex.Options) { o.SessionID = "s1" })

	window, err := compact(t, tr, compactView())
	if err != nil {
		t.Fatalf("compacting the view: %v", err)
	}
	if n := srv.count(); n != 1 {
		t.Fatalf("the endpoint received %d requests, want one", n)
	}
	req := srv.request(t, 0)
	if req.path != "/codex/responses" {
		t.Errorf("the compaction went to %q, want the responses path", req.path)
	}
	if got := field[string](t, req, "instructions"); got != "you are terse" {
		t.Errorf("the instructions are %q, want the view's system message", got)
	}
	expectJSON(t, field[[]json.RawMessage](t, req, "input"), `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"list the files"}]},
		{"type":"message","role":"assistant","id":"msg_poo_0","status":"completed","content":[{"type":"output_text","text":"sure","annotations":[]}]},
		{"type":"function_call_output","call_id":"call_1","output":"a.txt"},
		{"type":"compaction_trigger"}
	]`)
	if _, ok := req.fields["tools"]; ok {
		t.Errorf("the compaction request offers tools: %s", req.body)
	}
	if got := req.header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("the authorization is %q, want the resolved token", got)
	}
	if got := req.header.Get("chatgpt-account-id"); got != "acct-1" {
		t.Errorf("the account is %q, want the resolved account", got)
	}

	if len(window.Items) != 1 || window.Items[0].Provider == nil {
		t.Fatalf("the window holds %+v, want the compaction item", window.Items)
	}
	item := window.Items[0].Provider
	if item.API != poo.APIResponses || item.Type != "compaction" || !item.Required {
		t.Errorf("the window item is %+v, want a required Responses compaction block", item)
	}
	if string(item.Data) != compactionItem {
		t.Errorf("the payload is %s, want the response's item verbatim", item.Data)
	}
}

// TestACompactionWindowKeepsTheResponsesOrder covers the items beside the
// compacted state. The window is the response's items in the order the endpoint
// sent them, and the text it kept visible rides as text.
func TestACompactionWindowKeepsTheResponsesOrder(t *testing.T) {
	srv := newServer(t,
		compactionStream(summaryItem("the summary"), compactionItem),
		compactionStream(compactionItem, summaryItem("the retained text")),
	)
	tr := newTransport(srv.URL)

	first, err := compact(t, tr, compactView())
	if err != nil {
		t.Fatalf("compacting the view: %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("the window holds %d items, want the response's 2: %+v", len(first.Items), first.Items)
	}
	if first.Items[0].Kind != poo.ContentText || first.Items[0].Text != "the summary" {
		t.Errorf("the window's first item is %+v, want the summary text", first.Items[0])
	}
	if first.Items[1].Provider == nil || string(first.Items[1].Provider.Data) != compactionItem {
		t.Errorf("the window's second item is %+v, want the compaction item", first.Items[1])
	}

	second, err := compact(t, tr, compactView())
	if err != nil {
		t.Fatalf("compacting the view again: %v", err)
	}
	if len(second.Items) != 2 {
		t.Fatalf("the window holds %d items, want the response's 2: %+v", len(second.Items), second.Items)
	}
	if second.Items[0].Provider == nil || string(second.Items[0].Provider.Data) != compactionItem {
		t.Errorf("the window's first item is %+v, want the compaction item first", second.Items[0])
	}
	if second.Items[1].Kind != poo.ContentText || second.Items[1].Text != "the retained text" {
		t.Errorf("the window's second item is %+v, want the retained text", second.Items[1])
	}
}

// TestACompactionItemStreamedOnlyAsAnItemIsRead covers an endpoint that sends
// the item and reports no output with its terminal event: the item is still
// the window.
func TestACompactionItemStreamedOnlyAsAnItemIsRead(t *testing.T) {
	stream := frame(`{"type":"response.output_item.done","output_index":0,"item":`+compactionItem+`}`) +
		frame(`{"type":"response.completed","response":{"status":"completed"}}`) + done
	srv := newServer(t, stream)
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	if err != nil {
		t.Fatalf("compacting the view: %v", err)
	}
	if len(window.Items) != 1 || window.Items[0].Provider == nil {
		t.Fatalf("the window holds %+v, want the streamed compaction item", window.Items)
	}
	if string(window.Items[0].Provider.Data) != compactionItem {
		t.Errorf("the payload is %s, want the streamed item verbatim", window.Items[0].Provider.Data)
	}
}

// TestATruncatedCompactionResponseFails covers an answer the endpoint cut at
// its output limit. The state it carries is only the part the endpoint reached,
// so installing it would drop the context the compaction never got to.
func TestATruncatedCompactionResponseFails(t *testing.T) {
	stream := frame(`{"type":"response.output_item.done","output_index":0,"item":`+compactionItem+`}`) +
		frame(`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`) +
		done
	srv := newServer(t, stream)
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	wantCompactionFailure(t, window, err)
}

// TestAMalformedCompactionItemFails covers an item the transport cannot read.
// Dropping it would install the items around it as if the response held
// nothing else, so the window is refused instead.
func TestAMalformedCompactionItemFails(t *testing.T) {
	stream := frame(`{"type":"response.output_item.done","output_index":0,"item":`+compactionItem+`}`) +
		frame(`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","content":[{"type":"output_text","text":7}]}}`) +
		frame(`{"type":"response.completed","response":{"status":"completed"}}`) + done
	srv := newServer(t, stream)
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	wantCompactionFailure(t, window, err)
}

// TestACompactionThatFailsAfterAnItemHasNoWindow covers a stream that ends in a
// failure after it carried items. The response never named its answer whole,
// so there is no window to install.
func TestACompactionThatFailsAfterAnItemHasNoWindow(t *testing.T) {
	stream := frame(`{"type":"response.output_item.done","output_index":0,"item":`+compactionItem+`}`) +
		frame(`{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"the endpoint broke"}}}`) +
		done
	srv := newServer(t, stream)
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	wantCompactionFailure(t, window, err)
}

// TestACompactionWindowInstallsTheTerminalItemsOnce covers an answer whose
// terminal event repeats the items the stream already sent. The terminal items
// are the response whole, so the window holds them once.
func TestACompactionWindowInstallsTheTerminalItemsOnce(t *testing.T) {
	stream := frame(`{"type":"response.output_item.done","output_index":0,"item":`+compactionItem+`}`) +
		compactionStream(summaryItem("the summary"), compactionItem)
	srv := newServer(t, stream)
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	if err != nil {
		t.Fatalf("compacting the view: %v", err)
	}
	if len(window.Items) != 2 {
		t.Fatalf("the window holds %d items, want the response's 2: %+v", len(window.Items), window.Items)
	}
	if window.Items[0].Kind != poo.ContentText || window.Items[0].Text != "the summary" {
		t.Errorf("the window's first item is %+v, want the summary text", window.Items[0])
	}
	if window.Items[1].Provider == nil || string(window.Items[1].Provider.Data) != compactionItem {
		t.Errorf("the window's second item is %+v, want the compaction item", window.Items[1])
	}
}

// TestACompactionResponseWithoutACompactionItemFails covers an answer that
// holds no compacted state: a window of visible text alone is not a
// compaction, and none is installed.
func TestACompactionResponseWithoutACompactionItemFails(t *testing.T) {
	srv := newServer(t, compactionStream(summaryItem("the summary")))
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	wantCompactionFailure(t, window, err)
}

// TestACompactionResponseWithTwoCompactionItemsFails covers an answer that
// holds two compacted states: the transport cannot tell which one is the
// window, so it installs neither.
func TestACompactionResponseWithTwoCompactionItemsFails(t *testing.T) {
	srv := newServer(t, compactionStream(compactionItem, compactionItem))
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	wantCompactionFailure(t, window, err)
}

// TestACompactionItemWithoutAPayloadFails covers an item the endpoint could
// not take back. The payload is what the next request gives it, and an item
// without one is not the compacted state.
func TestACompactionItemWithoutAPayloadFails(t *testing.T) {
	srv := newServer(t, compactionStream(`{"type":"compaction","id":"cmp_1"}`))
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	wantCompactionFailure(t, window, err)
}

// TestACompactionResponseWithAnItemAWindowCannotHoldFails covers data the
// transport cannot install. Dropping it would lose context the endpoint meant
// to keep, so the window is refused and the failure names the item.
func TestACompactionResponseWithAnItemAWindowCannotHoldFails(t *testing.T) {
	srv := newServer(t, compactionStream(
		`{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{}"}`,
		compactionItem,
	))
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	err = wantCompactionFailure(t, window, err)
	if !strings.Contains(err.Error(), "function_call") {
		t.Errorf("the failure is %q, want it to name the item it cannot hold", err)
	}
}

// TestACompactionWindowIsBounded covers the guard on the answer's size. A
// response that keeps far more items than a window can hold is refused, not
// installed without bound.
func TestACompactionWindowIsBounded(t *testing.T) {
	const kept = 1000
	items := make([]string, 0, kept+1)
	for i := 0; i < kept; i++ {
		items = append(items, summaryItem("kept"))
	}
	srv := newServer(t, compactionStream(append(items, compactionItem)...))
	tr := newTransport(srv.URL)

	window, err := compact(t, tr, compactView())
	wantCompactionFailure(t, window, err)
}

// TestACompactionWithoutAViewFails covers the caller that asks for a window
// with no history to encode: the request never goes out.
func TestACompactionWithoutAViewFails(t *testing.T) {
	srv := newServer(t, compactionStream(compactionItem))
	tr := newTransport(srv.URL)

	_, err := tr.Compact(context.Background(), poo.NativeCompaction{
		Model: poo.ModelInfo{ModelID: "gpt-6.1-sol"},
	})
	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindInvalidRequest {
		t.Fatalf("the compaction failed with %v, want an invalid request", err)
	}
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none", n)
	}
}

// TestACompactionRequestTheEndpointRefusesFails covers the failure path: the
// compaction is a request like any other, and the endpoint's refusal is
// classified the way a turn's is. A refusal is no answer to charge for.
func TestACompactionRequestTheEndpointRefusesFails(t *testing.T) {
	srv := newServer(t, compactionStream(compactionItem))
	srv.failRequest(0, http.StatusInternalServerError)
	tr := newTransport(srv.URL, func(o *codex.Options) { o.MaxAttempts = 1 })

	res, err := compactResult(t, tr, compactView())
	var perr *poo.ProviderError
	if !errors.As(err, &perr) || perr.Kind != poo.ProviderKindServer {
		t.Fatalf("the compaction failed with %v, want a server failure", err)
	}
	if len(res.Window.Items) != 0 {
		t.Errorf("the failed compaction returned %+v, want no window", res.Window)
	}
	if res.Billed || res.Usage != nil {
		t.Errorf("the refused compaction reports %+v, want an unbilled request with no usage", res)
	}
}

// TestCancellingTheRunEndsTheCompaction covers the cancellation: a run that
// stops while the endpoint is holding the compaction ends the call with the
// run's own failure.
func TestCancellingTheRunEndsTheCompaction(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(blocked)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := newTransport(srv.URL)
	result := make(chan error, 1)
	go func() {
		_, err := tr.Compact(ctx, poo.NativeCompaction{
			Model: poo.ModelInfo{ModelID: "gpt-6.1-sol"},
			View:  compactView(),
		})
		result <- err
	}()

	<-blocked
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("the compaction ended with %v, want the run's cancellation", err)
	}
}

// The compaction's accounting: the request is billed, so its answer carries
// what the endpoint reported, and nothing when it reported nothing.

// compactionStreamWithUsage renders the stream of a compaction response whose
// terminal event carries the request's token counts.
func compactionStreamWithUsage(usage string, items ...string) string {
	return frame(`{"type":"response.completed","response":{"status":"completed","usage":`+usage+`,"output":[`+strings.Join(items, ",")+`]}}`) + done
}

// TestACompactionReportsWhatTheRequestCost covers the accounting a
// compaction hands the core. The endpoint's own counts ride with the window,
// because compacting costs the same prompt and completion a turn does.
func TestACompactionReportsWhatTheRequestCost(t *testing.T) {
	srv := newServer(t, compactionStreamWithUsage(
		`{"input_tokens":60,"output_tokens":40,"input_tokens_details":{"cached_tokens":10}}`,
		compactionItem))
	tr := newTransport(srv.URL)

	res, err := compactResult(t, tr, compactView())
	if err != nil {
		t.Fatalf("compacting the view: %v", err)
	}
	if res.Usage == nil {
		t.Fatalf("the compaction reported no usage, want the endpoint's 60 in and 40 out")
	}
	if res.Usage.InputTokens != 60 || res.Usage.OutputTokens != 40 {
		t.Errorf("the compaction's usage is %d in and %d out, want the endpoint's 60 and 40",
			res.Usage.InputTokens, res.Usage.OutputTokens)
	}
	if res.Usage.CacheReadTokens == nil || *res.Usage.CacheReadTokens != 10 {
		t.Errorf("the compaction's cache read is %v, want the endpoint's 10", res.Usage.CacheReadTokens)
	}
	if len(res.Window.Items) != 1 || res.Window.Items[0].Provider == nil {
		t.Errorf("the window holds %+v, want the response's compaction item", res.Window.Items)
	}
}

// TestACompactionThatReportsNoUsageHasNone covers the other half. An endpoint
// that reports no token counts hands the core no report. The core then charges
// the estimate a side request gets, not a zero-cost request.
func TestACompactionThatReportsNoUsageHasNone(t *testing.T) {
	srv := newServer(t, compactionStream(compactionItem))
	tr := newTransport(srv.URL)

	res, err := compactResult(t, tr, compactView())
	if err != nil {
		t.Fatalf("compacting the view: %v", err)
	}
	if res.Usage != nil {
		t.Errorf("the compaction reported the usage %+v, want none: the endpoint reported none", res.Usage)
	}
	if len(res.Window.Items) != 1 || res.Window.Items[0].Provider == nil {
		t.Errorf("the window holds %+v, want the response's compaction item", res.Window.Items)
	}
}

// TestACompactionThatCannotBeInstalledStillReportsWhatItCost covers the answer
// the transport refuses. The endpoint read the prompt and billed it before the
// window was judged, so the failure carries the counts it reported. Dropping
// them would let a request that spent the run's tokens cost the run nothing.
func TestACompactionThatCannotBeInstalledStillReportsWhatItCost(t *testing.T) {
	srv := newServer(t, compactionStreamWithUsage(
		`{"input_tokens":60,"output_tokens":40}`,
		summaryItem("the summary")))
	tr := newTransport(srv.URL)

	res, err := compactResult(t, tr, compactView())
	wantCompactionFailure(t, res.Window, err)
	if !res.Billed {
		t.Error("the failed compaction reports an unbilled request, want it billed: the endpoint answered it")
	}
	if res.Usage == nil {
		t.Fatal("the failed compaction reported no usage, want the endpoint's 60 in and 40 out")
	}
	if res.Usage.InputTokens != 60 || res.Usage.OutputTokens != 40 {
		t.Errorf("the failed compaction's usage is %d in and %d out, want the endpoint's 60 and 40",
			res.Usage.InputTokens, res.Usage.OutputTokens)
	}
}

// TestACompactionThatEndsWithoutAnAnswerStillReportsTheRequest covers a stream
// the endpoint answered and then cut, before it reported any token counts. The
// request opened, so it was billed. The core has to estimate what it cost.
func TestACompactionThatEndsWithoutAnAnswerStillReportsTheRequest(t *testing.T) {
	srv := newServer(t, frame(`{"type":"response.created","response":{"status":"in_progress"}}`))
	tr := newTransport(srv.URL)

	res, err := compactResult(t, tr, compactView())
	wantCompactionFailure(t, res.Window, err)
	if !res.Billed {
		t.Error("the cut compaction reports an unbilled request, want it billed: the endpoint opened it")
	}
	if res.Usage != nil {
		t.Errorf("the cut compaction reported the usage %+v, want none: the endpoint reported none", res.Usage)
	}
}

// TestACompactionThatNeverReachedTheEndpointIsNotBilled covers the failures that
// never reach the endpoint's own work. A credential that could not be resolved
// sends no request, so there is nothing to charge for.
func TestACompactionThatNeverReachedTheEndpointIsNotBilled(t *testing.T) {
	srv := newServer(t, compactionStream(compactionItem))
	tr := newTransport(srv.URL, func(o *codex.Options) {
		o.MaxAttempts = 1
		o.Credentials = func(context.Context) (codex.Credential, error) {
			return codex.Credential{}, errors.New("the token store holds nothing")
		}
	})

	res, err := compactResult(t, tr, compactView())
	if err == nil {
		t.Fatalf("the compaction returned %+v, want a failure", res.Window)
	}
	if n := srv.count(); n != 0 {
		t.Fatalf("the endpoint received %d requests, want none", n)
	}
	if res.Billed {
		t.Error("the compaction reports a billed request, want it unbilled: no request went out")
	}
	if res.Usage != nil {
		t.Errorf("the compaction reported the usage %+v, want none", res.Usage)
	}
}
