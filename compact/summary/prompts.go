package summary

import _ "embed"

// The strategy's prompts live as markdown beside the package, and
// scripts/lint-prompts.sh lints them as agent instructions.
//
// They are whole request prompts, not sections of the system prompt, so
// they are embedded as they are: prompt.Compile renders sections with an
// environment, and a summary request carries no environment.
var (
	// systemPrompt keeps the model in the summarizer's role.
	//
	//go:embed prompts/system.md
	systemPrompt string
	// summarizePrompt asks for the first summary of a session.
	//
	//go:embed prompts/summarize.md
	summarizePrompt string
	// updatePrompt asks for a summary that carries the previous one
	// forward.
	//
	//go:embed prompts/update.md
	updatePrompt string
)
