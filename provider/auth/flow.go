package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Method is how a person signs in.
type Method string

const (
	// MethodBrowser is a browser, a loopback callback on the flow's redirect, and a pasted code
	// for a machine the browser cannot reach.
	MethodBrowser Method = "browser"
	// MethodKey is a key the person pastes. There is no redirect and no token to trade.
	MethodKey Method = "key"
)

// Flow is how one provider signs a person in: what the operator picks, where the authorization
// starts and what it asks for, and what a credential needs once it exists.
//
// A provider's sign-in is a value, not a code path: the legacy Codex flow, a first-party
// "Sign in with ChatGPT" flow and a key prompt are three declarations, and the machinery that
// runs them is one.
type Flow struct {
	// Name is what the operator picks, in the words the provider uses for itself.
	Name string
	// Provider is what the credential needs once it exists.
	Provider Provider
	// Method is how the person signs in.
	Method Method
	// AuthorizeURL is where the authorization starts. Empty for a key.
	AuthorizeURL string
	// TokenURL is where a code is traded. Empty means the provider's, then this package's
	// default.
	TokenURL string
	// ClientID is the client a new sign-in names. Empty means no client is named, which is a
	// flow that registers one first.
	ClientID string
	// RedirectURI is the callback the code comes back to. The authorization server checks it
	// against the client's registration, so it is the flow's own: the Codex client's is on
	// localhost and a first-party flow's is on 127.0.0.1.
	RedirectURI string
	// Scopes are what the sign-in asks for.
	Scopes []string
	// Params are what a provider's own flow adds to the authorization request.
	Params map[string]string
	// Originator names the agent the sign-in came from. It is a label the endpoint records, not
	// a host, and it is never used to authorize anything. Empty sends none.
	Originator string
}

// CodexLogin is OpenAI's Codex sign-in, the one pi calls legacy: the Codex CLI's public client,
// on the endpoint poo's Codex transport already sends to.
//
// The client is not poo's. A credential it grants records it, so a flow that has a client of its
// own is a declaration beside this one rather than a change to it.
var CodexLogin = Flow{
	Name:         "OpenAI Codex (legacy)",
	Provider:     Codex,
	Method:       MethodBrowser,
	AuthorizeURL: "https://auth.openai.com/oauth/authorize",
	TokenURL:     DefaultTokenURL,
	ClientID:     DefaultClientID,
	RedirectURI:  "http://localhost:1455/auth/callback",
	Scopes:       []string{"openid", "profile", "email", "offline_access"},
	Params: map[string]string{
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
	},
	Originator: "poo",
}

// Attempt is one sign-in in progress: the URL the operator opens, and the values the code that
// comes back is traded with.
//
// The verifier and the state are the secret half. Neither is written to a log or into an error,
// and both are needed again to trade the code.
type Attempt struct {
	// URL is the authorization URL, to open in a browser or hand to the operator.
	URL string
	// State is the value the callback must carry back, so a code from another attempt is
	// refused.
	State string
	// Verifier is the PKCE verifier the code is traded with.
	Verifier string
	// Flow is the sign-in this attempt belongs to, so the code is traded by the flow that asked
	// for it.
	Flow Flow
}

