package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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
	// Nonce asks the authorization to return an ID token that carries a value
	// only this attempt knows. A flow that sets it checks the token, and one
	// that does not reads no ID token at all.
	Nonce bool
	// Params are what a provider's own flow adds to the authorization request.
	Params map[string]string
	// Originator names the agent the sign-in came from. It is a label the endpoint records, not
	// a host, and it is never used to authorize anything. Empty sends none.
	Originator string
	// Unavailable says why the sign-in cannot run, when it cannot: a registration the operator
	// has not made yet, named by what it is missing. Empty means it can, and a flow that says
	// why tells the operator what to fix before a browser opens.
	Unavailable string
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
	// Nonce is the value this attempt's ID token must carry. Empty when the
	// flow asks for none.
	Nonce string
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
	if f.Unavailable != "" {
		return Attempt{}, fmt.Errorf("auth: %s: %s", f.Name, f.Unavailable)
	}
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
	nonce := ""
	if f.Nonce {
		if nonce, err = randomValue(32); err != nil {
			return Attempt{}, err
		}
	}
	digest := sha256.Sum256([]byte(verifier))
	endpoint, err := url.Parse(f.AuthorizeURL)
	if err != nil {
		return Attempt{}, fmt.Errorf("%w: %s: %w", ErrSignIn, f.AuthorizeURL, err)
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", f.ClientID)
	query.Set("redirect_uri", f.RedirectURI)
	query.Set("scope", strings.Join(f.Scopes, " "))
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	query.Set("code_challenge_method", "S256")
	if nonce != "" {
		query.Set("nonce", nonce)
	}
	for name, value := range f.Params {
		query.Set(name, value)
	}
	if f.Originator != "" {
		query.Set("originator", f.Originator)
	}
	endpoint.RawQuery = query.Encode()
	return Attempt{URL: endpoint.String(), State: state, Verifier: verifier, Nonce: nonce, Flow: f}, nil
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
// `code=...&state=...` fragment.
//
// A paste is how a sign-in completes on a machine whose loopback the browser cannot reach, so
// the shapes a browser produces and a person copies are both read. The state is the half the
// sign-in is checked against, so a paste that carries none — a code on its own — cannot complete
// it.
func ParseCallback(input string) Callback {
	value := strings.TrimSpace(input)
	if value == "" {
		return Callback{}
	}
	// A paste is usually a URL with a query, and the loopback handler's request URL is a path
	// with one: the query is what carries the answer either way, so it is read first.
	if at := strings.Index(value, "?"); at >= 0 {
		if parsed, err := url.ParseQuery(value[at+1:]); err == nil {
			return callbackOf(parsed)
		}
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		return callbackOf(parsed.Query())
	}
	if strings.Contains(value, "=") {
		if parsed, err := url.ParseQuery(strings.TrimPrefix(value, "?")); err == nil {
			return callbackOf(parsed)
		}
	}
	return Callback{Code: value}
}

// callbackOf reads the answer a query carries.
func callbackOf(query url.Values) Callback {
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
// A nil client is the package's own, with its bound on the answer and its refusal of a
// redirect.
//
// The credential records the client and the endpoint it was granted by, so a refresh names them
// (#219) instead of a default.
func (a Attempt) Exchange(ctx context.Context, client *http.Client, callback Callback) (Credential, error) {
	if callback.Error != "" {
		return Credential{}, fmt.Errorf("%w: the sign-in was refused: %s", ErrSignIn, describeRefusal(callback))
	}
	if callback.Code == "" {
		return Credential{}, fmt.Errorf("%w: no authorization code came back", ErrSignIn)
	}
	if callback.State == "" {
		return Credential{}, fmt.Errorf("%w: the answer carries no state: paste the URL the browser landed on, not the code on its own", ErrSignIn)
	}
	if callback.State != a.State {
		return Credential{}, fmt.Errorf("%w: the callback's state is not this sign-in's", ErrSignIn)
	}
	flow := a.Flow
	provider := flow.Provider
	endpoint := endpointFor(flow.TokenURL, provider, DefaultTokenURL)
	clientID := clientFor(flow.ClientID, provider, DefaultClientID)
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {callback.Code},
		"code_verifier": {a.Verifier},
		"redirect_uri":  {flow.RedirectURI},
	}
	// A desktop client trades its code with the secret Google issued it. The
	// secret is public by design and comes from the environment, so it is
	// named here and not read from the credential.
	if provider.ClientSecret != "" {
		form.Set("client_secret", provider.ClientSecret)
	}
	// A grant bound to a key pair proves itself on the code exchange too, with
	// the code's own hash as the proof's identifier: a random one fails, and
	// the code is single-use.
	var key *DPoPKey
	headers := map[string]string{}
	if provider.ProofKeys {
		created, err := newDPoPKey()
		if err != nil {
			return Credential{}, fmt.Errorf("%w: %v", ErrSignIn, err)
		}
		private, err := parseDPoPKey(&created)
		if err != nil {
			return Credential{}, err
		}
		proof, err := (dpopProof{JTI: proofID(callback.Code), HTU: endpoint}).sign(private, time.Now)
		if err != nil {
			return Credential{}, fmt.Errorf("%w: %v", ErrSignIn, err)
		}
		key = &created
		headers["DPoP"] = proof
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	answer, err := postForm(ctx, ErrSignIn, client, endpoint, form, headers)
	if err != nil {
		return Credential{}, err
	}
	if answer.AccessToken == "" || answer.RefreshToken == "" || answer.ExpiresIn <= 0 {
		return Credential{}, fmt.Errorf("%w: %s: the answer carries no credential", ErrSignIn, endpoint)
	}
	// A grant that does not carry the scope the provider needs is a sign-in
	// that bought nothing: the endpoint took the person's consent and gave back
	// less than it was asked for.
	if provider.RequiredScope != "" && !slices.Contains(strings.Fields(answer.Scope), provider.RequiredScope) {
		return Credential{}, fmt.Errorf("%w: %s did not grant %s", ErrSignIn, endpoint, provider.RequiredScope)
	}
	if flow.Nonce {
		if err := checkIDToken(answer.IDToken, clientID, a.Nonce, time.Now()); err != nil {
			return Credential{}, err
		}
	}
	credential := Credential{
		Kind:      KindOAuth,
		Access:    answer.AccessToken,
		Refresh:   answer.RefreshToken,
		Expires:   time.Now().Add(time.Duration(answer.ExpiresIn * float64(time.Second))),
		AccountID: provider.account(answer.AccessToken, ""),
		ClientID:  clientID,
		TokenURL:  endpoint,
		DPoP:      key,
	}
	if err := validate(credential, provider); err != nil {
		return Credential{}, fmt.Errorf("%w: %s: %v", ErrSignIn, endpoint, err)
	}
	return credential, nil
}

// describeRefusal renders a refused sign-in. The endpoint's error_description is untrusted,
// and a caller's log is no place for an answer's words, so the standard OAuth error code
// alone reports the refusal.
func describeRefusal(callback Callback) string {
	return callback.Error
}

// randomValue returns a base64url value from n random bytes.
func randomValue(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: reading randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
