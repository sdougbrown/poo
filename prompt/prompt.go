// Package prompt holds poo's system-prompt vocabulary.
//
// The system prompt is assembled from named sections, and a consumer can
// change it at every layer: a Go consumer, a profile, a settings file, a
// CLI flag or a hook. This package holds the two things every layer
// shares: the environment a section may read, and the surface a section
// list is edited through.
//
// The package imports the standard library only. The builder that renders
// a prompt arrives with the prompt stage; what is here is the vocabulary
// the rest of poo already depends on.
package prompt

// Env is what a dynamic section may read.
//
// It is a plain struct on purpose: a section that renders from the
// environment never mentions a core type, so the prompt package stays
// independent of the agent loop.
type Env struct {
	// Cwd is the directory the session works in.
	Cwd string
	// OS is the operating system's name, e.g. "linux".
	OS string
	// Date is the session's date, as "YYYY-MM-DD". A section that
	// renders the time would break the provider's prompt cache, so the
	// date is the most the environment carries.
	Date string
	// Model is the name of the model the session talks to.
	Model string
	// Tools are the names of the tools linked into the session, in
	// offer order.
	Tools []string
}

// Sections is the surface a named section list is edited through.
//
// A hook that changes the system prompt edits a Sections, and never sees
// the whole prompt: the render order, the defaults and the sections the
// consumer added are the builder's business.
type Sections interface {
	// Set replaces a section's text. A section that does not exist yet
	// is added.
	Set(name, text string)
	// Append adds to a section's text. A section that does not exist yet
	// is added.
	Append(name, text string)
	// Remove drops a section. Removing a section that is not there is
	// not an error.
	Remove(name string)
	// Insert adds a section after another one. A name that is not there
	// puts the section first.
	Insert(after, name, text string)
	// Replace takes over the whole prompt, dropping the sections.
	Replace(text string)
	// Names returns the section names, in render order.
	Names() []string
}

// Section is one named part of a prompt.
//
// A section's value is either a string or a function of the environment,
// so a section can be dynamic without a template engine.
type Section struct {
	// Name is the section's name.
	Name string `json:"name"`
	// Text is the section's text, when it is static.
	Text string `json:"text,omitempty"`
	// Render renders the section, when it is dynamic.
	Render func(Env) string `json:"-"`
}

// DefaultOrder is the order the sections render in.
//
// A consumer's own sections come last, in the order they were added.
var DefaultOrder = []string{"base", "tools", "catalog", "environment", "project"}
