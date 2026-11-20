package support_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// TestTheFailureClassTheUnionAdded covers the classification rows this
// package's two copies had drifted apart on. A failure that names a quota in
// its code or its words is a settled usage limit, whether or not the endpoint
// sent a status with it.
func TestTheFailureClassTheUnionAdded(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		code     string
		message  string
		wantKind poo.ProviderKind
	}{
		{
			name:     "a quota code",
			status:   http.StatusTooManyRequests,
			code:     "usage_not_included",
			wantKind: poo.ProviderKindUsageLimit,
		},
		{
			name:     "a balance in the message",
			status:   http.StatusTooManyRequests,
			message:  "your available balance is too low for this request",
			wantKind: poo.ProviderKindUsageLimit,
		},
		{
			name:     "a budget in the message",
			status:   http.StatusTooManyRequests,
			message:  "the account is out of budget",
			wantKind: poo.ProviderKindUsageLimit,
		},
		{
			name:     "a rate limit that says nothing about a quota",
			status:   http.StatusTooManyRequests,
			code:     "rate_limit_exceeded",
			wantKind: poo.ProviderKindRateLimited,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := support.FailureKind(c.status, c.code, "", c.message, poo.ProviderKindServer)
			if got != c.wantKind {
				t.Errorf("the failure is %q, want %q", got, c.wantKind)
			}
		})
	}
}

// TestAnOutputCapRejectionIsItsOwnKind covers the 400s and 422s a hosted API
// sends when max_tokens is above its cap: the failure is an output_limit, and
// it carries the largest value the message allows. A context overflow that
// mentions max_tokens stays a context_length failure, and a message that only
// says the input plus max_tokens is too long is not an output limit at all.
func TestAnOutputCapRejectionIsItsOwnKind(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		message   string
		wantKind  poo.ProviderKind
		wantLimit int
	}{
		{
			name:      "OpenAI",
			message:   "max_tokens is too large: 32768. This model supports at most 16384 completion tokens, whereas you provided 32768.",
			wantKind:  poo.ProviderKindOutputLimit,
			wantLimit: 16384,
		},
		{
			name:      "DeepSeek",
			message:   "Invalid max_tokens value, the valid range of max_tokens is [1, 8192]",
			wantKind:  poo.ProviderKindOutputLimit,
			wantLimit: 8192,
		},
		{
			name:      "Anthropic",
			message:   "max_tokens: 32768 > 8192, which is the maximum allowed number of output tokens for claude-3-5-sonnet-20241022",
			wantKind:  poo.ProviderKindOutputLimit,
			wantLimit: 8192,
		},
		{
			name:      "DashScope",
			message:   "Range of max_tokens should be [1, 8192]",
			wantKind:  poo.ProviderKindOutputLimit,
			wantLimit: 8192,
		},
		{
			name:      "a cap in max_completion_tokens",
			message:   "Invalid 'max_completion_tokens': integer above maximum value. Expected a value less than or equal to 4096, but got 32768 instead.",
			wantKind:  poo.ProviderKindOutputLimit,
			wantLimit: 4096,
		},
		{
			name:      "a 422 that calls max_tokens too large",
			status:    http.StatusUnprocessableEntity,
			message:   "max_tokens is too large: 32768. This model supports at most 16384 completion tokens.",
			wantKind:  poo.ProviderKindOutputLimit,
			wantLimit: 16384,
		},
		{
			name:     "a cap the message does not state",
			message:  "max_tokens is too large for this model",
			wantKind: poo.ProviderKindOutputLimit,
		},
		{
			name:     "an input plus max_tokens that exceeds the model's limit",
			message:  "This request's input plus max_tokens exceeds the model's limit",
			wantKind: poo.ProviderKindInvalidRequest,
		},
		{
			name:     "a context overflow that names max_tokens",
			message:  "'max_tokens' or 'max_completion_tokens' is too large: 32768. This model's maximum context length is 40960 tokens and your request has 12000 input tokens (32768 > 40960 - 12000).",
			wantKind: poo.ProviderKindContextLength,
		},
		{
			name:     "a context overflow in the OpenAI words",
			message:  "This model's maximum context length is 8192 tokens. However, you requested 9000 tokens (5000 in the messages, 4000 in the completion).",
			wantKind: poo.ProviderKindContextLength,
		},
		{
			name:     "a max_tokens that is not a number",
			message:  "max_tokens must be a positive integer",
			wantKind: poo.ProviderKindInvalidRequest,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"error": map[string]any{"message": c.message}})
			if err != nil {
				t.Fatalf("encoding the body: %v", err)
			}
			status := c.status
			if status == 0 {
				status = http.StatusBadRequest
			}
			resp := &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     http.Header{},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}
			var perr *poo.ProviderError
			if err := support.ClassifyStatus(resp); !errors.As(err, &perr) {
				t.Fatalf("the failure is %v, want a provider error", err)
			}
			if perr.Kind != c.wantKind {
				t.Errorf("the failure is %q, want %q", perr.Kind, c.wantKind)
			}
			if perr.OutputLimit != c.wantLimit {
				t.Errorf("the output limit is %d, want %d", perr.OutputLimit, c.wantLimit)
			}
		})
	}
}

