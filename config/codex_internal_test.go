package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
	"github.com/sdougbrown/poo/provider/codex"
	"github.com/sdougbrown/poo/provider/piauth"
)

// writeCodexAuthFile writes pi's auth.json with a live Codex credential and
// returns its path.
func writeCodexAuthFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), piimport.AuthFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing pi's auth file: %v", err)
	}
	return path
}

// liveCodexAuth is pi's auth.json with a credential that needs no refresh.
const liveCodexAuth = `{"openai-codex":{"type":"oauth","access":"live-token","refresh":"rt_live","expires":4102444800000,"accountId":"acct_live"}}`

// TestCodexCredentialsRetryWhenTheAuthFileIsLocked covers the lock. pi holds
// the auth file while it refreshes, so the failure is transient and retryable,
// and it names the lock it found.
func TestCodexCredentialsRetryWhenTheAuthFileIsLocked(t *testing.T) {
	path := writeCodexAuthFile(t, liveCodexAuth)
	lockPath := path + ".lock"
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	resolve := codexCredentials(piauth.Credentials{Path: path, Wait: 50 * time.Millisecond})
	_, err := resolve(context.Background())
	var failure *poo.ProviderError
	if !errors.As(err, &failure) {
		t.Fatalf("the lock is %v, want a provider failure", err)
	}
	if !failure.Retryable() {
		t.Errorf("the lock is not retryable: %+v", failure)
	}
	if !strings.Contains(failure.Message, lockPath) {
		t.Errorf("the failure is %q, and it does not name the lock %q", failure.Message, lockPath)
	}
}

// TestCodexCredentialsKeepAnUnreadableAuthFileTerminal covers the other
// failures: a malformed auth file will not read on another attempt, so it is
// returned as it came rather than retried.
func TestCodexCredentialsKeepAnUnreadableAuthFileTerminal(t *testing.T) {
	path := writeCodexAuthFile(t, `{"openai-codex":`)
	resolve := codexCredentials(piauth.Credentials{Path: path, Wait: time.Millisecond})
	_, err := resolve(context.Background())
	var failure *poo.ProviderError
	if errors.As(err, &failure) {
		t.Fatalf("the malformed file was mapped to a provider failure: %+v", failure)
	}
	if !errors.Is(err, piauth.ErrStorage) {
		t.Errorf("the failure is %v, want the storage failure it came from", err)
	}
}

// TestCodexCredentialsPreserveCancellation covers the context: a cancelled
// resolution is the caller's cancellation, not a transient provider failure.
func TestCodexCredentialsPreserveCancellation(t *testing.T) {
	path := writeCodexAuthFile(t, liveCodexAuth)
	if err := os.Mkdir(path+".lock", 0o700); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	resolve := codexCredentials(piauth.Credentials{Path: path, Wait: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolve(ctx)
	var failure *poo.ProviderError
	if errors.As(err, &failure) {
		t.Fatalf("the cancellation was mapped to a provider failure: %+v", failure)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the failure is %v, want the cancellation", err)
	}
}

// TestTheCodexTransportRetriesALockedAuthFile covers the mapping where it is
// consumed. A locked auth file is transient, so the transport spends its
// attempts on it, and no request goes out without a credential.
func TestTheCodexTransportRetriesALockedAuthFile(t *testing.T) {
	path := writeCodexAuthFile(t, liveCodexAuth)
	if err := os.Mkdir(path+".lock", 0o700); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	transport := codex.New(codex.Options{
		Endpoint:    codex.DefaultEndpoint,
		Model:       "gpt-6.1-sol",
		Credentials: codexCredentials(piauth.Credentials{Path: path, Wait: 20 * time.Millisecond}),
		MaxAttempts: 3,
		Backoff:     []time.Duration{0},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("the transport sent a request without a credential")
		})},
	})
	var attempts []poo.Attempt
	_, _, err := transport.Stream(context.Background(), poo.ChatRequest{
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		OnAttempt: func(attempt poo.Attempt) { attempts = append(attempts, attempt) },
	})
	var failure *poo.ProviderError
	if !errors.As(err, &failure) || !failure.Retryable() {
		t.Fatalf("the request is %v, want the retryable lock failure", err)
	}
	if len(attempts) != 3 {
		t.Fatalf("the transport made %d attempts, want its three", len(attempts))
	}
	for i, attempt := range attempts[:2] {
		if !attempt.Retried {
			t.Errorf("attempt %d was not retried", i+1)
		}
	}
	if attempts[2].Retried {
		t.Error("the last attempt was retried")
	}
}

