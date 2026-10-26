package bash

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// section is the guidance the bash tool brings to the prompt.
//
// The section says what the shell is: a fresh one every call, with
// nothing carried over. It says what happens to a background process,
// and it says how long work is run instead.
var section = prompt.MustCompile("bash", sectionText, nil)

// PromptSection renders the section that tells the model about the
// bash tool.
//
// The section follows the tool: a build that leaves the bash tool out
// leaves its guidance out too.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section.Render(env)
}
