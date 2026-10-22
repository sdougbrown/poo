package openai

import (
	"encoding/json"
	"fmt"
	"sort"

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

// shapeReasoning sets the parameters that turn the reasoning on.
//
// The descriptor holds the rule: the setting is mapped through the
// model's reasoning control, and the fields the mapping names are the
// ones the request carries.
//
// A setting the model cannot honour is a configuration error, and the
// request never goes out.
func (t *Transport) shapeReasoning(shape *requestShape) error {
	fields, err := t.cfg.Info.ReasoningFields(t.reasoningSetting())
	if err != nil {
		return configErrorf("%s", err.Error())
	}
	for _, field := range fields {
		shape.setParam(field.Path, field.Value)
	}
	return nil
}

// shapeSampling sets the sampling parameters the request carries.
//
// The descriptor holds the rule: the model's own defaults are the
// base, the configured overrides win over them, and a model that takes
// no temperature never gets one.
func (t *Transport) shapeSampling(shape *requestShape) {
	sampling := t.cfg.Info.EffectiveSampling(t.cfg.Sampling)
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
		request: &out,
		params:  out.Params,
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
//
// The ceiling is at least one token: a request asks for room, and a
// room of zero is no room at all.
func (t *Transport) maxTokens(req poo.ChatRequest) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	limit := t.cfg.Info.MaxTokens(0)
	if limit < 1 {
		limit = 1
	}
	return limit
}

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
