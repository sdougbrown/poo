package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/sdougbrown/poo"
)

// The request shaping.
//
// The plan: the request carries the output limit, the sampling
// parameters, the reasoning parameter and the tools. The limit is
// always computed, the tools are always in a stable order, and the
// reasoning parameter follows the model's reasoning control.

// requestSettings is the model info one request is shaped by, with the
// quirks it names.
//
// The request carries its own model info when the core supplies one; a
// direct consumer that passes none keeps the transport's configured
// one.
type requestSettings struct {
	// info is the model's info.
	info poo.ModelInfo
	// quirks are the profiles the model info named, in its order.
	quirks []quirkProfile
}

// requestSettings returns the settings a request is shaped by.
func (t *Transport) requestSettings(req poo.ChatRequest) requestSettings {
	info := req.ModelInfo
	if zeroModelInfo(info) {
		info = t.cfg.ModelInfo
	}
	return requestSettings{info: info, quirks: quirksFor(info.Quirks, t.logger)}
}

// zeroModelInfo reports that a request carried no model info.
func zeroModelInfo(info poo.ModelInfo) bool {
	return info.Name == "" &&
		info.Endpoint == "" &&
		info.ModelID == "" &&
		info.API == "" &&
		len(info.Ops) == 0 &&
		len(info.Inputs) == 0 &&
		info.ContextLimit == 0 &&
		info.MaxOutput == 0 &&
		info.Sampling == poo.Sampling{} &&
		info.PrefixCache == nil &&
		info.Tools == poo.ToolCalling{} &&
		info.Reasoning == poo.ReasoningControl{} &&
		info.Temperature == "" &&
		info.PromptCache == "" &&
		info.PromptVariant == "" &&
		len(info.Quirks) == 0 &&
		len(info.Source) == 0
}

// reasoningSetting returns the reasoning setting the request uses.
//
// The configured setting wins, and the model info's reasoning control
// decides how it is spelled.
func (t *Transport) reasoningSetting() string {
	if t.cfg.Reasoning != "" {
		return t.cfg.Reasoning
	}
	return t.cfg.ReasoningEffort
}

// shapeReasoning sets the parameters that turn the reasoning on.
//
// The model info holds the rule: the setting is mapped through the
// model's reasoning control, and the fields the mapping names are the
// ones the request carries.
//
// A setting the model cannot honour is a configuration error, and the
// request never goes out.
func (t *Transport) shapeReasoning(info poo.ModelInfo, shape *requestShape) error {
	fields, err := info.ReasoningFields(t.reasoningSetting())
	if err != nil {
		return configErrorf("%s", reasonError(err))
	}
	for _, field := range fields {
		shape.setParam(field.Path, field.Value)
	}
	return nil
}

// reasonError renders a configuration error for the request's failure.
//
// The error names the setting that is at fault, so the reader knows
// what to change.
func reasonError(err error) string {
	var cfg *poo.ConfigError
	if errors.As(err, &cfg) {
		return cfg.Field + ": " + cfg.Message
	}
	return err.Error()
}

// shapeSampling sets the sampling parameters the request carries.
//
// The model info holds the rule: the model's own defaults are the
// base, the configured overrides win over them, and a model that takes
// no temperature never gets one.
func (t *Transport) shapeSampling(info poo.ModelInfo, shape *requestShape) {
	sampling := info.EffectiveSampling(t.cfg.Sampling)
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
// model info's controls (the reasoning parameter, the sampling
// parameters), and the quirks the model info named, in its order.
func (t *Transport) buildBody(req poo.ChatRequest) ([]byte, error) {
	return t.buildBodyWith(req, t.requestSettings(req))
}

// buildBodyWith turns a request into the wire payload under the settings
// the caller already resolved.
func (t *Transport) buildBodyWith(req poo.ChatRequest, settings requestSettings) ([]byte, error) {
	model := req.Model
	if model == "" {
		model = t.cfg.Model
	}
	out := chatRequest{
		Model:     model,
		MaxTokens: t.maxTokens(req, settings.info),
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
	if mode := t.toolsMode(settings.info); mode == "native" || mode == "auto" {
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
	if err := t.shapeReasoning(settings.info, shape); err != nil {
		return nil, err
	}
	t.shapeSampling(settings.info, shape)
	for _, quirk := range settings.quirks {
		if quirk.shapeRequest != nil {
			quirk.shapeRequest(shape)
		}
	}
	if len(out.Params) == 0 {
		out.Params = nil
	}

	return json.Marshal(out)
}

// maxTokens returns the output limit the request asks for.
//
// The caller computes the limit, because only it knows the size of
// the history. A request that arrives without one gets the model's own
// limit, or the default, and never the endpoint's.
//
// The limit is at least one token: a request asks for room, and a
// room of zero is no room at all.
func (t *Transport) maxTokens(req poo.ChatRequest, info poo.ModelInfo) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	limit := info.MaxTokens(0)
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
