package poo_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// stallingEndpoint answers with one chunk and then stalls.
func stallingEndpoint(t *testing.T) *httptest.Server {
	t.Helper()
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"first"}}]}`+"\n\n")
		fl.Flush()
		select {
		case <-stop:
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })
	return srv
}

// settleOf returns the run's settle record.
func settleOf(t *testing.T, sink *recordingSink) *poo.RunSettled {
	t.Helper()
	for _, rec := range sink.records {
		if s, ok := rec.(*poo.RunSettled); ok {
			return s
		}
	}
	t.Fatal("the log holds no run.settled record")
	return nil
}

// TestACancelledRequestSettlesCancelled covers a cancellation that
// arrives while the provider is answering.
func TestACancelledRequestSettlesCancelled(t *testing.T) {
	srv := stallingEndpoint(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	opts := testOptions(t, tr, tool, sink)
	r := poo.NewRun(opts)
	res, err := r.Execute(ctx, "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if poo.ExitCode(res.Reason) != 130 {
		t.Errorf("the exit code is %d, want 130", poo.ExitCode(res.Reason))
	}
	settle := settleOf(t, sink)
	if settle.Reason != poo.ReasonCancelled {
		t.Errorf("the settle record says %q, want cancelled", settle.Reason)
	}
	if settle.Detail.Source != poo.SourceCancelContext {
		t.Errorf("the settle source is %q, want cancel:context", settle.Detail.Source)
	}
}

// TestADeadlineDuringTheRequestSettlesCancelled covers a run whose
// deadline passes while the provider is answering.
func TestADeadlineDuringTheRequestSettlesCancelled(t *testing.T) {
	srv := stallingEndpoint(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(ctx, "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if settleOf(t, sink).Reason != poo.ReasonCancelled {
		t.Error("the settle record does not say cancelled")
	}
	if got := settleOf(t, sink).Detail.Source; got != poo.SourceCancelDeadline {
		t.Errorf("the settle source is %q, want cancel:deadline", got)
	}
}

// TestAStartedRunAlwaysSettles covers the promise the log makes: a
// run.started is always followed by a run.settled.
func TestAStartedRunAlwaysSettles(t *testing.T) {
	srv := stallingEndpoint(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	if _, err := r.Execute(ctx, "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	names := sink.names()
	if len(names) == 0 || names[0] != "run.started" {
		t.Fatalf("the records are %v, want a run.started first", names)
	}
	if names[len(names)-1] != "run.settled" {
		t.Errorf("the records are %v, want a run.settled last", names)
	}
}

// TestAStreamThatBreaksStillReportsItsUsage covers a provider that dies
// mid-stream: the request still has a usage record.
func TestAStreamThatBreaksStillReportsItsUsage(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n" +
		"data: " + `{"error":{"message":"the model crashed","type":"server_error"}}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if sink.count("usage") != 1 {
		t.Errorf("the log holds %d usage records, want 1", sink.count("usage"))
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestTheTextModeDoesNotDuplicateTheProse covers the text mode end to
// end: the model's history holds the prose once, and no call markup.
func TestTheTextModeDoesNotDuplicateTheProse(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"content":"I will list. <tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls\"}}</tool_call> done."},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.ToolsMode = "text"
	r := poo.NewRun(opts)
	if _, err := r.Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	var assistant *poo.MessageRecord
	for _, rec := range sink.records {
		if m, ok := rec.(*poo.MessageRecord); ok && m.Origin == "model" {
			assistant = m
		}
	}
	if assistant == nil {
		t.Fatal("the log holds no model message")
	}
	var text string
	for _, c := range assistant.Content {
		if c.Kind == poo.ContentText {
			text += c.Text
		}
	}
	if strings.Contains(text, "tool_call") {
		t.Errorf("the model's message holds the call markup: %q", text)
	}
	if strings.Count(text, "I will list.") != 1 {
		t.Errorf("the prose is %q, want it once", text)
	}
	if !strings.Contains(text, "done") {
		t.Errorf("the prose is %q, want the model's own words", text)
	}
}

// TestTheHistoryTheModelSeesHoldsTheProseOnce covers the same thing from
// the model's side: the request it answers holds the prose once.
func TestTheHistoryTheModelSeesHoldsTheProseOnce(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"content":"I will list. <tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls\"}}</tool_call> done."},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.ToolsMode = "text"
	r := poo.NewRun(opts)
	if _, err := r.Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	var assistant poo.Message
	for _, rec := range sink.records {
		if m, ok := rec.(*poo.MessageRecord); ok && m.Origin == "model" {
			assistant = poo.Message{Role: m.Role, Content: m.Content}
		}
	}
	if assistant.Role != poo.RoleAssistant {
		t.Fatalf("the model message's role is %q, want assistant", assistant.Role)
	}
	text := assistant.Text()
	if strings.Count(text, "I will list.") != 1 {
		t.Errorf("the prose is %q, want it once", text)
	}
	if strings.Contains(text, "tool_call") {
		t.Errorf("the message holds the call markup: %q", text)
	}
}

// TestAZeroContextLimitSettlesBeforeAnyRequest pins what a zero
// ContextLimit means: no room, so the run ends without sending.
func TestAZeroContextLimitSettlesBeforeAnyRequest(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "never asked"}}}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.ContextLimit = 0

	res, err := poo.NewRun(opts).Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if got := settleOf(t, sink).Detail.Source; got != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", got)
	}
	if len(tr.requests) != 0 {
		t.Errorf("the transport was asked %d times, want none", len(tr.requests))
	}
}

// TestARateLimitCarriesTheWaitItAskedFor covers a rate limit: the
// endpoint asks the client to wait, and the settle record has to carry
// the number, because the plan's provider_limit row settles on a
// Retry-After over thirty seconds.
func TestARateLimitCarriesTheWaitItAskedFor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down","code":"rate_limit_exceeded"}}`)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}

	settle := settleOf(t, sink)
	if settle.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", settle.Detail)
	}
	if settle.Detail.Provider.Kind != "rate_limited" {
		t.Errorf("the provider's kind is %q, want rate_limited", settle.Detail.Provider.Kind)
	}
	if settle.Detail.Provider.Status != http.StatusTooManyRequests {
		t.Errorf("the provider's status is %d, want 429", settle.Detail.Provider.Status)
	}
	if settle.Detail.Provider.RetryAfterSec != 60 {
		t.Errorf("the provider's retry-after is %d, want 60", settle.Detail.Provider.RetryAfterSec)
	}
}

// TestATruncatedStreamKeepsItsOriginalKind covers a stream that breaks
// after a delta: the plan reclassifies the failure as an interrupted
// stream and keeps the kind the failure had in the code, so a reader can
// tell a broken connection from a broken endpoint.
func TestATruncatedStreamKeepsItsOriginalKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`+"\n\n")
		fl.Flush()
		// The connection closes before the stream ends.
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}

	settle := settleOf(t, sink)
	if settle.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", settle.Detail)
	}
	if settle.Detail.Provider.Kind != "interrupted_stream" {
		t.Errorf("the provider's kind is %q, want interrupted_stream", settle.Detail.Provider.Kind)
	}
	if settle.Detail.Provider.Code != "network" {
		t.Errorf("the provider's code is %q, want the original kind network", settle.Detail.Provider.Code)
	}
}
