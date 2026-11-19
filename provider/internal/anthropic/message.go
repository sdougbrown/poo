package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// Message is the wire shape of one message: a role and its content blocks.
//
// The Messages API alternates user and assistant messages, and a tool's
// result is the user message that answers the assistant message whose blocks
// asked for the tool.
type Message struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

// Block is one content block of a message.
//
// The API's blocks are one of a few kinds, each with its own fields; the
// kinds are told apart by Type, and the fields a kind does not carry stay
// empty.
type Block struct {
	Type string `json:"type"`
	// Text is the text of a text block.
	Text string `json:"text,omitempty"`
	// Thinking is the thinking of a thinking block, in the field the API
	// names for it, and Signature is the signature that block carries.
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// ID and Name carry a tool use, and Input holds its arguments.
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// ToolUseID names the call a tool result answers, and Content holds the
	// result itself. IsError marks a result the model must see as a
	// failure.
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// Source is the attachment of an image or a document block.
	Source *Source `json:"source,omitempty"`
}

// Source is an attachment's bytes, in the only form the API takes them.
type Source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// Messages turns core messages into the wire shape, and returns the system
// prompt the messages carry.
//
// The system prompt travels in the request's own field, above the messages
// the API alternates. A tool's result is the user message that answers the
// call, so a run's tool results arrive as user messages with result blocks.
// The thinking the model did rides back as a thinking block with its
// signature, because a request that answers its own thinking must return it
// unchanged.
func Messages(msgs []poo.Message) ([]Message, string, error) {
	var system []string
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case poo.RoleSystem:
			text, err := systemText(m)
			if err != nil {
				return nil, "", err
			}
			system = append(system, text)
			continue
		case poo.RoleUser, poo.RoleAssistant:
		case poo.RoleTool:
			// A tool's result is the user message that answers the call,
			// so a run's tool results arrive as user messages with result
			// blocks.
			blocks, err := blocks(m)
			if err != nil {
				return nil, "", err
			}
			if len(blocks) > 0 {
				out = append(out, Message{Role: "user", Content: blocks})
			}
			continue
		default:
			return nil, "", &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: fmt.Sprintf("the Messages API carries no %q role", m.Role),
			}
		}
		blocks, err := blocks(m)
		if err != nil {
			return nil, "", err
		}
		if len(blocks) == 0 {
			// A message the wire cannot spell is dropped rather than
			// refused: an empty user message between the turns is one the
			// API refuses, and the history around it says what it said.
			continue
		}
		out = append(out, Message{Role: string(m.Role), Content: blocks})
	}
	return out, strings.Join(system, "\n\n"), nil
}

// systemText collects a system message's text.
func systemText(m poo.Message) (string, error) {
	var parts []string
	for _, c := range m.Content {
		if c.Provider != nil {
			if err := support.CarryError(poo.APIAnthropicMessages, false, c.Provider); err != nil {
				return "", err
			}
		}
		if c.Kind != poo.ContentText {
			return "", &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the system prompt carries a block the Messages API cannot spell",
			}
		}
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n\n"), nil
}

// blocks turns one message's content into wire blocks.
func blocks(m poo.Message) ([]Block, error) {
	var out []Block
	var pending []Block
	for _, c := range m.Content {
		// A thinking block's own companion is its signature, which the
		// block carries. Every other companion — and a thinking block's
		// companion when it belongs to another API — is data the Messages
		// request has no place for.
		if c.Provider != nil && !(c.Kind == poo.ContentThinking && c.Provider.API == poo.APIAnthropicMessages) {
			if err := support.CarryError(poo.APIAnthropicMessages, false, c.Provider); err != nil {
				return nil, err
			}
		}
		switch c.Kind {
		case poo.ContentText:
			out = append(out, Block{Type: "text", Text: c.Text})
		case poo.ContentThinking:
			out = append(out, thinkingBlock(c))
		case poo.ContentToolCall:
			out = append(out, Block{Type: "tool_use", ID: c.ToolCallID, Name: c.Name, Input: c.Arguments})
		case poo.ContentToolResult:
			// The result is a block of the user message that answers the
			// call. A result that follows results goes in the same message,
			// because one assistant message's calls take one answer.
			out = append(out, Block{
				Type:      "tool_result",
				ToolUseID: c.ToolCallID,
				Content:   c.Text,
				IsError:   c.IsError,
			})
		case poo.ContentImage, poo.ContentAudio:
			block, ok := attachment(c)
			if !ok {
				continue
			}
			pending = append(pending, block)
		}
	}
	// An attachment rides after the blocks of its own message: the API
	// takes one anywhere, and the last place is where a reader of the body
	// finds it beside the text it answers.
	return append(out, pending...), nil
}

// thinkingBlock turns a thinking block and its signature companion into the
// one block the API expects.
func thinkingBlock(c poo.ContentBlock) Block {
	block := Block{Type: "thinking", Thinking: c.Text}
	if c.Provider != nil && c.Provider.API == poo.APIAnthropicMessages && len(c.Provider.Data) > 0 {
		var signature string
		if err := json.Unmarshal(c.Provider.Data, &signature); err == nil {
			block.Signature = signature
		}
	}
	return block
}

// attachment turns a core attachment into an image or a document block.
//
// The API takes an image by its media type and a PDF as a document. Sound it
// does not take, so an audio block is left out and the message says what it
// said without it.
func attachment(c poo.ContentBlock) (Block, bool) {
	if c.Kind == poo.ContentAudio {
		return Block{}, false
	}
	kind := "image"
	if c.MediaType == "application/pdf" {
		kind = "document"
	}
	return Block{
		Type: kind,
		Source: &Source{
			Type:      "base64",
			MediaType: c.MediaType,
			Data:      base64.StdEncoding.EncodeToString(c.Data),
		},
	}, true
}
