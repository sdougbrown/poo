package main

import (
	_ "embed"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// The system prompt is assembled from named sections, and the builder
// renders them in the order the plan fixes: the base, the tools, the
// catalog, the environment and the project, and then the sections of
// the consumer's own.
//
// The sections below are the ones this command sends. The base is the
// operating contract, in the shape mini-swe-agent uses, and the tools
// bring their own guidance.

//go:embed environment.md
var environmentText string

//go:embed toolcalls.md
var toolCallsText string

// environmentTemplate returns the line that names the model.
//
// The core's own environment section names the working directory, the
// operating system and the date as well. This command's prompt has
// always named the model alone, and the eval baseline was measured with
// it.
func environmentTemplate(modelID string) *prompt.Template {
	return prompt.MustCompile("environment", environmentText, func(prompt.Env) any {
		return prompt.Env{Model: modelID}
	})
}

// toolCallsData is the data the text mode's section renders with.
//
// Tools are the tool specs the run offers, in offer order.
type toolCallsData struct {
	Tools []poo.ToolSpec
}

// systemPrompt returns the sections the command brings to the prompt.
//
// The base and the tools' sections belong to the core and the tools: the
// session assembles them, so a session that is not this command gets the
// same operating contract and the same guidance. This mutation adds what
// only the command knows.
//
// In the text and auto modes the model cannot call a tool through the
// wire format, so the tools are described in a section of their own and
// the model answers with a call written out. The format is the one the
// transport's parser reads. The native mode needs neither: the
// transport sends the tools in the request, so the prompt says nothing
// about them.
//
// The project's instructions are a section of their own, and the
// section is there only when the caller loaded them.
func systemPrompt(modelID string, tools []poo.ToolSpec, toolsMode, project string) func(*prompt.Builder) {
	return func(b *prompt.Builder) {
		if len(tools) > 0 && toolsMode != "native" {
			b.SetTemplate("tool-calls", toolCallsTemplate(tools))
		}
		if project != "" {
			b.Set("project", project)
		}
		b.SetTemplate("environment", environmentTemplate(modelID))
	}
}

// toolCallsTemplate returns the section that names the tools and the
// call format.
//
// The section is empty in the native mode: the transport sends the
// tools in the request, so the model reads them there.
func toolCallsTemplate(tools []poo.ToolSpec) *prompt.Template {
	return prompt.MustCompile("tool-calls", toolCallsText, func(prompt.Env) any {
		return toolCallsData{Tools: tools}
	})
}
