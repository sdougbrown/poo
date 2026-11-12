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
			},
			"away": {
				"baseUrl": "http://away:4000/v1",
				"api": "openai-completions",
				"models": [
					{"id": "spark", "input": ["text"], "contextWindow": 4096}
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
	if model.Reasoning == nil || model.Reasoning.Mode != poo.ReasoningEffort || model.Reasoning.Param != "reasoning_effort" {
		t.Errorf("the model's reasoning imported as %+v", model.Reasoning)
	}
	if model.API != "" {
		t.Errorf("the default wire API imported as %q, want the empty default", model.API)
	}
	away, ok := settings.Models["away/spark"]
	if !ok {
		t.Fatalf("the remote model imported under no name; the models are %v", settings.Models)
	}
	if away.API != "" {
		t.Errorf("the remote model's API is %q, want the default", away.API)
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

func TestPIImportRefusesUnsupportedAPIs(t *testing.T) {
	for _, api := range []string{"openai-responses", "anthropic-messages"} {
		t.Run(api, func(t *testing.T) {
			modelsFile := writePIModels(t, `{"providers":{"remote":{"api":"`+api+`","baseUrl":"https://example.com","models":[{"id":"model"}]}}}`)
			_, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
			if err == nil || !strings.Contains(err.Error(), api) {
				t.Fatalf("unsupported API %q was not refused: %v", api, err)
			}
		})
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

// TestAModelsFileWithoutProvidersIsARefusal covers a models file that
// parses but holds no providers: the run refuses rather than proceed
// with zero imported models in silence.
func TestAModelsFileWithoutProvidersIsARefusal(t *testing.T) {
	modelsFile := writePIModels(t, `{"providers": {}}`)
	_, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil {
		t.Error("a models file without providers loaded")
	}
	if err != nil && !strings.Contains(err.Error(), "holds no providers") {
		t.Errorf("the refusal was %v, want it to name the empty file", err)
	}
}

// TestAModelsFileUnderHomeLoads covers the path the settings may name
// with a ~: the expansion happens where the settings load, so a
// pi_models_file of ~/models.json reads the operator's home directory.
func TestAModelsFileUnderHomeLoads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "models.json")
	if err := os.WriteFile(path, []byte(`{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	settings, err := Parse([]byte(`{"version":1,"pi_models_file":"~/models.json"}`), FormatJSON)
	if err != nil {
		t.Fatalf("a models file under ~ did not load: %v", err)
	}
	if _, ok := settings.Models["home/qwen"]; !ok {
		t.Errorf("the import did not take the model: %+v", settings.Models)
	}
}

// TestAModelsFileThatCannotRunIsARefusal covers the empty fields: a
// provider with no baseUrl and a model with no id would import into
// settings that cannot build a transport, so the file is refused where
// it is read rather than failing later with nothing to point at.
func TestAModelsFileThatCannotRunIsARefusal(t *testing.T) {
	cases := map[string]struct {
		file string
		want string
	}{
		"no base url": {
			file: `{"providers": {"home": {"models": [{"id": "qwen"}]}}}`,
			want: "names no baseUrl",
		},
		"no model id": {
			file: `{"providers": {"home": {"baseUrl": "http://grace:4000/v1", "models": [{"id": ""}]}}}`,
			want: "names a model with no id",
		},
		"no models": {
			file: `{"providers": {"home": {"baseUrl": "http://grace:4000/v1"}}}`,
			want: "serves no models",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			modelsFile := writePIModels(t, tc.file)
			_, err := Parse([]byte(`{"version":1,"pi_models_file":"`+modelsFile+`"}`), FormatJSON)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the refusal was %v, want it to say %q", err, tc.want)
			}
		})
	}
}
