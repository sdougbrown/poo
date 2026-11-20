package poo_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/session"
)

// reportingTransport is a transport that reports the attempt a request
// ends with, the way a wire transport does.
type reportingTransport struct {
	*pootest.Transport
}

func (t *reportingTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	reader, rendered, err := t.Transport.Stream(ctx, req)
	if err != nil && req.OnAttempt != nil {
		req.OnAttempt(poo.Attempt{Number: 1, Err: err})
	}
	return reader, rendered, err
}

// interruptAttemptTransport interrupts the run on its first request.
type interruptAttemptTransport struct {
	*pootest.Transport
	session *poo.Session
	t       *testing.T
	once    sync.Once
}

func (tr *interruptAttemptTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	tr.once.Do(func() {
		if _, err := tr.session.Send("stop", poo.Interrupt); err != nil {
			tr.t.Errorf("interrupting the run: %v", err)
		}
	})
	return tr.Transport.Stream(ctx, req)
}

// outputLimitRejection is the 400 a hosted API sends when max_tokens is
// above its cap.
func outputLimitRejection(limit int) error {
	return &poo.ProviderError{
		Kind:        poo.ProviderKindOutputLimit,
		Message:     "max_tokens is too large",
		Status:      400,
		OutputLimit: limit,
	}
}

func outputLimitEvents(s *poo.Session) []poo.Event {
	var out []poo.Event
	for _, ev := range s.Events() {
		if ev.Name == poo.EventModelOutputLimit {
			out = append(out, ev)
		}
	}
	return out
}

func requestedTokens(tr interface{ Requests() []poo.ChatRequest }) []int {
	var out []int
	for _, r := range tr.Requests() {
		out = append(out, r.MaxTokens)
	}
	return out
}

// TestAnEndpointCapLearnedFromARejectionServesTheRetryAndTheNextTurn covers
// a model that declares no max_output: the request that the endpoint
// rejects for its max_tokens is sent once more under the cap the endpoint
// named, the session says what it learned, and the later requests keep the
// cap.
func TestAnEndpointCapLearnedFromARejectionServesTheRetryAndTheNextTurn(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{text: "first"},
		{text: "second"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.Name = "big"
	opts.Model.Source = map[string]string{"model_id": poo.LayerSettings}
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "one"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if res := runSession(t, s, "two"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the second run settled %q, want end_turn", res.Reason)
	}

	want := []int{32768, 16384, 16384}
	if got := requestedTokens(tr); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the requests carried max_tokens %v, want %v", got, want)
	}
	events := outputLimitEvents(s)
	if len(events) != 1 {
		t.Fatalf("the session announced %d output limits, want 1", len(events))
	}
	payload := events[0].Payload
	if payload["limit"] != 16384 {
		t.Errorf("the event names the limit %v, want 16384", payload["limit"])
	}
	if setting, _ := payload["setting"].(string); setting != "models.big.max_output" {
		t.Errorf("the event names the setting %q, want models.big.max_output", setting)
	}
	if text, _ := payload["message"].(string); !strings.Contains(text, "16384") || !strings.Contains(text, "models.big.max_output") {
		t.Errorf("the event's message %q names neither the cap nor the setting", text)
	}
}

// TestALearnedCapOnADiscoveredModelNamesTheDeclaration covers a model the
// settings do not declare: there is no models entry to set, so the warning
// asks for a declaration instead of naming a setting that does not exist.
func TestALearnedCapOnADiscoveredModelNamesTheDeclaration(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.Name = "big"
	opts.Model.Source = map[string]string{"model_id": "discovery"}
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "one"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	events := outputLimitEvents(s)
	if len(events) != 1 {
		t.Fatalf("the session announced %d output limits, want 1", len(events))
	}
	if setting, _ := events[0].Payload["setting"].(string); setting != "" {
		t.Errorf("the event names the setting %q, want none: the settings declare no model", setting)
	}
	text, _ := events[0].Payload["message"].(string)
	if want := "declare the model in settings with its endpoint, model_id and max_output"; !strings.Contains(text, want) {
		t.Errorf("the event's message %q does not ask for a declaration", text)
	}
	if strings.Contains(text, "models.big.max_output") {
		t.Errorf("the event's message %q names a setting no settings entry has", text)
	}
}

