package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
	"gopkg.in/yaml.v3"
)

// Google's client, as a test's operator registered theirs.
var testGoogleClient = auth.GoogleClient{ID: "app-client.apps.googleusercontent.com", Secret: "app-secret"}

// googleFlow points the plan's sign-in at a fake server.
func googleFlow(server *planServer) auth.Flow {
	flow := auth.GoogleAIPlanLogin
	flow.Provider = auth.GoogleAIPlan(testGoogleClient)
	flow.ClientID = testGoogleClient.ID
	flow.AuthorizeURL = server.URL + "/authorize"
	flow.TokenURL = server.URL + "/token"
	return flow
}

// testKey creates the key pair a grant is bound to, as the credential stores it.
func testKey(t *testing.T) auth.DPoPKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("creating the key: %v", err)
	}
	coordinate := func(value *big.Int) string {
		out := make([]byte, 32)
		value.FillBytes(out)
		return base64.RawURLEncoding.EncodeToString(out)
	}
	return auth.DPoPKey{
		Kty: "EC",
		Crv: "P-256",
		X:   coordinate(private.X),
		Y:   coordinate(private.Y),
		D:   coordinate(private.D),
	}
}

// testIDToken renders an ID token with the claims a sign-in is checked on.
func testIDToken(t *testing.T, clientID, nonce string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	claims, err := json.Marshal(map[string]any{
		"iss":   "https://accounts.google.com",
		"aud":   clientID,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonce,
	})
	if err != nil {
		t.Fatalf("encoding the claims: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
}

// proof is one DPoP proof as a test reads it.
type proof struct {
	// header is the proof's header, decoded.
	header map[string]any
	// claims is the proof's payload, decoded.
	claims map[string]any
	// key is the public half the header carries.
	key *ecdsa.PublicKey
	// valid reports that the signature checks against that public half.
	valid bool
}

// readProof decodes a proof and checks its signature, the way the endpoint
// would: the JOSE form, not DER.
func readProof(t *testing.T, value string) proof {
	t.Helper()
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		t.Fatalf("the proof is %q, want three parts", value)
	}
	decoded := make([][]byte, 2)
	for i, part := range parts[:2] {
		raw, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			t.Fatalf("decoding the proof's part %d: %v", i, err)
		}
		decoded[i] = raw
	}
	out := proof{}
	if err := json.Unmarshal(decoded[0], &out.header); err != nil {
		t.Fatalf("decoding the proof's header: %v", err)
	}
	if err := json.Unmarshal(decoded[1], &out.claims); err != nil {
		t.Fatalf("decoding the proof's claims: %v", err)
	}
	if _, ok := out.header["jwk"].(map[string]any)["d"]; ok {
		t.Fatal("the proof's header carries the private key")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("the proof's signature is %d bytes, want the JOSE form's 64", len(signature))
	}
	out.key = publicOf(t, out.header)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	out.valid = ecdsa.Verify(out.key, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]))
	return out
}

// publicOf rebuilds the public key a proof's header carries.
func publicOf(t *testing.T, header map[string]any) *ecdsa.PublicKey {
	t.Helper()
	jwk, ok := header["jwk"].(map[string]any)
	if !ok {
		t.Fatalf("the proof's header carries no key: %v", header)
	}
	coordinate := func(name string) *big.Int {
		raw, err := base64.RawURLEncoding.DecodeString(fmt.Sprint(jwk[name]))
		if err != nil {
			t.Fatalf("decoding the key's %s: %v", name, err)
		}
		return new(big.Int).SetBytes(raw)
	}
	key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: coordinate("x"), Y: coordinate("y")}
	if !key.Curve.IsOnCurve(key.X, key.Y) {
		t.Fatalf("the proof's key is not on the curve: %v", jwk)
	}
	return key
}

// planServer is a fake plan endpoint: it answers a scripted sequence of token
// answers, and records the forms and proofs it received.
type planServer struct {
	*httptest.Server
	mu sync.Mutex
	// answers are the responses, in order. The last one repeats.
	answers []planAnswer
	// forms are the forms the token endpoint was posted.
	forms []url.Values
	// proofs are the DPoP headers the requests carried.
	proofs []string
}

// planAnswer is one scripted answer.
type planAnswer struct {
	status int
	body   string
	nonce  string
	date   string
}

