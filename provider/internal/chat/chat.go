// Package chat holds the Chat Completions API's wire half.
//
// This package holds the request shape and its encoder, the conversion of the
// core's messages into the wire's messages, the fork's body rewrite, the
// answer's SSE reader, and the repairs a model's text may need: the thinking it
// writes in its own tags, a stream that repeats itself, and the tool calls it
// writes as text.
//
// Nothing here resolves a credential, chooses a URL or sets a header, and
// nothing here decides which repairs a model gets. That is the dividing line: a
// transport composes these pieces, names the profiles its model needs, and
// alone knows where a request goes or who pays for it.
package chat

import (
	"encoding/json"

	"github.com/sdougbrown/poo/provider/internal/support"
)

// Request is the wire shape of a chat completion request.
type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	// MaxTokens is always sent, because some local servers default to 512.
	MaxTokens int  `json:"max_tokens"`
	Stream    bool `json:"stream"`
	// StreamOptions asks for the usage chunk, which some servers honour and
	// others ignore.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
	// Params holds the parameters whose names the endpoint chooses: the
	// reasoning parameter, the sampling parameters and whatever a quirk
	// adds. They are written after the fields above, in name order, so the
	// same request always has the same body.
	Params map[string]json.RawMessage `json:"-"`
}

// MarshalJSON writes the request, with the named parameters merged into the top
// level.
//
// The merge is the support package's, the one every wire half shares: the
// parameters are written last and in name order, so the body of two
// requests that ask for the same thing is the same bytes.
func (r Request) MarshalJSON() ([]byte, error) {
	type plain Request
	body, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	return support.MergeParams(body, r.Params)
}

// StreamOptions asks for a final usage chunk.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Tool is the wire shape of a tool.
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

// Encode serializes a request.
func Encode(req Request) ([]byte, error) {
	return json.Marshal(req)
}
