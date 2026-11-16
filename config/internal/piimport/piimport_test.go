package piimport_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
	"github.com/sdougbrown/poo/provider/piauth"
)

// TestAPisFilesThatOpenWithABOMAreRead covers the files a Windows editor
// writes. pi strips the byte order mark before parsing, and this reader strips
// it from the models file and from the auth file: one file must not be readable
// by pi and refused by poo, on either path.
func TestAPisFilesThatOpenWithABOMAreRead(t *testing.T) {
	dir := t.TempDir()
	models := filepath.Join(dir, "models.json")
	if err := os.WriteFile(models, []byte("\ufeff"+`{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	auth := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(auth, []byte("\ufeff"+`{"home": {"type": "api_key", "key": "test-key-123"}}`), 0o600); err != nil {
		t.Fatalf("writing the auth file: %v", err)
	}
	file, err := piimport.Read(models, "")
	if err != nil {
		t.Fatalf("reading the models file: %v", err)
	}
	provider := file.Providers["home"]
	if provider.APIKey != "test-key-123" {
		t.Errorf("the provider imported with the key %q, want the one the file holds", provider.APIKey)
	}
}

// writeModels writes a models file and returns its path.
func writeModels(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	return path
}

// TestTheCodexAPIImportsForTheCodexProvider covers pi's own name for the
// Codex wire. The Codex provider is read as the Responses API, so a models file
// that names it imports into a model poo can run.
func TestTheCodexAPIImportsForTheCodexProvider(t *testing.T) {
	file, err := piimport.Read(writeModels(t, `{
		"providers": {
			"openai-codex": {
				"baseUrl": "https://chatgpt.com/backend-api",
				"api": "openai-codex-responses",
				"models": [{"id": "gpt-6.1-sol"}]
			}
		}
	}`), "")
	if err != nil {
		t.Fatalf("reading the models file: %v", err)
	}
	provider := file.Providers[piauth.ProviderCodex]
	if provider.API != string(poo.APIResponses) {
		t.Errorf("the provider's API is %q, want the Responses API", provider.API)
	}
	if len(provider.Models) != 1 || provider.Models[0].ID != "gpt-6.1-sol" {
		t.Errorf("the provider's models are %+v, want the Codex model", provider.Models)
	}
}

// TestTheCodexAPIIsRefusedForAnotherProvider covers the other half. Another
// provider that named pi's Codex API would be sent the wrong requests. The
// import refuses it rather than reading it as something it is not.
func TestTheCodexAPIIsRefusedForAnotherProvider(t *testing.T) {
	_, err := piimport.Read(writeModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"api": "openai-codex-responses",
				"models": [{"id": "qwen"}]
			}
		}
	}`), "")
	if err == nil || !strings.Contains(err.Error(), "does not know") {
		t.Fatalf("the import is %v, want the API refused", err)
	}
}
