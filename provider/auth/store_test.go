package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
)

// The stored credential, and the token the fake endpoint trades it for.
const (
	oldAccess  = "old-access-token"
	oldRefresh = "old-refresh-token"
	oldAccount = "acct-old"
	newAccess  = "new-access-token"
	newRefresh = "new-refresh-token"
	newAccount = "acct-new"
)

// stored is an auth.json holding one Codex credential that expires at the
// given millisecond. It holds the neighbours a real file holds too: an api key
// whose value names an environment variable, and another provider's oauth
// credential.
const stored = `{
  "anthropic": {
    "type": "api_key",
    "key": "$ANTHROPIC_API_KEY"
  },
  "openai-codex": {
    "type": "oauth",
    "access": "` + oldAccess + `",
    "refresh": "` + oldRefresh + `",
    "expires": %d,
    "accountId": "` + oldAccount + `"
  },
  "zai": {
    "type": "oauth",
    "access": "zai-access",
    "refresh": "zai-refresh",
    "expires": 4102444800000,
    "accountId": "zai-account"
  }
}`

// The clock the tests age a credential by, and the expiries they give it
// relative to that clock.
const nowMillis = 1_700_000_000_000

// clock is the test's fixed clock.
func clock() time.Time { return time.UnixMilli(nowMillis) }

// expires is when a credential's access token stops working, when the
// test gives it that much life.
func expires(life time.Duration) int64 { return nowMillis + life.Milliseconds() }

// authFile writes an auth.json into a directory of the test's own and
// returns its path.
func authFile(t *testing.T, body string) string {
	t.Helper()
	return authFileWithMode(t, body, 0o644)
}

// authFileWithMode writes an auth.json with the given mode.
func authFileWithMode(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("writing the auth file: %v", err)
	}
	return path
}

// codexToken builds an access token whose claim names an account, in the
// shape of the JWTs the token endpoint answers with.
func codexToken(t *testing.T, account string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account},
	})
	if err != nil {
		t.Fatalf("encoding the token's claims: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
}

// tokenEndpoint is a token endpoint that answers a refresh with the given
// answer, and that a test can ask how it was called.
type tokenEndpoint struct {
	// Server is the endpoint.
	*httptest.Server
	// mu guards calls and forms.
	mu sync.Mutex
	// calls counts the refreshes the endpoint answered.
	calls int
	// forms holds the form of each refresh, by call.
	forms []map[string]string
}

// newTokenEndpoint serves refreshes. answer says what each one carries. The
// during hook runs while the refresh is being answered, so a test can watch the
// lock a refresh holds.
func newTokenEndpoint(t *testing.T, answer func() (int, string), during func()) *tokenEndpoint {
	t.Helper()
	endpoint := &tokenEndpoint{}
	endpoint.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if during != nil {
			during()
		}
		if err := r.ParseForm(); err == nil {
			form := map[string]string{}
			for name := range r.PostForm {
				form[name] = r.PostForm.Get(name)
			}
			endpoint.mu.Lock()
			endpoint.forms = append(endpoint.forms, form)
			endpoint.mu.Unlock()
		}
		endpoint.mu.Lock()
		endpoint.calls++
		endpoint.mu.Unlock()
		status, body := answer()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(endpoint.Close)
	return endpoint
}

// traded is the answer a token endpoint gives for a fresh token.
func traded(t *testing.T, access string, life time.Duration) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"access_token":  access,
		"refresh_token": newRefresh,
		"expires_in":    int(life.Seconds()),
		"token_type":    "Bearer",
	})
	if err != nil {
		t.Fatalf("encoding the endpoint's answer: %v", err)
	}
	return string(body)
}

// count is the number of refreshes the endpoint answered.
func (e *tokenEndpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// form is the form of one refresh.
func (e *tokenEndpoint) form(t *testing.T, call int) map[string]string {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if call >= len(e.forms) {
		t.Fatalf("the endpoint answered %d refreshes, want more than %d", len(e.forms), call)
	}
	return e.forms[call]
}

// entries reads an auth.json as its providers' entries.
func entries(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the auth file: %v", err)
	}
	var read map[string]json.RawMessage
	if err := json.Unmarshal(data, &read); err != nil {
		t.Fatalf("the auth file is not JSON: %v", err)
	}
	return read
}

