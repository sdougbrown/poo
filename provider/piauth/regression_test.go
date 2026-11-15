package piauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/piauth"
)

// TestALockTakenDuringARefreshIsNotWrittenUnder covers the write a taken lock
// forbids. The writer that broke this holder's lock expects to own the file, so
// the caller is told the traded token was not stored. The file is left to that
// writer, and its lock is left alone too.
func TestALockTakenDuringARefreshIsNotWrittenUnder(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := readFile(t, path)
	lockPath := path + ".lock"
	entered, answer, endpoint := heldEndpoint(t)
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint, Now: clock}

	failed := make(chan error, 1)
	go func() {
		_, err := credentials.Access(context.Background())
		failed <- err
	}()
	<-entered
	takeOver(t, lockPath, time.Now().Add(-time.Second))
	close(answer)

	if err := <-failed; !errors.Is(err, piauth.ErrPersist) {
		t.Fatalf("the taken lock is %v, want ErrPersist: the token was traded and not stored", err)
	}
	if after := readFile(t, path); before != after {
		t.Errorf("the file was written under a lock another writer took:\n%s", after)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("the taker's lock was removed by the writer it was taken from: %v", err)
	}
}

// TestALockTakenWithTheHeldMtimeIsDetected covers the takeover a bare mtime
// comparison cannot see. The taker's directory carries the same mtime the
// holder recorded, which a coarse-grained filesystem gives for a takeover
// inside one tick.
func TestALockTakenWithTheHeldMtimeIsDetected(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := readFile(t, path)
	lockPath := path + ".lock"
	entered, answer, endpoint := heldEndpoint(t)
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint, Now: clock}

	failed := make(chan error, 1)
	go func() {
		_, err := credentials.Access(context.Background())
		failed <- err
	}()
	<-entered
	held, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stating the held lock: %v", err)
	}
	takeOver(t, lockPath, held.ModTime())
	close(answer)

	if err := <-failed; !errors.Is(err, piauth.ErrPersist) {
		t.Fatalf("the takeover with the held mtime is %v, want ErrPersist", err)
	}
	if after := readFile(t, path); before != after {
		t.Errorf("the file was written under a lock whose directory was replaced:\n%s", after)
	}
}

// TestABomFileRefreshesLikeAPiFile covers the file pi reads. pi strips the byte
// order mark before parsing, so a BOM auth.json is pi's. The write that follows
// is without one, the way pi writes it.
func TestABomFileRefreshesLikeAPiFile(t *testing.T) {
	path := authFile(t, "\ufeff"+fmt.Sprintf(stored, expires(-time.Hour)))
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	if _, err := credentials.Access(context.Background()); err != nil {
		t.Fatalf("the BOM file did not reach a token: %v", err)
	}
	data := readFile(t, path)
	if strings.HasPrefix(data, "\ufeff") {
		t.Errorf("the file still starts with a byte order mark:\n%s", data)
	}
	if stored := storedCredential(t, path); stored.Access != access {
		t.Errorf("the file holds the token %q, want the traded one", stored.Access)
	}
	if _, ok := entries(t, path)["anthropic"]; !ok {
		t.Error("the neighbouring credential was dropped")
	}
}

// TestARefreshKeepsTheFieldsOfTheEntryItDoesNotOwn covers an entry that carries
// more than this package models, as pi's own credential type allows. The fields
// it does not own survive the write.
func TestARefreshKeepsTheFieldsOfTheEntryItDoesNotOwn(t *testing.T) {
	body := strings.Replace(
		fmt.Sprintf(stored, expires(-time.Hour)),
		`"accountId": "`+oldAccount+`"`,
		`"accountId": "`+oldAccount+`",
    "token_type": "Bearer",
    "scope": "openid profile email offline_access",
    "id_token": "an-id-token",
    "future": {"nested": true}`, 1)
	path := authFile(t, body)
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background()); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}

	var entry map[string]json.RawMessage
	if err := json.Unmarshal(entries(t, path)[piauth.ProviderCodex], &entry); err != nil {
		t.Fatalf("the entry is not an object: %v", err)
	}
	for field, want := range map[string]string{
		"token_type": `"Bearer"`,
		"scope":      `"openid profile email offline_access"`,
		"id_token":   `"an-id-token"`,
	} {
		if got := string(entry[field]); got != want {
			t.Errorf("the entry holds %s as %s, want the %s it had", field, got, want)
		}
	}
	var future struct {
		Nested bool `json:"nested"`
	}
	if err := json.Unmarshal(entry["future"], &future); err != nil || !future.Nested {
		t.Errorf("the entry's own nested field did not survive: %s (%v)", entry["future"], err)
	}
	var got string
	if err := json.Unmarshal(entry["access"], &got); err != nil || got != access {
		t.Errorf("the entry's access token is %q, want the traded one: %v", got, err)
	}
}

