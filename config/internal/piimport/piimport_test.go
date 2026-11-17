package piimport_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
	"github.com/sdougbrown/poo/provider/auth"
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

// TestTheBuiltinProvidersReadThroughTheModelsFileShape covers the built-in
// providers' data: it is read through the same shape a file on disk takes, so
// a provider declared there imports with the API the endpoint speaks, no key
// it does not carry, and the levels its models declare — with minimal in none
// of them, for the level the endpoint does not take.
func TestTheBuiltinProvidersReadThroughTheModelsFileShape(t *testing.T) {
	file, err := piimport.Builtin("")
	if err != nil {
		t.Fatalf("reading the built-in providers: %v", err)
	}
	provider, ok := file.Providers[auth.ProviderCodex]
	if !ok {
		t.Fatalf("the built-in providers hold no %s provider: %v", auth.ProviderCodex, file.Providers)
	}
	if provider.API != string(poo.APIResponses) {
		t.Errorf("the provider's API is %q, want the Responses API", provider.API)
	}
	if !provider.SkipProbe {
		t.Errorf("the provider is probed, want its models taken as declared")
	}
	if provider.APIKeyEnv != "" || provider.APIKey != "" {
		t.Errorf("the provider carries a key %q/%q, want none: its credential is not an api key", provider.APIKeyEnv, provider.APIKey)
	}
	if len(provider.Models) == 0 {
		t.Fatalf("the provider serves no models")
	}
	withOff, withoutOff := 0, 0
	for _, model := range provider.Models {
		for _, level := range model.Levels {
			if level == "minimal" {
				t.Errorf("the model %s takes minimal, a level the endpoint does not spell", model.ID)
			}
			if level == "off" {
				withOff++
			}
		}
		if len(model.Levels) > 0 && model.Levels[0] != "off" {
			withoutOff++
		}
	}
	if withOff == 0 || withoutOff == 0 {
		t.Errorf("the models take levels in one shape only: %d with an off level, %d without", withOff, withoutOff)
	}
}

// TestAReplacementReplacesTheBuiltinProviders covers the override: a file
// named instead of the built-in data is read whole, so a binary that ships
// without the newer list can still serve the models it names.
func TestAReplacementReplacesTheBuiltinProviders(t *testing.T) {
	file, err := piimport.Builtin(writeModels(t, `{
		"providers": {
			"openai-codex": {
				"baseUrl": "https://chatgpt.com/backend-api",
				"api": "openai-codex-responses",
				"skipProbe": true,
				"models": [{"id": "gpt-test"}]
			}
		}
	}`))
	if err != nil {
		t.Fatalf("reading the replacement: %v", err)
	}
	provider, ok := file.Providers[auth.ProviderCodex]
	if !ok {
		t.Fatalf("the replacement holds no %s provider: %v", auth.ProviderCodex, file.Providers)
	}
	if len(provider.Models) != 1 || provider.Models[0].ID != "gpt-test" {
		t.Errorf("the provider's models are %+v, want the replacement's alone", provider.Models)
	}
}

// TestAReplacementThatIsNotThereIsRefused covers the override's promise: a
// path named for the built-in data is read or the load fails, never skipped
// in silence.
func TestAReplacementThatIsNotThereIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	_, err := piimport.Builtin(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("the import is %v, want the absent replacement refused", err)
	}
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
	provider := file.Providers[auth.ProviderCodex]
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

// TestPooOwnCredentialFileNamesTheKeysToo covers the shape of the credential
// file the settings resolve. It is one file whichever shape it is written with:
// the keys a models file's providers import come out of pi's top-level entries,
// and out of the entries poo's own versioned file holds under its credentials
// field.
func TestPooOwnCredentialFileNamesTheKeysToo(t *testing.T) {
	models := writeModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	for _, c := range []struct {
		name string
		body string
	}{
		{name: "poo's own file", body: `{
			"version": 1,
			"credentials": {"home": {"type": "api_key", "key": "test-key-123"}}
		}`},
		{name: "pi's file", body: `{"home": {"type": "api_key", "key": "test-key-123"}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			auth := filepath.Join(t.TempDir(), "auth.json")
			if err := os.WriteFile(auth, []byte(c.body), 0o600); err != nil {
				t.Fatalf("writing the credential file: %v", err)
			}
			file, err := piimport.Read(models, auth)
			if err != nil {
				t.Fatalf("reading the models file: %v", err)
			}
			if key := file.Providers["home"].APIKey; key != "test-key-123" {
				t.Errorf("the provider imported with the key %q, want the one the file holds", key)
			}
		})
	}
}