// TestTheCodexTransportDoesNotRetryAnUnreadableAuthFile covers the other side:
// a malformed file fails the same way on every attempt, so the transport spends
// one attempt and settles.
func TestTheCodexTransportDoesNotRetryAnUnreadableAuthFile(t *testing.T) {
	path := writeCodexAuthFile(t, `{"openai-codex":`)
	transport := codex.New(codex.Options{
		Endpoint:    codex.DefaultEndpoint,
		Model:       "gpt-6.1-sol",
		Credentials: codexCredentials(piauth.Credentials{Path: path, Wait: 20 * time.Millisecond}),
		MaxAttempts: 3,
		Backoff:     []time.Duration{0},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("the transport sent a request without a credential")
		})},
	})
	var attempts []poo.Attempt
	_, _, err := transport.Stream(context.Background(), poo.ChatRequest{
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		OnAttempt: func(attempt poo.Attempt) { attempts = append(attempts, attempt) },
	})
	var failure *poo.ProviderError
	if !errors.As(err, &failure) || failure.Retryable() {
		t.Fatalf("the request is %v, want a terminal provider failure", err)
	}
	if len(attempts) != 1 {
		t.Errorf("the transport made %d attempts, want one", len(attempts))
	}
}

// roundTripFunc turns a function into an HTTP transport.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls the function.
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// expiredCodexAuth is pi's auth.json with a credential that needs a refresh.
const expiredCodexAuth = `{"openai-codex":{"type":"oauth","access":"expired-token","refresh":"rt_old","expires":1,"accountId":"acct_old"}}`

// TestCodexCredentialErrorKeepsATradedTokenTerminal covers the traded token
// that was not stored. The failure is terminal even with a lock in its chain,
// because another attempt would trade a refresh token that is already spent.
func TestCodexCredentialErrorKeepsATradedTokenTerminal(t *testing.T) {
	err := fmt.Errorf("%w: another writer took the lock: %w", piauth.ErrPersist, piauth.ErrLocked)
	mapped := codexCredentialError(context.Background(), err)
	var failure *poo.ProviderError
	if !errors.As(mapped, &failure) {
		t.Fatalf("the traded token is %v, want a provider failure", mapped)
	}
	if failure.Retryable() {
		t.Errorf("the traded token is retryable: %+v", failure)
	}
}

// TestCodexCredentialErrorKeepsAnUncertainRefreshTerminal covers the refresh
// whose answer was lost. The endpoint may have traded the token, so the
// failure is terminal even when the transport error is a network one.
func TestCodexCredentialErrorKeepsAnUncertainRefreshTerminal(t *testing.T) {
	err := fmt.Errorf("%w: %w", piauth.ErrRefresh, &net.DNSError{Err: "i/o timeout", IsTimeout: true})
	mapped := codexCredentialError(context.Background(), err)
	var failure *poo.ProviderError
	if !errors.As(mapped, &failure) {
		t.Fatalf("the refresh is %v, want a provider failure", mapped)
	}
	if failure.Retryable() {
		t.Errorf("the refresh is retryable: %+v", failure)
	}
}

// TestCodexCredentialErrorPreservesCancellation covers the context: a
// resolution that ended with it is the caller's cancellation, whatever the
// credential file was doing.
func TestCodexCredentialErrorPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := fmt.Errorf("%w: another writer took the lock: %w", piauth.ErrPersist, piauth.ErrLocked)
	mapped := codexCredentialError(ctx, err)
	var failure *poo.ProviderError
	if errors.As(mapped, &failure) {
		t.Fatalf("the cancellation was classified as a provider failure: %+v", failure)
	}
}

// blockingTokenEndpoint answers one refresh after the test releases it, and
// counts every request it receives. The first request closes entered.
type blockingTokenEndpoint struct {
	*httptest.Server
	requests atomic.Int64
	entered  chan struct{}
	answer   chan struct{}
	once     sync.Once
}

// newBlockingTokenEndpoint starts the endpoint.
func newBlockingTokenEndpoint(t *testing.T) *blockingTokenEndpoint {
	t.Helper()
	endpoint := &blockingTokenEndpoint{entered: make(chan struct{}), answer: make(chan struct{})}
	endpoint.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint.requests.Add(1)
		endpoint.once.Do(func() { close(endpoint.entered) })
		<-endpoint.answer
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fresh-token","refresh_token":"rt_fresh","expires_in":3600}`)
	}))
	t.Cleanup(endpoint.Close)
	return endpoint
}

