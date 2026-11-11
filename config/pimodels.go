package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

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

// piAPI maps the wire API pi names to the API poo speaks. An empty
// mapping means chat completions, poo's default.
var piAPI = map[string]poo.API{
	"openai-completions": poo.APIChatCompletions,
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

	providers := make([]string, 0, len(file.Providers))
	for name := range file.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)
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
			settings.Endpoints[provider] = Endpoint{
				Endpoint: definition.BaseURL,
				APIKey:   definition.APIKey,
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
					Param: "reasoning_effort",
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