// TestACapAnEndpointNamesInsideTheStreamIsLearned covers the same rejection
// sent as an error event after the stream opened: nothing of the answer was
// read, so the request is asked again under the cap, and the run ends
// normally.
func TestACapAnEndpointNamesInsideTheStreamIsLearned(t *testing.T) {
	tr := scripted([]fakeResponse{
		{fail: outputLimitRejection(16384)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.Name = "big"
	opts.Model.Source = map[string]string{"model_id": poo.LayerSettings}
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "one"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	want := []int{32768, 16384}
	if got := requestedTokens(tr); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the requests carried max_tokens %v, want %v", got, want)
	}
}

// TestARejectionThatStatesNoCapRetriesUnderTheOlderDefault covers a
// rejection whose message names no number.
func TestARejectionThatStatesNoCapRetriesUnderTheOlderDefault(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(0)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "one"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	want := []int{32768, 8192}
	if got := requestedTokens(tr); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the requests carried max_tokens %v, want %v", got, want)
	}
}

// TestADeclaredMaxOutputIsNotRetried covers the user's word: a model
// that declares its limit is never second-guessed, and the run settles as
// any rejected request does.
func TestADeclaredMaxOutputIsNotRetried(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(1024)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	opts.Model.MaxOutput = 4096
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
	if got := len(outputLimitEvents(s)); got != 0 {
		t.Errorf("the session announced %d output limits, want 0", got)
	}
}

// TestACapUnderTheMinimumIsNotLearned covers a rejection that names a cap
// no request could ask for: it is not the endpoint's output limit, so the
// request is not sent again and the model keeps no cap.
func TestACapUnderTheMinimumIsNotLearned(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(100)},
		{text: "never"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
	if got := s.ChatModel().MaxOutput; got != 0 {
		t.Errorf("the model's max_output is %d, want it unchanged at 0", got)
	}
	if got := len(outputLimitEvents(s)); got != 0 {
		t.Errorf("the session announced %d output limits, want 0", got)
	}
}

// TestAnInterruptBeforeTheResendKeepsTheFirstResult covers an interrupt
// that arrives with the rejection: the request is not sent again, and the
// interrupted turn follows instead of a learned cap.
func TestAnInterruptBeforeTheResendKeepsTheFirstResult(t *testing.T) {
	tr := &interruptAttemptTransport{Transport: scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{text: "never"},
	}), t: t}
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)
	tr.session = s

	runSession(t, s, "one")
	interrupted := false
	for _, ev := range s.Events() {
		if ev.Name == poo.EventTurnInterrupted {
			interrupted = true
		}
	}
	if !interrupted {
		t.Error("the interrupt did not end the first turn")
	}
	if got := s.ChatModel().MaxOutput; got != 0 {
		t.Errorf("the interrupted request learned a max_output of %d, want 0", got)
	}
}

// TestARejectedAttemptAndItsResendShareOneRequestNumbering covers the
// record contract: the rejected attempt says a retry followed it, and the
// resend's attempts carry on the same request's numbering.
func TestARejectedAttemptAndItsResendShareOneRequestNumbering(t *testing.T) {
	tr := &reportingTransport{Transport: scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{err: outputLimitRejection(8192)},
		{text: "never"},
	})}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	records := attemptRecords(sink)
	if len(records) != 2 {
		t.Fatalf("the sink kept %d attempt records, want 2", len(records))
	}
	for i, rec := range records {
		if rec.Request != 1 {
			t.Errorf("the attempt %d names request %d, want 1", i+1, rec.Request)
		}
		if rec.Attempt != i+1 {
			t.Errorf("the attempt is %d, want %d", rec.Attempt, i+1)
		}
	}
	var retries int
	for _, ev := range s.Events() {
		if ev.Name != poo.EventProviderRetry {
			continue
		}
		retries++
		if ev.Payload["attempt"] != 1 {
			t.Errorf("the retry names the attempt %v, want 1", ev.Payload["attempt"])
		}
	}
	if retries != 1 {
		t.Errorf("the session announced %d retries, want 1", retries)
	}
}

// TestASecondRejectionSettlesTheRun covers the bound: the request is
// retried once, and a cap that is still refused ends the run.
func TestASecondRejectionSettlesTheRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{err: outputLimitRejection(8192)},
		{text: "never"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the transport got %d requests, want 2", got)
	}
}

// TestACapNoLowerThanTheRequestIsNotRetried covers a rejection a retry
// cannot help: the request already asked for no more than the cap that
// the rejection names or that poo falls back to.
func TestACapNoLowerThanTheRequestIsNotRetried(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(0)},
		{text: "answer"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 32768
	s := newTestSession(t, opts)

	res := runSession(t, s, "one")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
}

// TestALearnedOutputLimitIsRecorded covers the log: the cap the session
// learns is a record beside the event, so a reader of the log knows why
// max_tokens dropped, and the record round-trips through the codec.
func TestALearnedOutputLimitIsRecorded(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: outputLimitRejection(16384)},
		{text: "answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "one"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}

	var rec *poo.OutputLimitLearnedRecord
	for _, r := range sink.allRecords() {
		if learned, ok := r.(*poo.OutputLimitLearnedRecord); ok {
			rec = learned
		}
	}
	if rec == nil {
		t.Fatal("the sink kept no output-limit record")
	}
	if rec.Meta().Record != "model.output_limit" {
		t.Errorf("the record is a %q, want model.output_limit", rec.Meta().Record)
	}
	if rec.ModelID != "test-model" {
		t.Errorf("the record names the model %q, want test-model", rec.ModelID)
	}
	if rec.Previous != 32768 || rec.Limit != 16384 {
		t.Errorf("the record holds %d -> %d, want 32768 -> 16384", rec.Previous, rec.Limit)
	}

	codec := session.NewCodec()
	line, err := codec.Encode(rec)
	if err != nil {
		t.Fatalf("encoding the record: %v", err)
	}
	decoded, err := codec.Decode(line)
	if err != nil {
		t.Fatalf("decoding the record: %v", err)
	}
	got, ok := decoded.(*poo.OutputLimitLearnedRecord)
	if !ok {
		t.Fatalf("the line decoded to a %T, want *poo.OutputLimitLearnedRecord", decoded)
	}
	if got.ModelID != rec.ModelID || got.Previous != rec.Previous || got.Limit != rec.Limit {
		t.Errorf("the record round-tripped as %+v, want %+v", got, rec)
	}
}
