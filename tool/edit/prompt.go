package edit

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// section is the guidance the tool brings to the system prompt.
//
// The tool changes a file only when the text it was given is in the
// file, so the model is told how to write that text.
var section = prompt.MustCompile("edit", sectionText, nil)

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section.Render(env)
}
