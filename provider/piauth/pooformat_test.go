package piauth_test

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

	"github.com/sdougbrown/poo/provider/piauth"
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
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	token, err := credentials.Access(context.Background())
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
	entry := held[piauth.ProviderCodex]
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
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background()); err != nil {
		t.Fatalf("the credential did not reach a token: %v", err)
	}

	written := entries(t, path)
	if _, ok := written["version"]; ok {
		t.Error("the file gained a version, so pi cannot read it any more")
	}
	if _, ok := written["credentials"]; ok {
		t.Error("the file gained a credentials envelope, so pi cannot read it any more")
	}
	if _, ok := written[piauth.ProviderCodex]; !ok {
		t.Errorf("the file holds no %s entry at its top level, which is where pi reads one", piauth.ProviderCodex)
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
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

	_, err := credentials.Access(context.Background())
	if !errors.Is(err, piauth.ErrStorage) {
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
			format: piauth.FormatPoo,
			live:   true,
		},
		{
			name:   "pi's file, a live token",
			body:   fmt.Sprintf(stored, expires(time.Hour)),
			format: piauth.FormatPi,
			live:   true,
		},
		{
			name:   "poo's file, a token inside the leeway",
			body:   fmt.Sprintf(pooStored, expires(time.Minute)),
			format: piauth.FormatPoo,
		},
		{
			name:   "pi's file, an expired token",
			body:   fmt.Sprintf(stored, expires(-time.Hour)),
			format: piauth.FormatPi,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := strings.Replace(c.body, oldAccess, live, 1)
			path := authFile(t, body)
			endpoint := newTokenEndpoint(t, func() (int, string) {
				return http.StatusInternalServerError, `{"error":"the endpoint was asked"}`
			}, nil)
			credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}

			status, err := credentials.Status()
			if err != nil {
				t.Fatalf("the status failed: %v", err)
			}
			if status.Path != path {
				t.Errorf("the status names the file %q, want %q", status.Path, path)
			}
			if status.Format != c.format {
				t.Errorf("the status names the shape %q, want %q", status.Format, c.format)
			}
			if status.Provider != piauth.ProviderCodex {
				t.Errorf("the status names the provider %q, want %q", status.Provider, piauth.ProviderCodex)
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
			if _, err := (piauth.Credentials{Path: path}).Status(); !errors.Is(err, piauth.ErrNoCredential) {
				t.Fatalf("the status is %v, want ErrNoCredential", err)
			}
		})
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
			credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
			token, err := credentials.Access(context.Background())
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
	credentials := piauth.Credentials{Path: path, TokenURL: endpoint.URL, Now: clock}
	if _, err := credentials.Access(context.Background()); err != nil {
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
	credentials := piauth.Credentials{Path: path, Now: clock}
	entry := piauth.Credential{
		Access:    access,
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}

	status, err := credentials.Set(context.Background(), entry, false)
	if err != nil {
		t.Fatalf("the credential was not stored: %v", err)
	}
	if status.AccountID != newAccount || !status.Live || status.Format != piauth.FormatPoo {
		t.Errorf("the status is %+v, want the stored credential, live, in poo's file", status)
	}
	if _, held := pooFile(t, path); held[piauth.ProviderCodex]["access"] != access {
		t.Errorf("the file holds %v, want the credential", held)
	}

	// A second write is the one a command must not make on its own.
	if _, err := credentials.Set(context.Background(), entry, false); !errors.Is(err, piauth.ErrExists) {
		t.Fatalf("a second write is %v, want ErrExists", err)
	}
	if _, err := credentials.Set(context.Background(), entry, true); err != nil {
		t.Fatalf("the replaced write failed: %v", err)
	}
}

// TestSetRefusesPisFile covers the file a write must not touch: pi's, which
// another tool owns.
func TestSetRefusesPisFile(t *testing.T) {
	body := fmt.Sprintf(stored, expires(time.Hour))
	path := authFile(t, body)
	_, err := (piauth.Credentials{Path: path}).Set(context.Background(), piauth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}, true)
	if !errors.Is(err, piauth.ErrForeignFile) {
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
	credentials := piauth.Credentials{Path: path, Now: clock}
	access := codexToken(t, newAccount)
	if _, err := credentials.Set(context.Background(), piauth.Credential{
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
	if after[piauth.ProviderCodex]["access"] != access {
		t.Errorf("the file holds %v, want the stored credential", after[piauth.ProviderCodex])
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
	entry := piauth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}
	if _, err := (piauth.Credentials{Path: path, Now: clock}).Set(context.Background(), entry, false); err != nil {
		t.Fatalf("the first write failed: %v", err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stating the directory the write created: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("the directory's mode is %v, want 0700", info.Mode().Perm())
	}
	if _, held := pooFile(t, path); held[piauth.ProviderCodex]["access"] == "" {
		t.Errorf("the file holds no credential: %v", held)
	}
}

// TestSetRefusesAnEntryItCannotRead covers the replace rule's real question. An entry poo
// cannot decode — an api_key for the Codex provider, say — is still an entry, so replacing
// it is still a decision, and Set must not take it on its own.
func TestSetRefusesAnEntryItCannotRead(t *testing.T) {
	body := `{"version": 1, "credentials": {"openai-codex": {"type": "api_key", "key": "sk-written-by-hand"}}}`
	path := authFile(t, body)
	credentials := piauth.Credentials{Path: path, Now: clock}

	held, err := credentials.Holds()
	if err != nil {
		t.Fatalf("asking whether the file holds an entry: %v", err)
	}
	if !held {
		t.Fatal("the file holds an entry, and Holds says it does not")
	}
	_, err = credentials.Set(context.Background(), piauth.Credential{
		Access:    codexToken(t, newAccount),
		Refresh:   newRefresh,
		Expires:   clock().Add(time.Hour),
		AccountID: newAccount,
	}, false)
	if !errors.Is(err, piauth.ErrExists) {
		t.Fatalf("the write is %v, want ErrExists", err)
	}
	if got := readFile(t, path); got != body {
		t.Errorf("the file was written:\n%s", got)
	}
}
