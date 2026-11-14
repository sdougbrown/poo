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
	"path/filepath"
	"sort"
	"strings"

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
	// APIKeyEnv names the environment variable that holds the key,
	// when the file or the auth beside it names one.
	APIKeyEnv string
	// APIKey is the key itself, when poo can send it.
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
	// Levels are the reasoning levels the model declares, lowest first.
	Levels []string
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
	// ThinkingLevelMap maps a level name to the value the provider takes
	// for it, or to null when the provider does not take that level: the
	// keys are pi's levels, off, minimal, low, medium, high, xhigh and
	// max.
	ThinkingLevelMap map[string]json.RawMessage `json:"thinkingLevelMap"`
	Compat           struct {
		SupportsReasoningEffort bool `json:"supportsReasoningEffort"`
	} `json:"compat"`
}

// modalities are the input modalities pi names that poo reads. A
// modality outside the map is a refusal.
var modalities = map[string]bool{
	string(poo.ModalityText):  true,
	string(poo.ModalityImage): true,
	string(poo.ModalityAudio): true,
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

	// The auth file beside the models file is read once, on the first
	// provider the models file leaves no usable key for: a file that is
	// not there reads as no keys, and the flag — not a nil map — keeps
	// it from being read again for every keyless provider after it.
	authRead := false
	var keys map[string]credential
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
		env, key := keyFromValue(definition.APIKey)
		if env == "" && key == "" {
			if !authRead {
				keys, err = readAuth(filepath.Join(filepath.Dir(path), authFile))
				if err != nil {
					return File{}, err
				}
				authRead = true
			}
			authEnv, authKey := storedKey(keys[name])
			if authEnv != "" {
				env = authEnv
			}
			key = authKey
		}
		provider := Provider{
			BaseURL:   definition.BaseURL,
			API:       api,
			APIKeyEnv: env,
			APIKey:    key,
		}
		for _, model := range definition.Models {
			if model.ID == "" {
				return File{}, fmt.Errorf("the provider %s names a model with no id", name)
			}
			inputs, err := inputs(name, model.ID, model.Input)
			if err != nil {
				return File{}, err
			}
			entry := Model{
				ID:                      model.ID,
				Inputs:                  inputs,
				ContextWindow:           model.ContextWindow,
				MaxTokens:               model.MaxTokens,
				Reasoning:               model.Reasoning,
				SupportsReasoningEffort: model.Compat.SupportsReasoningEffort,
			}
			if entry.Reasoning && entry.SupportsReasoningEffort {
				entry.Levels = thinkingLevels(model.ThinkingLevelMap)
			}
			provider.Models = append(provider.Models, entry)
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

// credential is one provider's entry in pi's auth.json.
type credential struct {
	// Type is how the credential authenticates.
	Type string `json:"type"`
	// Key is the api key, for the api_key type.
	Key string `json:"key"`
}

// authFile is the name pi's credentials file goes by, beside the
// models file.
const authFile = "auth.json"

// readAuth reads pi's auth.json: credentials by provider name. A file
// that is not there is a file without credentials, not a failure.
func readAuth(path string) (map[string]credential, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var credentials map[string]credential
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return credentials, nil
}

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

// storedKey reads one auth-file credential as the settings hold keys.
// Only the api_key credential carries a key poo can use; an OAuth token
// imports without one.
func storedKey(entry credential) (string, string) {
	if entry.Type != "api_key" {
		return "", ""
	}
	return keyFromValue(entry.Key)
}

// reasoningLevels is pi's canonical level order, lowest first: the
// order an imported model's levels take, so they climb the way pi's own
// cycle does.
var reasoningLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// effortLevels are the levels a reasoning model is imported with when
// pi's file says nothing more about them.
//
// The set is pi's own effort default, not poo's wider named vocabulary:
// a model that declares no map has told poo nothing about minimal or
// xhigh, and claiming them would send a level the provider never
// accepted. A map is how a model declares more.
var effortLevels = []string{"off", "low", "medium", "high"}

// thinkingLevels turns a model's thinkingLevelMap into the levels it
// accepts.
//
// A level the map gives no value is a level the provider does not take,
// so only the keys with a value are levels. A model with no map is the
// effort default, and a map that leaves no level takes none.
func thinkingLevels(levels map[string]json.RawMessage) []string {
	if len(levels) == 0 {
		return append([]string(nil), effortLevels...)
	}
	out := make([]string, 0, len(levels))
	for _, level := range reasoningLevels {
		value, ok := levels[level]
		if !ok || isNull(value) {
			continue
		}
		out = append(out, level)
	}
	return out
}

// isNull reports that a models file's value is null: the level the key
// names is one the provider does not take.
func isNull(value json.RawMessage) bool {
	return len(value) == 0 || string(value) == "null"
}
