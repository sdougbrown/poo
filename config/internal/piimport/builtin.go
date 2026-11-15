package piimport

import "github.com/sdougbrown/poo"

// CodexProvider is the provider id pi's built-in Codex provider uses.
//
// It is the name a Codex model is imported under — openai-codex/<id> — and
// the key pi stores the credential under in auth.json.
const CodexProvider = "openai-codex"

// codexModel is one model pi's bundled Codex provider serves.
type codexModel struct {
	// ID is what the endpoint calls the model.
	ID string
	// ContextWindow is the model's context window.
	ContextWindow int
	// NoOff says the model has no level that turns reasoning off.
	NoOff bool
}

// codexModels are the models pi's bundled openai-codex provider serves, with
// the limits and the effort levels that provider gives them.
//
// The list is a snapshot of pi's own catalog. A model pi adds later is still
// selectable: the settings' models, or a models file, may name it under the
// same provider.
var codexModels = []codexModel{
	{ID: "gpt-5.6-luna", ContextWindow: 272000},
	{ID: "gpt-5.6-sol", ContextWindow: 272000},
	{ID: "gpt-5.6-terra", ContextWindow: 272000},
	{ID: "gpt-6-astra", ContextWindow: 272000, NoOff: true},
	{ID: "gpt-6-luna", ContextWindow: 272000},
	{ID: "gpt-6-sol", ContextWindow: 272000},
	{ID: "gpt-6.1-sol", ContextWindow: 500000, NoOff: true},
}

// codexLevels are the reasoning levels a Codex model takes.
//
// pi maps its own minimal level onto the endpoint's low, so poo names the
// levels the endpoint takes, and minimal is not one of them.
var codexLevels = []string{"low", "medium", "high", "xhigh", "max"}

// BuiltinCodex returns pi's built-in Codex provider in the shape a models
// file names providers, so the settings import it through the same path.
//
// endpoint is the base the Codex requests go to. The provider carries no key:
// its credential is pi's OAuth entry in auth.json, which the transport
// resolves, not an api key the settings store.
func BuiltinCodex(endpoint string) File {
	models := make([]Model, 0, len(codexModels))
	for _, model := range codexModels {
		levels := append([]string(nil), codexLevels...)
		if !model.NoOff {
			levels = append([]string{"off"}, levels...)
		}
		models = append(models, Model{
			ID:                      model.ID,
			Inputs:                  []string{string(poo.ModalityText), string(poo.ModalityImage)},
			ContextWindow:           model.ContextWindow,
			MaxTokens:               128000,
			Reasoning:               true,
			SupportsReasoningEffort: true,
			Levels:                  levels,
		})
	}
	return File{Providers: map[string]Provider{
		CodexProvider: {
			BaseURL:   endpoint,
			API:       string(poo.APIResponses),
			ToolsMode: string(poo.ToolsNative),
			Models:    models,
		},
	}}
}
