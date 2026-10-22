package main

import (
	"crypto/rand"
	"encoding/hex"
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

// baseSection is the operating contract the model works under.
//
// It is the shape mini-swe-agent uses: what the model is, how to
// work, and what "done" looks like. The guidance for a tool is not
// here: a tool brings a section of its own, and a tool that is not
// linked takes its guidance with it.
const baseSection = "You are a coding agent. You solve the task by inspecting the code and changing what is needed.\n\n" +
	"How to work:\n" +
	"- Inspect before you edit. Read the code around the place you will change.\n" +
	"- Prefer narrow reads over whole files: read the lines that matter and search for what you need, rather than reading everything.\n" +
	"- Make the smallest change that solves the task, then check that it works.\n" +
	"- Work out what is wrong before you try to fix it. If a command fails, read the error and adapt.\n" +
	"- Keep going until the task is done. Do not stop to ask whether you should go on.\n" +
	"\n" +
	"Done means:\n" +
	"- The task is actually complete, not just started.\n" +
	"- You have verified the change, for example by running the project's tests or the command that exercises the behaviour.\n" +
	"- You then stop and say what you did, in a few sentences. Do not keep going after the task is done."

// bashSection is the guidance the bash tool brings.
//
// The tool itself brings this section once it has prompt guidance of
// its own. Until then the command holds it, so the prompt the model
// reads does not change.
const bashSection = "Bash:\n" +
	"- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n" +
	"- Background processes are killed when the command returns.\n" +
	"- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges."

// systemPrompt returns the operating contract the model works under.
//
// The prompt is a set of named sections, and the mutation puts them in
// the builder the session renders.
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
		b.Set("base", baseSection)
		b.Set(prompt.ToolSection("bash"), bashSection)
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

// newSessionID returns a fresh identifier for a session or a run.
//
// The identifier is random rather than a ULID: the plan's ULID arrives with
// the session seam in Stage 8, and nothing in the skeleton depends on
// the ordering.
func newSessionID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error (it ends the program if the system's source fails).
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
