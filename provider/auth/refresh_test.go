package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
)

// TestAForcedRefreshTradesALiveCredentialOnce covers the job of the forced
// refresh: a credential with hours left is traded anyway, exactly once. What
// the endpoint traded is what the caller and the file hold. The access that
// follows finds the stored token live, so it asks for nothing.
func TestAForcedRefreshTradesALiveCredentialOnce(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(10*time.Hour)))
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Refresh(context.Background())
	if err != nil {
		t.Fatalf("the forced refresh did not reach a token: %v", err)
	}
	if endpoint.count() != 1 {
		t.Errorf("the endpoint was asked %d times, want the one forced trade", endpoint.count())
	}
	if token.Access != access || token.AccountID != newAccount {
		t.Errorf("the caller got %q for %q, want the traded token and account", token.Access, token.AccountID)
	}
	if want := clock().Add(time.Hour); !token.Expires.Equal(want) {
		t.Errorf("the token expires at %v, want %v", token.Expires, want)
	}
	stored := storedCredential(t, path)
	if stored.Access != access || stored.AccountID != newAccount {
		t.Errorf("the file holds %q for %q, want the traded token", stored.Access, stored.AccountID)
	}
	if want := clock().Add(time.Hour); !stored.Expires.Equal(want) {
		t.Errorf("the file's token expires at %v, want %v", stored.Expires, want)
	}
	var entry struct {
		Refresh string `json:"refresh"`
	}
	if err := json.Unmarshal(entries(t, path)[auth.ProviderCodex], &entry); err != nil {
		t.Fatalf("the entry is not an oauth credential: %v", err)
	}
	if entry.Refresh != newRefresh {
		t.Errorf("the file holds the refresh token %q, want the rotated %q", entry.Refresh, newRefresh)
	}

	again, err := credentials.Access(context.Background())
	if err != nil {
		t.Fatalf("the credential did not reach a token after the forced refresh: %v", err)
	}
	if again.Access != access {
		t.Errorf("the access got %q, want the token the forced refresh stored", again.Access)
	}
	if endpoint.count() != 1 {
		t.Errorf("the endpoint was asked %d times, want the forced refresh's one trade to serve the access too", endpoint.count())
	}
}

// TestAFailedForcedRefreshReturnsNoLiveCredential covers the failure that has a
// live credential sitting in the file. The caller asked for a trade, the trade
// failed, and the caller is told so rather than handed the credential it
// already had. The endpoint is asked once, not retried with a refresh token
// that may be spent.
func TestAFailedForcedRefreshReturnsNoLiveCredential(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(10*time.Hour)))
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusBadRequest, `{"error":"invalid_grant"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Refresh(context.Background())
	if !errors.Is(err, auth.ErrRefresh) {
		t.Fatalf("the refusal is %v, want ErrRefresh: a failed forced refresh is not the stored credential", err)
	}
	if token.Access != "" {
		t.Errorf("the failed forced refresh returned the credential %q", token.Access)
	}
	if endpoint.count() != 1 {
		t.Errorf("the endpoint was asked %d times, want the one trade that failed", endpoint.count())
	}
}

// TestConcurrentForcedRefreshesTradeInTurn covers two forced refreshes that
// overlap. Each trades once, in turn under the lock. The second trades the
// refresh token the first stored, not the one it read before waiting.
func TestConcurrentForcedRefreshesTradeInTurn(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(10*time.Hour)))
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = credentials.Refresh(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d did not reach a token: %v", i, err)
		}
	}
	if endpoint.count() != 2 {
		t.Fatalf("the endpoint was asked %d times, want one trade per forced call", endpoint.count())
	}
	if got := endpoint.form(t, 0)["refresh_token"]; got != oldRefresh {
		t.Errorf("the first trade carried the refresh token %q, want the stored %q", got, oldRefresh)
	}
	if got := endpoint.form(t, 1)["refresh_token"]; got != newRefresh {
		t.Errorf("the second trade carried the refresh token %q, want the rotated %q", got, newRefresh)
	}
	if stored := storedCredential(t, path); stored.Access != access {
		t.Errorf("the file holds the token %q, want the last traded one", stored.Access)
	}
}
