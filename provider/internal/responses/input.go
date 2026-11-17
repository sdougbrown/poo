package responses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// The input encoding.
//
// The request's input is the model's history turned into the API's items. The
// leading system message becomes the request's instructions, because the API
// carries the prompt in a field of its own. Everything else becomes an item:
// a user message, an assistant message, the calls it made, the results they
// returned, and the provider blocks the endpoint produced.

// inputMessage is one message item of the request's input.
//
// The leading system message becomes the request's instructions. A later one
// rides as this item with the developer role and no type. The API names a user
// or assistant message, and takes an instruction that arrives mid-conversation
// as the role alone.
type inputMessage struct {
	Type    string      `json:"type,omitempty"`
	Role    string      `json:"role"`
	Content []inputPart `json:"content"`
}

// outputMessage is one assistant message item of the request's input.
type outputMessage struct {
	Type    string       `json:"type"`
	Role    string       `json:"role"`
	Content []outputPart `json:"content"`
	ID      string       `json:"id"`
	Status  string       `json:"status"`
}

// inputPart is one content part an input item carries.
type inputPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	Detail   string      `json:"detail,omitempty"`
	ImageURL string      `json:"image_url,omitempty"`
	Audio    *inputAudio `json:"input_audio,omitempty"`
}

// inputAudio is an audio part's payload.
type inputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

// outputPart is one content part an assistant message item carries.
type outputPart struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

// functionCall is one tool call item.
type functionCall struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// functionCallOutput is one tool result item.
type functionCallOutput struct {
	Type   string          `json:"type"`
	CallID string          `json:"call_id"`
	Output json.RawMessage `json:"output"`
}

// Modalities are the attachments a request may carry.
//
// The model info a request carries decides them, and a fork has the body and
// no model info. The snapshot records the attachments its body was shaped
// for, so an appended attachment survives the fork.
type Modalities struct {
	// Image reports that the model reads images.
	Image bool
	// Audio reports that the model reads audio.
	Audio bool
}

// ModalitiesOf returns the attachments the model reads.
func ModalitiesOf(info poo.ModelInfo) Modalities {
	return Modalities{
		Image: info.Reads(poo.ModalityImage),
		Audio: info.Reads(poo.ModalityAudio),
	}
}

// ModalitiesFrom returns the attachments a snapshot's inputs say the model
// reads.
func ModalitiesFrom(inputs []poo.Modality) Modalities {
	var out Modalities
	for _, input := range inputs {
		switch input {
		case poo.ModalityImage:
			out.Image = true
		case poo.ModalityAudio:
			out.Audio = true
		}
	}
	return out
}

// Inputs returns the attachments a request shaped for these modalities was
// shaped for, in the shape the snapshot carries.
func (m Modalities) Inputs() []poo.Modality {
	var out []poo.Modality
	if m.Image {
		out = append(out, poo.ModalityImage)
	}
	if m.Audio {
		out = append(out, poo.ModalityAudio)
	}
	return out
}

