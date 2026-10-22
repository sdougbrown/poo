package bash

import (
	"github.com/sdougbrown/poo/prompt"
)

// bashSection is the guidance the bash tool brings to the prompt.
//
// The section says what the shell is: a fresh one every call, with
// nothing carried over. It says what happens to a background process,
// and it says how long work is run instead.
const bashSection = "Bash:\n" +
	"- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n" +
	"- Background processes are killed when the command returns.\n" +
	"- Work that has to keep running belongs in a background call: pass \"background\": true and the call returns at once, and the result arrives later as a message.\n" +
	"- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges."

// PromptSection renders the section that tells the model about the
// bash tool.
//
// The section follows the tool: a build that leaves the bash tool out
// leaves its guidance out too.
func (t *Tool) PromptSection(env prompt.Env) string {
	return bashSection
}
