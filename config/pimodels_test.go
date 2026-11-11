package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// writePIModels writes a models file in pi's format and returns its
// path.
func writePIModels(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	return path
}

// writePIAuth writes pi's auth.json beside the models file.
func writePIAuth(t *testing.T, modelsFile, body string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(modelsFile), "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing pi's auth file: %v", err)
	}
}

// TestASettingsFileImportsPIModels covers the import: every provider
// becomes an endpoint, every model a model named provider/id, and
// pi's fields map to the settings' own.
func TestASettingsFileImportsPIModels(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"api": "openai-completions",
				"apiKey": "dummy",
				"models": [
					{
						"id": "qwen",
						"input": ["text", "image"],
						"contextWindow": 196608,
						"maxTokens": 81920,
						"reasoning": true,
						"compat": {"supportsReasoningEffort": true}
					}
				]
			}
		}
	}`)
	settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}

	endpoint, ok := settings.Endpoints["home"]
	if !ok || endpoint.Endpoint != "http://grace:4000/v1" || endpoint.APIKey != "dummy" {
		t.Errorf("the provider imported as %+v", endpoint)
	}
	model, ok := settings.Models["home/qwen"]
	if !ok {
		t.Fatalf("the model imported under no name; the models are %v", settings.Models)
	}
	if model.ModelID != "qwen" || model.Endpoint != "home" {
		t.Errorf("the model imported as %+v", model)
	}
	if model.ContextLimit != 196608 || model.MaxOutput != 81920 {
		t.Errorf("the model's limits imported as %d/%d", model.ContextLimit, model.MaxOutput)
	}
	if len(model.Inputs) != 2 || model.Inputs[0] != "text" || model.Inputs[1] != "image" {
		t.Errorf("the model's inputs imported as %v", model.Inputs)
	}
	if model.Reasoning == nil || model.Reasoning.Mode != poo.ReasoningEffort || model.Reasoning.Param != poo.ReasoningPathEffort {
		t.Errorf("the model's reasoning imported as %+v", model.Reasoning)
	}
}

// TestTheSettingsOwnEntriesWinOverAnImport covers the collision rule:
// a model the settings configure is the one that survives.
func TestTheSettingsOwnEntriesWinOverAnImport(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen", "contextWindow": 196608}]
			}
		}
	}`)
	settings, err := Parse([]byte(`{
		"version":1,
		"pi_models_file":"`+modelsFile+`",
		"endpoints":{"home":{"endpoint":"http://mine:4000/v1"}},
		"models":{"home/qwen":{"endpoint":"home","model_id":"qwen","context_limit":1000}}
	}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if settings.Endpoints["home"].Endpoint != "http://mine:4000/v1" {
		t.Errorf("the import took the endpoint: %+v", settings.Endpoints["home"])
	}
	if settings.Models["home/qwen"].ContextLimit != 1000 {
		t.Errorf("the import took the model: %+v", settings.Models["home/qwen"])
	}
}

// TestAThinkingLevelMapBecomesTheModelSLevels covers pi's level map: the
// keys with a value are the levels the model takes, in pi's own order, and
// a key whose value is null is a level the provider does not have.
func TestAThinkingLevelMapBecomesTheModelSLevels(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [
					{
						"id": "qwen",
						"reasoning": true,
						"compat": {"supportsReasoningEffort": true},
						"thinkingLevelMap": {"off": null, "minimal": "minimal", "low": "low", "high": "high", "xhigh": null, "max": "max"}
					},
					{
						"id": "plain",
						"reasoning": true,
						"compat": {"supportsReasoningEffort": true}
					}
				]
			}
		}
	}`)
	settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}

	// The order is pi's, and the null off is not a level the model takes.
	mapped := settings.Models["home/qwen"].ReasoningLevels
	want := []string{"minimal", "low", "high", "max"}
	if strings.Join(mapped, ",") != strings.Join(want, ",") {
		t.Errorf("the thinkingLevelMap imported as %v, want %v", mapped, want)
	}
	// A reasoning model with no map is the effort default.
	plain := settings.Models["home/plain"].ReasoningLevels
	want = []string{"off", "low", "medium", "high"}
	if strings.Join(plain, ",") != strings.Join(want, ",") {
		t.Errorf("the model without a map imported as %v, want %v", plain, want)
	}
}

// TestAnUnknownPIAPPIsARefusal covers a provider poo cannot speak to:
// the import refuses rather than importing halfway.
func TestAnUnknownPIAPPIsARefusal(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"odd": {
				"baseUrl": "http://odd:4000/v1",
				"api": "smoke-signals",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	_, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "smoke-signals") {
		t.Errorf("an unknown api loaded: %v", err)
	}
}

// TestAnUnknownPIInputIsARefusal covers a modality poo cannot read.
func TestAnUnknownPIInputIsARefusal(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen", "input": ["smell"]}]
			}
		}
	}`)
	_, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "smell") {
		t.Errorf("an unknown input loaded: %v", err)
	}
}

// TestAMissingPIModelsFileIsARefusal covers a file that is not there:
// the settings name it, so a missing file is the settings' failure.
func TestAMissingPIModelsFileIsARefusal(t *testing.T) {
	_, err := Parse([]byte(`{"version":1,"pi_models_file":"/tmp/no-such-models.json"}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "no-such-models") {
		t.Errorf("a missing models file loaded: %v", err)
	}
}

