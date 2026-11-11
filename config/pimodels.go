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

	// The auth file beside the models file is read once, on the first
	// provider whose key the models file leaves empty and that the
	// settings do not already hold — the same pass that fills the key,
	// so the rule for when a key is needed lives in one place.
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
			key := definition.APIKey
			if key == "" {
				if keys == nil {
					keys, err = readPiAuth(filepath.Join(filepath.Dir(expanded), piAuthFile))
					if err != nil {
						return err
					}
				}
				key = piStoredKey(keys[provider])
			}
			settings.Endpoints[provider] = Endpoint{
				Endpoint: definition.BaseURL,
				APIKey:   key,
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
// pi stores a key under the api_key type, and a file that says api
// says the same thing. An OAuth credential authenticates in ways poo
// does not speak: it imports as no key at all.
func piStoredKey(credential piCredential) string {
	if credential.Type != "api_key" && credential.Type != "api" {
		return ""
	}
	// A key written as !command is pi's to run: poo does not run a
	// command to get a key, so the provider imports without one.
	if strings.HasPrefix(credential.Key, "!") {
		return ""
	}
	return credential.Key
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
