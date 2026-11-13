package config

import (
	"errors"
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
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
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
	// The import leaves ops unset: the catalog gives a model no family
	// knows the chat op, so an import cannot clobber a family's ops.
	if len(model.Ops) != 0 {
		t.Errorf("the model's ops imported as %v, want them left to the catalog", model.Ops)
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
		"models_file":"`+modelsFile+`",
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
			_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
			if err == nil || !strings.Contains(err.Error(), api) {
				t.Fatalf("unsupported API %q was not refused: %v", api, err)
			}
		})
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
					},
					{
						"id": "none",
						"reasoning": true,
						"thinkingLevelMap": {"off": null, "minimal": null, "low": null, "medium": null, "high": null, "xhigh": null}
					}
				]
			}
		}
	}`)
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
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
	// A map that is all nulls takes none of pi's levels: the model
	// declares nothing rather than inheriting the default.
	if got := settings.Models["home/none"].ReasoningLevels; len(got) != 0 {
		t.Errorf("the all-null map imported as %v, want no levels", got)
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
	_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
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
	_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "smell") {
		t.Errorf("an unknown input loaded: %v", err)
	}
}

// TestAMissingModelsFileIsARefusal covers a file that is not there:
// the settings name it, so a missing file is the settings' failure.
func TestAMissingModelsFileIsARefusal(t *testing.T) {
	_, err := Parse([]byte(`{"version":1,"models_file":"/tmp/no-such-models.json"}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "no-such-models") {
		t.Errorf("a missing models file loaded: %v", err)
	}
}

// TestAModelThatDoesNotReasonTakesNoControl covers the condition's
// false half: `compat.supportsReasoningEffort` alone does not make a
// model reason, so a model whose `reasoning` is false imports with no
// reasoning control at all.
func TestAModelThatDoesNotReasonTakesNoControl(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "plain", "reasoning": false, "compat": {"supportsReasoningEffort": true}}]
			}
		}
	}`)
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if got := settings.Models["home/plain"].Reasoning; got != nil {
		t.Errorf("a model that does not reason imported with the control %+v", got)
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
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
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
	_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil {
		t.Error("a broken models file loaded")
	}
}

// TestAModelsFileWithoutProvidersIsARefusal covers a models file that
// parses but holds no providers: the run refuses rather than proceed
// with zero imported models in silence.
func TestAModelsFileWithoutProvidersIsARefusal(t *testing.T) {
	modelsFile := writePIModels(t, `{"providers": {}}`)
	_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil {
		t.Error("a models file without providers loaded")
	}
	if err != nil && !strings.Contains(err.Error(), "holds no providers") {
		t.Errorf("the refusal was %v, want it to name the empty file", err)
	}
}

// TestAModelsFileUnderHomeLoads covers the path the settings may name
// with a ~: the expansion happens where the settings load, so a
// models_file of ~/models.json reads the operator's home directory.
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
	settings, err := Parse([]byte(`{"version":1,"models_file":"~/models.json"}`), FormatJSON)
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
			_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the refusal was %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// TestAnImportedModelJoinsASettingsEndpoint covers the partial collision:
// the settings name the endpoint and the models file fills in its models.
// A provider-level skip would drop them in exactly the case the import
// exists for.
func TestAnImportedModelJoinsASettingsEndpoint(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	settings, err := Parse([]byte(`{
		"version":1,
		"models_file":"`+modelsFile+`",
		"endpoints":{"home":{"endpoint":"http://mine:4000/v1"}}
	}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	model, ok := settings.Models["home/qwen"]
	if !ok {
		t.Fatalf("the settings' endpoint cost the file its models: %v", settings.Models)
	}
	if model.Endpoint != "home" {
		t.Errorf("the imported model points at %q, want the settings' home endpoint", model.Endpoint)
	}
	if settings.Endpoints["home"].Endpoint != "http://mine:4000/v1" {
		t.Errorf("the import took the settings' endpoint: %+v", settings.Endpoints["home"])
	}
}

