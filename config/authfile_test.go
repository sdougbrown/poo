package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo/config/internal/piimport"
	"github.com/sdougbrown/poo/provider/auth"
)

// piAuthFile is one auth.json of pi's: a Codex oauth credential, a provider whose
// key names an environment variable, and a provider whose key is written in the
// file. Two readers read it, and this is the fixture they both answer for.
const piAuthFile = `{
  "openai-codex": {
    "type": "oauth",
    "access": "an-access-token",
    "refresh": "a-refresh-token",
    "expires": 4102444800000,
    "accountId": "acct-pi"
  },
  "home": {"type": "api_key", "key": "$HOME_KEY"},
  "other": {"type": "api_key", "key": "a-literal-key"}
}`

// piModelsFile is a models file whose providers name no key, so they read theirs
// out of the auth file beside them.
const piModelsFile = `{
  "providers": {
    "home": {"baseUrl": "http://127.0.0.1:1/v1", "models": [{"id": "qwen"}]},
    "other": {"baseUrl": "http://127.0.0.1:2/v1", "models": [{"id": "qwen"}]}
  }
}`

// TestBothReadersOfPisAuthFileAgree covers the one file two readers read.
//
// The auth store reads the Codex credential out of pi's auth.json, and a models file's
// providers read their api keys out of the same file. They are two readers on
// purpose — they answer different questions, and a missing api key is not a missing
// credential — so what they must agree on is the file's shape. This is the fixture
// that pins it, and the byte order mark is the half a Windows editor adds and pi
// itself strips.
func TestBothReadersOfPisAuthFileAgree(t *testing.T) {
	for _, c := range []struct {
		name   string
		prefix string
	}{
		{name: "as pi writes it"},
		{name: "with a byte order mark", prefix: "\ufeff"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			authPath := filepath.Join(dir, "auth.json")
			if err := os.WriteFile(authPath, []byte(c.prefix+piAuthFile), 0o600); err != nil {
				t.Fatalf("writing the auth file: %v", err)
			}
			modelsPath := filepath.Join(dir, "models.json")
			if err := os.WriteFile(modelsPath, []byte(piModelsFile), 0o644); err != nil {
				t.Fatalf("writing the models file: %v", err)
			}

			// The credential reader: the Codex entry, by the account it names. The
			// token is live until 2100, so no refresh is asked for.
			token, err := auth.Credentials{Path: authPath}.Access(context.Background(), auth.Codex)
			if err != nil {
				t.Fatalf("the credential reader: %v", err)
			}
			if token.AccountID != "acct-pi" || token.Access != "an-access-token" {
				t.Errorf("the credential reader found %+v, want the Codex entry for acct-pi", token)
			}

			// The key reader: the providers that name no key of their own.
			file, err := piimport.Read(modelsPath, authPath)
			if err != nil {
				t.Fatalf("the key reader: %v", err)
			}
			if got := file.Providers["home"].APIKeyEnv; got != "HOME_KEY" {
				t.Errorf("the key reader found the environment %q, want HOME_KEY", got)
			}
			if got := file.Providers["other"].APIKey; got != "a-literal-key" {
				t.Errorf("the key reader found the key %q, want the one the file writes", got)
			}
		})
	}
}
