package poo

import (
	"fmt"
	"strconv"
	"strings"
)

// The paths the reasoning control uses in a request body.
//
// A model that spells the control differently carries its own path in
// its ReasoningControl, and these are the paths the core uses when it
// does not.
const (
	// ReasoningPathEffort is the effort field of the OpenAI-compatible
	// chat completions API.
	ReasoningPathEffort = "reasoning_effort"
	// ReasoningPathBudget is the thinking budget field of the Anthropic
	// messages API.
	ReasoningPathBudget = "thinking.budget_tokens"
	// ReasoningPathToggle is the chat template's thinking switch, which
	// the local servers read.
	ReasoningPathToggle = "chat_template_kwargs.enable_thinking"
)

// MaxTokens computes the output room for a request against a model.
//
// The room is the model's own output limit, or DefaultMaxOutput when
// the model declares none, and it never exceeds the window that is left
// after the prompt. The value is always computed, so a request always
// carries a max_tokens and never the endpoint's own default.
func MaxTokens(info ModelInfo, estimatedInput int) int {
	limit := info.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	if info.ContextLimit > 0 {
		room := info.ContextLimit - estimatedInput - OutputReserve
		if room < limit {
			limit = room
		}
	}
	return limit
}

// MaxTokens computes the output room for a request against the model.
func (m ModelInfo) MaxTokens(estimatedInput int) int {
	return MaxTokens(m, estimatedInput)
}

// EffectiveSampling returns the sampling parameters a request carries.
//
// The settings override the model's defaults, parameter by parameter: a
// setting that names a parameter replaces the model's default for that
// parameter alone and leaves the rest alone. A model that takes no
// temperature never gets one, so a fixed or an unsupported temperature
// drops the override as well.
func (m ModelInfo) EffectiveSampling(override Sampling) Sampling {
	out := m.Sampling
	if override.Temperature != nil {
		out.Temperature = override.Temperature
	}
	if override.TopP != nil {
		out.TopP = override.TopP
	}
	if override.TopK != nil {
		out.TopK = override.TopK
	}
	if override.MinP != nil {
		out.MinP = override.MinP
	}
	switch m.Temperature {
	case TempFixed, TempUnsupported:
		out.Temperature = nil
	}
	return out
}

// ReasoningLevelNames are the named reasoning levels poo understands,
// lowest first.
//
// A level is one of these, or a token budget: a positive number of tokens
// the model may think within. The order is the order a model's own levels
// climb, so a client that cycles through them goes from the least
// thinking to the most.
var ReasoningLevelNames = []string{"off", "minimal", "low", "medium", "high", "xhigh"}

// ReasoningSetting is the settings' reasoning value.
//
// The setting is off, an effort level or a token budget. The zero value
// is off, so a model that does not think needs no setting at all.
type ReasoningSetting struct {
	// Off means the model does not think.
	Off bool
	// Effort is the effort level: low, medium or high.
	Effort string
	// Budget is the token budget the model may think within.
	Budget int
}

// ParseReasoning parses a reasoning setting.
//
// The setting is one of the named levels, or a token budget. An empty value
// is off, and a value that is none of those is a configuration error.
func ParseReasoning(value string) (ReasoningSetting, error) {
	setting := strings.ToLower(strings.TrimSpace(value))
	if setting == "" {
		return ReasoningSetting{Off: true}, nil
	}
	for _, level := range ReasoningLevelNames {
		if setting != level {
			continue
		}
		if level == "off" {
			return ReasoningSetting{Off: true}, nil
		}
		return ReasoningSetting{Effort: level}, nil
	}
	budget, err := strconv.Atoi(setting)
	if err != nil || budget <= 0 {
		return ReasoningSetting{}, &ConfigError{
			Field: "reasoning.chat",
			Message: fmt.Sprintf("%q is not a reasoning setting: use %s or a token budget",
				value, strings.Join(ReasoningLevelNames, ", ")),
		}
	}
	return ReasoningSetting{Budget: budget}, nil
}

// ReasoningField is one request field that carries the reasoning
// control.
type ReasoningField struct {
	// Path is the field's path in the request body, e.g.
	// "reasoning_effort" or "chat_template_kwargs.enable_thinking".
	Path string
	// Value is the field's value: an effort level, a token budget or a
	// boolean.
	Value any
}

