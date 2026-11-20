package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// contextLengthRejection is the failure a hosted endpoint returns when a
// request's prompt and max_tokens together exceed its window. prompt is the
// endpoint's own count of the prompt, zero when it stated none.
func contextLengthRejection(message string, prompt int) error {
	return &poo.ProviderError{
		Kind:         poo.ProviderKindContextLength,
		Message:      message,
		Status:       400,
		PromptTokens: prompt,
	}
}

// roomClampedOptions is a session whose next request's max_tokens is the room
// the window leaves: the model declares a limit the window cannot spare.
func roomClampedOptions(t *testing.T, responses []fakeResponse) (*poo.Session, *pootest.Transport) {
	t.Helper()
	tr := scripted(responses)
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.Name = "small"
	opts.Model.ContextLimit = 4096
	opts.Model.MaxOutput = 8192
	return newTestSession(t, opts), tr
}

// roomAfterPrompt is the max_tokens a window of 4096 leaves for a prompt of
// prompt tokens.
func roomAfterPrompt(prompt int) int {
	return 4096 - prompt - poo.OutputReserve
}

// TestARoomClampedRequestRefusedForItsContextIsSentAgain covers a request
// whose max_tokens was the room the window left, refused because the prompt
// was larger than the estimate: the endpoint's own count gives the smaller
// limit the request is sent again with.
func TestARoomClampedRequestRefusedForItsContextIsSentAgain(t *testing.T) {
	const message = "This model's maximum context length is 4096 tokens. However, you requested 5829 tokens (3000 in the messages, 2829 in the completion)"
	s, tr := roomClampedOptions(t, []fakeResponse{
		{err: contextLengthRejection(message, 3000)},
		{text: "done"},
	})

	if res := runSession(t, s, "a short prompt"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	got := requestedTokens(tr)
	if len(got) != 2 {
		t.Fatalf("the requests carried max_tokens %v, want two requests", got)
	}
	if want := roomAfterPrompt(3000); got[1] != want {
		t.Errorf("the resend carried max_tokens %d, want %d", got[1], want)
	}
	if got[1] >= got[0] {
		t.Errorf("the resend carried max_tokens %d, not under the refused %d", got[1], got[0])
	}
}

// TestAnAnthropicContextRejectionCarriesItsPromptCount covers the Messages
// API's shape of the same failure: the count before the plus is the prompt.
func TestAnAnthropicContextRejectionCarriesItsPromptCount(t *testing.T) {
	const message = "input length and max_tokens exceed context limit: 3000 + 2829 > 4096"
	s, tr := roomClampedOptions(t, []fakeResponse{
		{err: contextLengthRejection(message, 3000)},
		{text: "done"},
	})

	if res := runSession(t, s, "a short prompt"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := requestedTokens(tr); len(got) != 2 || got[1] != roomAfterPrompt(3000) {
		t.Fatalf("the requests carried max_tokens %v, want the second %d", got, roomAfterPrompt(3000))
	}
}

// TestAnUnstatedPromptCountHalvesTheResentLimit covers a rejection that
// states no count: half the refused max_tokens is the limit the request is
// sent again with.
func TestAnUnstatedPromptCountHalvesTheResentLimit(t *testing.T) {
	s, tr := roomClampedOptions(t, []fakeResponse{
		{err: contextLengthRejection("the prompt is too long", 0)},
		{text: "done"},
	})

	if res := runSession(t, s, "a short prompt"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	got := requestedTokens(tr)
	if len(got) != 2 {
		t.Fatalf("the requests carried max_tokens %v, want two requests", got)
	}
	if want := got[0] / 2; got[1] != want {
		t.Errorf("the resend carried max_tokens %d, want half the refused %d", got[1], want)
	}
}

// TestARequestTheWindowDidNotClampIsNotSentAgain covers a request whose
// max_tokens was the model's own limit: a smaller one would not make the
// prompt fit, so the run settles as it did before.
func TestARequestTheWindowDidNotClampIsNotSentAgain(t *testing.T) {
	tr := scripted([]fakeResponse{
		{err: contextLengthRejection("the prompt is too long", 3000)},
		{text: "never"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Model.ContextLimit = 262144
	s := newTestSession(t, opts)

	if res := runSession(t, s, "a short prompt"); res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
}

// TestASentAgainRequestRefusedAgainSettlesContextExhausted covers the one
// resend a request gets: the second refusal is the run's.
func TestASentAgainRequestRefusedAgainSettlesContextExhausted(t *testing.T) {
	const message = "This model's maximum context length is 4096 tokens. However, you requested 5829 tokens (3000 in the messages, 2829 in the completion)"
	s, tr := roomClampedOptions(t, []fakeResponse{
		{err: contextLengthRejection(message, 3000)},
		{err: contextLengthRejection(message, 3000)},
	})

	res := runSession(t, s, "a short prompt")
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the transport got %d requests, want 2", got)
	}
}

// TestALimitUnderTheMinimumIsNotSentAgain covers a rejection whose count
// leaves no room a request may ask for: there is nothing to send.
func TestALimitUnderTheMinimumIsNotSentAgain(t *testing.T) {
	const message = "This model's maximum context length is 4096 tokens. However, you requested 5829 tokens (4000 in the messages, 1829 in the completion)"
	s, tr := roomClampedOptions(t, []fakeResponse{
		{err: contextLengthRejection(message, 4000)},
		{text: "never"},
	})

	if res := runSession(t, s, "a short prompt"); res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1", got)
	}
}

// TestAContextRefusalAndItsResendShareOneRequestNumbering covers the record
// contract: the refusal says a retry followed it, and the resend's attempts
// carry on the same request.
func TestAContextRefusalAndItsResendShareOneRequestNumbering(t *testing.T) {
	const message = "This model's maximum context length is 4096 tokens. However, you requested 5829 tokens (3000 in the messages, 2829 in the completion)"
	tr := &reportingTransport{Transport: scripted([]fakeResponse{
		{err: contextLengthRejection(message, 3000)},
		{err: contextLengthRejection(message, 3000)},
		{text: "never"},
	})}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.Name = "small"
	opts.Model.ContextLimit = 4096
	opts.Model.MaxOutput = 8192
	s := newTestSession(t, opts)

	if res := runSession(t, s, "a short prompt"); res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
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
		if rec.Kind != string(poo.ProviderKindContextLength) {
			t.Errorf("the attempt %d is a %q, want context_length", i+1, rec.Kind)
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

// TestARefusedPromptCountCalibratesTheEstimator covers the count the endpoint
// states: it is the prompt's real size, so the estimator learns the ratio the
// way it learns it from a usage report, and the next run asks for the room
// that count leaves rather than the room the estimate claimed.
func TestARefusedPromptCountCalibratesTheEstimator(t *testing.T) {
	const message = "This model's maximum context length is 4096 tokens. However, you requested 5829 tokens (3000 in the messages, 2829 in the completion)"
	s, tr := roomClampedOptions(t, []fakeResponse{
		{err: contextLengthRejection(message, 3000)},
		{text: "done"},
	})

	if res := runSession(t, s, "a short prompt"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if res := runSession(t, s, "another prompt"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the second run settled %q, want end_turn", res.Reason)
	}
	got := requestedTokens(tr)
	if len(got) < 3 {
		t.Fatalf("the requests carried max_tokens %v, want a third request", got)
	}
	// An estimate that ignored the count would read the view at the
	// starting ratio and ask for nearly the whole window.
	uncalibrated := 4096 - int(float64(poo.EstimateHistoryChars(tr.Requests()[2].Messages))/3.5) - poo.OutputReserve
	if got[2] >= uncalibrated {
		t.Errorf("the third request carried max_tokens %d, no lower than an uncalibrated %d: the prompt count did not calibrate the estimate",
			got[2], uncalibrated)
	}
	if want := roomAfterPrompt(3000); got[2] >= want {
		t.Errorf("the third request carried max_tokens %d, want under a calibrated %d", got[2], want)
	}
}
