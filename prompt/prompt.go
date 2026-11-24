// Package prompt holds poo's system-prompt vocabulary.
//
// The system prompt is assembled from named sections, and a consumer can
// change it at every layer: a Go consumer, a profile, a settings file, a
// CLI flag or a hook. This package holds the three things every layer
// shares: the environment a section may read, the surface a section
// list is edited through, and the builder that renders the prompt.
//
// A section holds a default text and, beside it, the named alternatives
// it offers. The builder renders the prompt in one variant, named by the
// environment: a section that offers an alternative for that name
// renders it, and a section that does not renders its default text.
//
// The package imports the standard library only.
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
	// Profile is the name of the profile the session was built with.
	// The empty name means the session built with no profile.
	Profile string
	// Tools are the names of the tools linked into the session, in
	// offer order.
	Tools []string
	// Feeds are the feeds the session runs, in declaration order.
	//
	// A section that names them tells the model what may arrive beside
	// the user's turn.
	Feeds []Feed
	// OptionalFeeds are the feeds a subscription starts, in declaration
	// order.
	//
	// A section that names them tells the model what it may ask for, and
	// what each feed's scope means.
	OptionalFeeds []Feed
	// Variant is the name of the prompt variant the prompt renders in.
	//
	// A section that offers an alternative for that name renders the
	// alternative, and a section that does not renders its default
	// text. The empty name and "default" mean the default text.
	Variant string
}

// DefaultVariant is the name of a section's default text.
//
// The empty name means the same thing: a section's default text is
// what the prompt renders when no other variant is named.
const DefaultVariant = "default"

// Feed is a feed the session runs or may start, as a prompt section names
// it.
type Feed struct {
	// Name is the feed's name.
	Name string
	// Description is what the feed is for.
	Description string
	// Scope are the values a subscription must supply, in name order, and
	// empty for a feed that takes none.
	Scope []FeedScope
}

// FeedScope is one value a subscription must supply for a feed, as a
// prompt section names it.
type FeedScope struct {
	// Name is the field's name, which the call passes its value under.
	Name string
	// Pattern is the expression the whole value must match, or "" when
	// any string is accepted.
	Pattern string
}

// Sections is the surface a named section list is edited through.
//
// A hook that changes the system prompt edits a Sections, and never sees
// the whole prompt: the render order, the defaults and the sections the
// consumer added are the builder's business.
//
// A section is always text. A dynamic section — one that renders from
// the environment — is contributed by the core or by a tool, and a
// consumer that wants one sets it through the builder rather than
// through this surface.
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
//
// A section holds a default value and, beside it, the named
// alternatives it offers. The alternatives are texts of their own: a
// prompt that renders in a variant renders the alternative the section
// offers for that name, and the section's default value otherwise.
type Section struct {
	// Name is the section's name.
	Name string
	// Text is the section's default text, when it is static.
	Text string
	// Render renders the section's default text, when it is dynamic.
	Render func(Env) string
	// renderError renders a loaded template, whose failure is the
	// caller's to report.
	renderError func(Env) (string, error)
	// Variants holds the section's named alternatives, keyed by name.
	//
	// The default text is not among them: the empty name and
	// "default" name the section's own text.
	Variants map[string]Content
}

// Content is one text of a section.
//
// It is either a string or a function of the environment, so a section
// can be dynamic without a template engine.
type Content struct {
	// Text is the text, when it is static.
	Text string
	// Render renders the text, when it is dynamic.
	Render func(Env) string
	// renderError renders a loaded template, whose failure is the
	// caller's to report.
	renderError func(Env) (string, error)
}

// String returns the content's text in the given environment.
//
// A dynamic content renders; a static one is its own text.
func (c Content) String(env Env) string {
	text, err := c.text(env)
	if err != nil {
		panic(err)
	}
	return text
}

// text returns the content's text in the given environment, or the
// error a loaded template reported.
func (c Content) text(env Env) (string, error) {
	if c.renderError != nil {
		return c.renderError(env)
	}
	if c.Render != nil {
		return c.Render(env), nil
	}
	return c.Text, nil
}

// ProfileSection is the name of the prompt's profile section.
//
// A profile whose own prose the build applies renders there: the
// section sits right after the base, so the profile's own words sit
// beside the operating contract every profile shares.
const ProfileSection = "profile"

// PersonaSection is the name of the prompt's persona section.
//
// The section is the prompt's first: it says what the model is, in one
// line, and renders bare, before the first tags open. A consumer edits
// it like any other section; the default names the profile the session
// was built with.
const PersonaSection = "persona"

// DefaultOrder is the order the sections render in.
//
// "tools" stands for the per-tool sections, which are named
// "tools.<name>" and render together where "tools" sits, in the order
// the tools are offered in. "skills" is what the person can do, and the
// catalog is what the session can call: the catalog comes first, so the
// model reads what it has before what it could follow. A consumer's own
// sections come last, in the order they were added.
//
// The slice is read-only: a consumer that changes it changes the prompt
// for everybody.
var DefaultOrder = []string{"persona", "base", "profile", "tools", "catalog", "skills", "environment", "user", "project"}

// Order returns the order the sections render in.
//
// The caller gets a copy, so a change to it does not change the order
// everybody else sees.
func Order() []string {
	return append([]string(nil), DefaultOrder...)
}
