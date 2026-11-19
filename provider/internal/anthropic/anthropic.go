// Package anthropic holds the Messages API's wire half.
//
// This package holds the request shape and its encoder, the conversion of the
// core's messages into the wire's messages, the fork's body rewrite, and the
// answer's SSE reader.
//
// Nothing here resolves a credential, chooses a URL or sets a header, and
// nothing here decides what a request carries beyond what it is handed. That
// is the dividing line, the one the chat and responses packages keep: a
// transport composes these pieces and alone knows where a request goes or
// who pays for it.
package anthropic

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/sdougbrown/poo"
)

// Request is the wire shape of a Messages request.
type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
	// System is the system prompt, carried above the messages the API
	// alternates.
	System string `json:"system,omitempty"`
	// Params holds the parameters whose names the endpoint chooses: the
	// reasoning parameters, the sampling parameters and whatever else a
	// model asks for. They are written after the fields above, in name
	// order, so the same request always has the same body.
	Params map[string]json.RawMessage `json:"-"`
}

// MarshalJSON writes the request, with the named parameters merged into the
// top level.
//
// The parameters are written last and in name order, so the body of two
// requests that ask for the same thing is the same bytes.
func (r Request) MarshalJSON() ([]byte, error) {
	type plain Request
	body, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	if len(r.Params) == 0 {
		return body, nil
	}
	names := make([]string, 0, len(r.Params))
	for name := range r.Params {
		names = append(names, name)
	}
	sort.Strings(names)

	var b bytes.Buffer
	b.Write(body[:len(body)-1])
	for _, name := range names {
		b.WriteByte(',')
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(r.Params[name])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Encode serializes a request.
func Encode(req Request) ([]byte, error) {
	return json.Marshal(req)
}

// Tool is the wire shape of a tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// Tools turns the core's tool specs into the wire shape, in the stable order
// the core sorts them into.
//
// A spec with no schema is a tool that opted out of one, and the API takes
// no tool without an input schema, so the tool is sent the empty object.
func Tools(specs []poo.ToolSpec) []Tool {
	var out []Tool
	for _, spec := range specs {
		schema := spec.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, Tool{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: schema,
		})
	}
	return out
}

// AppendMessages appends messages to a serialized request body, and replaces
// the output limit.
//
// The fork re-sends the snapshot as it was built: the body is decoded, the
// messages go after the last one and the output limit is replaced, and the
// result is serialized again. The key order is the one the body already had,
// so a fork's prefix is the one the endpoint has already cached.
func AppendMessages(body []byte, messages []Message, maxTokens int) ([]byte, error) {
	return appendBody(body, messages, maxTokens)
}
