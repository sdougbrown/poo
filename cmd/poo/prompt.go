package main

import (
	"fmt"
	"strings"

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
			b.Set("tool-calls", toolCallsSection(tools))
		}
		if project != "" {
			b.Set("project", project)
		}
		b.Set("environment", environmentSection(modelID))
	}
}

// environmentSection renders the line that names the model.
//
// The core's own environment section names the working directory, the
// operating system and the date as well. This command's prompt has
// always named the model alone, and the eval baseline was measured with
// it.
func environmentSection(modelID string) string {
	return fmt.Sprintf("You are running as the model %s.", modelID)
}

// toolCallsSection renders the section that names the tools and the
// call format.
//
// The section is empty in the native mode: the transport sends the
// tools in the request, so the model reads them there.
func toolCallsSection(tools []poo.ToolSpec) string {
	var b strings.Builder
	b.WriteString("Tools:\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
	}
	b.WriteString("\nCall a tool by writing a <tool_call> block holding a JSON object with a name and an arguments object, for example:\n")
	b.WriteString("<tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls -la\"}}</tool_call>\n")
	b.WriteString("Call one tool at a time, and wait for its result before the next call.")
	return b.String()
}
