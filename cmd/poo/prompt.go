package main

import (
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
)

// systemPrompt is the operating contract the model works under.
//
// The full prompt builder, with named sections and per-tool contributions,
// arrives in Stage 4. The skeleton ships one short contract, in the shape
// mini-swe-agent uses: how to work, read narrowly, every bash call is a
// fresh shell, and what "done" looks like.
//
// In the text and auto modes the model cannot call a tool through the
// wire format, so the tools are described here and the model answers with
// a call written out. The format is the one the transport's parser reads.
// The native mode needs neither: the transport sends the tools in the
// request, so the prompt says nothing about them.
func systemPrompt(modelID string, tools []poo.ToolSpec, toolsMode string) string {
	var b strings.Builder

	b.WriteString("You are a coding agent working in a shell. You solve the task by inspecting the repository and changing what is needed.\n\n")
	b.WriteString("How to work:\n")
	b.WriteString("- Inspect before you edit. Read the files that matter and the code around the place you will change.\n")
	b.WriteString("- Prefer narrow reads over whole files. Use the read tool for a specific line range, and use bash with grep, sed, head or tail when you need to search or slice.\n")
	b.WriteString("- Make the smallest change that solves the task, then check that it works.\n")
	b.WriteString("- Work out what is wrong before you try to fix it. If a command fails, read the error and adapt.\n\n")

	b.WriteString("Bash:\n")
	b.WriteString("- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n")
	b.WriteString("- Background processes are killed when the command returns.\n")
	b.WriteString("- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges.\n\n")

	if len(tools) > 0 && toolsMode != "native" {
		b.WriteString("Tools:\n")
		for _, t := range tools {
			fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
		}
		b.WriteString("\nCall a tool by writing a <tool_call> block holding a JSON object with a name and an arguments object, for example:\n")
		b.WriteString("<tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls -la\"}}</tool_call>\n")
		b.WriteString("Call one tool at a time, and wait for its result before the next call.\n\n")
	}

	b.WriteString(jobsSection(tools))

	b.WriteString("Done means:\n")
	b.WriteString("- The task is actually complete, not just started.\n")
	b.WriteString("- You have verified the change, for example by running the project's tests or the command that exercises the behaviour.\n")
	b.WriteString("- You then stop and say what you did, in a few sentences. Do not keep going after the task is done.\n")

	if modelID != "" {
		fmt.Fprintf(&b, "\nYou are running as the model %s.\n", modelID)
	}
	return b.String()
}

// jobsSection renders the section that tells the model about the
// background jobs.
//
// A session with no job-capable tool has no jobs, so the section is
// empty and the prompt does not mention them.
func jobsSection(tools []poo.ToolSpec) string {
	allowed := false
	for _, t := range tools {
		if t.Background == poo.BackgroundAllowed || t.Background == poo.BackgroundAlways {
			allowed = true
			break
		}
	}
	if !allowed {
		return ""
	}
	// The core owns the section, so the model reads the same guidance
	// whatever built the prompt.
	return poo.JobsPromptSection()
}

// newSessionID returns a fresh identifier for a session.
func newSessionID() string {
	return poo.NewULID()
}
