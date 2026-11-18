package chat

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// Message is the wire shape of one message.
//
// Content is a string for a message whose content is text, and the parts array
// when the message carries an image: the Chat Completions API takes an image
// only in a user message's parts, so an image a tool returned is sent in the
// message that follows its result.
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ContentPart is one part of a message's parts array.
type ContentPart struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	ImageURL   *ImageURL   `json:"image_url,omitempty"`
	InputAudio *InputAudio `json:"input_audio,omitempty"`
	AudioURL   *AudioURL   `json:"audio_url,omitempty"`
}

// InputAudio is the audio a part carries: base64 and the format the API names
// for it.
type InputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

// ImageURL is the image a part carries, as a data URL.
type ImageURL struct {
	URL string `json:"url"`
}

// AudioURL is the audio a part carries, as a URL.
//
// The spec calls the part `input_audio` and carries base64 with a format name;
// a server that mirrors `image_url` for sound names this instead. The quirk
// that emits it is what a model on such a server names.
type AudioURL struct {
	URL string `json:"url"`
}

// ToolCall is the wire shape of a tool call.
type ToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// Messages turns core messages into the wire shape, one or two at a time.
//
// A tool result that carries attachments becomes the tool message plus a user
// message carrying them: the Chat Completions API takes an image only in a user
// message's parts, unlike the Responses and Messages APIs, which take one inside
// the tool result itself.
func Messages(msgs []poo.Message) ([]Message, error) {
	out := make([]Message, 0, len(msgs))
	var pending []ContentPart
	flush := func() {
		if len(pending) == 0 {
			return
		}
		out = append(out, Message{Role: "user", Content: pending})
		pending = nil
	}
	for _, m := range msgs {
		cm, attachments, err := message(m)
		if err != nil {
			return nil, err
		}
		// A batch of tool results answers one assistant message, so the
		// pictures ride in one user message after the last of them: a
		// server that enforces alternation reads one turn, not one per
		// result.
		if cm.Role != "tool" {
			flush()
		}
		out = append(out, cm)
		pending = append(pending, attachments...)
	}
	flush()
	return out, nil
}

// message turns one core message into the wire shape.
func message(m poo.Message) (Message, []ContentPart, error) {
	cm := Message{Role: string(m.Role)}

	var text strings.Builder
	var calls []ToolCall
	var attachments []poo.ContentBlock
	toolResult := false
	// The provider blocks are checked in a pass of their own, so a block
	// after a tool result is checked too.
	for _, c := range m.Content {
		if c.Provider != nil {
			if err := support.CarryError(poo.APIChatCompletions, false, c.Provider); err != nil {
				return Message{}, nil, err
			}
		}
	}
	for _, c := range m.Content {
		switch c.Kind {
		case poo.ContentProvider:
			// The block's payload is opaque and has no wire field here.
			// CarryError has already refused a required block, so
			// what is left is optional and omitted. The plan's "the
			// omission is recorded" is the loop's responsibility: the
			// wire cannot write records.
		case poo.ContentText:
			text.WriteString(c.Text)
		case poo.ContentThinking:
			// The wire shape has no separate thinking field, so thinking is
			// dropped rather than merged into the answer.
		case poo.ContentToolCall:
			var ct ToolCall
			ct.Index = len(calls)
			ct.ID = c.ToolCallID
			ct.Type = "function"
			ct.Function.Name = c.Name
			ct.Function.Arguments = string(c.Arguments)
			calls = append(calls, ct)
		case poo.ContentImage, poo.ContentAudio:
			attachments = append(attachments, c)
		case poo.ContentToolResult:
			// The loop keeps going: an attachment the tool returned follows
			// its result block, in the same message.
			toolResult = true
			cm.Role = "tool"
			cm.ToolCallID = c.ToolCallID
			cm.Content = c.Text
		}
	}
	if toolResult {
		parts, err := attachmentParts(attachments)
		return cm, parts, err
	}

	if text.Len() > 0 {
		cm.Content = text.String()
	}
	cm.ToolCalls = calls
	parts, err := attachmentParts(attachments)
	return cm, parts, err
}

// attachmentParts renders the blocks a tool returned as the parts a user
// message carries, one part per modality.
//
// A block the wire cannot render fails the request: an attachment the core
// accepts and this wire leaves out is a picture or a recording the model never
// hears about, and a silent drop is worse than a loud one.
func attachmentParts(attachments []poo.ContentBlock) ([]ContentPart, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	parts := make([]ContentPart, 0, len(attachments))
	for _, attachment := range attachments {
		switch attachment.Kind {
		case poo.ContentImage:
			parts = append(parts, ContentPart{
				Type:     "image_url",
				ImageURL: &ImageURL{URL: support.AttachmentDataURL(attachment)},
			})
		case poo.ContentAudio:
			format := support.AudioFormat(attachment.MediaType)
			if format == "" {
				return nil, fmt.Errorf("the Chat Completions wire has no format name for %q", attachment.MediaType)
			}
			parts = append(parts, ContentPart{
				Type: "input_audio",
				InputAudio: &InputAudio{
					Data:   base64.StdEncoding.EncodeToString(attachment.Data),
					Format: format,
				},
			})
		}
	}
	return parts, nil
}