// newPlanServer starts a fake plan endpoint.
func newPlanServer(t *testing.T, answers ...planAnswer) *planServer {
	t.Helper()
	fake := &planServer{answers: answers}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		fake.mu.Lock()
		fake.forms = append(fake.forms, r.PostForm)
		fake.proofs = append(fake.proofs, r.Header.Get("DPoP"))
		index := len(fake.forms) - 1
		if index >= len(fake.answers) {
			index = len(fake.answers) - 1
		}
		answer := fake.answers[index]
		fake.mu.Unlock()
		if answer.nonce != "" {
			w.Header().Set("DPoP-Nonce", answer.nonce)
		}
		if answer.date != "" {
			w.Header().Set("Date", answer.date)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		_, _ = w.Write([]byte(answer.body))
	}))
	t.Cleanup(fake.Close)
	return fake
}

// planAuthFile writes poo's own auth file with a plan credential whose access
// token has expired, so the next use trades the refresh token.
func planAuthFile(t *testing.T, endpoint string) string {
	t.Helper()
	key := testKey(t)
	entry := map[string]any{
		"type":      "oauth",
		"access":    "stale",
		"refresh":   "the-refresh",
		"expires":   time.Now().Add(-time.Hour).UnixMilli(),
		"clientId":  testGoogleClient.ID,
		"tokenUrl":  endpoint + "/token",
		"dpopKey":   key,
		"dpopNonce": "n0",
	}
	credentials := map[string]any{"version": 1, "credentials": map[string]any{auth.ProviderGoogleAIPlan: entry}}
	raw, err := json.Marshal(credentials)
	if err != nil {
		t.Fatalf("encoding the file: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	return path
}

// TestThePlanSignInAsksForItsNonceAndScope covers the authorization request: the
// app's own client, the loopback callback, the plan's scope, and a nonce only
// this attempt knows.
func TestThePlanSignInAsksForItsNonceAndScope(t *testing.T) {
	server := newPlanServer(t)
	flow := googleFlow(server)
	attempt, err := flow.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	parsed, err := url.Parse(attempt.URL)
	if err != nil {
		t.Fatalf("the URL: %v", err)
	}
	query := parsed.Query()
	if got := query.Get("scope"); got != "openid email profile "+auth.GoogleQuotaScope {
		t.Errorf("the scope is %q, want the plan's own", got)
	}
	if got := query.Get("client_id"); got != testGoogleClient.ID {
		t.Errorf("the client is %q, want the app's own", got)
	}
	// The nonce is asked for, and it is the value the ID token will be checked
	// against.
	if attempt.Nonce == "" || query.Get("nonce") != attempt.Nonce {
		t.Errorf("the nonce is %q in the URL and %q in the attempt", query.Get("nonce"), attempt.Nonce)
	}
	if got := query.Get("redirect_uri"); got != auth.GoogleAIPlanLogin.RedirectURI {
		t.Errorf("the redirect is %q, want the desktop client's loopback", got)
	}
}

// TestThePlanSignInTradesItsCodeWithASignedProof covers the exchange: the app's
// secret, the verifier, and a proof dated for the token endpoint whose identifier
// is the code's own hash.
func TestThePlanSignInTradesItsCodeWithASignedProof(t *testing.T) {
	server := newPlanServer(t, planAnswer{
		status: http.StatusOK,
		body: `{"access_token":"the-access","refresh_token":"the-refresh","expires_in":3599,` +
			`"scope":"openid email profile ` + auth.GoogleQuotaScope + `","id_token":"__ID__"}`,
	})
	flow := googleFlow(server)
	attempt, err := flow.Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	server.mu.Lock()
	server.answers[0].body = strings.Replace(server.answers[0].body, "__ID__", testIDToken(t, testGoogleClient.ID, attempt.Nonce), 1)
	server.mu.Unlock()

	credential, err := attempt.Exchange(context.Background(), server.Client(), auth.Callback{Code: "4/0Aabc", State: attempt.State})
	if err != nil {
		t.Fatalf("the exchange: %v", err)
	}
	form := server.forms[0]
	if form.Get("client_secret") != testGoogleClient.Secret {
		t.Errorf("the trade sent no app secret: %v", form)
	}
	if form.Get("code") != "4/0Aabc" || form.Get("code_verifier") != attempt.Verifier {
		t.Errorf("the trade sent %v, want the code and its verifier", form)
	}
	if form.Get("client_id") != testGoogleClient.ID {
		t.Errorf("the trade named the client %q, want the app's own", form.Get("client_id"))
	}
	sent := readProof(t, server.proofs[0])
	if !sent.valid {
		t.Error("the proof's signature does not check against the key its header carries")
	}
	if sent.header["alg"] != "ES256" || sent.header["typ"] == nil {
		t.Errorf("the proof's header is %v, want ES256", sent.header)
	}
	if sent.claims["htm"] != "POST" || sent.claims["htu"] != server.URL+"/token" {
		t.Errorf("the proof is for %v %v, want the token endpoint", sent.claims["htm"], sent.claims["htu"])
	}
	// Google documents the code's hash as the exchange's identifier: a random
	// one fails, and the code is single-use.
	if want := proofOf("4/0Aabc"); sent.claims["jti"] != want {
		t.Errorf("the proof's identifier is %v, want the code's hash %q", sent.claims["jti"], want)
	}
	// The grant is bound to the key the proof was signed with, and the key is
	// what the credential keeps.
	if credential.DPoP == nil {
		t.Fatal("the credential keeps no proof key, want the one the grant is bound to")
	}
	if want := base64.RawURLEncoding.EncodeToString(sent.key.X.Bytes()); credential.DPoP.X != want {
		t.Errorf("the credential's key is %q, want the one the proof carried %q", credential.DPoP.X, want)
	}
}

// proofOf is the identifier a code exchange's proof carries.
func proofOf(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// TestThePlanSignInRefusesAGrantItCannotUse covers the two answers that look
// like success: a grant without the plan's scope, and an ID token that carries
// another attempt's nonce.
func TestThePlanSignInRefusesAGrantItCannotUse(t *testing.T) {
	cases := []struct {
		name  string
		body  func(t *testing.T, nonce string) string
		wants string
	}{
		{
			name: "scope",
			body: func(t *testing.T, nonce string) string {
				return `{"access_token":"a","refresh_token":"r","expires_in":3599,"scope":"openid email profile",` +
					`"id_token":"` + testIDToken(t, testGoogleClient.ID, nonce) + `"}`
			},
			wants: "did not grant",
		},
		{
			name: "nonce",
			body: func(t *testing.T, _ string) string {
				return `{"access_token":"a","refresh_token":"r","expires_in":3599,"scope":"openid email profile ` +
					auth.GoogleQuotaScope + `","id_token":"` + testIDToken(t, testGoogleClient.ID, "another-attempt") + `"}`
			},
			wants: "ID token failed its checks",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := newPlanServer(t, planAnswer{status: http.StatusOK})
			flow := googleFlow(server)
			attempt, err := flow.Authorize()
			if err != nil {
				t.Fatalf("the attempt: %v", err)
			}
			server.mu.Lock()
			server.answers[0].body = c.body(t, attempt.Nonce)
			server.mu.Unlock()

			_, err = attempt.Exchange(context.Background(), server.Client(), auth.Callback{Code: "the-code", State: attempt.State})
			if err == nil {
				t.Fatal("the sign-in was accepted, want it refused")
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Errorf("the refusal is %v, want it to say %q", err, c.wants)
			}
		})
	}
}

// TestAPlanRefreshAnswersTheNonceTheEndpointNames covers the nonce round trip:
// a refresh carrying a stale nonce is answered with the one to use, and the
// credential keeps it so the next refresh spends no round trip on it.
func TestAPlanRefreshAnswersTheNonceTheEndpointNames(t *testing.T) {
	server := newPlanServer(t,
		planAnswer{status: http.StatusBadRequest, body: `{"error":"use_dpop_nonce"}`, nonce: "n1"},
		planAnswer{status: http.StatusOK, body: `{"access_token":"fresh","expires_in":3599}`},
	)
	path := planAuthFile(t, server.URL)
	credentials := auth.Credentials{Path: path, HTTP: server.Client()}

	token, err := credentials.Access(context.Background(), auth.GoogleAIPlan(testGoogleClient))
	if err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if token.Access != "fresh" {
		t.Errorf("the token is %q, want the traded one", token.Access)
	}
	if len(server.proofs) != 2 {
		t.Fatalf("the endpoint was asked %d times, want the retry", len(server.proofs))
	}
	first := readProof(t, server.proofs[0])
	second := readProof(t, server.proofs[1])
	if first.claims["nonce"] != "n0" || second.claims["nonce"] != "n1" {
		t.Errorf("the nonces are %v then %v, want the stored one then the endpoint's", first.claims["nonce"], second.claims["nonce"])
	}
	// A stored credential is what the next refresh reads, so the nonce has to
	// be in the file.
	held := planEntry(t, path)
	if held["dpopNonce"] != "n1" {
		t.Errorf("the file holds the nonce %v, want the endpoint's", held["dpopNonce"])
	}
	if held["dpopKey"] == nil {
		t.Error("the file lost the proof key, want the grant's own")
	}
}

// planEntry reads the plan's entry from the file.
//
// The file a store writes back is poo's own YAML, so the entry is read the way
// an operator reads it.
func planEntry(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	var document struct {
		Credentials map[string]map[string]any `yaml:"credentials"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("decoding the file: %v", err)
	}
	held, ok := document.Credentials[auth.ProviderGoogleAIPlan]
	if !ok {
		t.Fatalf("the file holds no %s entry", auth.ProviderGoogleAIPlan)
	}
	return held
}

// TestAPlanRefreshDatesItsProofByTheEndpointsClock covers the other retry: a
// clock ahead of the endpoint's is a proof the endpoint refuses, and the retry
// is dated by the answer's own Date header.
func TestAPlanRefreshDatesItsProofByTheEndpointsClock(t *testing.T) {
	serverDate := "Wed, 25 Nov 2026 12:00:00 GMT"
	server := newPlanServer(t,
		planAnswer{status: http.StatusBadRequest, body: `{"error":"invalid_dpop_proof"}`, date: serverDate},
		planAnswer{status: http.StatusOK, body: `{"access_token":"fresh","expires_in":3599}`},
	)
	path := planAuthFile(t, server.URL)
	credentials := auth.Credentials{Path: path, HTTP: server.Client()}

	if _, err := credentials.Access(context.Background(), auth.GoogleAIPlan(testGoogleClient)); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if len(server.proofs) != 2 {
		t.Fatalf("the endpoint was asked %d times, want the one retry", len(server.proofs))
	}
	sent := readProof(t, server.proofs[1])
	stamp, err := http.ParseTime(serverDate)
	if err != nil {
		t.Fatalf("parsing the date: %v", err)
	}
	if sent.claims["iat"] != float64(stamp.Unix()) {
		t.Errorf("the retry is dated %v, want the endpoint's clock %d", sent.claims["iat"], stamp.Unix())
	}
	if sent.claims["jti"] == readProof(t, server.proofs[0]).claims["jti"] {
		t.Error("the retry reuses the first proof's identifier, want a fresh one")
	}
}

// TestAPlanRefreshThatCannotBeTradedAsksForASignIn covers the answers that end
// the credential: a grant that is expired or revoked, and a credential whose
// key is gone.
func TestAPlanRefreshThatCannotBeTradedAsksForASignIn(t *testing.T) {
	for _, answer := range []planAnswer{
		{status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`},
		{status: http.StatusBadRequest, body: `{"error":"unauthorized_client"}`},
	} {
		server := newPlanServer(t, answer)
		path := planAuthFile(t, server.URL)
		credentials := auth.Credentials{Path: path, HTTP: server.Client()}
		_, err := credentials.Access(context.Background(), auth.GoogleAIPlan(testGoogleClient))
		if err == nil {
			t.Fatal("the refresh was accepted, want it refused")
		}
		if !strings.Contains(err.Error(), "sign in again") {
			t.Errorf("the refusal is %v, want it to ask for a sign-in", err)
		}
	}
}

// TestAPlanCredentialWithoutItsKeyIsSignedInAgain covers the credential a file
// was edited out of: the key is what makes the grant usable, and a credential
// without one is refused before anything is traded.
func TestAPlanCredentialWithoutItsKeyIsSignedInAgain(t *testing.T) {
	server := newPlanServer(t, planAnswer{status: http.StatusOK, body: `{"access_token":"fresh","expires_in":3599}`})
	path := planAuthFile(t, server.URL)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	var document struct {
		Version     int                       `json:"version"`
		Credentials map[string]map[string]any `json:"credentials"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decoding the file: %v", err)
	}
	delete(document.Credentials[auth.ProviderGoogleAIPlan], "dpopKey")
	edited, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encoding the file: %v", err)
	}
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	credentials := auth.Credentials{Path: path, HTTP: server.Client()}
	if _, err := credentials.Access(context.Background(), auth.GoogleAIPlan(testGoogleClient)); err == nil {
		t.Fatal("the credential was used without its key")
	}
	if len(server.forms) != 0 {
		t.Errorf("the endpoint was asked %d times, want none: the grant cannot be signed", len(server.forms))
	}
}
