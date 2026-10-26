package openai

import (
	"errors"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The retry policy, on its own.
//
// The plan: three attempts per request, the first try and two retries,
// with a backoff of one second and then two. A rate limit is worth
// another attempt only when the endpoint asked for a wait of thirty
// seconds or less, and a rejection never is.

// TestTheBackoffIsOneSecondThenTwo covers the plan's backoff.
func TestTheBackoffIsOneSecondThenTwo(t *testing.T) {
	policy := defaultRetryPolicy()
	if policy.attempts != 3 {
		t.Errorf("the budget is %d attempts, want 3", policy.attempts)
	}
	server := &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "boom"}
	for _, c := range []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
	} {
		wait, ok := policy.delay(c.attempt, server)
		if !ok {
			t.Errorf("attempt %d is not retried, want a wait", c.attempt)
			continue
		}
		if wait != c.want {
			t.Errorf("the wait after attempt %d is %v, want %v", c.attempt, wait, c.want)
		}
	}
}

// TestTheBudgetRunsOut covers the budget: the third attempt is the last
// one, so a failure after it has no wait.
func TestTheBudgetRunsOut(t *testing.T) {
	policy := defaultRetryPolicy()
	server := &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "boom"}
	if _, ok := policy.delay(3, server); ok {
		t.Error("the third attempt is retried, want the budget to be spent")
	}
}

// TestOnlyTheWorthwhileFailuresAreRetried covers the rule: a rate
// limit, a server failure and a network failure are worth another
// attempt, and a rejection is not.
func TestOnlyTheWorthwhileFailuresAreRetried(t *testing.T) {
	policy := defaultRetryPolicy()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"a server failure", &poo.ProviderError{Kind: poo.ProviderKindServer}, true},
		{"a network failure", &poo.ProviderError{Kind: poo.ProviderKindNetwork}, true},
		{"a rate limit", &poo.ProviderError{Kind: poo.ProviderKindRateLimited}, true},
		{"a rate limit with a long wait", &poo.ProviderError{Kind: poo.ProviderKindRateLimited, RetryAfter: 31 * time.Second}, false},
		{"a rejected request", &poo.ProviderError{Kind: poo.ProviderKindInvalidRequest}, false},
		{"a rejected credential", &poo.ProviderError{Kind: poo.ProviderKindAuthFailed}, false},
		{"an exhausted quota", &poo.ProviderError{Kind: poo.ProviderKindUsageLimit}, false},
		{"an inactive plan", &poo.ProviderError{Kind: poo.ProviderKindPlanInactive}, false},
		{"a failure that is not a provider error", errors.New("boom"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := policy.delay(1, c.err)
			if ok != c.want {
				t.Errorf("the failure is retried=%t, want %t", ok, c.want)
			}
		})
	}
}

// TestTheWaitIsCappedAtThirtySeconds covers the cap: a rate limit that
// asks for a long wait is not retried at all, and one that asks for a
// short one waits no longer than the cap.
func TestTheWaitIsCappedAtThirtySeconds(t *testing.T) {
	policy := defaultRetryPolicy()
	short := &poo.ProviderError{Kind: poo.ProviderKindRateLimited, RetryAfter: 5 * time.Second}
	wait, ok := policy.delay(1, short)
	if !ok {
		t.Fatal("the rate limit is not retried, want a wait")
	}
	if wait != 5*time.Second {
		t.Errorf("the wait is %v, want the endpoint's five seconds", wait)
	}
	long := &poo.ProviderError{Kind: poo.ProviderKindRateLimited, RetryAfter: 120 * time.Second}
	if _, ok := policy.delay(1, long); ok {
		t.Error("the rate limit is retried, want the wait to be over the budget")
	}
}
