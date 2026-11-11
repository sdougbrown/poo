package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
)

// piModelsFile is a models file written for pi: providers, each with
// the models its endpoint serves.
//
// Only the fields poo maps are here; a field pi carries that poo does
// not is ignored, the way the wire's forward-compatibility rule reads
// a later server's events.
type piModelsFile struct {
	Providers map[string]piProvider `json:"providers"`
}

// piProvider is one provider in pi's models file.
type piProvider struct {
	// BaseURL is the endpoint the provider serves.
	BaseURL string `json:"baseUrl"`
	// API is the wire API the provider speaks.
	API string `json:"api"`
	// APIKey is the provider's key, written in the file.
	APIKey string `json:"apiKey"`
	// Models are the models the provider serves.
	Models []piModel `json:"models"`
}

// piModel is one model in pi's models file.
type piModel struct {
	// ID is what the endpoint calls the model.
	ID string `json:"id"`
	// Input are the modalities the model reads.
	Input []string `json:"input"`
	// ContextWindow is the model's context window.
	ContextWindow int `json:"contextWindow"`
	// MaxTokens is the most tokens the model writes in one response.
	MaxTokens int `json:"maxTokens"`
	// Reasoning says the model reasons.
	Reasoning bool `json:"reasoning"`
	// ThinkingLevelMap maps a level name to the value the provider takes
	// for it, or to null when the provider does not take that level: the
	// keys are pi's levels, off, minimal, low, medium, high, xhigh and
	// max.
	ThinkingLevelMap map[string]json.RawMessage `json:"thinkingLevelMap"`
	// Compat carries the provider's compatibility notes.
	Compat struct {
		SupportsReasoningEffort bool `json:"supportsReasoningEffort"`
	} `json:"compat"`
}

// piCredential is one provider's credential in an auth.json shaped like
// pi's.
type piCredential struct {
	// Type is how the credential authenticates: a key pi stores, or
	// an OAuth token poo cannot use.
	Type string `json:"type"`
	// Key is the credential's key, for an api credential.
	Key string `json:"key"`
}

// piAuthFile is the name an auth.json shaped like pi's goes by, beside
// the models file.
const piAuthFile = "auth.json"

// piAPI maps the wire API names pi uses beyond the default to the API
// poo speaks. The default — pi's "openai-completions" — needs no
// mapping: an empty API says chat completions.
var piAPI = map[string]poo.API{
	"openai-responses":   poo.APIResponses,
	"anthropic-messages": poo.APIAnthropicMessages,
}

// piModalities are the input modalities pi names that poo reads. A
// modality outside the map is a refusal.
var piModalities = map[string]bool{
	"text":  true,
	"image": true,
}

// piReasoningLevels is pi's canonical level order, lowest first.
//
// It is the order an imported model's levels take, so the levels climb the
// way pi's own cycle does.
var piReasoningLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// piEffortLevels are the levels a reasoning model is imported with when
// pi's file says nothing more about them.
var piEffortLevels = []string{"off", "low", "medium", "high"}

// piThinkingLevels turns a model's thinkingLevelMap into the levels it
// accepts.
//
// A level the map gives no value is a level the provider does not take, so
// only the keys with a value are levels. A model with no map is the effort
// default, and a map that leaves no level takes none.
func piThinkingLevels(levels map[string]json.RawMessage) []string {
	if len(levels) == 0 {
		return append([]string(nil), piEffortLevels...)
	}
	out := make([]string, 0, len(levels))
	for _, level := range piReasoningLevels {
		value, ok := levels[level]
		if !ok || isJSONNull(value) {
			continue
		}
		out = append(out, level)
	}
	return out
}

// isJSONNull reports that a models file's value is null: the level the key
// names is one the provider does not take.
func isJSONNull(value json.RawMessage) bool {
	return len(value) == 0 || string(value) == "null"
}