// TestARefreshDoesNotFollowARedirect covers the redirect that would replay the
// refresh token at wherever it points. The redirect is the answer, it is not
// followed, and the credential is untouched.
func TestARefreshDoesNotFollowARedirect(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := readFile(t, path)
	var leaked atomic.Bool
	var leakedHost atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
		leakedHost.Store(r.Host)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"stolen","refresh_token":"stolen","expires_in":3600}`))
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/oauth/token", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	credentials := piauth.Credentials{Path: path, TokenURL: redirect.URL, Now: clock}

	_, err := credentials.Access(context.Background())
	if !errors.Is(err, piauth.ErrRefresh) {
		t.Fatalf("the redirect is %v, want ErrRefresh", err)
	}
	if leaked.Load() {
		t.Errorf("the refresh was replayed at the redirect's target %v", leakedHost.Load())
	}
	if !strings.Contains(err.Error(), "307") {
		t.Errorf("the error is %q, want it to name the redirect's status", err)
	}
	if after := readFile(t, path); before != after {
		t.Errorf("the file was written from a redirect's answer:\n%s", after)
	}
}

// TestARefreshDoesNotFollowARedirectEvenWhenTheClientWould covers the client a
// caller supplies. The credentials' redirect policy is the package's, whatever
// the caller's client does. A client whose own policy follows a 307 cannot hand
// the token over, because a 307 replays the refresh body at its target.
func TestARefreshDoesNotFollowARedirectEvenWhenTheClientWould(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	before := readFile(t, path)
	var replayed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the redirect target's body: %v", err)
		}
		if strings.Contains(string(body), oldRefresh) {
			replayed.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"stolen","expires_in":3600}`))
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/oauth/token", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	var consulted atomic.Int32
	client := &http.Client{
		// What a caller's own client would do with a 307: follow it, and
		// let Go replay the body at the target.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			consulted.Add(1)
			return nil
		},
	}
	credentials := piauth.Credentials{Path: path, TokenURL: redirect.URL, HTTP: client, Now: clock}

	_, err := credentials.Access(context.Background())
	if !errors.Is(err, piauth.ErrRefresh) {
		t.Fatalf("the redirect is %v, want ErrRefresh", err)
	}
	if replayed.Load() {
		t.Error("the refresh token reached the redirect's target: the caller's redirect policy was used")
	}
	if consulted.Load() != 0 {
		t.Errorf("the caller's redirect policy was consulted %d times, want the refresh to decide the redirect itself", consulted.Load())
	}
	if after := readFile(t, path); before != after {
		t.Errorf("the file was written from a redirect's answer:\n%s", after)
	}
}

// TestARefreshThatCannotBeStoredIsReportedAsSuch covers the outcome the caller
// must be able to tell from a refusal. The endpoint traded the refresh token,
// so the token in the file may be spent. The file is not the one the caller is
// told about.
func TestARefreshThatCannotBeStoredIsReportedAsSuch(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the file mode this test needs")
	}
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("making the auth file unwritable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	before := readFile(t, path)
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err := credentials.Access(context.Background())
	if !errors.Is(err, piauth.ErrPersist) {
		t.Fatalf("the failed write is %v, want ErrPersist", err)
	}
	if errors.Is(err, piauth.ErrLocked) || errors.Is(err, piauth.ErrRefresh) {
		t.Errorf("the failed write also reads as %v, and the caller cannot tell what happened", err)
	}
	if endpoint.count() != 1 {
		t.Errorf("the endpoint was asked %d times, want the one trade that was not stored", endpoint.count())
	}
	if after := readFile(t, path); before != after {
		t.Errorf("the file changed even though the write failed:\n%s", after)
	}
}

// TestARefusalIsNotAPersistenceFailure covers the other half of the
// distinction: a refused grant traded nothing, so the caller may retry the
// file as it stands.
func TestARefusalIsNotAPersistenceFailure(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusBadRequest, `{"error":"invalid_grant"}`
	}, nil)
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	_, err := credentials.Access(context.Background())
	if !errors.Is(err, piauth.ErrRefresh) || errors.Is(err, piauth.ErrPersist) {
		t.Fatalf("the refusal is %v, want ErrRefresh and not ErrPersist", err)
	}
}

