package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/catalog"
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
						"input": ["text", "image", "audio"],
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

	endpoint, ok := settings.modelsFileLayer.Endpoints["home"]
	if !ok || endpoint.Endpoint != "http://grace:4000/v1" {
		t.Errorf("the provider imported as %+v", endpoint)
	}
	// A key written in pi's file is a literal, and a literal key lives in the
	// settings: the layer never carries one.
	if key := settings.Endpoints["home"].APIKey; key != "dummy" {
		t.Errorf("the provider's literal key imported as %q", key)
	}
	if key := settings.modelsFileLayer.Endpoints["home"].APIKey; key != "" {
		t.Errorf("the layer carries the literal key %q, and a models file never does", key)
	}
	model, ok := settings.modelsFileLayer.Models["home/qwen"]
	if !ok {
		t.Fatalf("the model imported under no name; the models are %v", settings.Models)
	}
	if model.ModelID != "qwen" || model.Endpoint != "home" {
		t.Errorf("the model imported as %+v", model)
	}
	if model.ContextLimit != 196608 || model.MaxOutput != 81920 {
		t.Errorf("the model's limits imported as %d/%d", model.ContextLimit, model.MaxOutput)
	}
	if len(model.Inputs) != 3 || model.Inputs[0] != "text" || model.Inputs[1] != "image" || model.Inputs[2] != "audio" {
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
	away, ok := settings.modelsFileLayer.Models["away/spark"]
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
	// The settings' own declarations are the settings', and they are what the
	// catalog reads over the file's layer.
	if settings.Endpoints["home"].Endpoint != "http://mine:4000/v1" {
		t.Errorf("the import took the endpoint: %+v", settings.Endpoints["home"])
	}
	if settings.Models["home/qwen"].ContextLimit != 1000 {
		t.Errorf("the import took the model: %+v", settings.Models["home/qwen"])
	}
}

func TestPIImportRefusesUnsupportedAPIs(t *testing.T) {
	for _, api := range []string{"openai-responses"} {
		t.Run(api, func(t *testing.T) {
			modelsFile := writePIModels(t, `{"providers":{"remote":{"api":"`+api+`","baseUrl":"https://example.com","models":[{"id":"model"}]}}}`)
			_, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
			if err == nil || !strings.Contains(err.Error(), api) {
				t.Fatalf("unsupported API %q was not refused: %v", api, err)
			}
		})
	}
}