// InputItems turns the model's history into the request's input items.
//
// The leading system message is the request's instructions, because the API
// carries the prompt in its own field. A later system message rides as a
// developer item, which is where the API takes an instruction that arrives
// mid-conversation. The firstID is the id the first assistant message item
// takes, so a fork numbers its appended items after the snapshot's.
//
// Every message's provider blocks are checked in one pass of their own, before
// the role decides what the message becomes. This API can only be given back
// a block it produced. A required block is therefore refused wherever it
// rides, whether another API produced it or it has no payload to give back. An
// optional one is left out. The pass covers the instructions, a caller-built
// user message and a tool result, as well as the assistant message the
// endpoint returned.
func InputItems(messages []poo.Message, reads Modalities, firstID int) ([]json.RawMessage, string, error) {
	var items []json.RawMessage
	instructions := ""
	next := firstID
	for i, m := range messages {
		for _, c := range m.Content {
			if err := support.CarryError(poo.APIResponses, true, c.Provider); err != nil {
				return nil, "", err
			}
		}
		switch m.Role {
		case poo.RoleSystem:
			if i == 0 && instructions == "" {
				instructions = m.Text()
				continue
			}
			text := m.Text()
			if text == "" {
				continue
			}
			raw, err := json.Marshal(inputMessage{
				Role:    "developer",
				Content: []inputPart{{Type: "input_text", Text: text}},
			})
			if err != nil {
				return nil, "", encodeError("an instruction item")
			}
			items = append(items, raw)
		case poo.RoleUser:
			converted, err := userItems(m, reads)
			if err != nil {
				return nil, "", err
			}
			items = append(items, converted...)
		case poo.RoleAssistant:
			converted, err := assistantItems(m, &next)
			if err != nil {
				return nil, "", err
			}
			items = append(items, converted...)
		case poo.RoleTool:
			converted, err := toolResultItems(m, reads)
			if err != nil {
				return nil, "", err
			}
			items = append(items, converted...)
		}
	}
	return items, instructions, nil
}

// userItems turns a user message into the items the API takes.
//
// The text and the attachments ride in one message item. A provider block this
// API produced rides as the top-level item it came from. The parts around it
// stay on either side, in the order the history holds them. A block this API
// cannot carry leaves the message's parts unchanged. The core's provider window
// is this shape with no parts.
func userItems(m poo.Message, reads Modalities) ([]json.RawMessage, error) {
	var items []json.RawMessage
	var parts []inputPart
	flush := func() error {
		if len(parts) == 0 {
			return nil
		}
		raw, err := json.Marshal(inputMessage{Type: "message", Role: "user", Content: parts})
		if err != nil {
			return encodeError("a user message")
		}
		parts = nil
		items = append(items, raw)
		return nil
	}
	for _, c := range m.Content {
		if c.Kind == poo.ContentProvider {
			raw, ok, err := providerItem(c.Provider)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if err := flush(); err != nil {
				return nil, err
			}
			items = append(items, raw)
			continue
		}
		parts = append(parts, inputParts([]poo.ContentBlock{c}, reads)...)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return items, nil
}

// assistantItems turns an assistant message into the items the API takes.
//
// A message is one item on the wire only when it carries text. A tool call
// and a reasoning item ride beside it, in the order the blocks appear. The API
// matches a call to its output by position as well as by id.
func assistantItems(m poo.Message, next *int) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for _, c := range m.Content {
		switch c.Kind {
		case poo.ContentProvider:
			raw, ok, err := providerItem(c.Provider)
			if err != nil {
				return nil, err
			}
			if ok {
				items = append(items, raw)
			}
		case poo.ContentThinking:
			// The reasoning text has no field of its own here: the item
			// the endpoint returned is what carries it back, verbatim.
			raw, ok, err := providerItem(c.Provider)
			if err != nil {
				return nil, err
			}
			if ok {
				items = append(items, raw)
			}
		case poo.ContentText:
			if c.Text == "" {
				continue
			}
			raw, err := json.Marshal(outputMessage{
				Type:    "message",
				Role:    "assistant",
				ID:      fmt.Sprintf("msg_poo_%d", *next),
				Status:  "completed",
				Content: []outputPart{{Type: "output_text", Text: c.Text, Annotations: []any{}}},
			})
			if err != nil {
				return nil, encodeError("an assistant message")
			}
			*next = *next + 1
			items = append(items, raw)
		case poo.ContentToolCall:
			arguments := strings.TrimSpace(string(c.Arguments))
			if arguments == "" {
				arguments = "{}"
			}
			raw, err := json.Marshal(functionCall{
				Type:      "function_call",
				CallID:    c.ToolCallID,
				Name:      c.Name,
				Arguments: arguments,
			})
			if err != nil {
				return nil, encodeError("a tool call")
			}
			items = append(items, raw)
		}
	}
	return items, nil
}