// TestAContextRejectionCarriesItsPromptCount covers the words a
// context-length refusal uses for the prompt's own count. The session reads
// the count to ask for the room it leaves, so a shape read as the whole
// request or as the completion is a smaller max_tokens than the window can
// spare.
func TestAContextRejectionCarriesItsPromptCount(t *testing.T) {
	cases := []struct {
		name     string
		message  string
		wantKind poo.ProviderKind
		want     int
	}{
		{
			name:     "vLLM states the prompt's share of the request",
			message:  "This model's maximum context length is 4096 tokens. However, you requested 5829 tokens (3000 in the messages, 2829 in the completion). Please reduce the length of the messages or completion.",
			wantKind: poo.ProviderKindContextLength,
			want:     3000,
		},
		{
			name:     "the Messages API states the prompt and the completion",
			message:  "input length and max_tokens exceed context limit: 3000 + 2829 > 4096",
			wantKind: poo.ProviderKindContextLength,
			want:     3000,
		},
		{
			name:     "a refusal that states no count",
			message:  "the prompt is too long",
			wantKind: poo.ProviderKindContextLength,
		},
		{
			name:     "an output cap states no prompt",
			message:  "max_tokens is too large: 32768. This model supports at most 16384 completion tokens.",
			wantKind: poo.ProviderKindOutputLimit,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"error": map[string]any{"message": c.message}})
			if err != nil {
				t.Fatalf("encoding the body: %v", err)
			}
			resp := &http.Response{
				StatusCode: http.StatusBadRequest,
				Status:     http.StatusText(http.StatusBadRequest),
				Header:     http.Header{},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}
			var perr *poo.ProviderError
			if err := support.ClassifyStatus(resp); !errors.As(err, &perr) {
				t.Fatalf("the failure is %v, want a provider error", err)
			}
			if perr.Kind != c.wantKind {
				t.Errorf("the failure is %q, want %q", perr.Kind, c.wantKind)
			}
			if perr.PromptTokens != c.want {
				t.Errorf("the prompt count is %d, want %d", perr.PromptTokens, c.want)
			}
		})
	}
}

// TestAnOutputCapRejectionNeedsABadRequest covers the status: the same
// words under a 500 are a server failure, because only a 400 is the
// endpoint rejecting the request's own max_tokens.
func TestAnOutputCapRejectionNeedsABadRequest(t *testing.T) {
	got := support.FailureKind(http.StatusInternalServerError, "", "", "max_tokens is too large: 32768", poo.ProviderKindServer)
	if got != poo.ProviderKindServer {
		t.Errorf("the failure is %q, want %q", got, poo.ProviderKindServer)
	}
}