// writePIAuth writes pi's auth.json beside the models file.
func writePIAuth(t *testing.T, modelsFile, body string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(modelsFile), "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing pi's auth file: %v", err)
	}
}

// TestAProviderWithoutAKeyTakesPiAuthsKey covers the pair of files: a
// models file written for pi names the provider, and the key is in
// pi's auth.json beside it. pi writes the type as api_key; a file that
// says api says the same thing.
func TestAProviderWithoutAKeyTakesPiAuthsKey(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	writePIAuth(t, modelsFile, `{"home": {"type": "api_key", "key": "test-key-123"}}`)
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if key := settings.Endpoints["home"].APIKey; key != "test-key-123" {
		t.Errorf("the provider imported with the key %q", key)
	}
}

// TestAnAuthKeyInTheEnvironmentNamesTheEnvironment covers a credential
// whose key names the environment with pi's $ spelling: the endpoint
// imports holding the environment variable, not the spelling itself.
func TestAnAuthKeyInTheEnvironmentNamesTheEnvironment(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	writePIAuth(t, modelsFile, `{"home": {"type": "api_key", "key": "$HOME_KEY"}}`)
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	endpoint := settings.Endpoints["home"]
	if endpoint.APIKeyEnv != "HOME_KEY" || endpoint.APIKey != "" {
		t.Errorf("the provider imported with env %q and key %q, want HOME_KEY and none", endpoint.APIKeyEnv, endpoint.APIKey)
	}
}

// TestAModelsFileKeyFollowsTheSameRules covers a models file that
// carries its own key in pi's spellings: a $NAME names the
// environment, a !command is a key poo does not run, and a literal
// imports as itself.
func TestAModelsFileKeyFollowsTheSameRules(t *testing.T) {
	for test, key := range map[string]string{
		"environment": "$AWAY_KEY",
		"command":     "!pass show poo",
		"literal":     "file-key-456",
	} {
		t.Run(test, func(t *testing.T) {
			modelsFile := writePIModels(t, `{
				"providers": {
					"away": {
						"baseUrl": "http://away:4000/v1",
						"apiKey": "`+key+`",
						"models": [{"id": "spark"}]
					}
				}
			}`)
			settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
			if err != nil {
				t.Fatalf("the settings did not load: %v", err)
			}
			endpoint := settings.Endpoints["away"]
			var env, literal string
			switch test {
			case "environment":
				env, literal = "AWAY_KEY", ""
			case "command":
				env, literal = "", ""
			case "literal":
				env, literal = "", "file-key-456"
			}
			if endpoint.APIKeyEnv != env || endpoint.APIKey != literal {
				t.Errorf("the %s key imported as env %q and key %q", test, endpoint.APIKeyEnv, endpoint.APIKey)
			}
		})
	}
}