// storedCredential reads one provider's entry as pi would read it.
func storedCredential(t *testing.T, path string) auth.Token {
	t.Helper()
	var entry struct {
		Type      string `json:"type"`
		Access    string `json:"access"`
		Refresh   string `json:"refresh"`
		Expires   int64  `json:"expires"`
		AccountID string `json:"accountId"`
	}
	if err := json.Unmarshal(entries(t, path)[auth.ProviderCodex], &entry); err != nil {
		t.Fatalf("the %s entry is not an oauth credential: %v", auth.ProviderCodex, err)
	}
	if entry.Type != "oauth" {
		t.Fatalf("the %s entry is a %s credential", auth.ProviderCodex, entry.Type)
	}
	return auth.Token{
		Access:    entry.Access,
		Expires:   time.UnixMilli(entry.Expires),
		AccountID: entry.AccountID,
	}
}

// readFile is a file's bytes, for the tests that compare a file before
// and after.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// TestAnExpiredTokenIsRefreshedAndWrittenBack covers the whole job. An expired
// credential is traded for a new one, the new one is what the caller gets, and
// the file holds it.
func TestAnExpiredTokenIsRefreshedAndWrittenBack(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}
	if token.Access != access {
		t.Errorf("the caller got the token %q, want the one the endpoint traded", token.Access)
	}
	if token.AccountID != newAccount {
		t.Errorf("the token names the account %q, want %q", token.AccountID, newAccount)
	}
	if want := clock().Add(time.Hour); !token.Expires.Equal(want) {
		t.Errorf("the token expires at %v, want %v", token.Expires, want)
	}
	stored := storedCredential(t, path)
	if stored.Access != access || stored.AccountID != newAccount {
		t.Errorf("the file holds %q for %q, want the traded token", stored.Access, stored.AccountID)
	}
	if form := endpoint.form(t, 0); form["grant_type"] != "refresh_token" || form["refresh_token"] != oldRefresh {
		t.Errorf("the refresh carried the form %v, want the stored refresh token", form)
	}
	if form := endpoint.form(t, 0); form["client_id"] != auth.DefaultClientID {
		t.Errorf("the refresh carried the client %q, want pi's", form["client_id"])
	}
}

// TestALiveTokenIsUsedWithoutAskingTheEndpoint covers the credential that
// needs nothing: pi refreshed it already, so there is nothing to trade and
// nothing to write.
func TestALiveTokenIsUsedWithoutAskingTheEndpoint(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(10*time.Hour)))
	before := readFile(t, path)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}
	if token.Access != oldAccess {
		t.Errorf("the caller got the token %q, want the stored one", token.Access)
	}
	if token.AccountID != oldAccount {
		t.Errorf("the token names the account %q, want the stored %q", token.AccountID, oldAccount)
	}
	if endpoint.count() != 0 {
		t.Errorf("the endpoint was asked %d times, want none", endpoint.count())
	}
	if after := readFile(t, path); before != after {
		t.Errorf("the file was written for a token that needed nothing:\n%s", after)
	}
}

// TestATokenInsidePisLeewayIsRefreshed covers the token pi would refresh too.
// One with another two minutes to live is traded. pi refreshes five minutes
// before expiry, and two writers of one credential must not disagree about
// what expiry means.
func TestATokenInsidePisLeewayIsRefreshed(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(2*time.Minute)))
	access := codexToken(t, oldAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}
	if token.Access != access {
		t.Errorf("the caller got the token %q, want a traded one", token.Access)
	}
	if endpoint.count() != 1 {
		t.Errorf("the endpoint was asked %d times, want one", endpoint.count())
	}
}

