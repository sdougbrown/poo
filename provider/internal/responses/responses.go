// Package responses holds the Responses API's wire half.
//
// This package holds request shapes, history conversion, event streams, and
// compaction items. A transport composes them and chooses the endpoint,
// authentication, headers, and request policy.
//
// Nothing here resolves a credential, chooses a URL or sets a header. That is
// the dividing line: this package never says where a request goes or who pays
// for it.
package responses

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
)

// Request is the wire shape of a Responses API request.
type Request struct {
	Model             string            `json:"model"`
	Instructions      string            `json:"instructions"`
	Input             []json.RawMessage `json:"input"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolChoice        string            `json:"tool_choice,omitempty"`
	ParallelToolCalls bool              `json:"parallel_tool_calls"`
	Reasoning         *Reasoning        `json:"reasoning,omitempty"`
	Text              *Text             `json:"text,omitempty"`
	Include           []string          `json:"include,omitempty"`
	PromptCacheKey    string            `json:"prompt_cache_key,omitempty"`
	Store             bool              `json:"store"`
	Stream            bool              `json:"stream"`
}

// Reasoning is the request's reasoning control.
type Reasoning struct {
	// Effort is the reasoning effort, or "none".
	Effort string `json:"effort"`
	// Summary is the summary the endpoint returns with the reasoning.
	Summary string `json:"summary,omitempty"`
}

// Text is the request's answer shape.
type Text struct {
	// Verbosity is how much the answer says.
	Verbosity string `json:"verbosity"`
}

// Tool is one tool the request offers.
type Tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

// Encode serializes a request's wire fields.
func Encode(req Request) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, encodeError(err.Error())
	}
	return body, nil
}

// encodeError is a request that cannot be serialized.
func encodeError(what string) error {
	return &poo.ProviderError{
		Kind:    poo.ProviderKindInvalidRequest,
		Message: "the request does not encode: " + what,
	}
}
