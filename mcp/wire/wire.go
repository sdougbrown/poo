// Package wire holds the vocabulary a server's tools and answers travel in.
//
// The client copies the server's values into these shapes, and the readers
// over them — the settings' per-server views, the tool that dispatches —
// name the same ones. The package imports the standard library alone, so a
// reader of the tools names them without linking the client that speaks the
// protocol.
package wire

import "encoding/json"

// Tool is one tool a server listed, as the session sees it. The values
// are copies: a caller may read them after the server is gone.
type Tool struct {
	// Name is the tool's own name, unqualified. A call qualifies it
	// with the server's.
	Name string
	// Description is what the server said the tool does.
	Description string
	// InputSchema is the tool's input schema, as the server sent it.
	InputSchema json.RawMessage
	// Annotations are the hints the server attached to the tool. They
	// are the server's own claims about itself, never a permission.
	Annotations ToolAnnotations
}

// ToolAnnotations are the hints a server attached to a tool, in the
// shape the callers read. The protocol's omitted booleans become the
// protocol's own defaults, and the protocol documents them: a hint the
// server did not name reads as the protocol says — destructive and
// open-world default to true, read-only and idempotent to false.
type ToolAnnotations struct {
	// Title is the tool's human name.
	Title string
	// ReadOnlyHint says the tool does not modify its environment.
	ReadOnlyHint bool
	// DestructiveHint says the tool may perform destructive updates.
	DestructiveHint bool
	// IdempotentHint says repeated calls have no further effect.
	IdempotentHint bool
	// OpenWorldHint says the tool interacts with outside entities.
	OpenWorldHint bool
}

// CallResult is what a tool call returned.
type CallResult struct {
	// IsError reports the server's own error: the call reached the
	// server and the tool ran, and the tool's answer is that it failed.
	// The call itself is a success, so the error text travels in the
	// contents like any other answer.
	IsError bool
	// Text holds the text contents the server returned, in order.
	Text []string
	// Images holds the image contents the server returned, in order.
	Images []CallImage
	// Structured is the structured content the server returned, or nil
	// when it returned none.
	Structured json.RawMessage
}

// CallImage is one image content a tool call returned.
type CallImage struct {
	// MediaType is the image's MIME type.
	MediaType string
	// Data is the image's bytes.
	Data []byte
}