// TestATokenAtTheLeewayBoundaryIsRefreshed covers the millisecond the leeway
// is decided on. A token with exactly pi's five minutes left is traded,
// because a token pi would refresh is not one to use. One millisecond more
// than that is used as it is.
func TestATokenAtTheLeewayBoundaryIsRefreshed(t *testing.T) {
	for _, c := range []struct {
		name   string
		life   time.Duration
		traded bool
	}{
		{name: "exactly the leeway", life: auth.DefaultLeeway, traded: true},
		{name: "a millisecond past it", life: auth.DefaultLeeway + time.Millisecond, traded: false},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := authFile(t, fmt.Sprintf(stored, expires(c.life)))
			access := codexToken(t, oldAccount)
			endpoint := newTokenEndpoint(t, func() (int, string) {
				return http.StatusOK, traded(t, access, time.Hour)
			}, nil)
			credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

			token, err := credentials.Access(context.Background(), auth.Codex)
			if err != nil {
				t.Fatalf("the credential did not reach a token: %v", err)
			}
			if traded := endpoint.count() == 1; traded != c.traded {
				t.Errorf("the token with %v to live was traded %v, want %v", c.life, traded, c.traded)
			}
			if !c.traded && token.Access != oldAccess {
				t.Errorf("the caller got the token %q, want the stored one", token.Access)
			}
		})
	}
}

// TestARefreshKeepsEveryOtherCredential covers the file around the credential.
// pi's neighbours keep their values, the file keeps the mode its owner gave it,
// and it is laid out the way pi writes one.
func TestARefreshKeepsEveryOtherCredential(t *testing.T) {
	path := authFileWithMode(t, fmt.Sprintf(stored, expires(-time.Hour)), 0o640)
	before := entries(t, path)
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background(), auth.Codex); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}

	after := entries(t, path)
	for _, provider := range []string{"anthropic", "zai"} {
		if string(before[provider]) != string(after[provider]) {
			t.Errorf("the %s credential was rewritten:\n before %s\n after  %s", provider, before[provider], after[provider])
		}
	}
	data := readFile(t, path)
	if strings.HasSuffix(data, "\n") {
		t.Error("the file ends with a newline, which pi's own write does not")
	}
	for _, want := range []string{"\n  \"openai-codex\": {\n", "\n    \"access\": ", "\n    \"type\": \"oauth\""} {
		if !strings.Contains(data, want) {
			t.Errorf("the file is not laid out the way pi writes one, with %q:\n%s", want, data)
		}
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stating the auth file: %v", err)
	} else if info.Mode().Perm() != 0o640 {
		t.Errorf("the file's mode is %v, want the 0640 its owner gave it", info.Mode().Perm())
	}
}

// TestARefreshHoldsPisLock covers the coordination: while the token is
// being traded the lock pi takes is held, and it is given up after.
func TestARefreshHoldsPisLock(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	lockPath := path + ".lock"
	var held atomic.Bool
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, func() {
		_, err := os.Stat(lockPath)
		held.Store(err == nil)
	})
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background(), auth.Codex); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}

	if !held.Load() {
		t.Error("the refresh traded the token without holding pi's lock")
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the lock is still there after the refresh: %v", err)
	}
}

// TestASecondCallerWaitsForTheFirstRefresh covers two callers of one
// credential. One trades the refresh token, and the other finds the token the
// first stored instead of trading it again.
func TestASecondCallerWaitsForTheFirstRefresh(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	tokens := make([]auth.Token, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens[i], errs[i] = credentials.Access(context.Background(), auth.Codex)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d did not reach a token: %v", i, err)
		}
		if tokens[i].Access != access {
			t.Errorf("caller %d got the token %q, want the traded one", i, tokens[i].Access)
		}
	}
	if endpoint.count() != 1 {
		t.Errorf("the endpoint was asked %d times, want one refresh for both callers", endpoint.count())
	}
}

// TestARefreshThatFailsLeavesTheFileAlone covers the endpoint's refusal:
// nothing was traded or written, and the error names the refusal without
// naming a secret.
func TestARefreshThatFailsLeavesTheFileAlone(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := readFile(t, path)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"the refresh token is not valid"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err := credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrRefresh) {
		t.Fatalf("the refusal is %v, want ErrRefresh", err)
	}
	for _, secret := range []string{oldAccess, oldRefresh} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error carries the credential: %v", err)
		}
	}
	for _, want := range []string{"400", "invalid_grant"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error is %q, want it to name %q", err, want)
		}
	}
	if after := readFile(t, path); before != after {
		t.Errorf("the file was written after a failed refresh:\n%s", after)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the lock is still there after a failed refresh: %v", err)
	}
}

