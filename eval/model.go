package eval

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Model is one entry of the model catalog.
//
// The catalog is the list of models a baseline is measured against. It
// lives in evals/models.yaml.
type Model struct {
	// Name is the catalog's name for the model, e.g. "gemma4". It is the
	// key a report and a comparison use.
	Name string `yaml:"name" json:"name"`
	// BaseURL is the endpoint's base URL, e.g. "http://localhost:4000/v1".
	BaseURL string `yaml:"base_url" json:"base_url"`
	// ModelID is the identifier the endpoint knows the model by.
	ModelID string `yaml:"model_id" json:"model_id"`
	// APIKeyEnv names the environment variable holding the API key. The
	// runner never reads the value of a model's key: it passes the name
	// to the poo subprocess, which reads it and scrubs it from the
	// tools' child environment. (The judge reads its own key; see
	// JudgeOptions.)
	APIKeyEnv string `yaml:"api_key_env,omitempty" json:"api_key_env,omitempty"`
	// ContextLimit is the model's context window, in tokens.
	ContextLimit int `yaml:"context_limit" json:"context_limit"`
	// ToolsMode is how the run asks for tool calls: "native", "text" or
	// "auto". Empty means "native".
	ToolsMode string `yaml:"tools_mode,omitempty" json:"tools_mode,omitempty"`
	// ReasoningEffort is passed to poo as --reasoning-effort when set,
	// for a model that thinks only when asked to.
	ReasoningEffort string `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"`
	// Notes is a free-text note about the entry.
	Notes string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// DefaultToolsMode is the tools mode a model gets when the catalog does
// not name one.
//
// Native is the default: an endpoint that speaks the tool-call format
// properly does not need the text parser, and the text parser costs a
// little accuracy when it fires on prose that is not a call.
const DefaultToolsMode = "native"

// EffectiveToolsMode returns the model's tools mode, with the default
// filled in.
func (m Model) EffectiveToolsMode() string {
	if m.ToolsMode == "" {
		return DefaultToolsMode
	}
	return m.ToolsMode
}

// Endpoint is the key that decides which runs may run at the same time.
//
// Two models on the same endpoint share its capacity, so the runner
// limits the concurrency per endpoint.
func (m Model) Endpoint() string {
	return strings.TrimRight(m.BaseURL, "/")
}

// LoadModels reads a model catalog.
//
// The catalog is a YAML list of model entries. A field the harness does
// not know is an error: a typo in a context limit should not silently
// become a default.
func LoadModels(path string) ([]Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: reading %s: %w", path, err)
	}

	var models []Model
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&models); err != nil {
		return nil, fmt.Errorf("eval: parsing %s: %w", path, err)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("eval: %s lists no models", path)
	}

	seen := map[string]bool{}
	for i, m := range models {
		if err := m.validate(); err != nil {
			return nil, fmt.Errorf("eval: %s: model %d: %w", path, i, err)
		}
		if seen[m.Name] {
			return nil, fmt.Errorf("eval: %s: two models are named %q", path, m.Name)
		}
		seen[m.Name] = true
	}
	return models, nil
}

// validate checks one catalog entry.
func (m Model) validate() error {
	switch {
	case strings.TrimSpace(m.Name) == "":
		return fmt.Errorf("name must not be empty")
	case strings.TrimSpace(m.BaseURL) == "":
		return fmt.Errorf("base_url must not be empty")
	case strings.TrimSpace(m.ModelID) == "":
		return fmt.Errorf("model_id must not be empty")
	case m.ContextLimit <= 0:
		return fmt.Errorf("context_limit must be positive")
	}
	switch m.EffectiveToolsMode() {
	case "native", "text", "auto":
	default:
		return fmt.Errorf("tools_mode must be native, text or auto, not %q", m.ToolsMode)
	}
	return nil
}

// SelectModels keeps the models named by names.
//
// An empty names list keeps every model, so a run with no filter is the
// whole catalog.
func SelectModels(models []Model, names []string) []Model {
	if len(names) == 0 {
		return models
	}
	want := map[string]bool{}
	for _, n := range names {
		want[strings.TrimSpace(n)] = true
	}
	var out []Model
	for _, m := range models {
		if want[m.Name] {
			out = append(out, m)
		}
	}
	return out
}
