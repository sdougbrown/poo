package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultHTTP is the client a token request goes through when a caller names
// none.
var defaultHTTP = &http.Client{Timeout: DefaultTimeout}

// refusingClient is the client every token path goes through: a copy of base's
// transport and timeout, with a redirect refused rather than followed. Go
// replays a 307 or 308 request body to wherever the redirect points, and a
// token request's body is a code or a refresh token. A redirect is answered as
// the refusal it is, with the status the error names.
//
// A caller that names no client gets this package's own. The redirect policy is
// the part this decides, and a token path has no business picking a transport:
// one that had to remember to pass a client would crash the first time it
// forgot.
func refusingClient(base *http.Client) *http.Client {
	if base == nil {
		base = defaultHTTP
	}
	return &http.Client{
		Transport: base.Transport,
		Timeout:   base.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// tokenResponse is the token endpoint's answer to a refresh or a sign-in.
type tokenResponse struct {
	// AccessToken is the new bearer token.
	AccessToken string `json:"access_token"`
	// RefreshToken is the token that replaces the one traded. An endpoint may
	// leave it out, and the traded token stays valid then.
	RefreshToken string `json:"refresh_token"`
	// ExpiresIn is how many seconds the access token lives.
	ExpiresIn float64 `json:"expires_in"`
	// Scope is what the grant carries, as the endpoint words it.
	Scope string `json:"scope"`
	// IDToken is the token that names the person, for a sign-in that asks for
	// one.
	IDToken string `json:"id_token"`
	// Error and ErrorDescription are the refusal, when the answer is one.
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// tokenAnswer is one answer from a token endpoint, before it is turned into a
// credential or a failure.
//
// A key-bound grant's refresh decides by these fields rather than by the status
// alone: a stale nonce and a clock the endpoint disagrees with are answers it
// retries, and both are reported beside the status.
type tokenAnswer struct {
	// Status is the HTTP status.
	Status int
	// Body is the answer's own fields.
	Body tokenResponse
	// Nonce is the DPoP-Nonce the answer carried, when it carried one.
	Nonce string
	// ServerTime is the answer's Date header in Unix seconds, which a proof
	// dated by a wrong local clock is dated by instead.
	ServerTime int64
}

// trade trades the credential's refresh token for a new access token.
//
// The request is the one pi sends: a form POST carrying the refresh grant, the
// refresh token and the client id, and the client's secret for a client that
// has one. The answer carries the new pair and the access token's lifetime. The
// account is read from the new token, and a token that names no account keeps
// the one already stored.
//
// A provider that binds its grants to a key pair is traded through that key:
// every attempt carries a proof, and two refusals are retried once — a stale
// nonce with the one the endpoint sends, and a local clock the endpoint
// disagrees with with the endpoint's own time.
//
// A failure reports the status and a standard OAuth error code, and nothing
// else from the answer. An answer is untrusted, and a caller's log is no place
// for its words.
func (c Credentials) trade(ctx context.Context, p Provider, stored credential) (credential, error) {
	endpoint := c.tokenURL(p, stored)
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {stored.Refresh},
		"client_id":     {c.clientID(p, stored)},
	}
	if p.ClientSecret != "" {
		form.Set("client_secret", p.ClientSecret)
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	if p.ProofKeys {
		return c.tradeBound(ctx, p, stored, endpoint, form)
	}
	answer, err := postForm(ctx, ErrRefresh, c.HTTP, endpoint, form, nil)
	if err != nil {
		return credential{}, err
	}
	return c.refreshed(p, stored, answer, nil, "")
}

// tradeBound trades a key-bound grant, retrying the two refusals a proof can be
// fixed for.
//
// The two are independent, so the loop retries while either applies and stops
// when the answer asks for neither. A third round buys nothing: the nonce the
// endpoint sent was already the one sent back, and the clock was already the
// endpoint's.
func (c Credentials) tradeBound(ctx context.Context, p Provider, stored credential, endpoint string, form url.Values) (credential, error) {
	key, err := parseDPoPKey(stored.DPoP)
	if err != nil {
		return credential{}, err
	}
	nonce := stored.DPoPNonce
	var iat int64
	var answer tokenAnswer
	for round := 0; ; round++ {
		proof, err := dpopProof{JTI: randomValueOrEmpty(24), HTU: endpoint, Nonce: nonce, IAT: iat}.sign(key, c.now)
		if err != nil {
			return credential{}, fmt.Errorf("%w: %v", ErrRefresh, err)
		}
		answer, err = postToken(ctx, ErrRefresh, c.HTTP, endpoint, form, map[string]string{"DPoP": proof})
		if err != nil {
			return credential{}, err
		}
		if round == 2 {
			break
		}
		// A nonce the endpoint has just named is the one to send back, and a
		// proof dated by a clock the endpoint disagrees with is dated by the
		// endpoint's own time.
		if answer.Body.Error == "use_dpop_nonce" && answer.Nonce != "" && answer.Nonce != nonce {
			nonce = answer.Nonce
			continue
		}
		if answer.Body.Error == "invalid_dpop_proof" && iat == 0 && answer.ServerTime != 0 {
			iat = answer.ServerTime
			continue
		}
		break
	}
	if answer.Status < 200 || answer.Status > 299 || answer.Body.AccessToken == "" {
		// The two the endpoint names as final: the grant expired or was
		// revoked, or the client is no longer allowed for this token.
		switch answer.Body.Error {
		case "invalid_grant", "unauthorized_client":
			return credential{}, fmt.Errorf("%w: %s: sign in again", ErrRefresh, endpoint)
		}
		return credential{}, fmt.Errorf("%w: %s: status %d%s", ErrRefresh, endpoint, answer.Status, errorCodeOf(answer.Body.Error))
	}
	// The newest nonce is kept, and one the successful answer did not send is
	// the one that just worked.
	if answer.Nonce != "" {
		nonce = answer.Nonce
	}
	return c.refreshed(p, stored, answer.Body, stored.DPoP, nonce)
}

// refreshed builds the credential a traded answer grants.
//
// The grant is the same grant: the client, the endpoint and the proof key stay
// the credential's own, whatever the provider declares by default.
func (c Credentials) refreshed(p Provider, stored credential, answer tokenResponse, key *DPoPKey, nonce string) (credential, error) {
	if answer.AccessToken == "" || answer.ExpiresIn <= 0 {
		return credential{}, fmt.Errorf("%w: the answer carries no token or no lifetime", ErrRefresh)
	}
	refresh := answer.RefreshToken
	if refresh == "" {
		refresh = stored.Refresh
	}
	return credential{
		Type:      string(KindOAuth),
		Access:    answer.AccessToken,
		Refresh:   refresh,
		Expires:   c.now().Add(time.Duration(answer.ExpiresIn * float64(time.Second))).UnixMilli(),
		AccountID: p.account(answer.AccessToken, stored.AccountID),
		ClientID:  stored.ClientID,
		TokenURL:  stored.TokenURL,
		DPoP:      key,
		DPoPNonce: nonce,
	}, nil
}

// randomValueOrEmpty is randomValue for a path that has already failed once, so
// an identifier that cannot be made ends the proof instead of the process.
func randomValueOrEmpty(n int) string {
	value, err := randomValue(n)
	if err != nil {
		return ""
	}
	return value
}

// errorCodeOf renders a refused grant's code as the suffix an error carries.
//
// Only a code from the OAuth error set is carried: everything else an answer
// says is untrusted.
func errorCodeOf(code string) string {
	for _, known := range []string{
		"invalid_request", "invalid_client", "invalid_grant", "unauthorized_client",
		"unsupported_grant_type", "invalid_scope", "server_error", "temporarily_unavailable",
		"use_dpop_nonce", "invalid_dpop_proof",
	} {
		if code == known {
			return " (" + code + ")"
		}
	}
	return ""
}

// errorCode is the endpoint's own error, as a suffix an error carries.
//
// The answer's error field is a string in an OAuth error, and an object with
// a code beside it in some others. Only a code from the OAuth error set is
// carried. Everything else an answer says is untrusted, and the status alone
// reports the refusal.
func errorCode(body []byte) string {
	var answer struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || len(answer.Error) == 0 {
		return ""
	}
	var code string
	if err := json.Unmarshal(answer.Error, &code); err != nil {
		var nested struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(answer.Error, &nested); err != nil || nested.Code == "" {
			return ""
		}
		code = nested.Code
	}
	// The codes RFC 6749 gives a refused grant, and the two it gives a
	// server's own failure.
	switch code {
	case "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client",
		"unsupported_grant_type", "invalid_scope", "server_error", "temporarily_unavailable",
		// RFC 9449's two: the endpoint wants a nonce, or it does not accept
		// the proof's signature or its date.
		"use_dpop_nonce", "invalid_dpop_proof":
		return " (" + code + ")"
	}
	return ""
}

// tokenURL is the endpoint a credential's refresh token is traded at.
//
// The ladder, in order: the caller's own override, the credential's record of where it was
// granted, the provider's declaration, and this package's default. The record sits above the
// declaration because the grant is what decides: a credential pi was signed in for is refreshed
// where pi's client trades, whatever poo's provider declaration says. The caller's override sits
// on top because it knows something none of the rest does — a test's fake endpoint, or an
// endpoint moved for this run.
func (c Credentials) tokenURL(p Provider, stored credential) string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return endpointFor(stored.TokenURL, p, DefaultTokenURL)
}

// clientID is the OAuth client the credential belongs to, by the same ladder.
func (c Credentials) clientID(p Provider, stored credential) string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return clientFor(stored.ClientID, p, DefaultClientID)
}

// endpointFor answers the token endpoint by the ladder: the override, then
// the provider's declaration, then the fallback.
func endpointFor(override string, p Provider, fallback string) string {
	switch {
	case override != "":
		return override
	case p.TokenURL != "":
		return p.TokenURL
	}
	return fallback
}

// clientFor answers the OAuth client by the same ladder.
func clientFor(override string, p Provider, fallback string) string {
	switch {
	case override != "":
		return override
	case p.ClientID != "":
		return p.ClientID
	}
	return fallback
}

// postForm posts a form to a token endpoint and reads its answer, and turns a
// refusal into the error the caller reports.
func postForm(ctx context.Context, refused error, client *http.Client, endpoint string, form url.Values, headers map[string]string) (tokenResponse, error) {
	answer, err := postToken(ctx, refused, client, endpoint, form, headers)
	if err != nil {
		return tokenResponse{}, err
	}
	if answer.Status < 200 || answer.Status > 299 {
		return tokenResponse{}, fmt.Errorf("%w: %s: status %d%s", refused, endpoint, answer.Status, errorCodeOf(answer.Body.Error))
	}
	return answer.Body, nil
}

// postToken posts a form to a token endpoint and reads its answer as it came.
//
// A redirect is refused rather than followed: a token request's body carries a
// code or a refresh token, and Go would replay it wherever the redirect
// pointed. The answer is bounded, and the two headers a key-bound grant reads
// are kept: the nonce the endpoint wants next, and the endpoint's own clock,
// which a proof is dated by when this machine's is wrong.
func postToken(ctx context.Context, refused error, client *http.Client, endpoint string, form url.Values, headers map[string]string) (tokenAnswer, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("%w: %s: %w", refused, endpoint, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := refusingClient(client).Do(request)
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("%w: %s: %w", refused, endpoint, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenResponse))
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("%w: %s: %w", refused, endpoint, err)
	}
	answer := tokenAnswer{
		Status: response.StatusCode,
		Nonce:  response.Header.Get("DPoP-Nonce"),
	}
	if seconds := response.Header.Get("Date"); seconds != "" {
		if at, err := http.ParseTime(seconds); err == nil {
			answer.ServerTime = at.Unix()
		}
	}
	if len(body) > 0 {
		// A refusal and a grant are the same object, and a body that does not
		// parse is reported by the status alone.
		_ = json.Unmarshal(body, &answer.Body)
	}
	return answer, nil
}
