// Package piimport reads a models file written for pi into poo's
// vocabulary.
//
// The file is pi's: its field names, its api names and its modality
// names are translated here, once, so the settings package holds only
// poo's words. What poo cannot speak is refused rather than imported
// halfway — a file that half-imports is worse than one that refuses.
package piimport

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/sdougbrown/poo"
)

// File is a models file read into poo's vocabulary: the providers it
// names, in name order, each with the models its endpoint serves.
type File struct {
	// Providers are the file's providers, by the name the file gives
	// them.
	Providers map[string]Provider
}

// Provider is one provider a models file names.
type Provider struct {
	// BaseURL is the endpoint the provider serves.
	BaseURL string
	// API is the wire API the provider speaks, as poo names it: empty
	// is the default.
	API string
	// APIKey is the key the file itself carries, when it carries one
	// poo can use.
	APIKey string
	// Models are the models the provider serves.
	Models []Model
}

// Model is one model a models file names.
type Model struct {
	// ID is what the endpoint calls the model.
	ID string
	// Inputs are the modalities the model reads.
	Inputs []string
	// ContextWindow is the model's context window.
	ContextWindow int
	// MaxTokens is the most tokens the model writes in one response.
	MaxTokens int
	// Reasoning says the model reasons.
	Reasoning bool
	// SupportsReasoningEffort says the provider spells reasoning as an
	// effort level.
	SupportsReasoningEffort bool
}

// piFile is a models file written for pi: providers, each with the
// models its endpoint serves.
//
// Only the fields poo maps are here; a field pi carries that poo does
// not is ignored, the way the wire's forward-compatibility rule reads
// a later server's events.
type piFile struct {
	Providers map[string]piProvider `json:"providers"`
}

// piProvider is one provider in pi's models file.
type piProvider struct {
	BaseURL string    `json:"baseUrl"`
	API     string    `json:"api"`
	APIKey  string    `json:"apiKey"`
	Models  []piModel `json:"models"`
}

// piModel is one model in pi's models file.
type piModel struct {
	ID            string   `json:"id"`
	Input         []string `json:"input"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
	Reasoning     bool     `json:"reasoning"`
	Compat        struct {
		SupportsReasoningEffort bool `json:"supportsReasoningEffort"`
	} `json:"compat"`
}

// modalities are the input modalities pi names that poo reads. A
// modality outside the map is a refusal.
var modalities = map[string]bool{
	string(poo.ModalityText):  true,
	string(poo.ModalityImage): true,
}

// Read reads the models file at path.
func Read(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var file piFile
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if len(file.Providers) == 0 {
		return File{}, fmt.Errorf("%s holds no providers", path)
	}
	names := make([]string, 0, len(file.Providers))
	for name := range file.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := File{Providers: make(map[string]Provider, len(names))}
	for _, name := range names {
		definition := file.Providers[name]
		// An endpoint with no URL, or a model with no id, imports into
		// something that cannot run: it fails later, when its transport
		// is built, with nothing left to say where it came from.
		if definition.BaseURL == "" {
			return File{}, fmt.Errorf("the provider %s names no baseUrl", name)
		}
		if len(definition.Models) == 0 {
			return File{}, fmt.Errorf("the provider %s serves no models", name)
		}
		api, err := wireAPI(name, definition.API)
		if err != nil {
			return File{}, err
		}
		provider := Provider{
			BaseURL: definition.BaseURL,
			API:     api,
			APIKey:  definition.APIKey,
		}
		for _, model := range definition.Models {
			if model.ID == "" {
				return File{}, fmt.Errorf("the provider %s names a model with no id", name)
			}
			inputs, err := inputs(name, model.ID, model.Input)
			if err != nil {
				return File{}, err
			}
			provider.Models = append(provider.Models, Model{
				ID:                      model.ID,
				Inputs:                  inputs,
				ContextWindow:           model.ContextWindow,
				MaxTokens:               model.MaxTokens,
				Reasoning:               model.Reasoning,
				SupportsReasoningEffort: model.Compat.SupportsReasoningEffort,
			})
		}
		out.Providers[name] = provider
	}
	return out, nil
}

// wireAPI maps a provider's api name onto the one poo speaks.
func wireAPI(provider, api string) (string, error) {
	if api == "" || api == "openai-completions" {
		return "", nil
	}
	return "", fmt.Errorf("the provider %s speaks %q, which poo does not know", provider, api)
}

// inputs checks a model's input modalities.
func inputs(provider, id string, names []string) ([]string, error) {
	for _, name := range names {
		if !modalities[name] {
			return nil, fmt.Errorf("the model %s/%s reads %q, which poo does not know", provider, id, name)
		}
	}
	return names, nil
}