// importPiModels reads the settings' pi models file and fills the
// endpoints and models the settings do not already hold.
//
// A model is imported under the name its provider and id make:
// provider/id, the name pi itself answers to. The settings' own
// entries win on a name collision, so a models file is an addition,
// never a takeover. A provider whose api poo does not speak, or a
// model whose inputs poo does not read, is an error: a file that
// half-imports is worse than one that refuses.
//
// An imported provider whose apiKey the models file leaves empty takes
// the key pi keeps for it in the auth.json beside the models file: a
// models file written for pi names the provider, and pi keeps the
// key out of the file. A file that is not there holds no keys, since
// an operator may keep them elsewhere; a file that does not parse is
// an error, because the operator pointed poo at a directory whose
// credentials poo cannot read.
func importPiModels(settings *Settings) error {
	path := settings.PIModelsFile
	if path == "" {
		return nil
	}
	expanded, err := expandHome(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return fmt.Errorf("config: reading %s: %w", expanded, err)
	}
	var file piModelsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("config: %s: %w", expanded, err)
	}
	if len(file.Providers) == 0 {
		return fmt.Errorf("config: %s holds no providers", expanded)
	}

	providers := make([]string, 0, len(file.Providers))
	for name := range file.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)

	// The auth file beside the models file is read once — on the first
	// provider whose key the models file leaves empty and that the
	// settings do not already hold, in the same pass that fills the
	// key, so the rule for when a key is needed lives in one place.
	// The flag, not a nil map, is the once-sentinel: a file that is
	// not there reads as no keys, and a sentinel on the map would
	// re-read the file for every keyless provider after it.
	authRead := false
	var keys map[string]piCredential
	for _, provider := range providers {
		definition := file.Providers[provider]
		api, err := piWireAPI(provider, definition.API)
		if err != nil {
			return err
		}
		if _, ok := settings.Endpoints[provider]; !ok {
			if settings.Endpoints == nil {
				settings.Endpoints = map[string]Endpoint{}
			}
			env, key := keyFromValue(definition.APIKey)
			if env == "" && key == "" {
				if !authRead {
					keys, err = readPiAuth(filepath.Join(filepath.Dir(expanded), piAuthFile))
					if err != nil {
						return err
					}
					authRead = true
				}
				authEnv, authKey := piStoredKey(keys[provider])
				if authEnv != "" {
					env = authEnv
				}
				key = authKey
			}
			settings.Endpoints[provider] = Endpoint{
				Endpoint:  definition.BaseURL,
				APIKeyEnv: env,
				APIKey:    key,
			}
		}
		for _, model := range definition.Models {
			name := provider + "/" + model.ID
			if _, ok := settings.Models[name]; ok {
				continue
			}
			entry := Model{
				Endpoint:     provider,
				ModelID:      model.ID,
				API:          api,
				ContextLimit: model.ContextWindow,
				MaxOutput:    model.MaxTokens,
			}
			inputs, err := piInputs(provider, model.ID, model.Input)
			if err != nil {
				return err
			}
			entry.Inputs = inputs
			if model.Reasoning && model.Compat.SupportsReasoningEffort {
				entry.Reasoning = &poo.ReasoningControl{
					Mode:  poo.ReasoningEffort,
					Param: poo.ReasoningPathEffort,
				}
				entry.ReasoningLevels = piThinkingLevels(model.ThinkingLevelMap)
			}
			if settings.Models == nil {
				settings.Models = map[string]Model{}
			}
			settings.Models[name] = entry
		}
	}
	return nil
}

// readPiAuth reads pi's auth.json: credentials by provider name. A file
// that is not there is a file without credentials, not a failure.
func readPiAuth(path string) (map[string]piCredential, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}
	var credentials map[string]piCredential
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return credentials, nil
}

// piStoredKey returns the key a credential holds, when poo can use it.
//
// pi stores a key under the api_key type. An OAuth credential
// authenticates in ways poo does not speak: it imports as no key at
// all.
// keyFromValue reads one key value as the settings hold keys: an
// environment name and a literal, either empty. A key pi would run a
// command for is a key poo does not run, and a $NAME names the
// environment variable to read at run time.
func keyFromValue(value string) (string, string) {
	switch {
	case value == "", strings.HasPrefix(value, "!"):
		return "", ""
	case strings.HasPrefix(value, "$"):
		return value[1:], ""
	}
	return "", value
}

// piStoredKey reads one auth-file credential as the settings hold
// keys. Only the api_key credential carries a key poo can use; an
// OAuth token imports without one.
func piStoredKey(credential piCredential) (string, string) {
	if credential.Type != "api_key" {
		return "", ""
	}
	return keyFromValue(credential.Key)
}

// piWireAPI maps a provider's api name. A provider that names an API
// poo does not speak is refused: a model that imports halfway cannot
// run.
func piWireAPI(provider, api string) (string, error) {
	if api == "" || api == "openai-completions" {
		return "", nil
	}
	if mapped, ok := piAPI[api]; ok {
		return string(mapped), nil
	}
	return "", fmt.Errorf("config: the provider %s speaks %q, which poo does not know", provider, api)
}

// piInputs checks a model's input modalities. A modality poo does not
// read is refused, for the same reason an unknown API is.
func piInputs(provider, id string, inputs []string) ([]string, error) {
	for _, input := range inputs {
		if !piModalities[input] {
			return nil, fmt.Errorf("config: the model %s/%s reads %q, which poo does not know", provider, id, input)
		}
	}
	return inputs, nil
}
