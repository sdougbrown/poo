// Package interactions holds the Interactions API's wire half.
//
// This package holds the request shape and its encoder, the conversion of the
// core's messages into the API's steps, the fork's body rewrite, and the
// answer's SSE reader.
//
// Nothing here resolves a credential, chooses a URL or sets a header, and
// nothing here decides what a request carries beyond what it is handed. That
// is the dividing line, the one the chat, responses and anthropic packages
// keep: a transport composes these pieces and alone knows where a request goes
// or who pays for it.
package interactions

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/transport"
)

// Request is the wire shape of an interaction request.
//
// The request is stateless: every request carries the whole history as steps,
// and Store is always false, so the endpoint keeps nothing of it.
type Request struct {
	Model             string `json:"model"`
	Input             []Step `json:"input"`
	SystemInstruction string `json:"system_instruction,omitempty"`
	Tools             []Tool `json:"tools,omitempty"`
	Store             bool   `json:"store"`
	Stream            bool   `json:"stream"`
	// Params holds the parameters whose names the endpoint chooses: the
	// generation config's fields and whatever else a model asks for. They are
	// written after the fields above, in name order, so the same request
	// always has the same body.
	Params map[string]json.RawMessage `json:"-"`
}

// MarshalJSON writes the request, with the named parameters merged into the
// top level.
//
// The merge is the support package's, the one every wire half shares: the
// parameters are written last and in name order, so the body of two requests
// that ask for the same thing is the same bytes.
func (r Request) MarshalJSON() ([]byte, error) {
	type plain Request
	body, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	return transport.MergeParams(body, r.Params)
}

// Encode serializes a request.
func Encode(req Request) ([]byte, error) {
	return json.Marshal(req)
}

// The step types the interaction's input carries.
const (
	// StepUserInput is what the user said.
	StepUserInput = "user_input"
	// StepModelOutput is what the model said.
	StepModelOutput = "model_output"
	// StepThought is thinking the model did, with the signature that lets
	// the model read it back.
	StepThought = "thought"
	// StepFunctionCall is a request for the client to run a tool.
	StepFunctionCall = "function_call"
	// StepFunctionResult is the result of a tool the client ran.
	StepFunctionResult = "function_result"
)

// SkipThoughtSignature is the signature Google documents for replaying a
// function call the model did not sign: the API refuses a call of the current
// turn whose thought step it cannot check, and this value skips the check.
//
// A call poo holds without a signature is one whose thought came from another
// API, where this one has nothing to check.
const SkipThoughtSignature = "skip_thought_signature_validator"

// Step is one step of an interaction's input.
//
// The API's steps are one of a few kinds, each with its own fields; the kinds
// are told apart by Type, and the fields a kind does not carry stay empty.
type Step struct {
	Type string `json:"type"`
	// Content is what a user input or a model output said.
	Content []Content `json:"content,omitempty"`
	// Signature is the signature a thought step carries, and Summary is the
	// thought itself.
	Signature string    `json:"signature,omitempty"`
	Summary   []Content `json:"summary,omitempty"`
	// ID, Name and Arguments carry a function call.
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// CallID names the call a function result answers, and Result holds the
	// result itself: the text of the tools that answer in words, or the
	// parts of the one that answers with an attachment.
	CallID  string `json:"call_id,omitempty"`
	Result  any    `json:"result,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

// Content is one part of a step's content.
type Content struct {
	Type string `json:"type"`
	// Text is the text of a text part.
	Text string `json:"text,omitempty"`
	// MediaType and Data are the attachment of an image or an audio part,
	// which the API takes inline and base64-encoded.
	MediaType string `json:"mime_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// Tool is one tool the request offers.
type Tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Tools turns the core's tool specs into the wire shape, in the stable order
// the core sorts them into.
//
// A spec with no schema is a tool that opted out of one, and the API's function
// declaration takes the empty object for a function that takes no arguments.
func Tools(specs []poo.ToolSpec) []Tool {
	var out []Tool
	for _, spec := range specs {
		schema := spec.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, Tool{
			Type:        "function",
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  schema,
		})
	}
	return out
}
