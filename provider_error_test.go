package poo_test

import (
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

func TestAProviderErrorShowsItsStatusWhenItHasOne(t *testing.T) {
	with := &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "overloaded", Status: 503}
	if got, want := with.Error(), "overloaded (status 503)"; got != want {
		t.Errorf("the error reads %q, want %q", got, want)
	}
	without := &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: "connection reset"}
	if got := without.Error(); got != "connection reset" {
		t.Errorf("the error reads %q, want the message alone", got)
	}
}

// The plan's retry rule: a rate limit is worth another attempt only when
// the endpoint asked for a wait of thirty seconds or less, a server
// failure and a network failure always are, and a rejection never is.

// TestTheRetryRuleFollowsThePlan covers the retryable failures, one row
// at a time.
func TestTheRetryRuleFollowsThePlan(t *testing.T) {
	cases := []struct {
		name string
		err  *poo.ProviderError
		want bool
	}{
		{
			name: "a rate limit with no wait asked is retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindRateLimited},
			want: true,
		},
		{
			name: "a rate limit with a short wait is retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindRateLimited, RetryAfter: 5 * time.Second},
			want: true,
		},
		{
			name: "a rate limit with a wait of exactly thirty seconds is retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindRateLimited, RetryAfter: 30 * time.Second},
			want: true,
		},
		{
			name: "a rate limit with a long wait is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindRateLimited, RetryAfter: 31 * time.Second},
			want: false,
		},
		{
			name: "a rate limit with a wait of five minutes is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindRateLimited, RetryAfter: 5 * time.Minute},
			want: false,
		},
		{
			name: "a server failure is retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindServer},
			want: true,
		},
		{
			name: "a network failure is retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindNetwork},
			want: true,
		},
		{
			name: "a rejected credential is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindAuthFailed},
			want: false,
		},
		{
			name: "a rejected request is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindInvalidRequest},
			want: false,
		},
		{
			name: "an exhausted quota is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindUsageLimit},
			want: false,
		},
		{
			name: "an inactive plan is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindPlanInactive},
			want: false,
		},
		{
			name: "a context-length failure is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindContextLength},
			want: false,
		},
		{
			name: "an interrupted stream is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindInterruptedStream},
			want: false,
		},
		{
			name: "a truncated output is not retryable",
			err:  &poo.ProviderError{Kind: poo.ProviderKindOutputTruncated},
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Retryable(); got != c.want {
				t.Errorf("the failure is retryable=%t, want %t", got, c.want)
			}
		})
	}
}
