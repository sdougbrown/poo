package catalog

import (
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
)

// curated is the curated layer: the family rules and the hosted entries
// that are generated from the models.dev data.
type curated struct {
	// models holds the hosted entries, by model id.
	models map[string]curatedModel
	// endpoints holds the hosted providers, by provider id.
	endpoints map[string]Endpoint
}

// curatedModel is one curated model: the fields the curated layer knows,
// the endpoint that serves it and the notes about its family.
type curatedModel struct {
	// endpoint is the endpoint's name: the provider's id.
	endpoint string
	// info holds the fields the curated layer sets.
	info poo.ModelInfo
	// notes are the notes of the families the model belongs to.
	notes []string
}

// newCurated returns the curated layer.
func newCurated() *curated {
	c := &curated{
		models:    map[string]curatedModel{},
		endpoints: map[string]Endpoint{},
	}
	for _, hosted := range hostedModels {
		model, endpoint := hostedLayer(hosted)
		c.models[hosted.ModelID] = curatedModel{
			endpoint: hosted.Provider,
			info:     model,
		}
		if endpoint.Name != "" {
			c.endpoints[hosted.Provider] = endpoint
		}
	}
	return c
}

// modelIDs returns the ids of the curated models, sorted.
func (c *curated) modelIDs() []string {
	out := make([]string, 0, len(c.models))
	for id := range c.models {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// forModel returns the curated fields, the endpoint's name and the notes
// for a model id.
//
// The family rules come first and the hosted entry wins: a family rule
// describes a family of models and a hosted entry describes one model.
// A model id that no rule and no entry knows has no curated fields.
func (c *curated) forModel(modelID string) (poo.ModelInfo, string, []string) {
	info := poo.ModelInfo{}
	var notes []string
	for _, family := range families {
		if !family.match(modelID) {
			continue
		}
		family.describe(&info)
		notes = append(notes, family.note)
	}
	endpoint := ""
	if hosted, ok := c.models[modelID]; ok {
		applyLayer(&info, map[string]string{}, LayerCurated, hosted.info)
		endpoint = hosted.endpoint
		notes = append(notes, hosted.notes...)
	}
	return info, endpoint, notes
}

// hostedLayer turns a generated hosted entry into the curated fields and
// the endpoint that serves it.
//
// The entry's provider says how the model is reached: an OpenAI-
// compatible provider takes an effort level for its reasoning, an
// Anthropic provider takes a token budget, and any other provider
// leaves the reasoning alone.
func hostedLayer(hosted hostedEntry) (poo.ModelInfo, Endpoint) {
	info := poo.ModelInfo{
		ModelID:      hosted.ModelID,
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		Inputs:       modalities(hosted.Inputs),
		ContextLimit: hosted.ContextLimit,
		MaxOutput:    hosted.MaxOutput,
		Temperature:  poo.TempFree,
		Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
	}
	if !hosted.Temperature {
		info.Temperature = poo.TempFixed
	}
	if !hosted.ToolCall {
		info.Tools = poo.ToolCalling{Mode: poo.ToolsNone}
	}
	if hosted.Reasoning {
		info.Reasoning = reasoningControl(hosted.Provider)
	}
	if mode := promptCacheMode(hosted.Provider); mode != "" {
		info.PromptCache = mode
	}
	endpoint := Endpoint{
		Name:     hosted.Provider,
		Endpoint: hosted.Endpoint,
		Credential: Credential{
			Source: CredentialEnv,
			Env:    firstOf(hosted.Env),
		},
	}
	if endpoint.Credential.Env == "" {
		endpoint.Credential.Source = CredentialNone
	}
	return info, endpoint
}

// reasoningControl returns the reasoning control a provider's models
// take through Chat Completions.
//
// Anthropic's thinking budget belongs to its own transport, which is
// Phase B: the Chat Completions transport cannot spell it.
func reasoningControl(provider string) poo.ReasoningControl {
	if provider == "openai" {
		return poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: poo.ReasoningPathEffort}
	}
	return poo.ReasoningControl{Mode: poo.ReasoningNone}
}

// promptCacheMode returns how a provider's prompt cache is used.
//
// An explicit cache belongs to the Anthropic transport, which is Phase
// B: Chat Completions has no breakpoint to place.
func promptCacheMode(provider string) poo.CacheMode {
	if provider == "openai" {
		return poo.CacheAuto
	}
	return ""
}

// modalities maps the modality names of a hosted entry.
func modalities(names []string) []poo.Modality {
	var out []poo.Modality
	for _, name := range names {
		switch strings.ToLower(name) {
		case "text":
			out = append(out, poo.ModalityText)
		case "image":
			out = append(out, poo.ModalityImage)
		case "audio":
			out = append(out, poo.ModalityAudio)
		}
	}
	return out
}

// firstOf returns the first name, or the empty string.
func firstOf(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
