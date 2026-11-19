package codex

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/responses"
	"github.com/sdougbrown/poo/provider/internal/support"
	"github.com/sdougbrown/poo/provider/internal/transport"
)

// The request policy.
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
	return transport.ZeroModelInfo(info)
}

// toolsMode returns how the run asks for tool calls.
//
// A model info that names a mode wins over the configured one.
func (t *Transport) toolsMode(info poo.ModelInfo) string {
	return transport.ToolsMode(info, t.cfg.ToolsMode)
}

// textVerbosity returns the verbosity the request asks for.
func (t *Transport) textVerbosity() string {
	if t.cfg.TextVerbosity != "" {
		return t.cfg.TextVerbosity
	}
	return "low"
}

// buildBody turns a core request into the wire payload.
func (t *Transport) buildBody(req poo.ChatRequest) ([]byte, error) {
	out, err := t.shapeRequest(req)
	if err != nil {
		return nil, err
	}
	return responses.Encode(out)
}

// shapeRequest shapes a core request into the request's wire fields.
//
// The payload is shaped in three passes: the request's own fields, the
// model info's reasoning control, and the tools in their stable order.
func (t *Transport) shapeRequest(req poo.ChatRequest) (responses.Request, error) {
	info := t.modelInfo(req)
	model := req.Model
	if model == "" {
		model = t.cfg.Model
	}
	if model == "" {
		model = info.ModelID
	}
	if model == "" {
		return responses.Request{}, support.ConfigErrorf("the request names no model")
	}

	out := responses.Request{
		Model:          model,
		ToolChoice:     "auto",
		Include:        []string{"reasoning.encrypted_content"},
		Text:           &responses.Text{Verbosity: t.textVerbosity()},
		PromptCacheKey: t.sessionID(req.SessionID),
		// The endpoint rejects a stored response, and every request
		// carries the whole history.
		Store:             false,
		Stream:            true,
		ParallelToolCalls: true,
	}

	items, instructions, err := responses.InputItems(req.Messages, responses.ModalitiesOf(info), 0)
	if err != nil {
		return responses.Request{}, err
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
		return responses.Request{}, err
	}
	out.Tools = tools

	if err := t.shapeReasoning(req, info, &out); err != nil {
		return responses.Request{}, err
	}

	return out, nil
}

// tools returns the tools the request offers, in the order it sends them.
//
// The endpoint makes native tool calls only. A model that asks for the tools
// to be described in the prompt is therefore a configuration error. It is not
// a request the model cannot answer.
func (t *Transport) tools(info poo.ModelInfo, specs []poo.ToolSpec) ([]responses.Tool, error) {
	switch mode := t.toolsMode(info); mode {
	case "none":
		return nil, nil
	case "native", "auto", "":
	default:
		return nil, support.ConfigErrorf("the Codex endpoint makes native tool calls, so %q is not a tools mode it serves", mode)
	}
	out := make([]responses.Tool, 0, len(specs))
	for _, spec := range transport.SortedTools(specs) {
		parameters := spec.Schema
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, responses.Tool{
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
func (t *Transport) shapeReasoning(req poo.ChatRequest, info poo.ModelInfo, out *responses.Request) error {
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
		out.Reasoning = &responses.Reasoning{Effort: "none"}
		return nil
	}
	out.Reasoning = &responses.Reasoning{Effort: effort, Summary: t.reasoningSummary()}
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
