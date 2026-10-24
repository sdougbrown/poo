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
var environmentTemplate = MustCompileTrimmed("environment", environmentText, nil)

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
	// source is the file a loaded template came from, when it is not
	// the package's own prose.
	source string
	// tmpl is the compiled template.
	tmpl *template.Template
	// data returns what the template renders with. Nil renders with
	// the environment.
	data func(Env) any
	// trimTrailingNewlines drops every trailing newline from the
	// rendered text, not only the file's sentinel newline.
	trimTrailingNewlines bool
}

// Compile parses a section's prose.
//
// The data function returns what the template renders with, and nil
// renders with the environment. Compile walks the template's parse tree
// and checks every field chain against the data's type, so a template
// that provably names a field the data does not have is an error here
// and not in a run. The check covers every branch, including one the
// data would not take. A chain whose type the tree does not state — a
// variable, a map, a function's result — is the run's to report.
func Compile(name, text string, data func(Env) any) (*Template, error) {
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("prompt: the section %q: %w", name, err)
	}
	t := &Template{name: name, tmpl: tmpl, data: data}
	sample := any(Env{})
	if data != nil {
		sample = data(Env{})
	}
	if err := validateTemplate(t, sample); err != nil {
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

// CompileTrimmed parses prose whose section drops every trailing
// newline, not only the file's sentinel newline.
//
// A section whose last line is a value the caller supplies uses it, so a
// value that ends in a newline does not leave one behind. A file the
// loader reads keeps the ordinary rule: only its own final newline is
// dropped.
func CompileTrimmed(name, text string, data func(Env) any) (*Template, error) {
	t, err := Compile(name, text, data)
	if err != nil {
		return nil, err
	}
	t.trimTrailingNewlines = true
	return t, nil
}

// MustCompileTrimmed is CompileTrimmed for the prose a package owns.
func MustCompileTrimmed(name, text string, data func(Env) any) *Template {
	t, err := CompileTrimmed(name, text, data)
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
// line keeps the newline it meant.
//
// Render is the must-style path the packages that own their prose use:
// their templates are compiled when the package loads, and a failure
// here is a bug in the prose. A loaded file uses RenderError, because a
// template can still fail on data the session supplies, and a consumer's
// prose should not panic a session.
func (t *Template) Render(env Env) string {
	text, err := t.RenderError(env)
	if err != nil {
		panic(err)
	}
	return text
}

// RenderError returns the section's text in an environment, or the
// error the template reported.
//
// The error names the file the template came from, when it has one.
func (t *Template) RenderError(env Env) (string, error) {
	return t.render(env)
}

// render renders the template, dropping the file's final newline.
func (t *Template) render(env Env) (string, error) {
	data := any(env)
	if t.data != nil {
		data = t.data(env)
	}
	var b strings.Builder
	if err := t.tmpl.Execute(&b, data); err != nil {
		err = fmt.Errorf("prompt: the section %q: %w", t.name, err)
		if t.source != "" {
			err = fmt.Errorf("%s: %w", t.source, err)
		}
		return "", err
	}
	text := b.String()
	if t.trimTrailingNewlines {
		return strings.TrimRight(text, "\n"), nil
	}
	return strings.TrimSuffix(text, "\n"), nil
}
