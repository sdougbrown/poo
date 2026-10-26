package prompt_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"text/template"

	"github.com/sdougbrown/poo/prompt"
)

// The validator reports a field only when it can prove the access
// fails: a field or method name that does not exist on a type it knows
// exactly, reached through a chain it models. Everything else is the
// run's to report, because a false rejection stops a valid binary from
// building.

// reviewEntry, reviewCatalog, reviewMethodData and reviewKey are the
// data shapes the templates below read. They stand in for the sections
// whose owners supply their own data.
type reviewEntry struct {
	Name        string
	Description string
}

type reviewCatalog struct {
	Entries []reviewEntry
}

type reviewMethodData struct{}

// Label is a pointer-receiver method, which the template's method set
// carries and the validator must not refuse.
func (*reviewMethodData) Label() string { return "pointer method" }

type reviewKey struct{ Name string }

type reviewHolder struct {
	Any   any
	Items []any
}

// loadBase compiles a template as the base section, rendering with the
// given data.
func loadBase(t *testing.T, text string, data any) (*prompt.Dir, error) {
	t.Helper()
	return prompt.Load(fstest.MapFS{
		"base.md": {Data: []byte(text)},
	}, prompt.Sources{"base": func(prompt.Env) any { return data }})
}

// stdlibRender renders a template with the standard library, which is
// the behaviour the prompt's own render must match.
func stdlibRender(t *testing.T, text string, data any) string {
	t.Helper()
	tmpl, err := template.New("base").Parse(text)
	if err != nil {
		t.Fatalf("parsing the template with the standard library: %v", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatalf("rendering the template with the standard library: %v", err)
	}
	return b.String()
}

// TestValidTemplatesLoad covers the templates a validator must not
// refuse, including the cases the review recorded.
func TestValidTemplatesLoad(t *testing.T) {
	env := prompt.Env{Model: "model", Cwd: "/work", Tools: []string{"bash"}}
	catalog := reviewCatalog{Entries: []reviewEntry{{Name: "tool", Description: "desc"}}}
	keys := map[reviewKey]string{{Name: "key"}: "value"}
	for _, tc := range []struct {
		name string
		text string
		data any
	}{
		{"with", `{{with .Model}}{{.}}{{else}}{{.Cwd}}{{end}}`, env},
		{"with shadow", `{{$x := .}}{{with $x := .Model}}{{$x}}{{end}}{{$x.Model}}`, env},
		{"if shadow", `{{$x := .}}{{if true}}{{$x := .Model}}{{$x}}{{end}}{{$x.Model}}`, env},
		{"range shadow", `{{$x := .}}{{range $x := .Tools}}{{$x}}{{else}}{{len $x}}{{end}}{{$x.Model}}`, env},
		{"range else dot", `{{range .Tools}}{{.}}{{else}}{{.Cwd}}{{end}}`, prompt.Env{Cwd: "/empty"}},
		{"range else variable", `{{range $x := .Tools}}{{$x}}{{else}}{{len $x}}{{end}}`, prompt.Env{}},
		{"pipeline", `{{.Model | printf "%s"}}`, env},
		{"index", `{{index .Tools 0}}`, env},
		{"catalog partial", `{{define "entry"}}{{.Name}} {{.Description}}{{end}}{{range .Entries}}{{template "entry" .}}{{end}}`, catalog},
		{"index reassignment", `{{$x := .}}{{$x = index .Entries 0}}{{$x.Name}}`, catalog},
		{"pointer method", `{{.Label}}`, &reviewMethodData{}},
		{"nested call", `{{define "entry"}}{{.Name}}{{end}}{{range .Entries}}{{template "entry" .}}{{end}}`, catalog},
		{"map key", `{{range $k, $v := .}}{{$k.Name}}={{$v}}{{end}}`, keys},
		{"known chain", `{{.Entries}}`, catalog},
		{"struct element", `{{range .Entries}}{{.Name}}{{end}}`, catalog},
		{"if body", `{{if .Model}}{{.Cwd}}{{end}}`, env},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := loadBase(t, tc.text, tc.data)
			if err != nil {
				t.Fatalf("loading the template: %v", err)
			}
			b := prompt.New(env)
			dir.Apply(b)
			got, err := b.RenderError()
			if err != nil {
				t.Fatalf("rendering the template: %v", err)
			}
			want := stdlibRender(t, tc.text, tc.data) + "\n"
			if got != want {
				t.Errorf("the prompt is %q, want %q", got, want)
			}
		})
	}
}

