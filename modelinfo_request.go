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

// fallbackOutputLimit is the output limit a session asks for after an
// endpoint rejects max_tokens without saying what it allows. It is the
// smallest default, so a model that takes any default takes it.
const fallbackOutputLimit = 8192

// DefaultMaxOutputFor returns the output limit of a model that declares
// none, from the model's context window in tokens.
//
// The limit grows with the window because a reasoning model's thinking
// counts against max_tokens, and the large-window models publish output
// limits to match: none with a window of 128k or more has a limit under
// 32000, and current hosted models allow at least 16k. A window under
// 64k gets 8192, a window from 64k up to 128k gets 16384, and a window
// of 128k or more gets 32768. An unknown window (zero or less) gets
// 16384.
func DefaultMaxOutputFor(contextLimit int) int {
	switch {
	case contextLimit <= 0:
		return 16384
	case contextLimit < 65536:
		return 8192
	case contextLimit < 131072:
		return 16384
	default:
		return 32768
	}
}

// OutputLimitSource names which limit a response stopped on.
type OutputLimitSource string

const (
	// OutputLimitDefault is DefaultMaxOutputFor the model's context
	// window, used because the model declares no output limit.
	OutputLimitDefault OutputLimitSource = "default"
	// OutputLimitModel is the output limit the model declares.
	OutputLimitModel OutputLimitSource = "model"
	// OutputLimitContext is the room the context window leaves after the
	// prompt, when that is under the model's output limit.
	OutputLimitContext OutputLimitSource = "context"
	// OutputLimitEndpoint is the endpoint's own limit, for a request
	// that carried none.
	OutputLimitEndpoint OutputLimitSource = "endpoint"
)

// OutputLimit is the output room a request carries and the limit that
// set it.
type OutputLimit struct {
	// Tokens is the request's max_tokens.
	Tokens int
	// Source is the limit that set Tokens.
	Source OutputLimitSource
	// Input is the estimated size of the prompt the room was computed
	// after, and Window the context window it was computed against.
	Input, Window int
}

// Truncation describes the stop of a response that hit the output limit.
//
// sent is the output limit the transport's request body carried, which
// is what the response stopped on; a body that carried none was stopped by
// the endpoint's own limit. The source is the settle's output_truncated
// source, with the cause after the colon, and the text says the cause in a
// sentence. A model with no declared output limit is told the setting that
// raises it: the model's entry when the settings declare the model, a
// declaration otherwise.
func (l OutputLimit) Truncation(info ModelInfo, sent int) SettleDetail {
	if sent <= 0 {
		l.Source = OutputLimitEndpoint
	} else {
		l.Tokens = sent
	}
	detail := SettleDetail{Source: string(ReasonOutputTruncated) + ":" + string(l.Source)}
	switch l.Source {
	case OutputLimitEndpoint:
		detail.Text = "the response stopped on the endpoint's own output limit; poo sends this endpoint none"
	case OutputLimitDefault:
		detail.Text = fmt.Sprintf("the response stopped at max_tokens %d, poo's default for a model with no max_output; %s to raise it",
			l.Tokens, outputSettingHint(info).clause)
	case OutputLimitContext:
		detail.Text = fmt.Sprintf("the response stopped at max_tokens %d, all the room the context window had left (about %d of %d tokens in use)",
			l.Tokens, l.Input, l.Window)
	default:
		detail.Text = fmt.Sprintf("the response stopped at max_tokens %d, the model's max_output", l.Tokens)
	}
	return detail
}

// outputSetting is how the user sets a model's own output limit.
type outputSetting struct {
	// clause names the setting, without a verb: "set models.X.max_output"
	// or the declaration that would create it.
	clause string
	// path is the settings path the clause sets, empty when the settings
	// declare no model for the clause to name.
	path string
}

