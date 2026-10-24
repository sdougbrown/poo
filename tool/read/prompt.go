package read

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// section is the guidance the tool brings to the system prompt.
//
// The tool shows a range of a file and names it, so the model knows
// where it is in the file and can carry on from there.
var section = prompt.MustCompile("read", sectionText, nil)

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section.Render(env)
}
