package catalog

import (
	"strings"

	"github.com/sdougbrown/poo"
)

// family is a rule about a family of models.
//
// A family is matched by the model's id, and the rule fills the fields
// every model of the family shares. The note says what the family's
// models need, and it is why the rule exists.
type family struct {
	// name is the family's name.
	name string
	// match reports whether a model id belongs to the family.
	match func(modelID string) bool
	// describe fills the fields the family knows.
	describe func(info *poo.ModelInfo)
	// note says what the family's models need.
	note string
}

// families are the curated rules for the model families poo knows.
//
// The rules are matched by the model's id, and every rule that matches
// fills its fields. A rule never clears a field an earlier rule set,
// so the rules may be listed in any order.
var families = []family{
	{
		name:  "gemma",
		match: hasPrefix("gemma"),
		describe: func(info *poo.ModelInfo) {
			info.ContextLimit = 262144
			info.Ops = []poo.Op{poo.OpChat, poo.OpSystemOne}
			info.Inputs = []poo.Modality{poo.ModalityText}
			info.Tools = poo.ToolCalling{Mode: poo.ToolsAuto, TextFormat: "auto"}
			info.Reasoning = poo.ReasoningControl{
				Mode:  poo.ReasoningToggle,
				Param: poo.ReasoningPathToggle,
			}
			info.Temperature = poo.TempFree
			info.PromptCache = poo.CacheAuto
		},
		note: "gemma4 writes its tool calls as text rather than through the wire " +
			"format, so the tools mode is auto and the text parser reads the calls. " +
			"It thinks only when the request asks it to, so the reasoning control is " +
			"the chat template's enable_thinking switch.",
	},
	{
		name:  "qwen",
		match: hasPrefix("qwen"),
		describe: func(info *poo.ModelInfo) {
			info.ContextLimit = 262144
			info.Ops = []poo.Op{poo.OpChat, poo.OpSystemOne}
			info.Inputs = []poo.Modality{poo.ModalityText}
			info.Tools = poo.ToolCalling{Mode: poo.ToolsNative, TextFormat: "auto"}
			info.Reasoning = poo.ReasoningControl{
				Mode:  poo.ReasoningToggle,
				Param: poo.ReasoningPathToggle,
			}
			info.Temperature = poo.TempFree
			info.PromptCache = poo.CacheAuto
		},
		note: "qwen streams its reasoning as reasoning_content, which the provider " +
			"keeps apart from the answer, so the transcript shows the model's " +
			"thinking.",
	},
	{
		name:  "minimax",
		match: hasPrefix("minimax"),
		describe: func(info *poo.ModelInfo) {
			info.ContextLimit = 1000000
			info.Ops = []poo.Op{poo.OpChat}
			info.Inputs = []poo.Modality{poo.ModalityText}
			info.Tools = poo.ToolCalling{Mode: poo.ToolsNative, TextFormat: "auto"}
			info.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningNone}
			info.Temperature = poo.TempFree
			info.PromptCache = poo.CacheAuto
		},
		note: "minimax thinks before it answers and takes no reasoning control, so " +
			"the catalog cannot turn the thinking off.",
	},
}

// hasPrefix returns a matcher for the model ids that start with a
// prefix.
func hasPrefix(prefix string) func(string) bool {
	return func(modelID string) bool {
		return strings.HasPrefix(strings.ToLower(modelID), prefix)
	}
}
