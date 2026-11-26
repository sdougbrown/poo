package write

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// section is the guidance the tool brings to the system prompt.
//
// The tool creates a file and refuses one that is already there, so the
// model is told where the boundary to Edit sits.
var section = prompt.MustCompile("write", sectionText, nil)

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section.Render(env)
}
