package grep

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// section is the guidance the tool brings to the system prompt.
//
// The tool shows where it searched and what it left out, so the model
// can narrow the search and carry on.
var section = prompt.MustCompile("grep", sectionText, nil)

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section.Render(env)
}
