package openai

import "github.com/sdougbrown/poo/provider/internal/chat"

// The text-mode tool-call markers, under the transport's own names.
//
// The parser that reads them is the wire half's. A consumer that describes the
// text mode names them here: the eval judge quotes the tags out of a
// transcript, because an endpoint may cut its answer at the one it finds.
const (
	// ToolCallOpen opens a tool call block in text mode.
	ToolCallOpen = chat.ToolCallOpen
	// ToolCallClose closes a tool call block in text mode.
	ToolCallClose = chat.ToolCallClose
	// MistralCallsOpen opens a Mistral-style call list.
	MistralCallsOpen = chat.MistralCallsOpen
)
