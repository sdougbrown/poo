package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
)

// codexStored is the Codex CLI's own auth.json: an api key beside the tokens
// object the CLI's sign-in wrote. The access token is a JWT, because the file
// records no expiry of its own and the entry's expiry is read from the
// token's exp claim.
const codexStored = `{
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "id-token",
    "access_token": "%s",
    "refresh_token": "codex-refresh-token",
    "account_id": "acct-codex"
  },
  "last_refresh": "2026-01-01T00:00:00.000Z"
}`

// codexKeyStored is the same file in api-key mode: an api key and no tokens.
// It holds no credential the Codex provider reads.
const codexKeyStored = `{
  "OPENAI_API_KEY": "sk-keys-mode",
  "tokens": null
}`

// codexAccess is a JWT whose exp claim names the given moment, and whose
// account claim names the Codex account.
func codexAccess(exp time.Time) string {
	claims, err := json.Marshal(map[string]any{
		"exp":                         float64(exp.Unix()),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-codex"},
	})
	if err != nil {
		panic(err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
}

// aCodexFile writes a Codex CLI auth.json holding a token that expires at the
// given life from the test's clock.
func aCodexFile(t *testing.T, life time.Duration) string {
	t.Helper()
	return authFile(t, fmt.Sprintf(codexStored, codexAccess(clock().Add(life))))
}

// anUnusedEndpoint is a token endpoint that fails the test if it is asked for
// anything. A read-only credential is used as it is, and no trade is the
// proof.
func anUnusedEndpoint(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the token endpoint was asked for %s", r.URL.Path)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestTheCodexCLIFileIsReadAsItsOwnShape covers the read of the file the Codex
// CLI owns: it is its own format, the account and expiry come from the tokens
// object and the token's own exp claim, and a live token is reported live.
func TestTheCodexCLIFileIsReadAsItsOwnShape(t *testing.T) {
	path := aCodexFile(t, time.Hour)
	status, err := (auth.Credentials{Path: path, Now: clock}).Status(auth.Codex)
	if err != nil {
		t.Fatalf("the Codex CLI's file did not read: %v", err)
	}
	if status.Format != auth.FormatCodex {
		t.Errorf("the file read as format %q, want %q", status.Format, auth.FormatCodex)
	}
	if status.AccountID != "acct-codex" {
		t.Errorf("the credential names %q, want the account the tokens object holds", status.AccountID)
	}
	if want := clock().Add(time.Hour); !status.Expires.Equal(want) {
		t.Errorf("the credential expires at %v, want the token's exp claim, %v", status.Expires, want)
	}
	if !status.Live {
		t.Errorf("a token with an hour left is reported expired")
	}
}

// TestALiveCodexTokenIsUsedWithoutATradeOrALock covers the read-only access:
// the token the file holds is the token the caller gets, no endpoint is
// asked, and no lock is left beside a file poo does not own.
func TestALiveCodexTokenIsUsedWithoutATradeOrALock(t *testing.T) {
	path := aCodexFile(t, time.Hour)
	endpoint := anUnusedEndpoint(t)
	credentials := auth.Credentials{Path: path, ReadOnly: true, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the read-only access failed: %v", err)
	}
	if token.Access != codexAccess(clock().Add(time.Hour)) || token.AccountID != "acct-codex" {
		t.Errorf("the access got %q for %q, want the token and account the file holds", token.Access, token.AccountID)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reading the file's directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 1 || names[0] != "auth.json" {
		t.Errorf("the file's directory holds %v, want the auth.json alone — no lock beside it", names)
	}
}

// TestAnExpiredCodexTokenIsReportedAsReadOnly covers the failure that has no
// retry: a read-only credential past its expiry is ErrReadOnly, and the
// endpoint is never asked to trade what poo does not own.
func TestAnExpiredCodexTokenIsReportedAsReadOnly(t *testing.T) {
	path := aCodexFile(t, -time.Minute)
	endpoint := anUnusedEndpoint(t)
	credentials := auth.Credentials{Path: path, ReadOnly: true, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrReadOnly) {
		t.Fatalf("the expired credential failed as %v, want ErrReadOnly", err)
	}
	if token != (auth.Token{}) {
		t.Errorf("the caller got a token with an expired credential")
	}
	if !strings.Contains(err.Error(), "expired at") {
		t.Errorf("the failure says %q, want the moment the token stopped working", err.Error())
	}
}

// TestTheCodexFileIsNeverWritten covers the rule the whole format rests on:
// the CLI owns the file, so a refresh and a sign-in are refused, and the
// file's bytes are what they were.
func TestTheCodexFileIsNeverWritten(t *testing.T) {
	path := aCodexFile(t, time.Hour)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	endpoint := anUnusedEndpoint(t)
	credentials := auth.Credentials{Path: path, ReadOnly: true, TokenURL: endpoint.URL, Now: clock}

	if _, err := credentials.Refresh(context.Background(), auth.Codex); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("the refresh failed as %v, want ErrReadOnly", err)
	}
	entry := auth.Credential{Kind: auth.KindOAuth, Access: "x", Refresh: "y", AccountID: "z"}
	if _, err := credentials.Set(context.Background(), auth.Codex, entry, true); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("the sign-in's write failed as %v, want ErrReadOnly", err)
	}
	if _, err := credentials.Status(auth.Codex); err != nil {
		t.Errorf("the read still works after the refused writes: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("the file changed under a read-only store")
	}
}

// TestACodexFileInAPIKeyModeHoldsNoCredential covers the mode the Codex
// provider does not read: an api key reaches the platform API, not the
// endpoint the ChatGPT token serves, so the file answers no credential — and
// holds none, so the endpoint is not offered on a credential no request can
// use.
func TestACodexFileInAPIKeyModeHoldsNoCredential(t *testing.T) {
	path := authFile(t, codexKeyStored)
	credentials := auth.Credentials{Path: path, Now: clock}
	if _, err := credentials.Status(auth.Codex); !errors.Is(err, auth.ErrNoCredential) {
		t.Errorf("the api-key file's credential failed as %v, want ErrNoCredential", err)
	}
	held, err := credentials.Holds(auth.ProviderCodex)
	if err != nil {
		t.Fatalf("reading what the file holds: %v", err)
	}
	if held {
		t.Errorf("the api-key file holds a credential for %s, want none", auth.ProviderCodex)
	}
}

// TestAReadOnlyStoreRefusesAPiFileToo covers the flag's reach: it is the
// store's mode, not the format's. The fixture is pi-shaped — the providers at
// the top level, no version field — and its writes are refused the same way a
// poo-format file's would be, because the read-only check fires before any
// format check. A live credential is reported live with no leeway: there is
// no refresh to front-run.
func TestAReadOnlyStoreRefusesAPiFileToo(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(2*time.Minute)))
	credentials := auth.Credentials{Path: path, ReadOnly: true, Now: clock}

	entry := auth.Credential{Kind: auth.KindOAuth, Access: "x", Refresh: "y", AccountID: "z"}
	if _, err := credentials.Set(context.Background(), auth.Codex, entry, true); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("the write failed as %v, want ErrReadOnly", err)
	}
	if err := credentials.Delete(context.Background(), auth.ProviderCodex); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("the delete failed as %v, want ErrReadOnly", err)
	}
	if _, err := credentials.Refresh(context.Background(), auth.Codex); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("the refresh failed as %v, want ErrReadOnly", err)
	}
	status, err := credentials.Status(auth.Codex)
	if err != nil {
		t.Fatalf("the read-only status failed: %v", err)
	}
	if !status.Live {
		t.Errorf("a token with two minutes left is live to a store that cannot refresh")
	}
	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the live credential did not reach a token: %v", err)
	}
	if token.Access != oldAccess {
		t.Errorf("the access got %q, want the token the file holds", token.Access)
	}
}

// TestACodexFileWithoutTheFlagIsNeverTraded covers the misconfiguration the
// flag exists to prevent: the settings name the CLI's file without
// auth_readonly. An access that would trade is refused before the endpoint is
// asked — a trade the file could not record would spend the CLI's refresh
// token — and the file's bytes are what they were.
func TestACodexFileWithoutTheFlagIsNeverTraded(t *testing.T) {
	path := aCodexFile(t, -time.Minute)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	endpoint := anUnusedEndpoint(t)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err = credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrForeignFile) {
		t.Fatalf("the trade failed as %v, want ErrForeignFile", err)
	}
	if !strings.Contains(err.Error(), "auth_readonly") {
		t.Errorf("the failure says %q, want the setting that shares the file", err.Error())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("the file changed under a trade that was refused")
	}
}