// TestARefusalCarriesOnlyAStandardCode covers what a refusal's error takes
// from the answer: the status, and a code the OAuth spec names. Everything
// else in an answer is untrusted. An answer that repeats the credential it was
// sent, or names a failure of its own, puts none of it in the caller's log. An
// answer that dresses its own words up as a standard code is not carried
// either.
func TestARefusalCarriesOnlyAStandardCode(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(sent string) string
		// carried is what the error must name, and absent what it must not:
		// the credential's own stretches, or the answer's words.
		carried string
		absent  []string
	}{
		{
			name:   "the answer is the refresh token",
			answer: func(sent string) string { return fmt.Sprintf(`{"error":%q}`, sent) },
			absent: tokenRuns(oldRefresh),
		},
		{
			name: "the answer's nested code is the refresh token",
			answer: func(sent string) string {
				return fmt.Sprintf(`{"error":{"code":%q}}`, sent)
			},
			absent: tokenRuns(oldRefresh),
		},
		{
			name:   "the answer names a failure of its own",
			answer: func(string) string { return `{"error":"quota_reached_for_this_workspace"}` },
			absent: []string{"quota"},
		},
		{
			name: "the answer dresses its own words up as a standard code",
			answer: func(sent string) string {
				return fmt.Sprintf(`{"error":%q}`, "invalid_grant: "+sent)
			},
			absent: append(tokenRuns(oldRefresh), "invalid_grant"),
		},
		{
			name:    "the answer names a standard code",
			answer:  func(string) string { return `{"error":"invalid_grant"}` },
			carried: "invalid_grant",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
			before := readFile(t, path)
			endpoint := newTokenEndpoint(t, func() (int, string) {
				return http.StatusBadRequest, c.answer(oldRefresh)
			}, nil)
			credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

			_, err := credentials.Access(context.Background())
			if !errors.Is(err, piauth.ErrRefresh) {
				t.Fatalf("the refusal is %v, want ErrRefresh", err)
			}
			if !strings.Contains(err.Error(), "status 400") {
				t.Errorf("the error is %q, want the status the endpoint refused with", err)
			}
			if c.carried != "" && !strings.Contains(err.Error(), c.carried) {
				t.Errorf("the error is %q, want it to name %q", err, c.carried)
			}
			for _, absent := range c.absent {
				if strings.Contains(err.Error(), absent) {
					t.Errorf("the error carries %q of the answer: %v", absent, err)
				}
			}
			if after := readFile(t, path); before != after {
				t.Errorf("the file was written after a refusal:\n%s", after)
			}
		})
	}
}

// heldEndpoint serves one refresh it holds open until the test closes answer.
// A test can then act on the lock while a refresh is in flight.
func heldEndpoint(t *testing.T) (entered chan struct{}, answer chan struct{}, url string) {
	t.Helper()
	entered = make(chan struct{})
	answer = make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-answer
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(traded(t, codexToken(t, newAccount), time.Hour)))
	}))
	t.Cleanup(server.Close)
	return entered, answer, server.URL
}

// tokenRuns are the eight-character stretches of a token. A run is long enough
// that the rest is a lookup away. It is short enough that an answer which
// repeats part of a token is not a coincidence.
func tokenRuns(secret string) []string {
	const run = 8
	var runs []string
	for i := 0; i+run <= len(secret); i++ {
		runs = append(runs, secret[i:i+run])
	}
	return runs
}

// takeOver breaks the lock a test's writer holds and creates a new directory in
// its place. It does what a writer that judged the lock stale would.
func takeOver(t *testing.T, lockPath string, mtime time.Time) {
	t.Helper()
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("breaking the lock: %v", err)
	}
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("taking the lock as another writer: %v", err)
	}
	if err := os.Chtimes(lockPath, mtime, mtime); err != nil {
		t.Fatalf("marking the taken lock: %v", err)
	}
}

// TestARefreshFollowsTheAuthFileSymlink covers the auth.json an operator keeps
// elsewhere. The write goes through the link, so the link is still a link and
// its target holds the refreshed credential. A write that replaced the path
// with a regular file would break the operator's file the first time a token
// expired.
func TestARefreshFollowsTheAuthFileSymlink(t *testing.T) {
	dir := t.TempDir()
	target := dir + "/pi-auth.json"
	if err := os.WriteFile(target, []byte(fmt.Sprintf(stored, expires(-time.Hour))), 0o600); err != nil {
		t.Fatalf("writing the target file: %v", err)
	}
	link := dir + "/auth.json"
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := piauth.Credentials{Path: link, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background()); err != nil {
		t.Fatalf("the linked credential did not reach a token: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("stating the link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the auth.json is no longer a symlink: the write replaced the link")
	}
	if stored := storedCredential(t, target); stored.Access != access {
		t.Errorf("the link's target holds the token %q, want the traded one", stored.Access)
	}
	if left, err := os.ReadDir(dir); err == nil {
		if len(left) != 2 {
			t.Errorf("the directory holds %d entries (%v), want the link and its target", len(left), left)
		}
	}
}