// TestARefreshThatOmitsTheRefreshTokenKeepsTheStoredOne covers an endpoint
// that rotates nothing: the traded token stays the credential's refresh
// token.
func TestARefreshThatOmitsTheRefreshTokenKeepsTheStoredOne(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	access := codexToken(t, oldAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, fmt.Sprintf(`{"access_token":%q,"expires_in":3600}`, access)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background(), auth.Codex); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}

	var entry struct {
		Refresh string `json:"refresh"`
		Access  string `json:"access"`
	}
	if err := json.Unmarshal(entries(t, path)[auth.ProviderCodex], &entry); err != nil {
		t.Fatalf("the entry is not an oauth credential: %v", err)
	}
	if entry.Refresh != oldRefresh {
		t.Errorf("the file holds the refresh token %q, want the stored %q", entry.Refresh, oldRefresh)
	}
	if entry.Access != access {
		t.Errorf("the file holds the access token %q, want the traded one", entry.Access)
	}
}

// TestTheAccountComesFromTheNewAccessToken covers the account a traded token
// names. A token claims its own account, and a token that claims none keeps
// the one the file already had.
func TestTheAccountComesFromTheNewAccessToken(t *testing.T) {
	for _, c := range []struct {
		name  string
		token func(t *testing.T) string
		want  string
	}{
		{
			name:  "the traded token's claim",
			token: func(t *testing.T) string { return codexToken(t, newAccount) },
			want:  newAccount,
		},
		{
			name:  "the stored account, for a token that names none",
			token: func(t *testing.T) string { return "an-opaque-token" },
			want:  oldAccount,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
			access := c.token(t)
			endpoint := newTokenEndpoint(t, func() (int, string) {
				return http.StatusOK, traded(t, access, time.Hour)
			}, nil)
			credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
			token, err := credentials.Access(context.Background(), auth.Codex)
			if err != nil {
				t.Fatalf("the credential did not reach a token: %v", err)
			}
			if token.AccountID != c.want {
				t.Errorf("the token names the account %q, want %q", token.AccountID, c.want)
			}
			if stored := storedCredential(t, path); stored.AccountID != c.want {
				t.Errorf("the file holds the account %q, want %q", stored.AccountID, c.want)
			}
		})
	}
}

// TestAForeignLockIsWaitedOut covers pi refreshing at the same moment. The lock
// is held when this package arrives, and is given up while this package is
// waiting. The wait then ends in a token.
func TestAForeignLockIsWaitedOut(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	lockPath := path + ".lock"
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("taking the lock the way pi takes it: %v", err)
	}
	access := codexToken(t, oldAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	waiting := make(chan auth.Token, 1)
	failed := make(chan error, 1)
	go func() {
		token, err := credentials.Access(context.Background(), auth.Codex)
		if err != nil {
			failed <- err
			return
		}
		waiting <- token
	}()
	select {
	case token := <-waiting:
		t.Fatalf("the credential returned the token %q while the lock was held", token.Access)
	case err := <-failed:
		t.Fatalf("the credential failed while the lock was held, and should have waited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("giving the lock up: %v", err)
	}

	select {
	case token := <-waiting:
		if token.Access != access {
			t.Errorf("the caller got the token %q, want the traded one", token.Access)
		}
	case err := <-failed:
		t.Fatalf("the credential did not take the lock once it was free: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the credential never took the lock that was given up")
	}
}

// TestAForeignLockTheWaitCannotOutlastIsReported covers a holder that never
// gives the lock up: the wait ends in a report, and the file is not
// written.
func TestAForeignLockTheWaitCannotOutlastIsReported(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	if err := os.Mkdir(path+".lock", 0o700); err != nil {
		t.Fatalf("taking the lock the way pi takes it: %v", err)
	}
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock, Wait: 100 * time.Millisecond}

	_, err := credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrLocked) {
		t.Fatalf("the held lock is %v, want ErrLocked", err)
	}
	if endpoint.count() != 0 {
		t.Errorf("the endpoint was asked %d times while the lock was held, want none", endpoint.count())
	}
	if stored := storedCredential(t, path); stored.Access != oldAccess {
		t.Errorf("the file holds %q, want the credential it held", stored.Access)
	}
}