// TestUnknownChainsAreNotChecked covers the constructs the parse tree
// cannot type: each must load, because a run reports a failure there
// and a rejection here would refuse a template that may be valid.
func TestUnknownChainsAreNotChecked(t *testing.T) {
	env := prompt.Env{Model: "model", Cwd: "/work", Tools: []string{"bash"}}
	holder := reviewHolder{Any: reviewCatalog{Entries: []reviewEntry{{Name: "tool"}}}, Items: []any{"item"}}
	keys := map[reviewKey]string{{Name: "key"}: "value"}
	for _, tc := range []struct {
		name string
		text string
		data any
	}{
		{"root variable", `{{$.Bad}}`, env},
		{"named variable", `{{$x := .Model}}{{$x.Bad.More}}`, env},
		{"variable after assignment", `{{$x := .}}{{$x = index .Tools 0}}{{$x.Bad}}`, env},
		{"function result", `{{(printf "%s" .Model).Bad}}`, env},
		{"builtin result", `{{(index .Tools 0).Bad}}`, env},
		{"method result", `{{.Label.Bad}}`, &reviewMethodData{}},
		{"interface field", `{{.Any.Bad}}`, holder},
		{"interface chain", `{{.Any.Bad.More}}`, holder},
		{"interface range", `{{range .Items}}{{.Bad}}{{end}}`, holder},
		{"interface with", `{{with .Any}}{{.Bad}}{{end}}`, holder},
		{"interface if", `{{if .Any}}{{.Any.Bad}}{{end}}`, holder},
		{"map key", `{{.Name}}`, keys},
		{"map range", `{{range .}}{{.Bad}}{{end}}`, keys},
		{"map range variables", `{{range $k, $v := .}}{{$k.Name}}={{$v.Bad}}{{end}}`, keys},
		{"range over variable", `{{$x := .Tools}}{{range $x}}{{$x.Bad}}{{end}}`, env},
		{"function in range", `{{range printf "%s" .Model}}{{.Bad}}{{end}}`, env},
		{"template argument", `{{define "part"}}x{{end}}{{template "part" .Bad}}`, env},
		{"template body", `{{define "part"}}{{.Bad}}{{end}}{{template "part" .}}`, env},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadBase(t, tc.text, tc.data); err != nil {
				t.Fatalf("loading a template with an unknown chain failed: %v", err)
			}
		})
	}
}

// TestAProvableFieldErrorRefusesTheTemplate covers the chains the
// validator does model: a name the known type does not have is refused
// at load, in every branch.
func TestAProvableFieldErrorRefusesTheTemplate(t *testing.T) {
	env := prompt.Env{Model: "model", Cwd: "/work", Tools: []string{"bash"}}
	catalog := reviewCatalog{Entries: []reviewEntry{{Name: "tool"}}}
	for _, tc := range []struct {
		name string
		text string
		data any
	}{
		{"root", `{{.BadField}}`, env},
		{"conditional", `{{if .Model}}{{.BadField}}{{end}}`, env},
		{"range", `{{range .Tools}}{{.BadField}}{{end}}`, env},
		{"with", `{{with .Model}}{{.BadField}}{{end}}`, env},
		{"nested chain", `{{.Model.BadField}}`, env},
		{"range element", `{{range .Entries}}{{.BadField}}{{end}}`, catalog},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadBase(t, tc.text, tc.data)
			if err == nil {
				t.Fatal("loading a template with a missing field succeeded")
			}
			for _, want := range []string{"base.md", "BadField"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error %q does not name %q", err, want)
				}
			}
		})
	}
}

// TestATemplateThatOnlyFailsAtRenderLoads covers the invalid templates
// the validator leaves to the run: both load and fail at render with
// the file named.
func TestATemplateThatOnlyFailsAtRenderLoads(t *testing.T) {
	env := prompt.Env{Model: "model", Cwd: "/work", Tools: []string{"bash"}}
	for _, tc := range []struct {
		name string
		text string
	}{
		{"nested call", `{{define "part"}}{{.Model}}{{end}}{{template "part" .Model}}`},
		{"builtin result", `{{(index .Tools 0).Bad}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := loadBase(t, tc.text, env)
			if err != nil {
				t.Fatalf("loading the template: %v", err)
			}
			b := prompt.New(env)
			dir.Apply(b)
			text, err := b.RenderError()
			if err == nil {
				t.Fatalf("rendering %q succeeded with %q", tc.text, text)
			}
			if !strings.Contains(err.Error(), "base.md") {
				t.Errorf("the error %q does not name the file", err)
			}
		})
	}
}
