package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/sdougbrown/poo/provider/auth"
)

// pooStored is poo's own auth file: a version, and the credentials under it.
// It holds the neighbours a real file holds too: the anthropic api key pi's
// file carries, unchanged.
const pooStored = `{
  "version": 1,
  "credentials": {
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
    }
  }
}`

// pooFile reads poo's own auth file as the YAML it is written with: the version,
// and each credential's fields.
func pooFile(t *testing.T, path string) (int, map[string]map[string]any) {
	t.Helper()
	var file struct {
		Version     int                       `yaml:"version"`
		Credentials map[string]map[string]any `yaml:"credentials"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, path)), &file); err != nil {
		t.Fatalf("the auth file is not poo's: %v", err)
	}
	return file.Version, file.Credentials
}

// TestAnExpiredPooCredentialIsRefreshedInPooShape covers the file poo owns: the
// traded credential is stored under the versioned envelope, and everything
// else the file carried keeps what it had.
func TestAnExpiredPooCredentialIsRefreshedInPooShape(t *testing.T) {
	path := authFile(t, fmt.Sprintf(pooStored, expires(-time.Hour)))
	_, before := pooFile(t, path)
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}
	if token.Access != access || token.AccountID != newAccount {
		t.Errorf("the token is %q for %q, want the traded one for %q", token.Access, token.AccountID, newAccount)
	}

	version, held := pooFile(t, path)
	if version != 1 {
		t.Errorf("the file's version is %d, want 1", version)
	}
	entry := held[auth.ProviderCodex]
	if entry["access"] != access {
		t.Errorf("the file holds %q, want the traded %q", entry["access"], access)
	}
	if entry["refresh"] != newRefresh {
		t.Errorf("the file holds the refresh token %q, want the traded %q", entry["refresh"], newRefresh)
	}
	if entry["accountId"] != newAccount {
		t.Errorf("the file names the account %q, want the traded %q", entry["accountId"], newAccount)
	}
	if !reflect.DeepEqual(before["anthropic"], held["anthropic"]) {
		t.Errorf("the anthropic credential was rewritten:\n before %v\n after  %v",
			before["anthropic"], held["anthropic"])
	}
	// The file is the one an operator edits, so it is written as YAML.
	data := readFile(t, path)
	if !strings.HasPrefix(data, "version: 1\ncredentials:\n") {
		t.Errorf("the file is not the YAML poo writes:\n%s", data)
	}
}

// TestARefreshKeepsPisFileInPisShape covers the compat file: a refresh of pi's
// auth.json writes pi's shape back, because pi is still reading it. A version
// added to that file would be a file pi cannot read.
func TestARefreshKeepsPisFileInPisShape(t *testing.T) {
	path := authFile(t, fmt.Sprintf(stored, expires(-time.Hour)))
	access := codexToken(t, newAccount)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, access, time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background(), auth.Codex); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}

	written := entries(t, path)
	if _, ok := written["version"]; ok {
		t.Error("the file gained a version, so pi cannot read it any more")
	}
	if _, ok := written["credentials"]; ok {
		t.Error("the file gained a credentials envelope, so pi cannot read it any more")
	}
	if _, ok := written[auth.ProviderCodex]; !ok {
		t.Errorf("the file holds no %s entry at its top level, which is where pi reads one", auth.ProviderCodex)
	}
}

// TestPooFileOfAnotherVersionIsRefused covers the version rule: a file this
// package would write wrong is refused before anything is read out of it or
// asked for. The file is left as it was.
func TestPooFileOfAnotherVersionIsRefused(t *testing.T) {
	body := strings.Replace(fmt.Sprintf(pooStored, expires(-time.Hour)), `"version": 1`, `"version": 2`, 1)
	path := authFile(t, body)
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err := credentials.Access(context.Background(), auth.Codex)
	if !errors.Is(err, auth.ErrStorage) {
		t.Fatalf("the other version is %v, want ErrStorage", err)
	}
	if !strings.Contains(err.Error(), "version 2") {
		t.Errorf("the error is %q, want it to name the version the file is", err)
	}
	if endpoint.count() != 0 {
		t.Errorf("the endpoint was asked %d times for a file of another version, want none", endpoint.count())
	}
	if got := readFile(t, path); got != body {
		t.Errorf("the file was written for a version this package does not read:\n%s", got)
	}
}

// TestStatusReportsTheCredentialWithoutTradingIt covers the report a caller
// makes about a credential it is not using: the file, the shape, the account
// and whether the token is live, with no refresh and no write.
func TestStatusReportsTheCredentialWithoutTradingIt(t *testing.T) {
	live := codexToken(t, oldAccount)
	for _, c := range []struct {
		name   string
		body   string
		format string
		live   bool
	}{
		{
			name:   "poo's file, a live token",
			body:   fmt.Sprintf(pooStored, expires(time.Hour)),
			format: auth.FormatPoo,
			live:   true,
		},
		{
			name:   "pi's file, a live token",
			body:   fmt.Sprintf(stored, expires(time.Hour)),
			format: auth.FormatPi,
			live:   true,
		},
		{
			name:   "poo's file, a token inside the leeway",
			body:   fmt.Sprintf(pooStored, expires(time.Minute)),
			format: auth.FormatPoo,
		},
		{
			name:   "pi's file, an expired token",
			body:   fmt.Sprintf(stored, expires(-time.Hour)),
			format: auth.FormatPi,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := strings.Replace(c.body, oldAccess, live, 1)
			path := authFile(t, body)
			endpoint := newTokenEndpoint(t, func() (int, string) {
				return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
			}, nil)
			credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

			status, err := credentials.Status(auth.Codex)
			if err != nil {
				t.Fatalf("the status failed: %v", err)
			}
			if status.Path != path {
				t.Errorf("the status names the file %q, want %q", status.Path, path)
			}
			if status.Format != c.format {
				t.Errorf("the status names the shape %q, want %q", status.Format, c.format)
			}
			if status.Provider != auth.ProviderCodex {
				t.Errorf("the status names the provider %q, want %q", status.Provider, auth.ProviderCodex)
			}
			if status.AccountID != oldAccount {
				t.Errorf("the status names the account %q, want %q", status.AccountID, oldAccount)
			}
			if status.Live != c.live {
				t.Errorf("the status says the token is live=%t, want %t", status.Live, c.live)
			}
			if endpoint.count() != 0 {
				t.Errorf("the status asked the endpoint %d times, and a report trades nothing", endpoint.count())
			}
			if got := readFile(t, path); got != body {
				t.Errorf("the status wrote the file:\n%s", got)
			}
		})
	}
}

// TestStatusOfAFileWithoutACredentialIsReported covers the report a caller
// makes when there is nothing to report on: no file, and a file without the
// provider's entry are both the absence of a credential.
func TestStatusOfAFileWithoutACredentialIsReported(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{name: "no file"},
		{name: "another provider", body: `{"anthropic": {"type": "api_key", "key": "$ANTHROPIC_API_KEY"}}`},
		{name: "poo's file without the entry", body: `{"version": 1, "credentials": {}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			if c.body != "" {
				if err := os.WriteFile(path, []byte(c.body), 0o644); err != nil {
					t.Fatalf("writing the auth file: %v", err)
				}
			}
			if _, err := (auth.Credentials{Path: path}).Status(auth.Codex); !errors.Is(err, auth.ErrNoCredential) {
				t.Fatalf("the status is %v, want ErrNoCredential", err)
			}
		})
	}
}