// toolResultItems turns a tool message into the items the API takes.
//
// A tool result is a top-level item, and the attachments that follow it
// in the same message are its own. The API carries them inside the
// result's output, rather than in a message of their own.
func toolResultItems(m poo.Message, reads Modalities) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for i := 0; i < len(m.Content); i++ {
		result := m.Content[i]
		if result.Kind != poo.ContentToolResult {
			continue
		}
		var attachments []poo.ContentBlock
		j := i + 1
		for ; j < len(m.Content); j++ {
			if c := m.Content[j]; c.Kind != poo.ContentImage && c.Kind != poo.ContentAudio {
				break
			}
			attachments = append(attachments, m.Content[j])
		}
		i = j - 1
		output, err := toolOutput(result.Text, attachments, reads)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(functionCallOutput{
			Type:   "function_call_output",
			CallID: result.ToolCallID,
			Output: output,
		})
		if err != nil {
			return nil, encodeError("a tool result")
		}
		items = append(items, raw)
	}
	return items, nil
}

// toolOutput renders a tool result's output.
//
// Text alone rides as a string. A result that carries an attachment the
// model reads rides as a list of parts. The attachment then travels inside
// the result, rather than in a message of its own.
func toolOutput(text string, attachments []poo.ContentBlock, reads Modalities) (json.RawMessage, error) {
	parts := inputParts(attachments, reads)
	hasText := strings.TrimSpace(text) != ""
	if len(parts) == 0 {
		switch {
		case hasText:
			return marshalOutput(text)
		case len(attachments) > 0:
			// The model reads no attachment of this kind, so the picture
			// is named rather than sent.
			return marshalOutput("(see attached attachment)")
		default:
			return marshalOutput("(no tool output)")
		}
	}
	out := make([]inputPart, 0, len(parts)+1)
	if hasText {
		out = append(out, inputPart{Type: "input_text", Text: text})
	}
	out = append(out, parts...)
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, encodeError("a tool result's output")
	}
	return raw, nil
}

// marshalOutput encodes a plain string as a JSON value.
func marshalOutput(text string) (json.RawMessage, error) {
	raw, err := json.Marshal(text)
	if err != nil {
		return nil, encodeError("a tool result's output")
	}
	return raw, nil
}

// inputParts turns content blocks into the parts an input item carries.
//
// An attachment the model does not read is left out: a data URL the
// endpoint refuses would fail every request the history carries.
func inputParts(blocks []poo.ContentBlock, reads Modalities) []inputPart {
	var parts []inputPart
	for _, c := range blocks {
		switch c.Kind {
		case poo.ContentText:
			if c.Text == "" {
				continue
			}
			parts = append(parts, inputPart{Type: "input_text", Text: c.Text})
		case poo.ContentImage:
			if !reads.Image {
				continue
			}
			parts = append(parts, inputPart{Type: "input_image", Detail: "auto", ImageURL: support.AttachmentDataURL(c)})
		case poo.ContentAudio:
			if !reads.Audio {
				continue
			}
			format := support.AudioFormat(c.MediaType)
			if format == "" {
				continue
			}
			parts = append(parts, inputPart{
				Type:  "input_audio",
				Audio: &inputAudio{Data: base64.StdEncoding.EncodeToString(c.Data), Format: format},
			})
		}
	}
	return parts
}

// providerItem returns the payload of a provider block this API owns,
// verbatim, and whether there is one to send.
//
// The payload is opaque: this package never interprets it, it only gives
// it back to the API that produced it. An empty payload leaves nothing to
// send.
func providerItem(p *poo.ProviderBlock) (json.RawMessage, bool, error) {
	if p == nil || p.API != poo.APIResponses || len(p.Data) == 0 {
		return nil, false, nil
	}
	if !json.Valid(p.Data) {
		return nil, false, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: fmt.Sprintf("the history holds a %q provider block whose payload is not JSON", p.Type),
		}
	}
	return json.RawMessage(p.Data), true, nil
}
