package prompt

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

// A section's prose is a text/template. The template's data is either
// the environment or a struct the section's owner builds; a file that
// names no field renders the same text whatever the data is.

//go:embed base.md
var baseText string

//go:embed environment.md
var environmentText string

// baseTemplate is the base section's default prose.
var baseTemplate = MustCompile("base", baseText, nil)

// environmentTemplate is the environment section's default prose.
var environmentTemplate = MustCompile("environment", environmentText, nil)

// Base returns the base section's default prose.
//
// The base is the operating contract the model works under. Its
// template renders with the environment.
func Base() *Template { return baseTemplate }

// Environment returns the environment section's default prose.
//
// The section tells the model where it is: the directory, the operating
// system, the date and the model. Its template renders with the
// environment.
func Environment() *Template { return environmentTemplate }

// Template is a section's prose, compiled.
//
// A template renders with the data its section was compiled with: the
// environment, or a struct the section's owner builds. The text a
// template renders is the section's text as it is, except that the
// file's final newline is not part of it.
type Template struct {
	// name is the section the template renders.
	name string
	// tmpl is the compiled template.
	tmpl *template.Template
	// data returns what the template renders with. Nil renders with
	// the environment.
	data func(Env) any
}

// Compile parses a section's prose.
//
// The data function returns what the template renders with, and nil
// renders with the environment. Compile renders the template once with
// a zero environment, so a template that names a field its data does
// not have is an error here and not in a run.
func Compile(name, text string, data func(Env) any) (*Template, error) {
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("prompt: the section %q: %w", name, err)
	}
	t := &Template{name: name, tmpl: tmpl, data: data}
	if _, err := t.render(Env{}); err != nil {
		return nil, err
	}
	return t, nil
}

// MustCompile is Compile for the prose a package owns.
//
// The embedded prose is compiled when the package loads, so a template
// that does not parse is a failure of the build and not of a run.
func MustCompile(name, text string, data func(Env) any) *Template {
	t, err := Compile(name, text, data)
	if err != nil {
		panic(err)
	}
	return t
}

// Data returns what the template renders with.
//
// Nil means the template renders with the environment. A loader uses
// it to compile a replacement file with the data its default carries.
func (t *Template) Data() func(Env) any { return t.data }

// Render returns the section's text in an environment.
//
// A prompt file ends with a newline, and the section it holds does not:
// Render drops one trailing newline, so a file that ends with a blank
// line keeps the newline it meant. A prompt file that names a field its
// data does not have is a bug in the prose, so Render panics; Compile
// has already run the template once, and a template that compiles
// renders.
func (t *Template) Render(env Env) string {
	text, err := t.render(env)
	if err != nil {
		panic(err)
	}
	return text
}

// render renders the template, dropping the file's final newline.
func (t *Template) render(env Env) (string, error) {
	data := any(env)
	if t.data != nil {
		data = t.data(env)
	}
	var b strings.Builder
	if err := t.tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("prompt: the section %q: %w", t.name, err)
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