// outputSettingHint names the setting that fixes a model's output limit.
//
// A model the settings declare gets its own max_output. Any other model has
// no entry to edit, so it gets the declaration that would create one with
// its endpoint, model_id and max_output. The truncation notice and the
// learned-limit warning both append the clause, so the setting is named the
// same way wherever a run explains an output limit.
func outputSettingHint(info ModelInfo) outputSetting {
	if !info.DeclaredInSettings() {
		return outputSetting{clause: "declare the model in settings with its endpoint, model_id and max_output"}
	}
	name := info.Name
	if name == "" {
		name = info.ModelID
	}
	path := fmt.Sprintf("models.%s.max_output", name)
	return outputSetting{clause: "set " + path, path: path}
}

// OutputCeiling returns the most output tokens a request for the model
// asks for before the room the window leaves is taken into account: the
// model's own limit, or DefaultMaxOutputFor its window when the model
// declares none.
func (m ModelInfo) OutputCeiling() int {
	if m.MaxOutput > 0 {
		return m.MaxOutput
	}
	return DefaultMaxOutputFor(m.ContextLimit)
}

// MaxTokens computes the output room for a request against a model.
//
// The room is the model's OutputLimit, and it never exceeds the window
// that is left after the prompt. The value is always computed, so a
// request always carries a max_tokens and never the endpoint's own
// default.
func MaxTokens(info ModelInfo, estimatedInput int) int {
	return info.OutputLimit(estimatedInput).Tokens
}

// OutputLimit computes the output room for a request against the model,
// and names the limit that set it.
//
// The model's own limit, or DefaultMaxOutputFor the model's context
// window when it declares none, gives the room; the room the window
// leaves after the prompt clamps it.
func (m ModelInfo) OutputLimit(estimatedInput int) OutputLimit {
	limit := OutputLimit{Tokens: m.MaxOutput, Source: OutputLimitModel, Input: estimatedInput, Window: m.ContextLimit}
	if limit.Tokens <= 0 {
		limit.Tokens, limit.Source = DefaultMaxOutputFor(m.ContextLimit), OutputLimitDefault
	}
	if m.ContextLimit > 0 {
		room := m.ContextLimit - estimatedInput - OutputReserve
		if room < limit.Tokens {
			limit.Tokens, limit.Source = room, OutputLimitContext
		}
	}
	return limit
}

// MaxTokens computes the output room for a request against the model.
func (m ModelInfo) MaxTokens(estimatedInput int) int {
	return m.OutputLimit(estimatedInput).Tokens
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
	// A declared level is the model's own list, and it still has to be
	// one the model's control can send: a budget model that declares a
	// name would pass a check that stopped here and then be refused when
	// the run builds its fields.
	if levels := m.ReasoningLevels; len(levels) > 0 {
		if _, ok := m.declaredLevel(setting); !ok {
			return &ConfigError{
				Field: "reasoning.chat",
				Message: fmt.Sprintf("%q is not a reasoning level for %s: it takes %s",
					level, modelLabel(m), strings.Join(levels, ", ")),
			}
		}
		if _, err := m.ReasoningFields(setting); err != nil {
			// The control cannot carry this name. The refusal names the
			// levels the model declares — that is the contract here — and
			// says why the control cannot send one of them.
			return &ConfigError{
				Field: "reasoning.chat",
				Message: fmt.Sprintf("%q is not a reasoning level %s can send: it takes %s, and its control sends a token budget rather than a level",
					level, modelLabel(m), strings.Join(levels, ", ")),
			}
		}
		return nil
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

// PromptLimit is the largest prompt a request to the model may carry: the
// smaller of the window and the endpoint's input cap, ignoring the one that
// is unknown. Zero means neither is known.
//
// The prompt-side checks read it: the compaction rule and the check a
// model switch makes. The output room of MaxTokens reads the window, which
// the prompt and the answer share.
func (m ModelInfo) PromptLimit() int {
	return PromptLimitOf(m.ContextLimit, m.InputLimit)
}

// PromptLimitOf is the smaller of a window and an input cap, where zero is
// unknown.
//
// A wire client that holds only the two numbers reads the same rule here,
// so the statusline and the session never disagree about the bound.
func PromptLimitOf(window, input int) int {
	if input > 0 && (window <= 0 || input < window) {
		return input
	}
	return window
}
