package prompt_test

import (
	"testing"

	"github.com/sdougbrown/poo/prompt"
)

// TestTheShippedTemplatesRender covers the package's own prose: the
// templates a package compiles when it loads render with the
// environment.
func TestTheShippedTemplatesRender(t *testing.T) {
	env := prompt.Env{
		Cwd:   "/work",
		OS:    "linux",
		Date:  "2026-02-03",
		Model: "gemma4",
		Tools: []string{"bash"},
	}
	for name, tmpl := range map[string]*prompt.Template{
		"base":        prompt.Base(),
		"environment": prompt.Environment(),
	} {
		text, err := tmpl.RenderError(env)
		if err != nil {
			t.Errorf("the %s template renders with the environment: %v", name, err)
		}
		if text == "" {
			t.Errorf("the %s template renders nothing", name)
		}
	}
}
