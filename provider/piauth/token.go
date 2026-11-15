package piauth

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
	// RefreshToken is the token that replaces the one traded. An endpoint
	// may leave it out, and the traded token stays valid then.
	RefreshToken string `json:"refresh_token"`
	// ExpiresIn is how many seconds the access token lives.
	ExpiresIn float64 `json:"expires_in"`
}

// refresh trades the credential's refresh token for a new access token.
//
// The request is the one pi sends: a form POST carrying the refresh
// grant, the refresh token and the client id. The answer carries the new
// pair and the access token's lifetime; the account is read from the new
// token, and a token that names no account keeps the one already stored.
//
// A failure reports the status and the endpoint's own error code, and
// nothing of the request: an error a caller logs must not carry a
// secret.
func (c Credentials) refresh(ctx context.Context, stored credential) (credential, error) {
	endpoint := c.tokenURL()
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {stored.Refresh},
		"client_id":     {c.clientID()},
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
		Type:      "oauth",
		Access:    answer.AccessToken,
		Refresh:   refresh,
		Expires:   c.now().Add(time.Duration(answer.ExpiresIn * float64(time.Second))).UnixMilli(),
		AccountID: accountID(answer.AccessToken, stored.AccountID),
	}
	return refreshed, nil
}

// errorCode is the endpoint's own error, as a suffix an error carries.
//
// The answer's error field is a string in an OAuth error and an object
// with a code beside it in some others; either names the refusal. An
// answer that is not that shape adds nothing.
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
	if code == "" {
		return ""
	}
	if len(code) > 64 {
		code = code[:64]
	}
	return " (" + strings.Map(withoutControl, code) + ")"
}

// withoutControl drops what an error code may not hold, so a code cannot
// carry a line break or a control character into a log.
func withoutControl(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}

// client is the client a refresh goes through.
//
// The redirect policy is this package's even when a caller supplies the
// client: Go replays a 307 or 308 request body to wherever the redirect
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

// tokenURL is the endpoint a refresh token is traded at.
func (c Credentials) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return DefaultTokenURL
}

// clientID is the OAuth client the credential belongs to.
func (c Credentials) clientID() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return DefaultClientID
}
