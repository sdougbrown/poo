package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// The request shaping.
//
// The body is the Responses API's shape, with the fields the Codex endpoint
// expects. It asks for no stored response, streams the answer, asks for the
// reasoning summaries, and sends the input items the history converts to.
//
// The endpoint refuses the fields the plain Responses API takes for tuning. It
// rejects an output limit, a temperature and a top-p, so the body carries none
// of them. The request's MaxTokens stays the core's business.

// defaultInstructions is the instruction the request carries when the
// history holds no system message.
//
// The Codex endpoint rejects a request without instructions, and a direct
// consumer that passes no system prompt still makes a request.
const defaultInstructions = "You are a helpful assistant."

// responsesRequest is the wire shape of a Codex request.
type responsesRequest struct {
	Model             string              `json:"model"`
	Instructions      string              `json:"instructions"`
	Input             []json.RawMessage   `json:"input"`
	Tools             []responsesTool     `json:"tools,omitempty"`
	ToolChoice        string              `json:"tool_choice,omitempty"`
	ParallelToolCalls bool                `json:"parallel_tool_calls"`
	Reasoning         *responsesReasoning `json:"reasoning,omitempty"`
	Text              *textControl        `json:"text,omitempty"`
	Include           []string            `json:"include,omitempty"`
	PromptCacheKey    string              `json:"prompt_cache_key,omitempty"`
	Store             bool                `json:"store"`
	Stream            bool                `json:"stream"`
}

// responsesReasoning is the request's reasoning control.
type responsesReasoning struct {
	// Effort is the reasoning effort, or "none".
	Effort string `json:"effort"`
	// Summary is the summary the endpoint returns with the reasoning.
	Summary string `json:"summary,omitempty"`
}

// textControl is the request's answer shape.
type textControl struct {
	// Verbosity is how much the answer says.
	Verbosity string `json:"verbosity"`
}

// responsesTool is one tool the request offers.
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

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

// modelInfo returns the model info a request is shaped by.
//
// The request carries its own model info when the core supplies one; a
// direct consumer that passes none keeps the transport's configured one.
func (t *Transport) modelInfo(req poo.ChatRequest) poo.ModelInfo {
	if zeroModelInfo(req.ModelInfo) {
		return t.cfg.ModelInfo
	}
	return req.ModelInfo
}

// zeroModelInfo reports that a request carried no model info.
func zeroModelInfo(info poo.ModelInfo) bool {
	return support.ZeroModelInfo(info)
}

// encodeError is a request that cannot be serialized.
func encodeError(what string) error {
	return &poo.ProviderError{
		Kind:    poo.ProviderKindInvalidRequest,
		Message: "the request does not encode: " + what,
	}
}

// toolsMode returns how the run asks for tool calls.
//
// A model info that names a mode wins over the configured one.
func (t *Transport) toolsMode(info poo.ModelInfo) string {
	return support.ToolsMode(info, t.cfg.ToolsMode)
}

// textVerbosity returns the verbosity the request asks for.
func (t *Transport) textVerbosity() string {
	if t.cfg.TextVerbosity != "" {
		return t.cfg.TextVerbosity
	}
	return "low"
}

// buildBody turns a core request into the wire payload.
//
// The payload is shaped in three passes: the request's own fields, the
// model info's reasoning control, and the tools in their stable order.
func (t *Transport) buildBody(req poo.ChatRequest) ([]byte, error) {
	info := t.modelInfo(req)
	model := req.Model
	if model == "" {
		model = t.cfg.Model
	}
	if model == "" {
		model = info.ModelID
	}
	if model == "" {
		return nil, support.ConfigErrorf("the request names no model")
	}

	out := responsesRequest{
		Model:          model,
		ToolChoice:     "auto",
		Include:        []string{"reasoning.encrypted_content"},
		Text:           &textControl{Verbosity: t.textVerbosity()},
		PromptCacheKey: t.sessionID(req.SessionID),
		// The endpoint rejects a stored response, and every request
		// carries the whole history.
		Store:             false,
		Stream:            true,
		ParallelToolCalls: true,
	}

	items, instructions, err := inputItems(req.Messages, modalitiesOf(info), 0)
	if err != nil {
		return nil, err
	}
	out.Input = items
	if out.Input == nil {
		out.Input = []json.RawMessage{}
	}
	out.Instructions = instructions
	if out.Instructions == "" {
		out.Instructions = defaultInstructions
	}

	tools, err := t.tools(info, req.Tools)
	if err != nil {
		return nil, err
	}
	out.Tools = tools

	if err := t.shapeReasoning(req, info, &out); err != nil {
		return nil, err
	}

	body, err := json.Marshal(out)
	if err != nil {
		return nil, encodeError(err.Error())
	}
	return body, nil
}

// tools returns the tools the request offers, in the order it sends them.
//
// The endpoint makes native tool calls only. A model that asks for the tools
// to be described in the prompt is therefore a configuration error. It is not
// a request the model cannot answer.
func (t *Transport) tools(info poo.ModelInfo, specs []poo.ToolSpec) ([]responsesTool, error) {
	switch mode := t.toolsMode(info); mode {
	case "none":
		return nil, nil
	case "native", "auto", "":
	default:
		return nil, support.ConfigErrorf("the Codex endpoint makes native tool calls, so %q is not a tools mode it serves", mode)
	}
	out := make([]responsesTool, 0, len(specs))
	for _, spec := range support.SortedTools(specs) {
		parameters := spec.Schema
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, responsesTool{
			Type:        "function",
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  parameters,
			Strict:      false,
		})
	}
	return out, nil
}