// Authorize starts a sign-in: the URL for the operator, and the values the code it brings back
// is traded with.
//
// The flow is PKCE: the URL carries the challenge, only this process holds the verifier, and a
// code intercepted in the browser is useless without it.
func (f Flow) Authorize() (Attempt, error) {
	if f.Method != MethodBrowser {
		return Attempt{}, fmt.Errorf("auth: %s does not sign in through a browser", f.Name)
	}
	if f.AuthorizeURL == "" || f.ClientID == "" || f.RedirectURI == "" {
		return Attempt{}, fmt.Errorf("auth: %s names no endpoint, client or redirect", f.Name)
	}
	verifier, err := randomValue(32)
	if err != nil {
		return Attempt{}, err
	}
	state, err := randomValue(16)
	if err != nil {
		return Attempt{}, err
	}
	digest := sha256.Sum256([]byte(verifier))
	endpoint, err := url.Parse(f.AuthorizeURL)
	if err != nil {
		return Attempt{}, fmt.Errorf("%w: %s: %w", ErrRefresh, f.AuthorizeURL, err)
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", f.ClientID)
	query.Set("redirect_uri", f.RedirectURI)
	query.Set("scope", strings.Join(f.Scopes, " "))
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	query.Set("code_challenge_method", "S256")
	for name, value := range f.Params {
		query.Set(name, value)
	}
	if f.Originator != "" {
		query.Set("originator", f.Originator)
	}
	endpoint.RawQuery = query.Encode()
	return Attempt{URL: endpoint.String(), State: state, Verifier: verifier, Flow: f}, nil
}

// Callback is what the authorization endpoint sent back, as a browser's redirect or a paste
// carries it.
type Callback struct {
	// Code is the authorization code to trade.
	Code string
	// State is the state the endpoint returned.
	State string
	// Error is an OAuth error the endpoint returned instead of a code.
	Error string
	// Description is the endpoint's own words about the error.
	Description string
}

// ParseCallback reads what an operator pasted back: the redirect URL the browser landed on, or a
// `code=...&state=...` fragment, or the code alone.
//
// A paste is how a sign-in completes on a machine whose loopback the browser cannot reach, so
// the shapes a browser produces and a person copies are both read.
func ParseCallback(input string) Callback {
	value := strings.TrimSpace(input)
	if value == "" {
		return Callback{}
	}
	query := url.Values{}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		query = parsed.Query()
	} else if strings.Contains(value, "=") {
		if parsed, err := url.ParseQuery(strings.TrimPrefix(value, "?")); err == nil {
			query = parsed
		}
	}
	if len(query) == 0 {
		return Callback{Code: value}
	}
	return Callback{
		Code:        query.Get("code"),
		State:       query.Get("state"),
		Error:       query.Get("error"),
		Description: query.Get("error_description"),
	}
}

// Exchange trades a callback for a credential.
//
// The trade is the one the URL was built for: the same client, the same redirect, and the
// verifier the challenge was made from. A callback that carries an error, or a state that is
// not this attempt's, is refused before anything is traded.
//
// The credential records the client and the endpoint it was granted by, so a refresh names them
// (#219) instead of a default.
func (a Attempt) Exchange(ctx context.Context, client *http.Client, callback Callback) (Credential, error) {
	if callback.Error != "" {
		return Credential{}, fmt.Errorf("%w: the sign-in was refused: %s", ErrRefresh, describeRefusal(callback))
	}
	if callback.Code == "" {
		return Credential{}, fmt.Errorf("%w: no authorization code came back", ErrRefresh)
	}
	if callback.State != a.State {
		return Credential{}, fmt.Errorf("%w: the callback's state is not this sign-in's", ErrRefresh)
	}
	flow := a.Flow
	endpoint := flow.TokenURL
	if endpoint == "" {
		endpoint = flow.Provider.TokenURL
	}
	if endpoint == "" {
		endpoint = DefaultTokenURL
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	answer, err := postForm(ctx, client, endpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {flow.ClientID},
		"code":          {callback.Code},
		"code_verifier": {a.Verifier},
		"redirect_uri":  {flow.RedirectURI},
	})
	if err != nil {
		return Credential{}, err
	}
	if answer.AccessToken == "" || answer.RefreshToken == "" || answer.ExpiresIn <= 0 {
		return Credential{}, fmt.Errorf("%w: %s: the answer carries no credential", ErrRefresh, endpoint)
	}
	if flow.Provider.Kind == KindOAuth && flow.Provider.needsAccount() &&
		flow.Provider.account(answer.AccessToken, "") == "" {
		return Credential{}, fmt.Errorf("%w: %s: the token names no account", ErrRefresh, endpoint)
	}
	return Credential{
		Kind:      KindOAuth,
		Access:    answer.AccessToken,
		Refresh:   answer.RefreshToken,
		Expires:   time.Now().Add(time.Duration(answer.ExpiresIn * float64(time.Second))),
		AccountID: flow.Provider.account(answer.AccessToken, ""),
		ClientID:  flow.ClientID,
		TokenURL:  endpoint,
	}, nil
}

// describeRefusal renders a refused sign-in. The endpoint's own words are kept short: they are
// untrusted, and a caller's log is no place for them.
func describeRefusal(callback Callback) string {
	if callback.Description == "" {
		return callback.Error
	}
	return callback.Error + ": " + callback.Description
}

// randomValue returns a base64url value from n random bytes.
func randomValue(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: reading randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
