package bash_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestThePromptSectionIsTheGuidance covers the plan's prompt rule: the
// tool brings its own section, and the section says what the shell is.
func TestThePromptSectionIsTheGuidance(t *testing.T) {
	section := bash.New().PromptSection(prompt.Env{})

	for _, want := range []string{
		"fresh shell",
		"Background processes are killed when the command returns",
		`"background": true`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the section does not say %q:\n%s", want, section)
		}
	}
}
