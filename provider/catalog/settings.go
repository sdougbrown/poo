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
}

// FileSettings are the endpoints and models a models file declares.
//
// They are the settings' own words, so a field the file sets is overridden
// field by field by the settings, and a field only one of them sets keeps the
// value that one gave it.
type FileSettings struct {
	// Endpoints are the endpoints the file declares, by name.
	Endpoints map[string]EndpointSettings
	// Models are the models the file declares, by the name the catalog holds
	// them under.
	Models map[string]ModelSettings
}

// EndpointSettings is one endpoint in the settings.
type EndpointSettings struct {
	// Endpoint is the endpoint's base URL.
	Endpoint string
	// APIKeyEnv names the environment variable holding the key.
	APIKeyEnv string
	// APIKey is a key written in the settings themselves.
	APIKey string
	// SessionHeader is the name of a header that carries the session's id,
	// for an endpoint whose gateway routes one conversation's requests
	// together. Empty means the endpoint asks for none.
	SessionHeader string
	// SkipProbe says the endpoint is not asked what it serves: its models are
	// declared by the settings or the curated layer. A request to the endpoint
	// would send nothing it understands.
	SkipProbe bool
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
	// ReasoningLevels are the reasoning levels the model accepts, lowest
	// first. A model that names none is set by its reasoning control
	// alone.
	ReasoningLevels []string
	// Temperature is whether the model accepts a temperature.
	Temperature poo.TempMode
	// PrefixCache reports whether the endpoint caches prompt prefixes.
	PrefixCache *bool
	// PromptCache is how the model's prompt cache is used.
	PromptCache poo.CacheMode
	// PromptVariant selects the prompt sections' variant text.
	PromptVariant string
	// Quirks are the named quirk profiles the transport applies.
	Quirks []string
	// Sampling are the model's own sampling defaults. A model that names none
	// takes whatever the request asks for.
	Sampling poo.Sampling
}

// layer turns a model into the fields of one layer.
//
// A field the model leaves empty is left out, so the earlier layers keep it.
func (m ModelSettings) layer(name, endpointURL string) poo.ModelInfo {
	info := poo.ModelInfo{
		Name:            name,
		Endpoint:        endpointURL,
		ModelID:         m.ModelID,
		API:             m.API,
		Ops:             m.Ops,
		Inputs:          m.Inputs,
		ContextLimit:    m.ContextLimit,
		MaxOutput:       m.MaxOutput,
		PrefixCache:     m.PrefixCache,
		Temperature:     m.Temperature,
		PromptCache:     m.PromptCache,
		PromptVariant:   m.PromptVariant,
		Quirks:          m.Quirks,
		Sampling:        m.Sampling,
		ReasoningLevels: m.ReasoningLevels,
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
// The credential reads the variable the settings name, and falls back
// to the key they carry when that variable holds none — the one rule
// the transport resolves by too. An endpoint that names neither needs
// no key.
func (e EndpointSettings) endpoint(name string) Endpoint {
	endpoint := Endpoint{Name: name, Endpoint: e.Endpoint, SessionHeader: e.SessionHeader, SkipProbe: e.SkipProbe}
	switch {
	case e.APIKeyEnv != "":
		endpoint.Credential = Credential{Source: CredentialEnv, Env: e.APIKeyEnv}
	case e.APIKey != "":
		endpoint.Credential = Credential{Source: CredentialStatic}
	default:
		endpoint.Credential = Credential{Source: CredentialNone}
	}
	return endpoint
}
