package prompt_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sdougbrown/poo/prompt"
)

// The loader maps a directory of prose onto the builder's sections: a
// file is named after the section it replaces, and a dot in its name
// names one of the section's variants.

// baseSources are the sections the tests' directories may replace.
func baseSources() prompt.Sources {
	return prompt.Sources{
		"base":        nil,
		"environment": nil,
		"tools.bash":  nil,
		"catalog":     nil,
	}
}

// loadedBase returns a builder with the embedded defaults and the
// directory applied.
func loadedBase(t *testing.T, files fstest.MapFS) *prompt.Builder {
	t.Helper()
	dir, err := prompt.Load(files, baseSources())
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	b := prompt.New(prompt.Env{})
	b.SetTemplateDefault("base", prompt.Base())
	dir.Apply(b)
	return b
}

// TestALoadedFileReplacesItsSection covers the rule that a file named
// after a section replaces that section's default text.
func TestALoadedFileReplacesItsSection(t *testing.T) {
	b := loadedBase(t, fstest.MapFS{
		"base.md": {Data: []byte("a base of my own\n")},
	})
	if got := b.Render(); got != "a base of my own\n" {
		t.Errorf("the prompt is %q, want the loaded base", got)
	}
}

// TestALoadedToolFileReplacesTheToolsSection covers the path mapping: a
// directory names the section's prefix, so tools/bash.md is the
// "tools.bash" section.
func TestALoadedToolFileReplacesTheToolsSection(t *testing.T) {
	env := prompt.Env{Tools: []string{"bash"}}
	dir, err := prompt.Load(fstest.MapFS{
		"tools/bash.md": {Data: []byte("a shell of my own\n")},
	}, baseSources())
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	b := prompt.New(env)
	b.SetTemplateDefault("tools.bash", prompt.MustCompile("bash", "the shell\n", nil))
	dir.Apply(b)

	if got := b.Render(); got != "a shell of my own\n" {
		t.Errorf("the prompt is %q, want the loaded section", got)
	}
}

// TestALoadedVariantReplacesOnlyTheAlternative covers the variant files:
// a sibling named after the variant replaces that alternative and leaves
// the default text alone.
func TestALoadedVariantReplacesOnlyTheAlternative(t *testing.T) {
	b := loadedBase(t, fstest.MapFS{
		"base.md":             {Data: []byte("the default base\n")},
		"base.small-local.md": {Data: []byte("the small local base\n")},
	})
	if got := b.Render(); got != "the default base\n" {
		t.Errorf("the prompt is %q, want the default base", got)
	}
	small := b.WithVariant("small-local")
	if got := small.Render(); got != "the small local base\n" {
		t.Errorf("the prompt in the variant is %q, want the small local base", got)
	}
}

// TestAMissingVariantFallsBackToTheDefault covers the fallback: a
// section that carries no alternative for the variant renders its
// default text, loaded or embedded.
func TestAMissingVariantFallsBackToTheDefault(t *testing.T) {
	b := loadedBase(t, fstest.MapFS{
		"base.md": {Data: []byte("the default base\n")},
	})
	if got := b.WithVariant("small-local").Render(); got != "the default base\n" {
		t.Errorf("the prompt in the variant is %q, want the default base", got)
	}

	embedded := loadedBase(t, fstest.MapFS{})
	if got := embedded.WithVariant("small-local").Render(); !strings.Contains(got, "You are a coding agent.") {
		t.Errorf("the variant without a file is %q, want the embedded base", got)
	}
}

// TestALoadedTemplateRendersWithTheEnvironment covers the templates: a
// file's actions render with the environment, as the embedded
// environment section does.
func TestALoadedTemplateRendersWithTheEnvironment(t *testing.T) {
	env := prompt.Env{Model: "gemma4"}
	dir, err := prompt.Load(fstest.MapFS{
		"environment.md": {Data: []byte("Model: {{.Model}}")},
	}, baseSources())
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	b := prompt.New(env)
	dir.Apply(b)

	want := "Model: gemma4\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestALoadedTemplateKeepsTheSectionsData covers the rule that a
// replacement renders with the data its section carries: the catalog's
// template reads the catalog's own data, not the environment.
func TestALoadedTemplateKeepsTheSectionsData(t *testing.T) {
	type data struct{ Count int }
	dir, err := prompt.Load(fstest.MapFS{
		"catalog.md": {Data: []byte("{{.Count}} tools")},
	}, prompt.Sources{"catalog": func(prompt.Env) any { return data{Count: 3} }})
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	b := prompt.New(prompt.Env{})
	dir.Apply(b)

	want := "3 tools\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestAnUnknownFileIsAnError covers the rule that a file whose name is
// no section is an error that names the file.
func TestAnUnknownFileIsAnError(t *testing.T) {
	_, err := prompt.Load(fstest.MapFS{
		"nope.md": {Data: []byte("text")},
	}, baseSources())
	if err == nil {
		t.Fatal("loading a directory with an unknown file succeeded")
	}
	for _, want := range []string{"nope.md", `unknown section "nope"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not name %q", err, want)
		}
	}
}

// TestAFileThatIsNotMarkdownIsAnError covers the file extension: a
// prompt file is markdown, whatever its name.
func TestAFileThatIsNotMarkdownIsAnError(t *testing.T) {
	_, err := prompt.Load(fstest.MapFS{
		"base.txt": {Data: []byte("text")},
	}, baseSources())
	if err == nil {
		t.Fatal("loading a directory with a text file succeeded")
	}
	if !strings.Contains(err.Error(), "base.txt") {
		t.Errorf("the error %q does not name the file", err)
	}
}

// TestTheFinalNewlineIsNotPartOfTheSection covers the newline rule: a
// file ends with one, and the section does not, so a file that ends with
// a blank line keeps the newline it meant.
func TestTheFinalNewlineIsNotPartOfTheSection(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{"one newline", "text\n", "text\n"},
		{"a blank line", "text\n\n", "text\n\n"},
		{"no newline", "text", "text\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := loadedBase(t, fstest.MapFS{
				"base.md": {Data: []byte(tc.text)},
			})
			if got := b.Render(); got != tc.want {
				t.Errorf("the prompt is %q, want %q", got, tc.want)
			}
		})
	}
}