// TestACommandAPIKeyFallsThroughToPIAuth covers the one case the
// models file names a key poo cannot run: a "!command" is pi's to
// execute, so the file leaves poo no key, and the auth.json beside it
// is where the provider's key comes from. The comments say "leaves no
// usable key" for exactly this case.
func TestACommandAPIKeyFallsThroughToPIAuth(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"away": {
				"baseUrl": "http://away:4000/v1",
				"apiKey": "!pass show poo",
				"models": [{"id": "spark"}]
			}
		}
	}`)
	writePIAuth(t, modelsFile, `{"away": {"type": "api_key", "key": "auth-key-123"}}`)
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if got := settings.Endpoints["away"].APIKey; got != "auth-key-123" {
		t.Errorf("the command key imported as %q, want the auth file's key", got)
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
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
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
		"models_file":"`+modelsFile+`",
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
		"other provider": `{"other": {"type": "api_key", "key": "other-key"}}`,
		"oauth":          `{"home": {"type": "oauth", "access": "gho_fake", "refresh": "ghr_fake", "expires": 1767225600000}}`,
		"command":        `{"home": {"type": "api_key", "key": "!security find-generic-password -ws home"}}`,
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
			settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
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
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
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
	_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "auth.json") {
		t.Errorf("a broken auth file loaded: %v", err)
	}
}

// writeModelsFile writes a models file into dir under the name the discovery
// reads, and returns its path.
func writeModelsFile(t *testing.T, dir, name string) string {
	t.Helper()
	body := `{"providers":{"home":{"baseUrl":"http://localhost:4000/v1","api":"openai-completions",` +
		`"models":[{"id":"gemma4","contextWindow":131072}]}}}`
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	return path
}

// writeSettingsWith writes a settings document into dir and returns its path.
func writeSettingsWith(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, SettingsFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestAModelsFileBesideTheSettingsIsImported covers the discovery: a
// models.json next to the settings file is read when the settings name none.
func TestAModelsFileBesideTheSettingsIsImported(t *testing.T) {
	dir := t.TempDir()
	writeModelsFile(t, dir, ModelsFileName)
	path := writeSettingsWith(t, dir, `{"version":1}`)

	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if _, ok := settings.Models["home/gemma4"]; !ok {
		t.Errorf("the settings hold %v, and not the discovered provider's model", settings.Models)
	}
}

// TestANamedModelsFileWinsOverTheOneBeside covers the order: a models_file the
// settings name is the one read, and the discovered name is not.
func TestANamedModelsFileWinsOverTheOneBeside(t *testing.T) {
	dir := t.TempDir()
	writeModelsFile(t, dir, ModelsFileName)
	named := filepath.Join(t.TempDir(), "chosen.json")
	if err := os.WriteFile(named, []byte(`{"providers":{"chosen":{"baseUrl":"http://example.test/v1","api":"openai-completions","models":[{"id":"m","contextWindow":8192}]}}}`), 0o644); err != nil {
		t.Fatalf("writing the named models file: %v", err)
	}
	path := writeSettingsWith(t, dir, `{"version":1,"models_file":"`+named+`"}`)

	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if _, ok := settings.Models["chosen/m"]; !ok {
		t.Errorf("the settings hold %v, and not the named file's model", settings.Models)
	}
	if _, ok := settings.Models["home/gemma4"]; ok {
		t.Error("the discovered models file was read as well as the named one")
	}
}

// TestARelativeModelsFileResolvesAgainstTheSettings covers the directory: a
// relative path is read from beside the settings file, not from the working
// directory.
func TestARelativeModelsFileResolvesAgainstTheSettings(t *testing.T) {
	dir := t.TempDir()
	models := writeModelsFile(t, dir, "shared.json")
	path := writeSettingsWith(t, dir, `{"version":1,"models_file":"shared.json"}`)

	t.Chdir(t.TempDir())
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings from %s: %v", models, err)
	}
	if _, ok := settings.Models["home/gemma4"]; !ok {
		t.Errorf("the settings hold %v, and not the relative file's model", settings.Models)
	}
}

// TestThePIModelsFileKeyStillLoads covers the rename: a settings file written
// before it still imports its models file, and a failure names the new key.
func TestThePIModelsFileKeyStillLoads(t *testing.T) {
	dir := t.TempDir()
	models := writeModelsFile(t, dir, "shared.json")
	settings, err := Parse([]byte(`{"version":1,"pi_models_file":"`+models+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("a settings file with the old key was refused: %v", err)
	}
	if _, ok := settings.Models["home/gemma4"]; !ok {
		t.Errorf("the old key imported %v", settings.Models)
	}

	_, err = Parse([]byte(`{"version":1,"models_file":"/tmp/no-such-models.json"}`), FormatJSON)
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("a missing models file is %v, want a settings failure", err)
	}
	if failure.Field != "models_file" {
		t.Errorf("the failure names the field %q, want models_file", failure.Field)
	}
}

// TestASettingsFileWithNoModelsFileIsUnchanged covers the absence: a settings
// file with no models file beside it and none named reads as it always did.
func TestASettingsFileWithNoModelsFileIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := writeSettingsWith(t, dir, `{"version":1,"roles":{"chat":"m"}}`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if len(settings.Models) != 0 || len(settings.Endpoints) != 0 {
		t.Errorf("the settings gained %v and %v", settings.Models, settings.Endpoints)
	}
}
