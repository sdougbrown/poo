package read

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// imageText is the line a session whose model reads images adds: the read
// returns the picture itself, so the model is told it can look at one.
const imageText = "- An image file comes back as the image itself, so a screenshot is examined rather than described.\n"

// section is the guidance the tool brings to the system prompt.
//
// The tool shows a range of a file and names it, so the model knows
// where it is in the file and can carry on from there.
var (
	section           = prompt.MustCompile("read", sectionText, nil)
	sectionWithImages = prompt.MustCompile("read", sectionText+imageText, nil)
)

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	if t.images {
		return sectionWithImages.Render(env)
	}
	return section.Render(env)
}