// TestRetryAfterReadsTheMillisecondHeader covers the header the Codex endpoint
// spells a wait in. A value read as seconds would turn a five-minute wait into
// five minutes of milliseconds, or into a retry the run should have settled on.
func TestRetryAfterReadsTheMillisecondHeader(t *testing.T) {
	cases := []struct {
		name string
		ms   string
		secs string
		want time.Duration
	}{
		{name: "milliseconds", ms: "300000", want: 300 * time.Second},
		{name: "a short wait in milliseconds", ms: "500", want: 500 * time.Millisecond},
		{name: "the standard header in seconds", secs: "7", want: 7 * time.Second},
		{name: "no header", want: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			header := http.Header{}
			if c.ms != "" {
				header.Set("Retry-After-Ms", c.ms)
			}
			if c.secs != "" {
				header.Set("Retry-After", c.secs)
			}
			got := support.RetryAfter(&http.Response{Header: header})
			if got != c.want {
				t.Errorf("the wait is %v, want %v", got, c.want)
			}
		})
	}
}

// TestTheMillisecondWaitIsCappedByThePolicy covers the two rules together: the
// millisecond header decides the wait, and a wait past the policy's cap is not
// a retry. A five-minute wait read as milliseconds, or not capped, turns a
// long limit into three quick attempts.
func TestTheMillisecondWaitIsCappedByThePolicy(t *testing.T) {
	policy := support.DefaultPolicy()
	long := &poo.ProviderError{
		Kind:       poo.ProviderKindRateLimited,
		RetryAfter: 300 * time.Second,
	}
	if wait, ok := policy.Delay(1, long); ok {
		t.Errorf("a five-minute limit is retried after %v, want the run to settle", wait)
	}
	short := &poo.ProviderError{
		Kind:       poo.ProviderKindRateLimited,
		RetryAfter: 400 * time.Millisecond,
	}
	wait, ok := policy.Delay(1, short)
	if !ok {
		t.Fatal("a four-hundred-millisecond limit is not retried, want a wait")
	}
	if wait != 400*time.Millisecond {
		t.Errorf("the wait is %v, want the endpoint's four hundred milliseconds", wait)
	}
}

// TestThePolicyTakesTheConsumersOverrides covers the configuration a consumer
// names. A consumer that sets an attempt count or a backoff gets it, and one
// that names neither keeps the plan's.
func TestThePolicyTakesTheConsumersOverrides(t *testing.T) {
	base := support.DefaultPolicy()
	override := base.With(5, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond})
	if override.Attempts != 5 {
		t.Errorf("the attempts are %d, want the consumer's five", override.Attempts)
	}
	if len(override.Backoff) != 2 || override.Backoff[0] != 10*time.Millisecond {
		t.Errorf("the backoff is %v, want the consumer's", override.Backoff)
	}
	if kept := base.With(0, nil); kept.Attempts != 3 || len(kept.Backoff) != 2 || kept.Backoff[0] != time.Second {
		t.Errorf("a consumer that names neither lost the defaults: %+v", kept)
	}
}

// TestAFailureAfterTheFirstDeltaIsAnInterruptedStream covers the composition
// the stream reader relies on: a failed request and a broken stream settle with
// the same kind for the same cause, and a failure after the first token is the
// interrupted stream the retry layer will not repeat.
func TestAFailureAfterTheFirstDeltaIsAnInterruptedStream(t *testing.T) {
	boom := &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: "the stream broke"}

	atOpen := support.ContextError(context.Background(), context.Background(), false, boom)
	var openErr *poo.ProviderError
	if !errors.As(atOpen, &openErr) || openErr.Kind != poo.ProviderKindNetwork {
		t.Fatalf("a failure with no token is %v, want the network failure it was", atOpen)
	}

	afterDelta := support.ContextError(context.Background(), context.Background(), true, boom)
	var streamErr *poo.ProviderError
	if !errors.As(afterDelta, &streamErr) {
		t.Fatalf("a failure after the first token is %v, want a provider failure", afterDelta)
	}
	if streamErr.Kind != poo.ProviderKindInterruptedStream {
		t.Errorf("the kind is %q, want interrupted_stream", streamErr.Kind)
	}
	if streamErr.Code != string(poo.ProviderKindNetwork) {
		t.Errorf("the code is %q, want the kind it had before the reclassification", streamErr.Code)
	}
}
