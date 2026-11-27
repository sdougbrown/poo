package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// The Google AI plan's sign-in.
//
// Google approves each app's OAuth client separately and does not allow one app
// to use another's, so poo ships no client: an operator registers a `Desktop
// app` client of their own in a Google Cloud project and hands poo its id and
// secret. The secret is public by design — an installed app cannot keep one
// (RFC 8252 8.5), and Google treats it as non-confidential — so it comes from
// the environment and is never written to the credential file.
//
// The grant is bound to a key pair created on this machine (RFC 9449), and the
// access token is a plain bearer token on model requests: only the token
// endpoint checks the proof.

const (
	// ProviderGoogleAIPlan is the provider id the plan's credential is stored
	// under. It is also the name of the endpoint the Interactions transport
	// reaches with it.
	ProviderGoogleAIPlan = "google-ai-plan"

	// GoogleAuthorizeURL is where the sign-in starts.
	GoogleAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"

	// GoogleTokenURL is where its code and its refresh token are traded.
	GoogleTokenURL = "https://oauth2.googleapis.com/token"

	// GoogleQuotaScope is the scope the plan answers on. A grant that does not
	// carry it is a sign-in that bought nothing.
	GoogleQuotaScope = "https://www.googleapis.com/auth/generative-language.peruserquota"

	// GoogleClientIDEnv and GoogleClientSecretEnv name the environment
	// variables an operator puts their app's Google client in.
	GoogleClientIDEnv     = "POO_GOOGLE_CLIENT_ID"
	GoogleClientSecretEnv = "POO_GOOGLE_CLIENT_SECRET"
)

// GoogleClient is one app's own Google OAuth client, registered as a `Desktop
// app` and approved by Google for the plan.
type GoogleClient struct {
	// ID is the client's id.
	ID string
	// Secret is the client's secret, which Google issues to an installed app
	// and treats as public.
	Secret string
}

// GoogleClientFromEnv reads the app's client from the environment.
//
// A missing half is an error rather than an empty client: a sign-in with no
// client is one Google refuses, and saying so here names the variable to set.
func GoogleClientFromEnv() (GoogleClient, error) {
	client := GoogleClient{
		ID:     strings.TrimSpace(os.Getenv(GoogleClientIDEnv)),
		Secret: strings.TrimSpace(os.Getenv(GoogleClientSecretEnv)),
	}
	var missing []string
	if client.ID == "" {
		missing = append(missing, GoogleClientIDEnv)
	}
	if client.Secret == "" {
		missing = append(missing, GoogleClientSecretEnv)
	}
	if len(missing) > 0 {
		return GoogleClient{}, fmt.Errorf("auth: the Google AI plan needs your app's own Google client: set %s",
			strings.Join(missing, " and "))
	}
	return client, nil
}

// GoogleAIPlan returns the plan's provider for one app's Google client.
//
// The client's secret is the provider's, because a refresh needs it: the
// credential records the client it was granted to, and the secret is the half
// that is never stored with it.
func GoogleAIPlan(client GoogleClient) Provider {
	return Provider{
		ID:            ProviderGoogleAIPlan,
		Kind:          KindOAuth,
		TokenURL:      GoogleTokenURL,
		ClientID:      client.ID,
		ClientSecret:  client.Secret,
		ProofKeys:     true,
		RequiredScope: GoogleQuotaScope,
	}
}

// GoogleAIPlanLogin is the plan's sign-in: a browser, a loopback callback on the
// Desktop client's path, PKCE, a nonce the ID token must carry, and a grant
// bound to a key pair this machine keeps.
var GoogleAIPlanLogin = Flow{
	Name:         "Google AI plan",
	Provider:     GoogleAIPlan(GoogleClient{}),
	Method:       MethodBrowser,
	AuthorizeURL: GoogleAuthorizeURL,
	TokenURL:     GoogleTokenURL,
	RedirectURI:  "http://127.0.0.1:1456/oauth2callback",
	Scopes:       []string{"openid", "email", "profile", GoogleQuotaScope},
	Nonce:        true,
}

// GoogleAIPlanFlow is the plan's sign-in for the app's client, with what is
// unavailable named when the operator has not registered one yet, so the flow
// is offered and says what to do rather than disappearing from the list.
func GoogleAIPlanFlow() Flow {
	flow := GoogleAIPlanLogin
	client, err := GoogleClientFromEnv()
	if err != nil {
		flow.Unavailable = err.Error()
		return flow
	}
	flow.Provider = GoogleAIPlan(client)
	flow.ClientID = client.ID
	return flow
}

// checkIDToken checks the claims Google's docs require of the ID token a
// sign-in returns: the issuer, the audience, the expiry and the nonce.
//
// The token came straight from Google's token endpoint over TLS, so its
// signature is not verified here: OpenID Connect Core 3.1.3.7 says what the
// transport already proved, and this is the half that checks the claims.
func checkIDToken(idToken, clientID, nonce string, now time.Time) error {
	parts := strings.Split(idToken, ".")
	if idToken == "" || len(parts) != 3 {
		return fmt.Errorf("%w: the sign-in returned no ID token", ErrSignIn)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("%w: the sign-in's ID token does not decode", ErrSignIn)
	}
	var claims struct {
		Issuer string `json:"iss"`
		Aud    string `json:"aud"`
		Exp    int64  `json:"exp"`
		Nonce  string `json:"nonce"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return fmt.Errorf("%w: the sign-in's ID token does not parse", ErrSignIn)
	}
	issuer := claims.Issuer == "https://accounts.google.com" || claims.Issuer == "accounts.google.com"
	notExpired := claims.Exp*1000 > now.Add(-time.Minute).UnixMilli()
	if !issuer || claims.Aud != clientID || claims.Nonce != nonce || !notExpired {
		return fmt.Errorf("%w: the sign-in's ID token failed its checks", ErrSignIn)
	}
	return nil
}

// proofID is the identifier one proof carries.
//
// The code exchange uses the code's own hash, which Google documents: a random
// identifier there fails, and the code is single-use, so the failure costs the
// sign-in.
func proofID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