// shapeReasoning sets the request's reasoning control.
//
// The model info holds the rule. The model's reasoning control validates the
// setting, and the effort it yields is the one the endpoint takes. The endpoint
// has no field for a token budget and none for a chat-template switch. A model
// that controls its reasoning another way is therefore a configuration error.
// Such a request would silently reason as much as it likes.
func (t *Transport) shapeReasoning(req poo.ChatRequest, info poo.ModelInfo, out *responsesRequest) error {
	setting := t.reasoningSetting(req)
	fields, err := info.ReasoningFields(setting)
	if err != nil {
		return support.ConfigErrorf("%s", support.ReasonError(err))
	}
	effort := ""
	for _, field := range fields {
		if field.Path != poo.ReasoningPathEffort {
			return support.ConfigErrorf("the Codex endpoint takes an effort level, not %s", field.Path)
		}
		value, ok := field.Value.(string)
		if !ok {
			return support.ConfigErrorf("the reasoning effort %v is not a level", field.Value)
		}
		effort = value
	}
	if effort == "" && info.Reasoning.Mode != poo.ReasoningEffort {
		// The model controls no reasoning, or it was told off and its
		// control says so by sending nothing.
		return nil
	}
	if effort == "" {
		// A reasoning model that is told off still asks the endpoint for
		// no reasoning: the level's own spelling of off is "none".
		out.Reasoning = &responsesReasoning{Effort: "none"}
		return nil
	}
	out.Reasoning = &responsesReasoning{Effort: effort, Summary: t.reasoningSummary()}
	return nil
}

// reasoningSetting returns the reasoning setting a request uses.
//
// The request's own level wins: it is the session's, and a session that
// changed it between the runs means it. A request that carries none
// falls back to the transport's configuration.
func (t *Transport) reasoningSetting(req poo.ChatRequest) string {
	if req.Reasoning != "" {
		return req.Reasoning
	}
	return t.cfg.Reasoning
}

// reasoningSummary returns the summary a reasoning request asks for.
func (t *Transport) reasoningSummary() string {
	if t.cfg.ReasoningSummary != "" {
		return t.cfg.ReasoningSummary
	}
	return "auto"
}

// modalities are the attachments a request may carry.
//
// The model info a request carries decides them, and a fork has the body
// and no model info. The snapshot records the attachments its body was
// shaped for, so an appended attachment survives the fork.
type modalities struct {
	// image reports that the model reads images.
	image bool
	// audio reports that the model reads audio.
	audio bool
}

// modalitiesOf returns the attachments the model reads.
func modalitiesOf(info poo.ModelInfo) modalities {
	return modalities{
		image: info.Reads(poo.ModalityImage),
		audio: info.Reads(poo.ModalityAudio),
	}
}

// inputs returns the attachments a request shaped for these modalities was
// shaped for, in the shape the snapshot carries.
func (m modalities) inputs() []poo.Modality {
	var out []poo.Modality
	if m.image {
		out = append(out, poo.ModalityImage)
	}
	if m.audio {
		out = append(out, poo.ModalityAudio)
	}
	return out
}

// modalitiesOfInputs returns the attachments a snapshot's inputs say the
// model reads.
func modalitiesOfInputs(inputs []poo.Modality) modalities {
	var out modalities
	for _, input := range inputs {
		switch input {
		case poo.ModalityImage:
			out.image = true
		case poo.ModalityAudio:
			out.audio = true
		}
	}
	return out
}

// inputItems turns the model's history into the request's input items.
//
// The leading system message is the request's instructions, because the
// API carries the prompt in its own field. A later system message rides
// as a developer item, which is where the API takes an instruction that
// arrives mid-conversation. The firstID is the id the first assistant
// message item takes, so a fork numbers its appended items after the
// snapshot's.
//
// Every message's provider blocks are checked in one pass of their own, before
// the role decides what the message becomes. This transport can only give a
// block back to the API that produced it. A required block is therefore
// refused wherever it rides, whether another API produced it or it has no
// payload to give back. An optional one is left out. The pass covers the
// instructions, a caller-built user message and a tool result, as well as the
// assistant message the endpoint returned.
func inputItems(messages []poo.Message, reads modalities, firstID int) ([]json.RawMessage, string, error) {
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
func userItems(m poo.Message, reads modalities) ([]json.RawMessage, error) {
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
			raw, ok, err := providerItem(c.Provider, poo.APIResponses)
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
			raw, ok, err := providerItem(c.Provider, poo.APIResponses)
			if err != nil {
				return nil, err
			}
			if ok {
				items = append(items, raw)
			}
		case poo.ContentThinking:
			// The reasoning text has no field of its own here: the item
			// the endpoint returned is what carries it back, verbatim.
			raw, ok, err := providerItem(c.Provider, poo.APIResponses)
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
func toolResultItems(m poo.Message, reads modalities) ([]json.RawMessage, error) {
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
func toolOutput(text string, attachments []poo.ContentBlock, reads modalities) (json.RawMessage, error) {
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
func inputParts(blocks []poo.ContentBlock, reads modalities) []inputPart {
	var parts []inputPart
	for _, c := range blocks {
		switch c.Kind {
		case poo.ContentText:
			if c.Text == "" {
				continue
			}
			parts = append(parts, inputPart{Type: "input_text", Text: c.Text})
		case poo.ContentImage:
			if !reads.image {
				continue
			}
			parts = append(parts, inputPart{Type: "input_image", Detail: "auto", ImageURL: support.AttachmentDataURL(c)})
		case poo.ContentAudio:
			if !reads.audio {
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
// The payload is opaque: the transport never interprets it, it only gives
// it back to the API that produced it. An empty payload leaves nothing to
// send.
func providerItem(p *poo.ProviderBlock, api poo.API) (json.RawMessage, bool, error) {
	if p == nil || p.API != api || len(p.Data) == 0 {
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
