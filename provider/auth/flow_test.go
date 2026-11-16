package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
)

// signInServer is a fake authorization server: it trades a code for a credential and records the
// forms it was posted.
type signInServer struct {
	*httptest.Server
	// mu guards the answers and the exchanges.
	mu sync.Mutex
	// answer is what the token endpoint replies with.
	answer string
	// status is the token endpoint's status.
	status int
	// redirect is where the token endpoint sends a client that is asked to follow one.
	redirect string
	// exchanges are the forms the token endpoint was posted.
	exchanges []url.Values
}

// newSignInServer starts a fake authorization server.
func newSignInServer(t *testing.T) *signInServer {
	t.Helper()
	fake := &signInServer{status: http.StatusOK}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		fake.mu.Lock()
		fake.exchanges = append(fake.exchanges, r.PostForm)
		answer, status, redirect := fake.answer, fake.status, fake.redirect
		fake.mu.Unlock()
		if redirect != "" {
			w.Header().Set("Location", redirect)
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(fake.Close)
	return fake
}

// answerWith sets the credential the token endpoint hands out.
func (s *signInServer) answerWith(body string) {
	s.mu.Lock()
	s.answer = body
	s.mu.Unlock()
}

// exchange is the form of one trade.
func (s *signInServer) exchange(t *testing.T, at int) url.Values {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if at >= len(s.exchanges) {
		t.Fatalf("the endpoint traded %d codes, want more than %d", len(s.exchanges), at)
	}
	return s.exchanges[at]
}

// flowFor points the Codex sign-in at a fake server, with the same shape it declares in
// production.
func flowFor(server *signInServer) auth.Flow {
	flow := auth.CodexLogin
	flow.TokenURL = server.URL + "/token"
	flow.AuthorizeURL = server.URL + "/authorize"
	return flow
}

// credentialAnswer is what a token endpoint answers a traded code with.
func credentialAnswer(access, refresh string, lifetime time.Duration) string {
	return `{"access_token":"` + access + `","refresh_token":"` + refresh + `","expires_in":` +
		strconv.Itoa(int(lifetime.Seconds())) + `}`
}

// TestTheCodexSignInAsksForWhatItsClientIsRegisteredWith covers the authorization request: the
// Codex CLI's client, its own redirect, PKCE, a state, and the two parameters its flow expects.
func TestTheCodexSignInAsksForWhatItsClientIsRegisteredWith(t *testing.T) {
	attempt, err := auth.CodexLogin.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	parsed, err := url.Parse(attempt.URL)
	if err != nil {
		t.Fatalf("the URL: %v", err)
	}
	if got := parsed.Host + parsed.Path; got != "auth.openai.com/oauth/authorize" {
		t.Errorf("the sign-in goes to %q, want the Codex authorization endpoint", got)
	}
	query := parsed.Query()
	for name, want := range map[string]string{
		"response_type":              "code",
		"client_id":                  auth.DefaultClientID,
		"redirect_uri":               "http://localhost:1455/auth/callback",
		"scope":                      "openid profile email offline_access",
		"state":                      attempt.State,
		"code_challenge_method":      "S256",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"originator":                 "poo",
	} {
		if got := query.Get(name); got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}
	digest := sha256.Sum256([]byte(attempt.Verifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(digest[:])
	if got := query.Get("code_challenge"); got != wantChallenge {
		t.Errorf("the challenge is %q, want the S256 of the verifier %q", got, wantChallenge)
	}
}

// TestTheCodexSignInTradesItsCode covers the exchange: the flow's client, redirect and verifier,
// and a credential that records the client and endpoint it was granted by.
func TestTheCodexSignInTradesItsCode(t *testing.T) {
	server := newSignInServer(t)
	flow := flowFor(server)
	attempt, err := flow.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	server.answerWith(credentialAnswer(codexToken(t, "acct_signed_in"), "a-new-refresh", time.Hour))

	credential, err := attempt.Exchange(context.Background(), server.Client(), auth.Callback{Code: "the-code", State: attempt.State})
	if err != nil {
		t.Fatalf("the exchange: %v", err)
	}
	form := server.exchange(t, 0)
	for name, want := range map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     auth.DefaultClientID,
		"code":          "the-code",
		"code_verifier": attempt.Verifier,
		"redirect_uri":  "http://localhost:1455/auth/callback",
	} {
		if got := form.Get(name); got != want {
			t.Errorf("the trade sent %s=%q, want %q", name, got, want)
		}
	}
	if credential.Access == "" || credential.Refresh != "a-new-refresh" {
		t.Errorf("the credential is %+v", credential)
	}
	if credential.AccountID != "acct_signed_in" {
		t.Errorf("the credential names the account %q, want the token's", credential.AccountID)
	}
	if credential.ClientID != auth.DefaultClientID {
		t.Errorf("the credential records the client %q, want the one that granted it", credential.ClientID)
	}
	if credential.TokenURL != flow.TokenURL {
		t.Errorf("the credential records the endpoint %q, want %q", credential.TokenURL, flow.TokenURL)
	}
	if credential.Kind != auth.KindOAuth {
		t.Errorf("the credential is %q, want an oauth one", credential.Kind)
	}
	want := time.Now().Add(time.Hour)
	if d := credential.Expires.Sub(want); d < -5*time.Second || d > 5*time.Second {
		t.Errorf("the credential expires at %v, want within a few seconds of %v", credential.Expires, want)
	}
}

// TestASignInFallsBackThroughTheLadder covers the rungs a sign-in takes when the flow names
// neither endpoint nor client: the provider's declaration answers both, and the credential
// records what it was granted by.
func TestASignInFallsBackThroughTheLadder(t *testing.T) {
	server := newSignInServer(t)
	flow := auth.Flow{
		Name:         "prov",
		Provider:     auth.Provider{ID: "prov", Kind: auth.KindOAuth, TokenURL: server.URL + "/token", ClientID: "prov-client", Account: func(access, stored string) string { return "acct_prov" }},
		Method:       auth.MethodBrowser,
		AuthorizeURL: server.URL + "/authorize",
		ClientID:     "prov-client",
		RedirectURI:  "http://127.0.0.1:1455/auth/callback",
	}
	attempt, err := flow.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	// The attempt's flow is a value: clear the client so the trade takes the provider's
	// declaration, not the flow's own name.
	attempt.Flow.ClientID = ""
	server.answerWith(credentialAnswer(codexToken(t, "acct_prov"), "a-new-refresh", time.Hour))

	credential, err := attempt.Exchange(context.Background(), server.Client(), auth.Callback{Code: "the-code", State: attempt.State})
	if err != nil {
		t.Fatalf("the exchange: %v", err)
	}
	if got := server.exchange(t, 0).Get("client_id"); got != "prov-client" {
		t.Errorf("the trade sent client_id=%q, want the provider's", got)
	}
	if credential.ClientID != "prov-client" {
		t.Errorf("the credential records the client %q, want the provider's", credential.ClientID)
	}
	if credential.TokenURL != server.URL+"/token" {
		t.Errorf("the credential records the endpoint %q, want the provider's", credential.TokenURL)
	}
}

// TestASignInFallsBackToTheDefaultClient covers the ladder's last rung on the sign-in side: a
// provider that declares no client sends, and records, the package's default client.
func TestASignInFallsBackToTheDefaultClient(t *testing.T) {
	server := newSignInServer(t)
	flow := auth.Flow{
		Name:         "prov",
		Provider:     auth.Provider{ID: "prov", Kind: auth.KindOAuth, TokenURL: server.URL + "/token", Account: func(access, stored string) string { return "acct_prov" }},
		Method:       auth.MethodBrowser,
		AuthorizeURL: server.URL + "/authorize",
		ClientID:     "flow-client",
		RedirectURI:  "http://127.0.0.1:1455/auth/callback",
	}
	attempt, err := flow.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	// The attempt's flow is a value: clear the client so neither the flow nor the provider
	// names one, and the default answers.
	attempt.Flow.ClientID = ""
	server.answerWith(credentialAnswer(codexToken(t, "acct_prov"), "a-new-refresh", time.Hour))

	credential, err := attempt.Exchange(context.Background(), server.Client(), auth.Callback{Code: "the-code", State: attempt.State})
	if err != nil {
		t.Fatalf("the exchange: %v", err)
	}
	if got := server.exchange(t, 0).Get("client_id"); got != auth.DefaultClientID {
		t.Errorf("the trade sent client_id=%q, want the default", got)
	}
	if credential.ClientID != auth.DefaultClientID {
		t.Errorf("the credential records the client %q, want the default", credential.ClientID)
	}
}

// TestASignInRefusesWhatIsNotItsAttempt covers the callbacks that must not be traded: a refusal,
// another attempt's state, and nothing at all.
func TestASignInRefusesWhatIsNotItsAttempt(t *testing.T) {
	server := newSignInServer(t)
	flow := flowFor(server)
	attempt, err := flow.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	server.answerWith(credentialAnswer(codexToken(t, "acct"), "refresh", time.Hour))
	for _, c := range []struct {
		name     string
		callback auth.Callback
		want     string
	}{
		{name: "the operator refused", callback: auth.Callback{Error: "access_denied", State: attempt.State}, want: "access_denied"},
		{name: "another attempt's state", callback: auth.Callback{Code: "c", State: "another-state"}, want: "state"},
		{name: "no code", callback: auth.Callback{State: attempt.State}, want: "no authorization code"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := attempt.Exchange(context.Background(), server.Client(), c.callback)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the exchange is %v, want a refusal naming %q", err, c.want)
			}
			if !errors.Is(err, auth.ErrSignIn) {
				t.Errorf("the refusal is %v, want ErrSignIn", err)
			}
			if errors.Is(err, auth.ErrRefresh) {
				t.Errorf("the refusal is %v, and it carries ErrRefresh", err)
			}
		})
	}
}