// TestTheStatusOfAnAPIKeyNamesNoClient covers the report of an entry a foreign
// writer left with a client on a key: the read drops the client fields a key
// does not carry, and the report must agree with what the read answers.
func TestTheStatusOfAnAPIKeyNamesNoClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	body := `{"version": 1, "credentials": {"ollama-cloud": {"type": "api_key", "key": "sk-test", "clientId": "app_a-key-never-has"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the auth file: %v", err)
	}
	status, err := (auth.Credentials{Path: path}).Status(apiKeyProvider)
	if err != nil {
		t.Fatalf("the status failed: %v", err)
	}
	if status.ClientID != "" {
		t.Errorf("the status names the client %q for an api key, want none", status.ClientID)
	}
}

// TestAPooFileInEitherEncodingIsRead covers the two spellings of one file. poo
// writes YAML, and a file an operator wrote as JSON is the same document: the
// shape is the version field, not the punctuation.
func TestAPooFileInEitherEncodingIsRead(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{name: "YAML", body: `version: 1
credentials:
  openai-codex:
    type: oauth
    access: ` + oldAccess + `
    refresh: ` + oldRefresh + `
    expires: ` + fmt.Sprint(expires(time.Hour)) + `
    accountId: ` + oldAccount + `
`},
		{name: "JSON", body: fmt.Sprintf(pooStored, expires(time.Hour))},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := authFile(t, c.body)
			endpoint := newTokenEndpoint(t, func() (int, string) {
				return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
			}, nil)
			credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
			token, err := credentials.Access(context.Background(), auth.Codex)
			if err != nil {
				t.Fatalf("the credential did not reach a token: %v", err)
			}
			if token.Access != oldAccess || token.AccountID != oldAccount {
				t.Errorf("the token is %q for %q, want the stored one", token.Access, token.AccountID)
			}
			if endpoint.count() != 0 {
				t.Errorf("a live token asked the endpoint %d times", endpoint.count())
			}
		})
	}
}

// TestPooFileWritesWholeNumbersWhole covers the file an operator reads. A millisecond
// epoch is whole, and a whole number written as 1.76e+12 is a timestamp nobody can
// check or edit by hand.
func TestPooFileWritesWholeNumbersWhole(t *testing.T) {
	path := authFile(t, fmt.Sprintf(pooStored, expires(-time.Hour)))
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, codexToken(t, newAccount), time.Hour)
	}, nil)
	credentials := auth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background(), auth.Codex); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}
	data := readFile(t, path)
	want := fmt.Sprintf("expires: %d", expires(time.Hour))
	if !strings.Contains(data, want) {
		t.Errorf("the file writes the expiry as something else than %q:\n%s", want, data)
	}
	if strings.Contains(data, "e+") {
		t.Errorf("the file writes a number in exponent form:\n%s", data)
	}
}

// TestSetKeepsACredentialAndRefusesToReplaceOne covers the store's write for a
// credential that did not come from a refresh: a file is created with the entry,
// every other entry kept, and a second write is refused until it is asked for.
func TestSetKeepsACredentialAndRefusesToReplaceOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	access := codexToken(t, newAccount)
	credentials := auth.Credentials{Path: path, Now: clock}
	entry := auth.Credential{
		Access:    access,
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}

	status, err := credentials.Set(context.Background(), auth.Codex, entry, false)
	if err != nil {
		t.Fatalf("the credential was not stored: %v", err)
	}
	if status.AccountID != newAccount || !status.Live || status.Format != auth.FormatPoo {
		t.Errorf("the status is %+v, want the stored credential, live, in poo's file", status)
	}
	if _, held := pooFile(t, path); held[auth.ProviderCodex]["access"] != access {
		t.Errorf("the file holds %v, want the credential", held)
	}

	// A second write is the one a command must not make on its own.
	if _, err := credentials.Set(context.Background(), auth.Codex, entry, false); !errors.Is(err, auth.ErrExists) {
		t.Fatalf("a second write is %v, want ErrExists", err)
	}
	if _, err := credentials.Set(context.Background(), auth.Codex, entry, true); err != nil {
		t.Fatalf("the replaced write failed: %v", err)
	}
}

// TestSetRefusesPisFile covers the file a write must not touch: pi's, which
// another tool owns.
func TestSetRefusesPisFile(t *testing.T) {
	body := fmt.Sprintf(stored, expires(time.Hour))
	path := authFile(t, body)
	_, err := (auth.Credentials{Path: path}).Set(context.Background(), auth.Codex, auth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}, true)
	if !errors.Is(err, auth.ErrForeignFile) {
		t.Fatalf("the write is %v, want ErrForeignFile", err)
	}
	if got := readFile(t, path); got != body {
		t.Errorf("pi's file was written:\n%s", got)
	}
}

// TestSetKeepsTheOtherEntries covers the neighbours: a file that already holds
// another provider's credential keeps it.
func TestSetKeepsTheOtherEntries(t *testing.T) {
	path := authFile(t, fmt.Sprintf(pooStored, expires(-time.Hour)))
	_, before := pooFile(t, path)
	credentials := auth.Credentials{Path: path, Now: clock}
	access := codexToken(t, newAccount)
	if _, err := credentials.Set(context.Background(), auth.Codex, auth.Credential{
		Access:    access,
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}, true); err != nil {
		t.Fatalf("the credential was not stored: %v", err)
	}
	version, after := pooFile(t, path)
	if version != 1 {
		t.Errorf("the file's version is %d, want 1", version)
	}
	if !reflect.DeepEqual(before["anthropic"], after["anthropic"]) {
		t.Errorf("the anthropic credential was rewritten:\n before %v\n after  %v", before["anthropic"], after["anthropic"])
	}
	if after[auth.ProviderCodex]["access"] != access {
		t.Errorf("the file holds %v, want the stored credential", after[auth.ProviderCodex])
	}
}

// TestSetCreatesTheDirectoryItWritesInto covers the first write of all: poo's file in
// a config directory that does not exist yet. The lock is a directory beside the file,
// so the file's own directory has to be there before the lock is taken.
func TestSetCreatesTheDirectoryItWritesInto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "poo", "auth.json")
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the test's directory already exists: %v", err)
	}
	entry := auth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}
	if _, err := (auth.Credentials{Path: path, Now: clock}).Set(context.Background(), auth.Codex, entry, false); err != nil {
		t.Fatalf("the first write failed: %v", err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stating the directory the write created: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("the directory's mode is %v, want 0700", info.Mode().Perm())
	}
	if _, held := pooFile(t, path); held[auth.ProviderCodex]["access"] == "" {
		t.Errorf("the file holds no credential: %v", held)
	}
}

// TestSetRefusesAnEntryItCannotRead covers the replace rule's real question. An entry poo
// cannot decode — an api_key for the Codex provider, say — is still an entry, so replacing
// it is still a decision, and Set must not take it on its own.
func TestSetRefusesAnEntryItCannotRead(t *testing.T) {
	body := `{"version": 1, "credentials": {"openai-codex": {"type": "api_key", "key": "sk-written-by-hand"}}}`
	path := authFile(t, body)
	credentials := auth.Credentials{Path: path, Now: clock}

	held, err := credentials.Holds(auth.Codex.ID)
	if err != nil {
		t.Fatalf("asking whether the file holds an entry: %v", err)
	}
	if !held {
		t.Fatal("the file holds an entry, and Holds says it does not")
	}
	_, err = credentials.Set(context.Background(), auth.Codex, auth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}, false)
	if !errors.Is(err, auth.ErrExists) {
		t.Fatalf("the write is %v, want ErrExists", err)
	}
	if got := readFile(t, path); got != body {
		t.Errorf("the file was written:\n%s", got)
	}
}

// apiKeyProvider is a provider that has no tokens and no account: what an api key needs.
var apiKeyProvider = auth.Provider{ID: "ollama-cloud", Kind: auth.KindAPIKey}

// TestTwoProvidersShareOneFile covers the store as a store: one file, a credential per
// provider, and each one read, replaced and deleted without touching the other.
func TestTwoProvidersShareOneFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	credentials := auth.Credentials{Path: path, Now: clock}
	ctx := context.Background()

	codex, err := credentials.Set(ctx, auth.Codex, auth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}, false)
	if err != nil {
		t.Fatalf("storing the Codex credential: %v", err)
	}
	key, err := credentials.Set(ctx, apiKeyProvider, auth.Credential{Kind: auth.KindAPIKey, Key: "an-api-key"}, false)
	if err != nil {
		t.Fatalf("storing the api key: %v", err)
	}
	if codex.Provider != auth.ProviderCodex || key.Provider != "ollama-cloud" {
		t.Errorf("the reports name %q and %q", codex.Provider, key.Provider)
	}
	if key.Kind != auth.KindAPIKey || key.Expires != (time.Time{}) || key.AccountID != "" {
		t.Errorf("the api key reports %+v, want no expiry and no account", key)
	}

	// Both are read back by their own provider, and only by it.
	if got, err := credentials.Read(apiKeyProvider); err != nil || got.Key != "an-api-key" {
		t.Errorf("reading the api key is %+v, %v", got, err)
	}
	if _, err := credentials.Read(auth.Codex); err != nil {
		t.Errorf("reading the Codex credential: %v", err)
	}
	// A provider reads the kind it accepts, and refuses another.
	if _, err := credentials.Status(apiKeyProvider); err != nil {
		t.Errorf("the api key's status: %v", err)
	}
	if _, err := credentials.Status(auth.Provider{ID: "ollama-cloud", Kind: auth.KindOAuth}); err == nil {
		t.Error("the Codex-kind read accepted an api key entry")
	}

	// Deleting one leaves the other.
	if err := credentials.Delete(ctx, "ollama-cloud"); err != nil {
		t.Fatalf("deleting the api key: %v", err)
	}
	if held, err := credentials.Holds("ollama-cloud"); err != nil || held {
		t.Errorf("the api key is still held (%t, %v)", held, err)
	}
	if held, err := credentials.Holds(auth.ProviderCodex); err != nil || !held {
		t.Errorf("the Codex credential was deleted with it (%t, %v)", held, err)
	}
	// Deleting what is not there is the state the caller asked for, not a failure.
	if err := credentials.Delete(ctx, "ollama-cloud"); err != nil {
		t.Errorf("deleting an entry that is not there: %v", err)
	}
}

// TestAnAPIKeyIsNotAToken covers the wrong question asked of the wrong kind: a token from an
// api key, and an api key from a provider that trades tokens.
func TestAnAPIKeyIsNotAToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	credentials := auth.Credentials{Path: path, Now: clock}
	if _, err := credentials.Set(context.Background(), apiKeyProvider, auth.Credential{Kind: auth.KindAPIKey, Key: "an-api-key"}, false); err != nil {
		t.Fatalf("storing the api key: %v", err)
	}
	if _, err := credentials.Access(context.Background(), apiKeyProvider); !errors.Is(err, auth.ErrNoCredential) {
		t.Errorf("Access on an api key is %v, want ErrNoCredential", err)
	}
}

// TestACredentialKindIsChecked covers what a credential must and must not carry. An oauth
// credential with no tokens, an api key with no key, and either kind carrying the other's
// fields are all refused before anything is written.
func TestACredentialKindIsChecked(t *testing.T) {
	for _, c := range []struct {
		name  string
		p     auth.Provider
		entry auth.Credential
	}{
		{name: "an oauth credential with no tokens", p: auth.Codex, entry: auth.Credential{}},
		{name: "an api key with no key", p: apiKeyProvider, entry: auth.Credential{Kind: auth.KindAPIKey}},
		{name: "an api key carrying tokens", p: apiKeyProvider, entry: auth.Credential{Kind: auth.KindAPIKey, Key: "k", Access: "a"}},
		{name: "an oauth credential carrying a key", p: auth.Codex, entry: auth.Credential{Access: "a", Refresh: "r", Key: "k", AccountID: newAccount}},
		{name: "an api key for a provider that reads oauth", p: auth.Codex, entry: auth.Credential{Kind: auth.KindAPIKey, Key: "k"}},
		{name: "an unknown kind", p: apiKeyProvider, entry: auth.Credential{Kind: "sigv4", Key: "k"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			if _, err := (auth.Credentials{Path: path, Now: clock}).Set(context.Background(), c.p, c.entry, false); err == nil {
				t.Fatal("the credential was stored")
			}
			if _, err := os.Stat(path); err == nil {
				t.Error("the file was written for a credential that was refused")
			}
		})
	}
}

// TestAProviderThatNamesNoAccountTakesAnyToken covers the rule being the provider's: an
// oauth provider with no account rule accepts a token that names no account, and Codex,
// which has one, does not.
func TestAProviderThatNamesNoAccountTakesAnyToken(t *testing.T) {
	noAccount := auth.Provider{ID: "another-oauth", Kind: auth.KindOAuth}
	path := filepath.Join(t.TempDir(), "auth.json")
	credentials := auth.Credentials{Path: path, Now: clock}
	entry := auth.Credential{Access: "an-opaque-token", Refresh: newRefresh, Expires: clock().Add(time.Hour)}
	if _, err := credentials.Set(context.Background(), noAccount, entry, false); err != nil {
		t.Fatalf("a provider with no account rule refused the token: %v", err)
	}
	codex := auth.Credential{Access: "an-opaque-token", Refresh: newRefresh, Expires: clock().Add(time.Hour)}
	if _, err := credentials.Set(context.Background(), auth.Codex, codex, true); !errors.Is(err, auth.ErrNoCredential) {
		t.Errorf("Codex took a token that names no account: %v", err)
	}
}

// TestDeleteRefusesPisFile covers the guard the two writes share: neither Set nor Delete may
// rewrite a file another tool owns. Delete is the one with no test, and a refactor that moved
// the check would have silently rewritten pi's auth.json.
func TestDeleteRefusesPisFile(t *testing.T) {
	body := fmt.Sprintf(stored, expires(time.Hour))
	path := authFile(t, body)
	err := (auth.Credentials{Path: path}).Delete(context.Background(), auth.ProviderCodex)
	if !errors.Is(err, auth.ErrForeignFile) {
		t.Fatalf("the delete is %v, want ErrForeignFile", err)
	}
	if got := readFile(t, path); got != body {
		t.Errorf("pi's file was written:\n%s", got)
	}
}

// TestAProviderNamesItsTokenEndpoint covers the provider's own endpoint, with no caller
// override: a caller that names none trades where its provider says, and only a provider that
// names none falls back to this package's default.
func TestAProviderNamesItsTokenEndpoint(t *testing.T) {
	path := authFile(t, fmt.Sprintf(pooStored, expires(-time.Hour)))
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, codexToken(t, newAccount), time.Hour)
	}, nil)
	declared := auth.Provider{ID: auth.ProviderCodex, Kind: auth.KindOAuth, TokenURL: endpoint.URL}
	credentials := auth.Credentials{Path: path, Now: clock}
	// The caller names no endpoint of its own.
	if _, err := credentials.Access(context.Background(), declared); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}
	if endpoint.count() != 1 {
		t.Errorf("the provider's endpoint was asked %d times, want one", endpoint.count())
	}
}

// TestAStoredCredentialReadsBack covers the round trip the two validators share: what Set
// accepts, Read returns — for either kind.
func TestAStoredCredentialReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	credentials := auth.Credentials{Path: path, Now: clock}
	oauth := auth.Credential{Access: codexToken(t, newAccount), Refresh: newRefresh, Expires: clock().Add(time.Hour), AccountID: newAccount}
	key := auth.Credential{Kind: auth.KindAPIKey, Key: "an-api-key"}
	for _, c := range []struct {
		provider auth.Provider
		entry    auth.Credential
	}{
		{auth.Codex, oauth},
		{apiKeyProvider, key},
	} {
		if _, err := credentials.Set(context.Background(), c.provider, c.entry, true); err != nil {
			t.Fatalf("storing %s: %v", c.provider.ID, err)
		}
		if _, err := credentials.Read(c.provider); err != nil {
			t.Errorf("reading back %s: %v", c.provider.ID, err)
		}
	}
}

// TestReplacingAnEntryDropsTheFieldsItsKindDoesNotCarry covers the merge that had to learn to
// delete: replacing an api key with an oauth credential left the old key behind, so the next
// read found a credential carrying both and refused it — a write that reports success and leaves
// a credential nothing can use.
func TestReplacingAnEntryDropsTheFieldsItsKindDoesNotCarry(t *testing.T) {
	dir := t.TempDir()
	oauthFile := filepath.Join(dir, "auth.json")
	both := auth.Provider{ID: "both", Kind: ""}
	credentials := auth.Credentials{Path: oauthFile, Now: clock}
	ctx := context.Background()

	if _, err := credentials.Set(ctx, apiKeyProvider, auth.Credential{Kind: auth.KindAPIKey, Key: "an-api-key"}, false); err != nil {
		t.Fatalf("storing the api key: %v", err)
	}
	// The same entry, replaced by an oauth credential of another provider that reads either kind.
	oauth := auth.Credential{Access: codexToken(t, newAccount), Refresh: newRefresh, Expires: clock().Add(time.Hour), AccountID: newAccount}
	if _, err := credentials.Set(ctx, both, oauth, true); err != nil {
		t.Fatalf("replacing the api key: %v", err)
	}
	_, held := pooFile(t, oauthFile)
	entry := held["both"]
	if _, ok := entry["key"]; ok {
		t.Errorf("the oauth entry kept the api key's field: %v", entry)
	}
	if _, err := credentials.Read(both); err != nil {
		t.Errorf("the replaced credential does not read back: %v", err)
	}

	// And back again: an api key over an oauth entry keeps no token fields.
	if _, err := credentials.Set(ctx, apiKeyProvider, auth.Credential{Kind: auth.KindAPIKey, Key: "another-key"}, true); err != nil {
		t.Fatalf("replacing the oauth credential: %v", err)
	}
	_, held = pooFile(t, oauthFile)
	entry = held[auth.ProviderCodex]
	for _, field := range []string{"access", "refresh", "expires", "accountId", "clientId", "tokenUrl"} {
		if _, ok := entry[field]; ok {
			t.Errorf("the api key entry kept %q: %v", field, entry)
		}
	}
}

// TestAnEntryThatNamesNoTypeIsAnOAuthOne covers an entry with tokens and no type: a hand-written
// file, or one from a tool that leaves the type out. It reads as an oauth credential, which is
// what Kind documents and what validate expects.
func TestAnEntryThatNamesNoTypeIsAnOAuthOne(t *testing.T) {
	body := fmt.Sprintf(`{"version": 1, "credentials": {"openai-codex": {"access": %q, "refresh": "r", "expires": %d, "accountId": %q}}}`,
		codexToken(t, oldAccount), expires(time.Hour), oldAccount)
	path := authFile(t, body)
	credentials := auth.Credentials{Path: path, Now: clock}
	token, err := credentials.Access(context.Background(), auth.Codex)
	if err != nil {
		t.Fatalf("a typed-less oauth entry did not read: %v", err)
	}
	if token.AccountID != oldAccount {
		t.Errorf("the token names the account %q, want %q", token.AccountID, oldAccount)
	}
}

// TestACredentialKeepsTheClientItWasGrantedTo covers the record that makes a refresh right. A
// refresh token belongs to the client that was granted it, so the credential says which client
// that is, and a refresh names it rather than the provider's default.
func TestACredentialKeepsTheClientItWasGrantedTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	// A provider that declares one client, and a credential granted to another — which is what
	// an imported credential is: pi's grant, in a store whose provider says something else.
	declared := auth.Provider{ID: "declared", Kind: auth.KindOAuth, ClientID: "poo-default", TokenURL: "http://declared.invalid/token"}
	entry := auth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   oldRefresh,
		Expires:   clock().Add(-time.Hour),
		AccountID: newAccount,
		ClientID:  "the-grant-that-was-made",
		TokenURL:  "http://granted.invalid/token",
	}
	credentials := auth.Credentials{Path: path, Now: clock}
	status, err := credentials.Set(context.Background(), declared, entry, false)
	if err != nil {
		t.Fatalf("storing the credential: %v", err)
	}
	if status.ClientID != "the-grant-that-was-made" {
		t.Errorf("the status names the client %q, want the one the credential records", status.ClientID)
	}
	if got, err := credentials.Read(declared); err != nil || got.ClientID != "the-grant-that-was-made" {
		t.Errorf("the credential reads back as %+v, %v", got, err)
	}

	// The refresh names the credential's client and endpoint, not the provider's.
	endpoint := newTokenEndpoint(t, func() (int, string) {
		return http.StatusOK, traded(t, codexToken(t, newAccount), time.Hour)
	}, nil)
	// The stored record is what the refresh goes to: name the fake on the credential itself,
	// leave the provider's declaration at a host that would fail, and pass no caller override,
	// so only the stored rung of the ladder answers.
	entry.TokenURL = endpoint.URL
	if _, err := credentials.Set(context.Background(), declared, entry, true); err != nil {
		t.Fatalf("re-storing the credential: %v", err)
	}
	if _, err := (auth.Credentials{Path: path, Now: clock}).Access(context.Background(), declared); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}
	if got := endpoint.form(t, 0)["client_id"]; got != "the-grant-that-was-made" {
		t.Errorf("the refresh named the client %q, want the credential's", got)
	}
	// And the record survives the refresh.
	if got, _ := credentials.Read(declared); got.ClientID != "the-grant-that-was-made" || got.TokenURL != endpoint.URL {
		t.Errorf("the refresh rewrote the record: %+v", got)
	}
}

// TestAnAPIKeyCarriesNoClient covers the kind that has none: a key is not granted to a client,
// so a credential naming one — a client, or the endpoint that client trades at — is refused.
func TestAnAPIKeyCarriesNoClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	for name, credential := range map[string]auth.Credential{
		"a client":    {Kind: auth.KindAPIKey, Key: "k", ClientID: "a-client"},
		"an endpoint": {Kind: auth.KindAPIKey, Key: "k", TokenURL: "https://grant.example/token"},
	} {
		_, err := (auth.Credentials{Path: path, Now: clock}).Set(context.Background(), apiKeyProvider, credential, false)
		if !errors.Is(err, auth.ErrNoCredential) {
			t.Fatalf("the api key with %s is %v, want ErrNoCredential", name, err)
		}
	}
}