// ReasoningFields returns the request fields that carry the reasoning
// setting.
//
// The mapping is:
//
//	setting        none            toggle                     effort                    budget
//	off            no field        enable_thinking=false      no field                  no field
//	named level    config error    enable_thinking=true       reasoning_effort=<level>  config error
//	<budget>       config error    enable_thinking=true       config error              budget=<n>
//
// The named levels are ReasoningLevelNames, and a level the model declares
// is one the model takes: a model whose endpoint knows a level poo does not
// name, such as pi's max, is sent that level by name. A model whose reasoning
// cannot be controlled rejects every setting but off: the setting asks for
// something the model cannot do. An effort model and a budget model each take
// their own kind of setting, and a setting of the other kind is a
// configuration error as well.
func (m ModelInfo) ReasoningFields(setting string) ([]ReasoningField, error) {
	if level, ok := m.declaredLevel(setting); ok {
		switch m.Reasoning.Mode {
		case ReasoningEffort:
			if level == "off" {
				return nil, nil
			}
			return []ReasoningField{{
				Path:  m.reasoningPath(ReasoningPathEffort),
				Value: level,
			}}, nil
		case ReasoningToggle:
			return []ReasoningField{{
				Path:  m.reasoningPath(ReasoningPathToggle),
				Value: level != "off",
			}}, nil
		}
		// A model that controls no reasoning sends nothing, and a budget
		// model takes no level by name.
		if m.Reasoning.Mode == "" || m.Reasoning.Mode == ReasoningNone {
			return nil, nil
		}
	}
	parsed, err := ParseReasoning(setting)
	if err != nil {
		return nil, err
	}
	mode := m.Reasoning.Mode
	if mode == "" {
		mode = ReasoningNone
	}
	switch mode {
	case ReasoningNone:
		if !parsed.Off {
			return nil, m.reasoningError(setting, "the model does not control its reasoning")
		}
		return nil, nil
	case ReasoningToggle:
		return []ReasoningField{{
			Path:  m.reasoningPath(ReasoningPathToggle),
			Value: !parsed.Off,
		}}, nil
	case ReasoningEffort:
		if parsed.Off {
			return nil, nil
		}
		if parsed.Budget > 0 {
			return nil, m.reasoningError(setting, "the model takes an effort level, not a token budget")
		}
		return []ReasoningField{{
			Path:  m.reasoningPath(ReasoningPathEffort),
			Value: parsed.Effort,
		}}, nil
	case ReasoningBudget:
		if parsed.Off {
			return nil, nil
		}
		if parsed.Effort != "" {
			return nil, m.reasoningError(setting, "the model takes a token budget, not an effort level")
		}
		return []ReasoningField{{
			Path:  m.reasoningPath(ReasoningPathBudget),
			Value: parsed.Budget,
		}}, nil
	}
	return nil, &ConfigError{
		Field:   "reasoning.chat",
		Message: fmt.Sprintf("%q is not a reasoning control", mode),
	}
}

// declaredLevel returns the setting's level when it names one of the levels
// the model declares, lowercased.
//
// A model that declares no levels has nothing to say here, and the setting
// is parsed the way poo names levels.
func (m ModelInfo) declaredLevel(setting string) (string, bool) {
	want := strings.ToLower(strings.TrimSpace(setting))
	for _, level := range m.ReasoningLevels {
		if strings.ToLower(strings.TrimSpace(level)) == want {
			return want, true
		}
	}
	return "", false
}

// reasoningPath returns the path the model's reasoning control uses.
func (m ModelInfo) reasoningPath(fallback string) string {
	if m.Reasoning.Param != "" {
		return m.Reasoning.Param
	}
	return fallback
}

// CheckReasoning reports whether the model can run at a reasoning level.
//
// The level is one of ReasoningLevelNames or a token budget. A model that
// declares its own levels takes one of those and no other: a model whose
// endpoint knows xhigh may not know minimal, and a level outside the list
// is refused rather than sent and ignored. A model that declares none is
// checked by its reasoning control, which says what kind of level it
// takes, and a model that controls no reasoning takes no level but off.
//
// The refusal is a configuration error, and it names the levels the model
// does take, so a client can offer them.
func (m ModelInfo) CheckReasoning(level string) error {
	setting := strings.ToLower(strings.TrimSpace(level))
	if _, ok := m.declaredLevel(setting); ok {
		return nil
	}
	if levels := m.ReasoningLevels; len(levels) > 0 {
		return &ConfigError{
			Field: "reasoning.chat",
			Message: fmt.Sprintf("%q is not a reasoning level for %s: it takes %s",
				level, modelLabel(m), strings.Join(levels, ", ")),
		}
	}
	parsed, err := ParseReasoning(setting)
	if err != nil {
		return err
	}
	mode := m.Reasoning.Mode
	if mode == "" {
		mode = ReasoningNone
	}
	if mode == ReasoningNone {
		if parsed.Off {
			return nil
		}
		return m.reasoningError(setting, "the model does not control its reasoning")
	}
	// The check is the run's own rule: the fields the run would send
	// are built here, so a level the model cannot take — a budget for
	// an effort model, an effort name for a budget one — is refused
	// by the same validation that refuses it at run time.
	_, err = m.ReasoningFields(setting)
	return err
}

// reasoningError reports a reasoning setting the model cannot honour.
func (m ModelInfo) reasoningError(setting, why string) *ConfigError {
	model := m.Name
	if model == "" {
		model = "the model"
	}
	return &ConfigError{
		Field:   "reasoning.chat",
		Message: fmt.Sprintf("the %q setting cannot be honoured for %s: %s", setting, model, why),
	}
}