// TestASignInRefusesAnAnswerThatIsNotACredential covers the endpoint's own failures: an error
// body, and an answer with no refresh token.
func TestASignInRefusesAnAnswerThatIsNotACredential(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer string
		status int
		want   string
	}{
		{name: "a refusal", answer: `{"error":"invalid_grant"}`, status: http.StatusBadRequest, want: "invalid_grant"},
		{name: "no refresh token", answer: `{"access_token":"a","expires_in":3600}`, status: http.StatusOK, want: "no credential"},
	} {
		t.Run(c.name, func(t *testing.T) {
			server := newSignInServer(t)
			flow := flowFor(server)
			attempt, err := flow.Authorize()
			if err != nil {
				t.Fatalf("the attempt: %v", err)
			}
			server.mu.Lock()
			server.answer, server.status = c.answer, c.status
			server.mu.Unlock()
			_, err = attempt.Exchange(context.Background(), server.Client(), auth.Callback{Code: "c", State: attempt.State})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the exchange is %v, want a refusal naming %q", err, c.want)
			}
			if !errors.Is(err, auth.ErrSignIn) {
				t.Errorf("the refusal is %v, want ErrSignIn", err)
			}
			if errors.Is(err, auth.ErrRefresh) {
				t.Errorf("the refusal is %v, and it carries ErrRefresh", err)
			}
		})
	}
}

