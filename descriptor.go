package poo

// ModelInfo describes one model the harness can use.
//
// Every model resolves to one ModelInfo: the catalog builds it from its
// curated entries, the endpoint's discovery and the settings, and the
// transports and the session read it. A zero field means unknown.
type ModelInfo struct {
	// Name is the name the settings and the wire use for the model.
	Name string `json:"name"`
	// Endpoint is the base URL of the endpoint that serves the model.
	Endpoint string `json:"endpoint"`
	// ModelID is what the endpoint calls the model.
	ModelID string `json:"model_id"`
	// API is the wire API the model is reached through.
	API API `json:"api"`
	// Ops are the operations the model serves.
	Ops []Op `json:"ops,omitempty"`
	// Inputs are the modalities the model reads.
	Inputs []Modality `json:"inputs,omitempty"`
	// ContextLimit is the model's context window in tokens.
	ContextLimit int `json:"context_limit,omitempty"`
	// MaxOutput is the most tokens the model writes in one response. Zero
	// means unknown, and the computed max_tokens is then capped at
	// DefaultMaxOutput, never left to the endpoint's default.
	MaxOutput int `json:"max_output,omitempty"`
	// Sampling holds the model's default sampling parameters.
	Sampling Sampling `json:"sampling,omitempty"`
	// PrefixCache reports whether the endpoint caches prompt prefixes.
	// Nil means unknown.
	PrefixCache *bool `json:"prefix_cache,omitempty"`
	// Tools is how the model makes tool calls.
	Tools ToolCalling `json:"tools,omitempty"`
	// Reasoning is how the model's reasoning is controlled.
	Reasoning ReasoningControl `json:"reasoning,omitempty"`
	// Temperature is whether the model accepts a temperature.
	Temperature TempControl `json:"temperature,omitempty"`
	// PromptCache is how the model's prompt cache is used.
	PromptCache CacheMode `json:"prompt_cache,omitempty"`
	// PromptVariant selects the prompt sections' variant text.
	PromptVariant string `json:"prompt_variant,omitempty"`
	// Quirks are the named quirk profiles the transport applies, in order.
	Quirks []string `json:"quirks,omitempty"`
	// Source names the catalog layers that set a field, by field name.
	Source map[string]string `json:"source,omitempty"`
}

// Op names an operation a model serves.
type Op string

const (
	// OpChat is a chat completion.
	OpChat Op = "chat"
	// OpSystemOne is a systemone decision call.
	OpSystemOne Op = "systemone"
	// OpDecisions is a decision made through chat.
	OpDecisions Op = "decisions"
	// OpCompact is a native compaction.
	OpCompact Op = "compact"
)

// Modality names a kind of input a model reads.
type Modality string

const (
	// ModalityText is text.
	ModalityText Modality = "text"
	// ModalityImage is an image.
	ModalityImage Modality = "image"
	// ModalityAudio is audio.
	ModalityAudio Modality = "audio"
)

// Sampling holds sampling parameters. A nil field is not sent.
type Sampling struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *float64 `json:"top_k,omitempty"`
	MinP        *float64 `json:"min_p,omitempty"`
}

// ToolCalling is how a model makes tool calls.
type ToolCalling struct {
	// Mode is native, text, auto or none.
	Mode ToolsMode `json:"mode,omitempty"`
	// TextFormat is the format of text tool calls: hermes, qwen_xml,
	// mistral, json or auto.
	TextFormat string `json:"text_format,omitempty"`
	// Parallel reports whether the model makes parallel tool calls.
	Parallel bool `json:"parallel,omitempty"`
}

// ToolsMode is how a session asks a model for tool calls.
type ToolsMode string

const (
	// ToolsNative sends the tools and reads the wire's tool calls.
	ToolsNative ToolsMode = "native"
	// ToolsText describes the tools in the prompt and parses the calls
	// from the text.
	ToolsText ToolsMode = "text"
	// ToolsAuto sends the tools and parses the text when the wire carries
	// no tool calls.
	ToolsAuto ToolsMode = "auto"
	// ToolsNone sends no tools.
	ToolsNone ToolsMode = "none"
)

// ReasoningControl is how a model's reasoning is controlled.
type ReasoningControl struct {
	// Mode is none, toggle, effort or budget.
	Mode ReasoningMode `json:"mode,omitempty"`
	// Param is how this API and model spell the parameter, for example
	// "reasoning_effort" or "chat_template_kwargs.enable_thinking".
	Param string `json:"param,omitempty"`
}

// ReasoningMode names a way of controlling reasoning.
type ReasoningMode string

const (
	// ReasoningNone means the model's reasoning cannot be controlled.
	ReasoningNone ReasoningMode = "none"
	// ReasoningToggle turns reasoning on or off.
	ReasoningToggle ReasoningMode = "toggle"
	// ReasoningEffort sets a reasoning effort level.
	ReasoningEffort ReasoningMode = "effort"
	// ReasoningBudget sets a reasoning token budget.
	ReasoningBudget ReasoningMode = "budget"
)

// TempControl is whether a model accepts a temperature.
type TempControl string

const (
	// TempFree accepts any temperature.
	TempFree TempControl = "free"
	// TempFixed takes no temperature: none is ever sent.
	TempFixed TempControl = "fixed"
	// TempUnsupported rejects a temperature.
	TempUnsupported TempControl = "unsupported"
)

// CacheMode is how a model's prompt cache is used.
type CacheMode string

const (
	// CacheAuto needs no request changes: the prefix stays stable.
	CacheAuto CacheMode = "auto"
	// CacheExplicit places cache breakpoints in the request.
	CacheExplicit CacheMode = "explicit"
	// CacheNone means the model has no prompt cache.
	CacheNone CacheMode = "none"
)

// HasOp reports whether the model serves an operation.
func (m ModelInfo) HasOp(op Op) bool {
	for _, o := range m.Ops {
		if o == op {
			return true
		}
	}
	return false
}