// takeOverLock replaces the lock directory, as a writer that broke the lock
// does.
func takeOverLock(t *testing.T, lockPath string) {
	t.Helper()
	if err := os.RemoveAll(lockPath); err != nil {
		t.Fatalf("removing the held lock: %v", err)
	}
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
}

// mustNotRunHTTPClient is the Codex endpoint's client: no request may go out
// without a credential.
func mustNotRunHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("the transport sent a request without a credential")
		return nil, errors.New("no credential")
	})}
}

// TestTheCodexTransportStopsAfterATradedTokenIsNotStored covers the ended
// credential: the refresh is traded, the lock is taken before the write, and
// the request settles without a second trade.
func TestTheCodexTransportStopsAfterATradedTokenIsNotStored(t *testing.T) {
	path := writeCodexAuthFile(t, expiredCodexAuth)
	endpoint := newBlockingTokenEndpoint(t)
	transport := codex.New(codex.Options{
		Endpoint: codex.DefaultEndpoint,
		Model:    "gpt-6.1-sol",
		Credentials: codexCredentials(piauth.Credentials{
			Path:     path,
			TokenURL: endpoint.URL,
			Wait:     20 * time.Millisecond,
		}),
		MaxAttempts: 3,
		Backoff:     []time.Duration{0},
		HTTPClient:  mustNotRunHTTPClient(t),
	})
	var attempts []poo.Attempt
	done := make(chan error, 1)
	go func() {
		_, _, err := transport.Stream(context.Background(), poo.ChatRequest{
			Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
			OnAttempt: func(attempt poo.Attempt) { attempts = append(attempts, attempt) },
		})
		done <- err
	}()
	select {
	case <-endpoint.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the refresh did not start")
	}
	takeOverLock(t, path+".lock")
	close(endpoint.answer)
	// The taker lets go, so a retry would take the lock and trade again.
	if err := os.RemoveAll(path + ".lock"); err != nil {
		t.Fatalf("releasing the taken lock: %v", err)
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the request did not settle")
	}
	var failure *poo.ProviderError
	if !errors.As(err, &failure) || failure.Retryable() {
		t.Fatalf("the request is %v, want a terminal provider failure", err)
	}
	if len(attempts) != 1 {
		t.Errorf("the transport made %d attempts, want one", len(attempts))
	}
	if got := endpoint.requests.Load(); got != 1 {
		t.Errorf("the token endpoint answered %d refreshes, want one", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading pi's auth file: %v", err)
	}
	if !strings.Contains(string(data), "expired-token") {
		t.Errorf("the file was written after the lock was taken:\n%s", data)
	}
}

// TestTheCodexTransportDoesNotRetryAnUncertainRefresh covers a refresh whose
// request reached the endpoint and whose answer did not come back. The token
// may be traded, so the request settles without a second exchange.
func TestTheCodexTransportDoesNotRetryAnUncertainRefresh(t *testing.T) {
	path := writeCodexAuthFile(t, expiredCodexAuth)
	var requests atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("the test endpoint does not hijack")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijacking the refresh: %v", err)
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(endpoint.Close)
	transport := codex.New(codex.Options{
		Endpoint: codex.DefaultEndpoint,
		Model:    "gpt-6.1-sol",
		Credentials: codexCredentials(piauth.Credentials{
			Path:     path,
			TokenURL: endpoint.URL,
			Wait:     20 * time.Millisecond,
		}),
		MaxAttempts: 3,
		Backoff:     []time.Duration{0},
		HTTPClient:  mustNotRunHTTPClient(t),
	})
	var attempts []poo.Attempt
	_, _, err := transport.Stream(context.Background(), poo.ChatRequest{
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		OnAttempt: func(attempt poo.Attempt) { attempts = append(attempts, attempt) },
	})
	var failure *poo.ProviderError
	if !errors.As(err, &failure) || failure.Retryable() {
		t.Fatalf("the request is %v, want a terminal provider failure", err)
	}
	if len(attempts) != 1 {
		t.Errorf("the transport made %d attempts, want one", len(attempts))
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the token endpoint received %d refreshes, want one", got)
	}
}
