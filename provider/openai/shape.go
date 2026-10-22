package openai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sdougbrown/poo"
)

// The request shaping.
//
// The plan: the request carries the output ceiling, the sampling
// parameters, the reasoning parameter and the tools. The ceiling is
// always computed, the tools are always in a stable order, and the
// reasoning parameter follows the model's reasoning control.

// reasoningSetting returns the reasoning setting the request uses.
//
// The configured setting wins, and the descriptor's reasoning control
// decides how it is spelled.
func (t *Transport) reasoningSetting() string {
	if t.cfg.Reasoning != "" {
		return t.cfg.Reasoning
	}
	return t.cfg.ReasoningEffort
}

// shapeReasoning sets the parameter that turns the reasoning on.
//
// The plan's mapping: an effort is an effort level, a budget is a
// number of thinking tokens, and a toggle is on or off. The parameter's
// name is the one the endpoint knows, which the descriptor names.
//
// A model whose reasoning cannot be controlled takes no parameter, and a
// setting that asks it to think is a configuration error.
func (t *Transport) shapeReasoning(shape *requestShape) error {
	mode := t.cfg.Info.Reasoning.Mode
	setting := strings.TrimSpace(shape.reasoning)
	if mode == "" {
		if setting == "" {
			return nil
		}
		// No descriptor: the configured value is an effort level.
		mode = poo.ReasoningEffort
	}
	if strings.EqualFold(setting, "off") {
		return nil
	}
	if mode == poo.ReasoningNone {
		return configErrorf("the model's reasoning cannot be controlled, but the reasoning setting is %q", setting)
	}
	switch mode {
	case poo.ReasoningEffort:
		level := strings.ToLower(setting)
		switch level {
		case "low", "medium", "high":
		default:
			return configErrorf("the reasoning setting %q is not an effort level", setting)
		}
		shape.setParam(t.reasoningParam("reasoning_effort"), level)
	case poo.ReasoningBudget:
		budget, err := strconv.Atoi(setting)
		if err != nil || budget <= 0 {
			return configErrorf("the reasoning setting %q is not a number of thinking tokens", setting)
		}
		shape.setParam(t.reasoningParam("reasoning_budget"), budget)
	case poo.ReasoningToggle:
		shape.setParam(t.reasoningParam("chat_template_kwargs.enable_thinking"), true)
	default:
		return configErrorf("the model's reasoning mode %q is not one the transport knows", mode)
	}
	return nil
}

// reasoningParam returns the name the endpoint knows the reasoning
// parameter by.
//
// The descriptor names it. A quirk may name another one, and the
// quirk's name wins: it is the model-specific knowledge.
func (t *Transport) reasoningParam(fallback string) string {
	if p := t.cfg.Info.Reasoning.Param; p != "" {
		return p
	}
	return fallback
}

// shapeSampling sets the sampling parameters the request carries.
//
// The descriptor's defaults are the base, and the configured overrides
// win over them. A model that takes no temperature never gets one.
func (t *Transport) shapeSampling(shape *requestShape) {
	sampling := t.effectiveSampling()
	if t.cfg.Info.Temperature == poo.TempFixed || t.cfg.Info.Temperature == poo.TempUnsupported {
		sampling.Temperature = nil
	}
	if v := sampling.Temperature; v != nil {
		shape.setParam("temperature", *v)
	}
	if v := sampling.TopP; v != nil {
		shape.setParam("top_p", *v)
	}
	if v := sampling.TopK; v != nil {
		shape.setParam("top_k", *v)
	}
	if v := sampling.MinP; v != nil {
		shape.setParam("min_p", *v)
	}
}

// effectiveSampling returns the sampling parameters the request uses.
//
// The descriptor's defaults are the base, and the configured overrides
// win over them, field by field.
func (t *Transport) effectiveSampling() poo.Sampling {
	out := t.cfg.Info.Sampling
	if v := t.cfg.Sampling.Temperature; v != nil {
		out.Temperature = v
	}
	if v := t.cfg.Sampling.TopP; v != nil {
		out.TopP = v
	}
	if v := t.cfg.Sampling.TopK; v != nil {
		out.TopK = v
	}
	if v := t.cfg.Sampling.MinP; v != nil {
		out.MinP = v
	}
	return out
}

// configErrorf is a failure in the transport's own configuration.
//
// It is a rejected request: the endpoint never saw it, and the run
// settles with the message.
func configErrorf(format string, args ...any) error {
	return &poo.ProviderError{
		Kind:    poo.ProviderKindInvalidRequest,
		Message: fmt.Sprintf(format, args...),
	}
}

// buildBody turns a core request into the wire payload.
//
// The payload is shaped in three passes: the request's own fields, the
// descriptor's controls (the reasoning parameter, the sampling
// parameters), and the quirks the descriptor named, in its order.
func (t *Transport) buildBody(req poo.ChatRequest) ([]byte, error) {
	out := chatRequest{
		Model:     req.Model,
		MaxTokens: t.maxTokens(req),
		Stream:    true,
		// The usage chunk is asked for in every mode, because the log's
		// usage record needs the token accounting and a server that
		// reports it on its own still ignores the field.
		StreamOptions: &streamOptions{IncludeUsage: true},
		Params:        map[string]json.RawMessage{},
	}

	for _, m := range req.Messages {
		cm, err := convertMessage(m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, cm)
	}

	// In native mode the tools go in the request, and the model answers
	// with tool_calls. In text mode the tools are described in the
	// prompt instead, and the model answers with text that the parser
	// reads. In auto mode both happen.
	if mode := t.toolsMode(); mode == "native" || mode == "auto" {
		for _, spec := range sortedTools(req.Tools) {
			var ct chatTool
			ct.Type = "function"
			ct.Function.Name = spec.Name
			ct.Function.Description = spec.Description
			ct.Function.Parameters = spec.Schema
			if len(ct.Function.Parameters) == 0 {
				ct.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			out.Tools = append(out.Tools, ct)
		}
	}

	shape := &requestShape{
		request:   &out,
		reasoning: t.reasoningSetting(),
		params:    out.Params,
	}
	if err := t.shapeReasoning(shape); err != nil {
		return nil, err
	}
	t.shapeSampling(shape)
	for _, quirk := range t.quirks {
		if quirk.shapeRequest != nil {
			quirk.shapeRequest(shape)
		}
	}
	if len(out.Params) == 0 {
		out.Params = nil
	}

	return json.Marshal(out)
}

// maxTokens returns the output ceiling the request asks for.
//
// The caller computes the ceiling, because only it knows the size of
// the history. A request that arrives without one gets the model's own
// ceiling, or the default, and never the endpoint's.
func (t *Transport) maxTokens(req poo.ChatRequest) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	limit := t.cfg.Info.MaxOutput
	if limit <= 0 {
		limit = poo.DefaultMaxOutput
	}
	// A model with a known context window cannot be asked for more than
	// its window holds.
	if ctxLimit := t.cfg.Info.ContextLimit; ctxLimit > 0 {
		if room := ctxLimit - outputReserve; room < limit {
			limit = room
		}
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

// outputReserve is the room a request leaves for the answer's own
// closing tokens.
const outputReserve = 256

// sortedTools returns the tools in the order the request sends them.
//
// The order is by name, so two sessions that registered the same tools
// in different orders send the same array. The array is part of the
// prompt's prefix, and a prefix that changes breaks the endpoint's
// cache.
func sortedTools(specs []poo.ToolSpec) []poo.ToolSpec {
	out := make([]poo.ToolSpec, len(specs))
	copy(out, specs)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
