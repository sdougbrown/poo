package mine

import _ "embed"

// The strategy's prompts live as markdown beside the package, and
// scripts/lint-prompts.sh lints them as agent instructions.
//
// They are whole request prompts, not sections of the system prompt, so
// they are embedded as they are: prompt.Compile renders sections with
// an environment, and a mining request carries no environment.
var (
	// systemPrompt keeps the model in the reconstruction's role.
	//
	//go:embed prompts/system.md
	systemPrompt string
	// pointerPrompt asks the fork what the reconstruction must keep.
	//
	//go:embed prompts/pointer.md
	pointerPrompt string
	// minePrompt asks for the first reconstruction of a session.
	//
	//go:embed prompts/mine.md
	minePrompt string
	// updatePrompt asks for a reconstruction that carries the previous
	// one forward.
	//
	//go:embed prompts/update.md
	updatePrompt string
)