// TestPIImportReadsTheMessagesAPI covers the one import path a Messages
// endpoint arrives by: pi names the API its own way, and the import maps the
// name onto the one poo speaks.
func TestPIImportReadsTheMessagesAPI(t *testing.T) {
	modelsFile := writePIModels(t, `{"providers":{"remote":{"api":"anthropic-messages","baseUrl":"https://example.com","models":[{"id":"model"}]}}}`)
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not read: %v", err)
	}
	model := settings.modelsFileLayer.Models["remote/model"]
	if model.API != poo.APIAnthropicMessages {
		t.Errorf("the imported model speaks %q, want the Messages API", model.API)
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
	mapped := settings.modelsFileLayer.Models["home/qwen"].ReasoningLevels
	want := []string{"minimal", "low", "high", "max"}
	if strings.Join(mapped, ",") != strings.Join(want, ",") {
		t.Errorf("the thinkingLevelMap imported as %v, want %v", mapped, want)
	}
	// A reasoning model with no map is the effort default.
	plain := settings.modelsFileLayer.Models["home/plain"].ReasoningLevels
	want = []string{"off", "low", "medium", "high"}
	if strings.Join(plain, ",") != strings.Join(want, ",") {
		t.Errorf("the model without a map imported as %v, want %v", plain, want)
	}
	// A map that is all nulls takes none of pi's levels: the model
	// declares nothing rather than inheriting the default.
	if got := settings.modelsFileLayer.Models["home/none"].ReasoningLevels; len(got) != 0 {
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
	if got := settings.modelsFileLayer.Models["home/plain"].Reasoning; got != nil {
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
	if settings.modelsFileLayer.Models["home/qwen"].Reasoning != nil {
		t.Errorf("the model imported with a reasoning control it never asked for: %+v", settings.modelsFileLayer.Models["home/qwen"].Reasoning)
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
	if _, ok := settings.modelsFileLayer.Models["home/qwen"]; !ok {
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
	model, ok := settings.modelsFileLayer.Models["home/qwen"]
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

// TestAPisAuthFileThatOpensWithABOMIsRead covers the file a Windows editor
// writes. pi strips the byte order mark before parsing, and so does poo: the
// models file and the Codex credential are read from a file the other tool
// reads too, so one reader refusing it would be a file pi can use and poo
// cannot.
func TestAPisAuthFileThatOpensWithABOMIsRead(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	writePIAuth(t, modelsFile, "\ufeff"+`{"home": {"type": "api_key", "key": "test-key-123"}}`)
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if key := settings.Endpoints["home"].APIKey; key != "test-key-123" {
		t.Errorf("the provider imported with the key %q", key)
	}
}

// TestPiAuthFileNamesTheKeyFileToo covers the one setting for pi's credential
// file. A models file whose provider names no key reads it from the auth file
// the settings resolved, not from the one beside the models file: a settings
// file that names the file has named it for every reader.
func TestPiAuthFileNamesTheKeyFileToo(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`)
	// The file beside the models file holds a different key, and the
	// setting names the one that counts.
	writePIAuth(t, modelsFile, `{"home": {"type": "api_key", "key": "beside-the-models"}}`)
	named := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(named, []byte(`{"home": {"type": "api_key", "key": "named-by-the-settings"}}`), 0o600); err != nil {
		t.Fatalf("writing the named auth file: %v", err)
	}
	settings, err := Parse([]byte(`{"version":1,"models_file":"`+modelsFile+`","pi_auth_file":"`+named+`"}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	if key := settings.Endpoints["home"].APIKey; key != "named-by-the-settings" {
		t.Errorf("the provider imported with the key %q, want the one the setting named", key)
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
	endpoint := settings.modelsFileLayer.Endpoints["home"]
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
			endpoint := settings.modelsFileLayer.Endpoints["away"]
			literal := settings.Endpoints["away"].APIKey
			var env, wantLiteral string
			switch test {
			case "environment":
				env, wantLiteral = "AWAY_KEY", ""
			case "command":
				env, wantLiteral = "", ""
			case "literal":
				env, wantLiteral = "", "file-key-456"
			}
			if endpoint.APIKeyEnv != env || literal != wantLiteral {
				t.Errorf("the %s key imported as env %q and key %q", test, endpoint.APIKeyEnv, literal)
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
	// The settings' own endpoint is the declaration the catalog reads, and its
	// key is the one the settings carry: the auth file's key was not imported
	// over it.
	if endpoint := settings.Endpoints["home"]; endpoint.APIKey != "settings-key-789" {
		t.Errorf("the import took the endpoint: %+v", endpoint)
	}
	served := catalog.New(settings.CatalogOptions()).Endpoints()["home"]
	if served.Endpoint != "http://mine:4000/v1" || served.Credential.Source != catalog.CredentialStatic {
		t.Errorf("the endpoint is served as %+v, want the settings'", served)
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
			if _, ok := settings.modelsFileLayer.Models["home/qwen"]; !ok {
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
	if _, ok := settings.modelsFileLayer.Models["home/gemma4"]; !ok {
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
	if _, ok := settings.modelsFileLayer.Models["chosen/m"]; !ok {
		t.Errorf("the settings hold %v, and not the named file's model", settings.Models)
	}
	if _, ok := settings.modelsFileLayer.Models["home/gemma4"]; ok {
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
	if _, ok := settings.modelsFileLayer.Models["home/gemma4"]; !ok {
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
	if _, ok := settings.modelsFileLayer.Models["home/gemma4"]; !ok {
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
	// A credential file in the user's config directory would offer the built-in
	// provider's models, so the directory is one of the test's own.
	tempUserConfigDir(t)
	dir := t.TempDir()
	path := writeSettingsWith(t, dir, `{"version":1,"roles":{"chat":"m"}}`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if len(settings.Models) != 0 || len(settings.Endpoints) != 0 {
		t.Errorf("the settings gained %v and %v", settings.Models, settings.Endpoints)
	}
	// No credential file is there either, so the built-in provider adds nothing to the
	// layer. Without this the guard could be lost and every load would gain its models.
	if len(settings.modelsFileLayer.Endpoints) != 0 || len(settings.modelsFileLayer.Models) != 0 {
		t.Errorf("the layer gained %v and %v, and no file contributed",
			settings.modelsFileLayer.Endpoints, settings.modelsFileLayer.Models)
	}
}

// TestParseDiscoversNoModelsFile covers the other reader's contract: a
// document held in memory has no directory, so nothing is imported beside it.
func TestParseDiscoversNoModelsFile(t *testing.T) {
	tempUserConfigDir(t)
	dir := t.TempDir()
	writeModelsFile(t, dir, ModelsFileName)
	t.Chdir(dir)

	settings, err := Parse([]byte(`{"version":1}`), FormatJSON)
	if err != nil {
		t.Fatalf("parsing the settings: %v", err)
	}
	if len(settings.Models) != 0 || len(settings.Endpoints) != 0 {
		t.Errorf("Parse imported %v and %v, and it discovers nothing", settings.Models, settings.Endpoints)
	}
	if len(settings.modelsFileLayer.Endpoints) != 0 || len(settings.modelsFileLayer.Models) != 0 {
		t.Errorf("Parse layered %v and %v, and it discovers nothing",
			settings.modelsFileLayer.Endpoints, settings.modelsFileLayer.Models)
	}
}

// TestAMissingRelativeModelsFileIsARefusal covers the named file's promise: a
// relative name that is not there refuses, and the sibling is not read in its
// place.
func TestAMissingRelativeModelsFileIsARefusal(t *testing.T) {
	dir := t.TempDir()
	writeModelsFile(t, dir, ModelsFileName)
	path := writeSettingsWith(t, dir, `{"version":1,"models_file":"missing.json"}`)

	_, err := Load(path)
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("a missing named models file loaded: %v", err)
	}
	if failure.Field != "models_file" {
		t.Errorf("the failure names the field %q, want models_file", failure.Field)
	}
}

// TestADiscoveredModelsFileThatDoesNotReadNamesTheFile covers the failure's
// subject: the settings named no file, so the failure names the file beside
// them rather than a setting that is unset.
func TestADiscoveredModelsFileThatDoesNotReadNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ModelsFileName), []byte(`{"providers":{}}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	path := writeSettingsWith(t, dir, `{"version":1}`)

	_, err := Load(path)
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("a discovered models file that does not read loaded: %v", err)
	}
	if failure.Field != "" {
		t.Errorf("the failure names the field %q, and the settings named no models file", failure.Field)
	}
	if !strings.Contains(failure.Message, "beside the settings") {
		t.Errorf("the message %q does not say where the file was found", failure.Message)
	}
	if !strings.Contains(failure.Error(), ModelsFileName) {
		t.Errorf("the failure %q does not name the file", failure.Error())
	}
}

// writePooModels writes poo's own models file into dir and returns its path.
func writePooModels(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, ModelsFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	return path
}

// pooModels is a models file of one endpoint serving one model.
const pooModels = `endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
    api_key_env: HOME_KEY
    models:
      qwen3:
        context_limit: 32768
        max_output: 4096
        inputs: [text, image]
        prompt_variant: gpt-6
        sampling:
          temperature: 0.3
`

// TestAModelsFileDeclaresWhatRuns covers the layer: a model written in the
// models file is a model a role can name, with no settings entry at all.
func TestAModelsFileDeclaresWhatRuns(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, pooModels)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
roles:
  chat: home/qwen3
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	cat := catalog.New(settings.CatalogOptions())
	if err := cat.CheckRoles(); err != nil {
		t.Fatalf("the role does not resolve: %v", err)
	}
	model, err := cat.Role(catalog.RoleChat)
	if err != nil {
		t.Fatalf("the chat model: %v", err)
	}
	if model.Name != "home/qwen3" || model.ModelID != "qwen3" {
		t.Errorf("the model is %q for %q, want home/qwen3 for qwen3", model.Name, model.ModelID)
	}
	if model.Provider != "home" || model.Endpoint != "http://127.0.0.1:1/v1" {
		t.Errorf("the model is served by %q at %q, want the file's endpoint", model.Provider, model.Endpoint)
	}
	if model.ContextLimit != 32768 || model.MaxOutput != 4096 {
		t.Errorf("the model's limits are %d and %d, want the file's", model.ContextLimit, model.MaxOutput)
	}
	if !model.Reads(poo.ModalityImage) {
		t.Error("the model does not read images, which the file says it does")
	}
	if model.Sampling.Temperature == nil || *model.Sampling.Temperature != 0.3 {
		t.Errorf("the temperature is %v, want the file's 0.3", model.Sampling.Temperature)
	}
	if model.Source["context_limit"] != catalog.LayerModelsFile {
		t.Errorf("the context limit comes from %q, want models_file", model.Source["context_limit"])
	}
	if model.PromptVariant != "gpt-6" {
		t.Errorf("the model's prompt variant is %q, want the file's gpt-6", model.PromptVariant)
	}
}

// TestTheSettingsOverruleTheModelsFile covers the composition: the file
// declares what runs, and the settings adjust it one field at a time.
func TestTheSettingsOverruleTheModelsFile(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, pooModels)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
endpoints:
  home:
    endpoint: http://127.0.0.1:2/v1
models:
  home/qwen3:
    endpoint: home
    model_id: qwen3
    context_limit: 65536
roles:
  chat: home/qwen3
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	cat := catalog.New(settings.CatalogOptions())
	model, err := cat.Role(catalog.RoleChat)
	if err != nil {
		t.Fatalf("the chat model: %v", err)
	}
	if model.ContextLimit != 65536 || model.Source["context_limit"] != catalog.LayerSettings {
		t.Errorf("the context limit is %d from %q, want the settings' 65536", model.ContextLimit, model.Source["context_limit"])
	}
	if model.Endpoint != "http://127.0.0.1:2/v1" {
		t.Errorf("the endpoint is %q, want the settings' URL", model.Endpoint)
	}
	// The fields the settings leave alone are still the file's, sampling included:
	// a model's own temperature belongs with the model, whichever layer declared it.
	if model.MaxOutput != 4096 || model.Source["max_output"] != catalog.LayerModelsFile {
		t.Errorf("the max output is %d from %q, want the file's 4096", model.MaxOutput, model.Source["max_output"])
	}
	if model.Sampling.Temperature == nil || *model.Sampling.Temperature != 0.3 {
		t.Errorf("the temperature is %v, want the file's 0.3", model.Sampling.Temperature)
	}
	if model.Source["sampling.temperature"] != catalog.LayerModelsFile {
		t.Errorf("the temperature comes from %q, want models_file", model.Source["sampling.temperature"])
	}
}

// TestAModelsFileRefusesALiteralKey covers the one key the file does not take. A
// models file is the one an operator shares or checks in, so a key written in it
// is refused rather than read, and the message names what to use instead.
func TestAModelsFileRefusesALiteralKey(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, `endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
    api_key: sk-written-in-the-file
    models:
      qwen3: {}
`)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("the settings loaded with a literal key in the models file")
	}
	for _, want := range []string{"api_key", "api_key_env", "auth file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure is %q, want it to name %q", err, want)
		}
	}
}

// TestBothModelsFilesBesideTheSettingsAreRefused covers the two names a models
// file goes by. Both are read as the models file, so a settings directory that
// holds one of each is refused rather than half read.
func TestBothModelsFilesBesideTheSettingsAreRefused(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, pooModels)
	if err := os.WriteFile(filepath.Join(dir, PiModelsFileName), []byte(`{"providers": {}}`), 0o644); err != nil {
		t.Fatalf("writing pi's models file: %v", err)
	}
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("the settings loaded with two models files beside them")
	}
	for _, want := range []string{ModelsFileName, PiModelsFileName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure is %q, want it to name %q", err, want)
		}
	}
}

// TestAModelsFileRefusesAnUnknownInput covers the one thing a model's `inputs` can say
// that poo cannot act on. A modality nothing reads would be a model that says it takes
// images while nothing sends it one, so the name is refused where it is written.
func TestAModelsFileRefusesAnUnknownInput(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, `endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
    models:
      qwen3:
        inputs: [text, smoke-signals]
`)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("the settings loaded with a modality poo does not read")
	}
	for _, want := range []string{"qwen3", "smoke-signals"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure is %q, want it to name %q", err, want)
		}
	}
}

// TestAModelsFileRefusesAKeyItDoesNotHave covers the contract the README promises: a
// key this file does not have is a failure that names it, rather than a setting with no
// effect. The strict decode is what makes that true, so a typo'd key is the test.
func TestAModelsFileRefusesAKeyItDoesNotHave(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, `endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
    models:
      qwen3:
        contxt_limit: 32768
`)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("the settings loaded with a key the models file does not have")
	}
	if !strings.Contains(err.Error(), "contxt_limit") {
		t.Errorf("the failure is %q, want it to name the key", err)
	}
}

// TestAModelsFileThatOpensWithABOMIsRead covers the file a Windows editor writes. The
// models file poo writes is YAML, and a BOM in front of it is a byte neither YAML nor
// JSON takes: the reader drops it, so a file an editor touched is a file poo reads.
func TestAModelsFileThatOpensWithABOMIsRead(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, "\ufeff"+pooModels)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
roles:
  chat: home/qwen3
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("a models file with a byte order mark did not load: %v", err)
	}
	model, err := catalog.New(settings.CatalogOptions()).Resolve("home/qwen3")
	if err != nil {
		t.Fatalf("the catalog resolves no home/qwen3: %v", err)
	}
	if model.ContextLimit != 32768 {
		t.Errorf("the model's context limit is %d, want the file's 32768", model.ContextLimit)
	}
}

// TestAPIImportIsTheModelsFileLayer covers where an imported pi model comes
// from: the models file's layer, not the settings, so `--explain` names
// models_file and the settings overrule it one field at a time.
func TestAPIImportIsTheModelsFileLayer(t *testing.T) {
	modelsFile := writePIModels(t, `{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen", "contextWindow": 196608, "maxTokens": 8192}]
			}
		}
	}`)
	settings, err := Parse([]byte(`{
		"version":1,
		"models_file":"`+modelsFile+`",
		"models":{"home/qwen":{"endpoint":"home","model_id":"qwen","context_limit":1024}}
	}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	model, err := catalog.New(settings.CatalogOptions()).Resolve("home/qwen")
	if err != nil {
		t.Fatalf("the catalog resolves no home/qwen: %v", err)
	}
	if model.ContextLimit != 1024 || model.Source["context_limit"] != catalog.LayerSettings {
		t.Errorf("the context limit is %d from %q, want the settings' 1024", model.ContextLimit, model.Source["context_limit"])
	}
	if model.MaxOutput != 8192 || model.Source["max_output"] != catalog.LayerModelsFile {
		t.Errorf("the max output is %d from %q, want the file's 8192", model.MaxOutput, model.Source["max_output"])
	}
}

// TestAPooModelsFileKeepsTheBuiltinProvider covers the two layers meeting. A credential
// file offers the built-in Codex provider, and a models file of poo's own shape says what
// else runs: the file folds over the built-in, so it overrules a model it names and keeps
// the ones it does not.
func TestAPooModelsFileKeepsTheBuiltinProvider(t *testing.T) {
	dir := tempUserConfigDir(t)
	// The credential file the settings resolve to, beside the models file, so the
	// built-in provider is offered.
	if err := os.WriteFile(filepath.Join(dir, AuthFileName), []byte(`{"openai-codex": {"type": "oauth", "access": "a", "refresh": "r", "expires": 4102444800000, "accountId": "acct"}}`), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	// A models file that names one built-in model, and nothing else.
	writePooModels(t, dir, `endpoints:
  openai-codex:
    endpoint: https://chatgpt.com/backend-api
    api: responses
    skip_probe: true
    models:
      gpt-6.1-sol:
        context_limit: 111111
`)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	// The model the file named is the file's.
	if got := settings.modelsFileLayer.Models["openai-codex/gpt-6.1-sol"].ContextLimit; got != 111111 {
		t.Errorf("the named model's context limit is %d, want the file's 111111", got)
	}
	// And the built-in models it did not name are still offered.
	if _, ok := settings.modelsFileLayer.Models["openai-codex/gpt-5.6-luna"]; !ok {
		t.Errorf("the built-in provider was dropped by the models file: %d models left", len(settings.modelsFileLayer.Models))
	}
	if _, ok := settings.modelsFileLayer.Endpoints["openai-codex"]; !ok {
		t.Error("the built-in endpoint was dropped by the models file")
	}
}

// TestAModelsFileDeclaresTheOperations covers the ops key: a model the file
// names with its own operations serves them, and a model named without any
// keeps the catalog's own answer.
func TestAModelsFileDeclaresTheOperations(t *testing.T) {
	dir := tempUserConfigDir(t)
	writePooModels(t, dir, `endpoints:
  jev:
    endpoint: http://127.0.0.1:1/v1
    api_key_env: JEVA
    skip_probe: true
    models:
      jev-latest:
        ops: [systemone]
      jev-preview: {}
`)
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	model, err := catalog.New(settings.CatalogOptions()).Resolve("jev/jev-latest")
	if err != nil {
		t.Fatalf("resolving jev/jev-latest: %v", err)
	}
	if !model.HasOp(poo.OpSystemOne) || model.HasOp(poo.OpChat) {
		t.Errorf("the model's ops are %v, want the systemone operation alone", model.Ops)
	}
	if model.Source["ops"] != catalog.LayerModelsFile {
		t.Errorf("the ops came from %q, want the models file", model.Source["ops"])
	}
	preview, err := catalog.New(settings.CatalogOptions()).Resolve("jev/jev-preview")
	if err != nil {
		t.Fatalf("resolving jev/jev-preview: %v", err)
	}
	if !preview.HasOp(poo.OpChat) {
		t.Errorf("the unnamed model's ops are %v, want the catalog's default chat", preview.Ops)
	}
}

// TestCatalogOptionsReadTheCachedLayer covers the settings' answer on the cached layer:
// CatalogOptions asks for it, so a model only the endpoint's last report knows is a model a
// build resolves. A regression that drops Cached from CatalogOptions fails this test — the
// discovered-only model stops resolving.
func TestCatalogOptionsReadTheCachedLayer(t *testing.T) {
	dir := tempUserConfigDir(t)
	state := filepath.Join(dir, "state")
	t.Setenv("XDG_STATE_HOME", state)
	cachePath := filepath.Join(state, "poo", "discovery.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("making the state directory: %v", err)
	}
	cache := `{"version":1,"fetched_at":"2026-11-17T00:00:00Z","endpoints":{"home":` +
		`{"fetched_at":"2026-11-17T00:00:00Z","shape":"vllm","models":{"qwen3":` +
		`{"model_id":"qwen3","context_limit":32768}}}}}`
	if err := os.WriteFile(cachePath, []byte(cache), 0o600); err != nil {
		t.Fatalf("writing the discovery cache: %v", err)
	}

	settings, err := Parse([]byte(`{
		"version":1,
		"endpoints":{"home":{"endpoint":"http://home.invalid:4000/v1"}},
		"roles":{"chat":"home/qwen3"}
	}`), FormatJSON)
	if err != nil {
		t.Fatalf("the settings did not load: %v", err)
	}
	model, err := catalog.New(settings.CatalogOptions()).Resolve("home/qwen3")
	if err != nil {
		t.Fatalf("a model only the cache knows did not resolve: %v", err)
	}
	if model.ContextLimit != 32768 || model.Source["context_limit"] != catalog.LayerDiscovery {
		t.Errorf("the context limit is %d from %q, want the cache's 32768 from discovery",
			model.ContextLimit, model.Source["context_limit"])
	}
}
