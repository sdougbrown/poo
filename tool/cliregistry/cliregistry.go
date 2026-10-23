// Package cliregistry adapts a cli-registry catalog to poo's tools.
//
// A registered CLI is a small manifest describing a real command: its
// argv, its flags and the JSON schema of its arguments. The package loads
// the manifests the settings name, lists them in the prompt's catalog
// section, and offers two tools over them:
//
//   - call_cli runs one registered CLI, naming it and passing its
//     arguments. It is side-effecting, so it goes through the permission
//     policy like any command.
//   - find_tool searches a large catalog with a decision model, because a
//     catalog of hundreds of entries does not fit in a prompt.
//
// The catalog is loaded once, when the session is built: a registry is
// the operator's configuration, and a session does not reload one.
//
// # Execution
//
// The package turns the manifests into tools; it does not run them.
// cli-registry v0.1.0 validates, builds argv and spawns in one call
// (Registry.Call), and it exports neither the validator nor the argv
// builder. Poo validates an argument object in Prepare with cli-registry's
// public schema compiler, so a bad call never reaches a process, and then
// hands the call to Registry.Call.
//
// A call_cli process therefore runs outside poo's executor chain: no
// bubblewrap, no masked paths, and not the session's scrubbed environment.
// It gets the benign default set (PATH, HOME, LANG, LC_ALL, TMPDIR) plus
// the variables its manifest declares. Running the argv through the
// executor needs two exports from cli-registry: Tool.Validate(args) and
// Tool.Argv(args).
package cliregistry

import (
	"fmt"
	"sort"
	"strings"

	cli "github.com/sdougbrown/cli-registry"
	"github.com/sdougbrown/cli-registry/schema"

	"github.com/sdougbrown/poo"
)

// The names the adapter uses.
const (
	// CallToolName is the name of the tool that runs a registered CLI.
	CallToolName = "call_cli"
	// FindToolName is the name of the tool that searches the catalog.
	FindToolName = "find_tool"
	// SectionName is the name of the prompt section that lists the
	// catalog.
	SectionName = "catalog"
)

// FindThreshold is the number of entries above which a session with a
// decision model searches the catalog instead of reading it.
//
// A catalog of thirty entries or fewer is short enough to show whole, and
// a search that costs a decision-model round trip would not pay for
// itself.
const FindThreshold = 30

// Entry is one registered CLI.
type Entry struct {
	// Name is the name the model calls the CLI by.
	Name string
	// Description is what the CLI does, as its manifest says.
	Description string
	// Schema is the JSON schema of the CLI's arguments.
	Schema map[string]any
}

// Catalog is the set of registered CLIs a session offers.
//
// The catalog is immutable after Load, so every tool over it may be
// called concurrently.
type Catalog struct {
	// registry is the compiled cli-registry registry.
	registry *cli.Registry
	// tools are the compiled descriptors, by name.
	tools map[string]cli.Tool
	// validators check a call's arguments before any process exists.
	validators map[string]*schema.ValueSchema
	// entries are the CLIs, in name order.
	entries []Entry
}

// Load reads the registries reachable from paths and compiles the
// catalog.
//
// A path is a manifest file, a directory of manifests, or several of
// them. A tool name defined by two manifests fails the load, and the
// failure names both files: a name a model calls has to mean one command.
//
// The arguments of every CLI are compiled here, once, so a call that does
// not fit its schema is refused before any process is started.
func Load(paths ...string) (*Catalog, error) {
	registry, err := cli.Load(paths...)
	if err != nil {
		return nil, err
	}
	c := &Catalog{
		registry:   registry,
		tools:      map[string]cli.Tool{},
		validators: map[string]*schema.ValueSchema{},
	}
	for _, tool := range registry.Tools() {
		validator, err := schema.Compile(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("cliregistry: the tool %q: %w", tool.Name, err)
		}
		c.tools[tool.Name] = tool
		c.validators[tool.Name] = validator
		c.entries = append(c.entries, Entry{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      tool.InputSchema,
		})
	}
	sort.Slice(c.entries, func(i, j int) bool { return c.entries[i].Name < c.entries[j].Name })
	return c, nil
}

// Len is the number of registered CLIs.
func (c *Catalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

// Entries returns the registered CLIs, in name order.
//
// The entries are copies: a caller that changes one changes its own.
func (c *Catalog) Entries() []Entry {
	if c == nil {
		return nil
	}
	return append([]Entry(nil), c.entries...)
}

// Lookup returns the entry for a name, and whether there is one.
func (c *Catalog) Lookup(name string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	_, ok := c.tools[name]
	if !ok {
		return Entry{}, false
	}
	for _, entry := range c.entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return Entry{}, false
}

// Names returns the registered names, in order.
func (c *Catalog) Names() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.entries))
	for _, entry := range c.entries {
		names = append(names, entry.Name)
	}
	return names
}

// namesHint says how a model finds a name it does not know.
//
// A short catalog is worth listing in the failure, so the model can
// correct the call at once. A long one is not: the failure would carry
// what the prompt was careful not to.
func (c *Catalog) namesHint() string {
	if c.Len() == 0 {
		return "no CLI is registered"
	}
	if c.Len() <= 8 {
		return "the catalog holds " + strings.Join(c.Names(), ", ")
	}
	return fmt.Sprintf("the catalog holds %d entries: search it, or call one the prompt lists", c.Len())
}

// Section renders the catalog's section of the system prompt.
//
// The full catalog is one line per entry: the name the model calls the
// CLI by, and what it does. A session that searches the catalog instead
// is told what the search is and how to use it, and the entries stay out
// of the prompt.
func (c *Catalog) Section(search bool) string {
	if search {
		return fmt.Sprintf("Registered CLIs:\n"+
			"%d command-line tools are registered, too many to list here. "+
			"Call %s with what you want to do, and it returns the tools that fit, with their arguments. "+
			"Then call %s with the name it gives you.",
			c.Len(), FindToolName, CallToolName)
	}
	var b strings.Builder
	b.WriteString("Registered CLIs:\n")
	fmt.Fprintf(&b, "Call one with %s: \"name\" is the line's name, and \"args\" is an object of its arguments. "+
		"The tool checks the arguments against the CLI's own schema and says what is wrong with them.\n", CallToolName)
	for _, entry := range c.entries {
		fmt.Fprintf(&b, "%s: %s\n", entry.Name, entry.Description)
	}
	return strings.TrimRight(b.String(), "\n")
}

// GeneratedToolset is the seam for the deferred per-CLI tool shape.
//
// A later version may offer every registered CLI as a tool of its own,
// generated from its manifest: the model would see each one in the tool
// list, with its arguments as that tool's schema. It is deferred until an
// eval shows the model ignoring the catalog.
//
// Nothing implements it. It is the boundary the shape plugs into, so the
// catalog and the tools over it stay one adapter.
type GeneratedToolset interface {
	// Tools returns one tool per registered CLI, in name order.
	Tools() []poo.Tool
}