// TestAnImportWithoutTheEffortCompatStaysDefault covers the condition's
// other side: a model that reasons but whose compat note does not name
// the effort support imports with the model's own reasoning control —
// a regression to `reasoning` alone would fabricate one.
func TestAnImportWithoutTheEffortCompatStaysDefault(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen", "reasoning": true}]
			}
		}
	}`)
	settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if settings.Models["home/qwen"].Reasoning != nil {
		t.Errorf("the model imported with a reasoning control it never asked for: %+v", settings.Models["home/qwen"].Reasoning)
	}
}

// TestABrokenModelsFileIsARefusal covers a models file that is not JSON:
// the import refuses it, instead of loading an empty import with no
// signal.
func TestABrokenModelsFileIsARefusal(t *testing.T) {
	modelsFile := writePIModels(t, `{"providers": {`)
	_, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil {
		t.Error("a broken models file loaded")
	}
}

// TestAProviderWithoutAKeyTakesPiAuthsKey covers the pair of files: a
// models file written for pi names the provider, and the key is in
// pi's auth.json beside it. pi writes the type as api_key; a file that
// says api says the same thing.
func TestAProviderWithoutAKeyTakesPiAuthsKey(t *testing.T) {
	for _, spelling := range []string{"api_key", "api"} {
		t.Run(spelling, func(t *testing.T) {
			modelsFile := writePIModels(t, `{
				"providers": {
					"home": {
						"baseUrl": "http://grace:4000/v1",
						"models": [{"id": "qwen"}]
					}
				}
			}`)
			writePIAuth(t, modelsFile, `{"home": {"type": "`+spelling+`", "key": "test-key-123"}}`)
			settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
			if err != nil {
				t.Fatalf("the settings did not load: %v", err)
			}
			if key := settings.Endpoints["home"].APIKey; key != "test-key-123" {
				t.Errorf("the provider imported with the key %q", key)
			}
		})
	}
}

// TestTheModelsFilesKeyWinsOverPIAuth covers a provider that carries
// its own key: the file poo reads is the one the operator pointed at,
// and pi's credentials are not consulted for it.
func TestTheModelsFilesKeyWinsOverPIAuth(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"apiKey": "file-key-456",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	writePIAuth(t, modelsFile, `{"home": {"type": "api_key", "key": "test-key-123"}}`)
	settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if key := settings.Endpoints["home"].APIKey; key != "file-key-456" {
		t.Errorf("the provider imported with the key %q", key)
	}
}

// TestTheSettingsOwnEndpointWinsOverPIAuth covers the collision rule
// with a key in the offing: the settings' endpoint entry is the one
// that survives, key and all.
func TestTheSettingsOwnEndpointWinsOverPIAuth(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	writePIAuth(t, modelsFile, `{"home": {"type": "api_key", "key": "test-key-123"}}`)
	settings, err := Parse([]byte(`{
		"version":1,
		"pi_models_file":"`+modelsFile+`",
		"endpoints":{"home":{"endpoint":"http://mine:4000/v1","api_key":"settings-key-789"}}
	}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	endpoint := settings.Endpoints["home"]
	if endpoint.Endpoint != "http://mine:4000/v1" || endpoint.APIKey != "settings-key-789" {
		t.Errorf("the import took the endpoint: %+v", endpoint)
	}
}

// TestACredentialPooCannotUseIsSkipped covers the credentials pi keeps
// that are not a key poo can send: an OAuth token, and a key written
// as a command for pi to run. Both import as a provider without a key,
// and neither is an error.
func TestACredentialPooCannotUseIsSkipped(t *testing.T) {
	cases := map[string]string{
		"oauth":   `{"home": {"type": "oauth", "access": "gho_fake", "refresh": "ghr_fake", "expires": 1767225600000}}`,
		"command": `{"home": {"type": "api_key", "key": "!security find-generic-password -ws home"}}`,
	}
	for name, auth := range cases {
		t.Run(name, func(t *testing.T) {
			modelsFile := writePIModels(t, `{
				"providers": {
					"home": {
						"baseUrl": "http://grace:4000/v1",
						"models": [{"id": "qwen"}]
					}
				}
			}`)
			writePIAuth(t, modelsFile, auth)
			settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
			if err != nil {
				t.Fatalf("the settings did not load: %v", err)
			}
			if key := settings.Endpoints["home"].APIKey; key != "" {
				t.Errorf("the provider imported with the key %q", key)
			}
			if _, ok := settings.Models["home/qwen"]; !ok {
				t.Errorf("the model imported under no name; the models are %v", settings.Models)
			}
		})
	}
}

// TestAMissingPIAuthFileIsFine covers a directory pi does not keep keys
// in: the provider imports without one, and nothing fails.
func TestAMissingPIAuthFileIsFine(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if key := settings.Endpoints["home"].APIKey; key != "" {
		t.Errorf("the provider imported with the key %q", key)
	}
}

// TestABrokenPIAuthFileIsARefusal covers credentials poo cannot read: a
// file beside the models file that does not parse is the operator's to
// fix, not something to import past.
func TestABrokenPIAuthFileIsARefusal(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	writePIAuth(t, modelsFile, `{"home": {`)
	_, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "auth.json") {
		t.Errorf("a broken auth file loaded: %v", err)
	}
}
