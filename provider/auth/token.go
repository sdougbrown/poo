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

// defaultHTTP is the client a refresh goes through when a caller names
// none.
var defaultHTTP = &http.Client{Timeout: DefaultTimeout}

// tokenResponse is the token endpoint's answer to a refresh.
type tokenResponse struct {
	// AccessToken is the new bearer token.
	AccessToken string `json:"access_token"`
	// RefreshToken is the token that replaces the one traded. An endpoint may
	// leave it out, and the traded token stays valid then.
	RefreshToken string `json:"refresh_token"`
	// ExpiresIn is how many seconds the access token lives.
	ExpiresIn float64 `json:"expires_in"`
}

// trade trades the credential's refresh token for a new access token.
//
// The request is the one pi sends: a form POST carrying the refresh grant,
// the refresh token and the client id. The answer carries the new pair and
// the access token's lifetime. The account is read from the new token, and a
// token that names no account keeps the one already stored.
//
// A failure reports the status and a standard OAuth error code, and nothing
// else from the answer. An answer is untrusted, and a caller's log is no
// place for its words.
func (c Credentials) trade(ctx context.Context, p Provider, stored credential) (credential, error) {
	endpoint := c.tokenURL(p, stored)
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {stored.Refresh},
		"client_id":     {c.clientID(p, stored)},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return credential{}, fmt.Errorf("%w: %s: %w", ErrRefresh, endpoint, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.client().Do(request)
	if err != nil {
		return credential{}, fmt.Errorf("%w: %s: %w", ErrRefresh, endpoint, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenResponse))
	if err != nil {
		return credential{}, fmt.Errorf("%w: %s: %w", ErrRefresh, endpoint, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return credential{}, fmt.Errorf("%w: %s: status %d%s", ErrRefresh, endpoint, response.StatusCode, errorCode(body))
	}
	var answer tokenResponse
	if err := json.Unmarshal(body, &answer); err != nil {
		return credential{}, fmt.Errorf("%w: %s: %w", ErrRefresh, endpoint, err)
	}
	if answer.AccessToken == "" || answer.ExpiresIn <= 0 {
		return credential{}, fmt.Errorf("%w: %s: the answer carries no token or no lifetime", ErrRefresh, endpoint)
	}
	refresh := answer.RefreshToken
	if refresh == "" {
		refresh = stored.Refresh
	}
	refreshed := credential{
		Type:      string(KindOAuth),
		Access:    answer.AccessToken,
		Refresh:   refresh,
		Expires:   c.now().Add(time.Duration(answer.ExpiresIn * float64(time.Second))).UnixMilli(),
		AccountID: p.account(answer.AccessToken, stored.AccountID),
		// The grant is the same grant: the client and the endpoint it was made with stay the
		// credential's own, whatever the provider declares by default.
		ClientID: stored.ClientID,
		TokenURL: stored.TokenURL,
	}
	return refreshed, nil
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
		"unsupported_grant_type", "invalid_scope", "server_error", "temporarily_unavailable":
		return " (" + code + ")"
	}
	return ""
}

// client is the client a refresh goes through.
//
// The redirect policy is this package's even when a caller supplies the
// client. Go replays a 307 or 308 request body to wherever the redirect
// points, and a refresh's body is the refresh token. A redirect is answered
// as the refusal it is, with the status the error names. The transport and
// the timeout are the caller's.
func (c Credentials) client() *http.Client {
	base := c.HTTP
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

// tokenURL is the endpoint a credential's refresh token is traded at.
//
// The ladder, in order: the caller's own override, the credential's record of where it was
// granted, the provider's declaration, and this package's default. The record sits above the
// declaration because the grant is what decides: a credential pi was signed in for is refreshed
// where pi's client trades, whatever poo's provider declaration says. The caller's override sits
// on top because it knows something none of the rest does — a test's fake endpoint, or an
// endpoint moved for this run.
func (c Credentials) tokenURL(p Provider, stored credential) string {
	switch {
	case c.TokenURL != "":
		return c.TokenURL
	case stored.TokenURL != "":
		return stored.TokenURL
	case p.TokenURL != "":
		return p.TokenURL
	}
	return DefaultTokenURL
}

// clientID is the OAuth client the credential belongs to, by the same ladder.
func (c Credentials) clientID(p Provider, stored credential) string {
	switch {
	case c.ClientID != "":
		return c.ClientID
	case stored.ClientID != "":
		return stored.ClientID
	case p.ClientID != "":
		return p.ClientID
	}
	return DefaultClientID
}