// TestASignInRefusesARedirect covers the trade that is pointed elsewhere. Go would replay the
// request body — which carries the code — wherever the redirect led, so a redirect is refused:
// the target receives nothing, and the failure is a sign-in failure, not a refresh failure.
func TestASignInRefusesARedirect(t *testing.T) {
	var leaked atomic.Bool
	var leakedHost atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
		leakedHost.Store(r.Host)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"stolen","refresh_token":"stolen","expires_in":3600}`))
	}))
	t.Cleanup(target.Close)
	server := newSignInServer(t)
	flow := flowFor(server)
	attempt, err := flow.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	server.mu.Lock()
	server.redirect = target.URL + "/token"
	server.mu.Unlock()
	_, err = attempt.Exchange(context.Background(), server.Client(), auth.Callback{Code: "c", State: attempt.State})
	if !errors.Is(err, auth.ErrSignIn) {
		t.Fatalf("the redirect is %v, want ErrSignIn", err)
	}
	if leaked.Load() {
		t.Errorf("the sign-in was replayed at the redirect's target %v", leakedHost.Load())
	}
}

// TestParseCallback covers the shapes a browser and a person produce.
func TestParseCallback(t *testing.T) {
	for _, c := range []struct {
		name  string
		input string
		code  string
		state string
	}{
		{name: "the URL the browser landed on", input: "http://localhost:1455/auth/callback?code=abc&state=xyz", code: "abc", state: "xyz"},
		{name: "a bare code", input: "abc", code: "abc"},
		{name: "a query fragment", input: "code=abc&state=xyz", code: "abc", state: "xyz"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := auth.ParseCallback(c.input)
			if got.Code != c.code || got.State != c.state {
				t.Errorf("the callback is %+v, want code %q state %q", got, c.code, c.state)
			}
		})
	}
	if got := auth.ParseCallback("http://localhost:1455/auth/callback?error=access_denied&error_description=nope"); got.Error != "access_denied" || got.Description != "nope" {
		t.Errorf("the refusal is %+v", got)
	}
}

// TestAKeyFlowDoesNotSignInThroughABrowser covers the method that has no authorize step: asking a
// key flow for an authorization is a misuse, not an empty attempt.
func TestAKeyFlowDoesNotSignInThroughABrowser(t *testing.T) {
	key := auth.Flow{Name: "Ollama Cloud", Provider: auth.Provider{ID: "ollama-cloud", Kind: auth.KindAPIKey}, Method: auth.MethodKey}
	if _, err := key.Authorize(); err == nil {
		t.Fatal("a key flow produced an authorization attempt")
	}
}

// TestParseCallbackReadsAPathWithAQuery covers the shape the loopback handler sees: a request URL
// is a path with a query, not an absolute URL, and the answer is in the query either way.
func TestParseCallbackReadsAPathWithAQuery(t *testing.T) {
	got := auth.ParseCallback("/auth/callback?code=abc&state=xyz")
	if got.Code != "abc" || got.State != "xyz" {
		t.Errorf("the callback is %+v, want code abc state xyz", got)
	}
}
