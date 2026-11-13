package read

import (
	_ "embed"

	"github.com/sdougbrown/poo"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// modalityLines are the lines a session whose model reads that modality adds. A
// read returns the file itself, so the model is told it can look at one or
// listen to one.
var modalityLines = []struct {
	modality poo.Modality
	text     string
}{
	{poo.ModalityImage, "- An image file comes back as the image itself, so a screenshot is examined rather than described."},
	{poo.ModalityAudio, "- An audio file comes back as the recording itself, so a clip is listened to rather than transcribed first."},
}

// section is the guidance the tool brings to the system prompt.
//
// The tool shows a range of a file and names it, so the model knows
// where it is in the file and can carry on from there.
var section = prompt.MustCompile("read", sectionText, nil)

// PromptSection renders the tool's own section of the system prompt.
//
// Each modality the model reads adds its own line to the section.
func (t *Tool) PromptSection(env prompt.Env) string {
	text := section.Render(env)
	for _, line := range modalityLines {
		if t.inputs[line.modality] {
			text += "\n" + line.text
		}
	}
	return text
}
