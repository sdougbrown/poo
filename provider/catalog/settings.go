package catalog

import (
	"github.com/sdougbrown/poo"
)

// Settings are the settings the catalog reads.
//
// The settings themselves are read elsewhere; the catalog takes them as
// a struct, so it does not care how they were loaded.
type Settings struct {
	// Endpoints are the endpoints the models are served by, by name.
	Endpoints map[string]EndpointSettings
	// Models are the models the user configured, by name.
	Models map[string]ModelSettings
	// Roles assigns a model to each role, by role name.
	Roles map[string]string
	// Reasoning is the reasoning.chat setting.
	Reasoning string
	// Sampling holds the per-model sampling overrides, by model name.
	Sampling map[string]poo.Sampling
}

// EndpointSettings is one endpoint in the settings.
type EndpointSettings struct {
	// BaseURL is the endpoint's base URL.
	BaseURL string
	// APIKeyEnv names the environment variable holding the key.
	APIKeyEnv string
	// APIKey is a key written in the settings themselves.
	APIKey string
}

// ModelSettings is one model in the settings.
//
// A field the settings leave empty is not set: the catalog keeps what
// the curated and the discovery layers said.
type ModelSettings struct {
	// Endpoint names the endpoint that serves the model.
	Endpoint string
	// ModelID is what the endpoint calls the model.
	ModelID string
	// API is the wire API the model is reached through. Empty means
	// chat completions.
	API poo.API
	// ToolsMode is how the run asks for tool calls. Empty means
	// whatever the model needs.
	ToolsMode poo.ToolsMode
	// Ops are the operations the model serves.
	Ops []poo.Op
	// Inputs are the modalities the model reads.
	Inputs []poo.Modality
	// ContextLimit is the model's context window.
	ContextLimit int
	// MaxOutput is the model's output limit.
	MaxOutput int
	// Reasoning is how the model's reasoning is controlled.
	Reasoning *poo.ReasoningControl
	// Temperature is whether the model accepts a temperature.
	Temperature poo.TempControl
	// PrefixCache reports whether the endpoint caches prompt prefixes.
	PrefixCache *bool
	// PromptCache is how the model's prompt cache is used.
	PromptCache poo.CacheMode
	// PromptVariant selects the prompt sections' variant text.
	PromptVariant string
	// Quirks are the named quirk profiles the transport applies.
	Quirks []string
}

// layer turns the settings' model into the fields of the settings
// layer.
//
// A field the settings leave empty is left out, so the curated and the
// discovery layers keep it.
func (m ModelSettings) layer(name, endpointURL string, sampling poo.Sampling) poo.ModelInfo {
	info := poo.ModelInfo{
		Name:          name,
		Endpoint:      endpointURL,
		ModelID:       m.ModelID,
		API:           m.API,
		Ops:           m.Ops,
		Inputs:        m.Inputs,
		ContextLimit:  m.ContextLimit,
		MaxOutput:     m.MaxOutput,
		PrefixCache:   m.PrefixCache,
		Temperature:   m.Temperature,
		PromptCache:   m.PromptCache,
		PromptVariant: m.PromptVariant,
		Quirks:        m.Quirks,
		Sampling:      sampling,
	}
	if m.ToolsMode != "" {
		info.Tools.Mode = m.ToolsMode
	}
	if m.Reasoning != nil {
		info.Reasoning = *m.Reasoning
	}
	return info
}

// endpoint turns the settings' endpoint into an endpoint.
//
// The credential source is the environment or a static value: the
// settings name the variable that holds the key, or they carry the key
// themselves. An endpoint that names neither needs no key.
func (e EndpointSettings) endpoint(name string) Endpoint {
	endpoint := Endpoint{Name: name, BaseURL: e.BaseURL}
	switch {
	case e.APIKeyEnv != "":
		endpoint.Credential = Credential{Source: CredentialEnv, Env: e.APIKeyEnv}
	case e.APIKey != "":
		endpoint.Credential = Credential{Source: CredentialStatic, Value: e.APIKey}
	default:
		endpoint.Credential = Credential{Source: CredentialNone}
	}
	return endpoint
}