// TestAStaleLockIsBroken covers the holder that died: a lock pi would break
// is broken, and the refresh goes ahead.
func TestAStaleLockIsBroken(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	lockPath := path + ".lock"
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("leaving a dead holder's lock: %v", err)
	}
	stale := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatalf("aging the lock: %v", err)
	}
	access := codexToken(t, oldAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	started := time.Now()
	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the stale lock was not broken: %v", err)
	}
	if token.Access != access {
		t.Errorf("the caller got the token %q, want the traded one", token.Access)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the refresh waited %v for a lock its holder had left", elapsed)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the broken lock is still there: %v", err)
	}
}

// TestAnAuthFileWithoutACodexCredentialIsReported covers the files that
// name no credential: none, an api key where the credential goes, and
// nothing under pi's provider id. None of them is a file to create or
// rewrite.
func TestAnAuthFileWithoutACodexCredentialIsReported(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{name: "no file"},
		{name: "an api key", body: `{"openai-codex": {"type": "api_key", "key": "sk-test"}}`},
		{
			name: "an oauth entry that names no account",
			body: `{"openai-codex": {"type": "oauth", "access": "an-opaque-token", "refresh": "r", "expires": 1}}`,
		},
		{name: "another provider", body: `{"anthropic": {"type": "api_key", "key": "$ANTHROPIC_API_KEY"}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "auth.json")
			if c.body != "" {
				if err := os.WriteFile(path, []byte(c.body), 0o644); err != nil {
					t.Fatalf("writing the auth file: %v", err)
				}
			}
			credentials := auth.Credentials{Path: path}
			if _, err := credentials.Access(context.Background(), auth.Codex); !errors.Is(err, auth.ErrNoCredential) {
				t.Fatalf("the file is %v, want ErrNoCredential", err)
			}
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				if c.body != "" {
					t.Error("the auth file was removed for a credential that is not there")
				}
				return
			}
			if got := readFile(t, path); got != c.body {
				t.Errorf("the file was written for a credential that is not there:\n%s", got)
			}
		})
	}
}

// TestAMalformedAuthFileIsNotWrittenOver covers the file this package cannot
// read. It is reported, and it is left as it was, because a write over it
// would drop whatever it holds.
func TestAMalformedAuthFileIsNotWrittenOver(t *testing.T) {
	body := `{"openai-codex": {"type": "oauth", "access": `
	path := authFile(t, body)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err := credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrStorage) {
		t.Fatalf("the malformed file is %v, want ErrStorage", err)
	}
	if got := readFile(t, path); got != body {
		t.Errorf("the malformed file was written over: %s", got)
	}
	if endpoint.count() != 0 {
		t.Errorf("the endpoint was asked %d times for a file that does not parse, want none", endpoint.count())
	}
}

// TestARefreshAnswerWithoutATokenIsReported covers the endpoint that answers
// with something that is not a refresh. The credential is not written from it,
// because an entry holding no token is a credential pi cannot use either.
func TestARefreshAnswerWithoutATokenIsReported(t *testing.T) {
	for _, answer := range []string{
		`{"expires_in":3600}`,
		`{"access_token":"a-token"}`,
		`{"access_token":"a-token","expires_in":0}`,
	} {
		path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
		before := readFile(t, path)
		endpoint := newTokenEndpoint(t, func() (int, string) {
			return http.StatusOK, answer
		}, nil)
		credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
		if _, err := credentials.Access(context.Background(), auth.Codex); !errors.Is(err, auth.ErrRefresh) {
			t.Errorf("the answer %s is %v, want ErrRefresh", answer, err)
		}
		if after := readFile(t, path); before != after {
			t.Errorf("the file was written from the answer %s:\n%s", answer, after)
		}
	}
}

// TestACancelledWaitLeavesTheLockAlone covers a caller that gives up while
// another writer holds the lock. The wait ends with the context, and the
// holder's lock is left for it to give up.
func TestACancelledWaitLeavesTheLockAlone(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	lockPath := path + ".lock"
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("taking the lock the way pi takes it: %v", err)
	}
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock, Wait: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	started := time.Now()
	_, err := credentials.Access(ctx, auth.Codex)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled wait is %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("the wait took %v to notice the context was done", elapsed)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("the holder's lock was removed by a wait that gave up: %v", err)
	}
	if endpoint.count() != 0 {
		t.Errorf("the endpoint was asked %d times for a wait that was cancelled, want none", endpoint.count())
	}
}

// TestAnAnswerLargerThanTheBoundIsRefused covers the bound on an untrusted
// answer. An endpoint that answers more than the bound is not read to the end:
// the answer cannot be one this package parses, and the file is left alone.
func TestAnAnswerLargerThanTheBoundIsRefused(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := entries(t, path)
	// The answer parses only when it is read whole: the access token is the
	// padding itself, so a truncated read is not a token this package takes.
	padding := strings.Repeat("a", (1<<20)+1024)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, `{"access_token":"` + padding + `","expires_in":3600}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err := credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrRefresh) {
		t.Fatalf("an answer over the bound is %v, want ErrRefresh", err)
	}
	after := entries(t, path)
	if string(after[auth.ProviderCodex]) != string(before[auth.ProviderCodex]) {
		t.Errorf("the file changed under a refused answer: %s", after[auth.ProviderCodex])
	}
}

// TestAnAnswerThatIsNotJSONIsRefused covers an endpoint that answers 200 with
// something that is not a token answer at all.
func TestAnAnswerThatIsNotJSONIsRefused(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := entries(t, path)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, "the token endpoint is a captive portal today"
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err := credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrRefresh) {
		t.Fatalf("an answer that is not JSON is %v, want ErrRefresh", err)
	}
	after := entries(t, path)
	if string(after[auth.ProviderCodex]) != string(before[auth.ProviderCodex]) {
		t.Errorf("the file changed under an unreadable answer: %s", after[auth.ProviderCodex])
	}
}

// TestACancelledRefreshWritesNothing covers the caller's cancellation while the
// endpoint is asked. The trade is the caller's, so the failure keeps both
// identities, and nothing is written under a refresh that was abandoned.
func TestACancelledRefreshWritesNothing(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := entries(t, path)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	endpoint := newTokenEndpoint(t, func() (int, string) {
		once.Do(func() { close(started) })
		<-release
		return http.StatusOK, traded(t, codexToken(t, newAccount), time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()
	defer close(release)

	_, err := credentials.Access(ctx, auth.Codex)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled refresh is %v, want context.Canceled", err)
	}
	if !errors.Is(err, auth.ErrRefresh) {
		t.Errorf("the cancelled refresh is %v, want the refresh's own failure too", err)
	}
	after := entries(t, path)
	if string(after[auth.ProviderCodex]) != string(before[auth.ProviderCodex]) {
		t.Errorf("the file changed under a cancelled refresh: %s", after[auth.ProviderCodex])
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the lock was left behind: %v", err)
	}
}

// TestAnEmptyPathIsRefused covers the guard a caller with no path reaches. It
// must not take a lock in the working directory, and it must not report a
// storage failure that a retry would.
func TestAnEmptyPathIsRefused(t *testing.T) {
	_, err := auth.Credentials{}.Access(context.Background(), auth.Codex)
	if err == nil {
		t.Fatal("a credential with no path reached a token")
	}
	if errors.Is(err, auth.ErrStorage) || errors.Is(err, auth.ErrLocked) {
		t.Errorf("the failure is %v, want neither a storage nor a lock failure", err)
	}
	if _, err := os.Stat(".lock"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a lock was taken in the working directory: %v", err)
	}
}

// TestTheClientIDOverrideIsSent covers the documented override. The endpoint
// matches a refresh token to the client it was granted to, so a caller that
// names one must reach the endpoint's form.
func TestTheClientIDOverrideIsSent(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, codexToken(t, newAccount), time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock, ClientID: "client-under-test"}

	if _, err := credentials.Access(context.Background(), auth.Codex); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if got := endpoint.form(t, 0)["client_id"]; got != "client-under-test" {
		t.Errorf("the form names the client %q, want the one the caller named", got)
	}
}

// TestTheLeewayOverrideTakesALiveToken covers the leeway override. A token
// with three minutes left is used as it is under a two-minute leeway, and the
// endpoint is not asked.
func TestTheLeewayOverrideTakesALiveToken(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(3*time.Minute)))
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusInternalServerError, `{"error":"the endpoint was asked for a live token"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock, Leeway: 2 * time.Minute}

	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the live token was not used: %v", err)
	}
	if token.Access != oldAccess {
		t.Errorf("the caller got %q, want the stored token", token.Access)
	}
	if endpoint.count() != 0 {
		t.Errorf("the endpoint was asked %d times for a token with three minutes left", endpoint.count())
	}
}
