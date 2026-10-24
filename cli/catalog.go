// Package cli holds the catalog of registered command-line tools a
// session may offer.
//
// A registered CLI is a small manifest describing a real command: its
// argv, its flags and the JSON schema of its arguments. The package loads
// the manifests the settings name, holds the library's compiled registry,
// and renders the catalog's section of the prompt.
//
// The tools over the catalog live in poo/tool/cliregistry. A binary that
// reads settings links this package to know what the operator
// registered; only a binary that offers the tools links those.
package cli

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	cliregistry "github.com/sdougbrown/cli-registry"

	"github.com/sdougbrown/poo/prompt"
)

// The names the catalog and its tools answer to.
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
	registry *cliregistry.Registry
	// tools are the compiled model infos, by name.
	tools map[string]cliregistry.Tool
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
// The arguments of every CLI are compiled by the library when the catalog
// loads, so a call that does not fit its schema is refused before any
// process is started.
func Load(paths ...string) (*Catalog, error) {
	registry, err := cliregistry.Load(paths...)
	if err != nil {
		return nil, err
	}
	c := &Catalog{
		registry: registry,
		tools:    map[string]cliregistry.Tool{},
	}
	for _, tool := range registry.Tools() {
		c.tools[tool.Name] = tool
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
// The list is a copy, so a caller may reorder it. The argument schemas
// are the catalog's own and are not to be changed.
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

// NamesHint says how a model finds a name it does not know.
//
// A short catalog is worth listing in the failure, so the model can
// correct the call at once. A long one is not: the failure would carry
// what the prompt was careful not to. A session that offers no search is
// pointed at the prompt's list instead.
func (c *Catalog) NamesHint(search bool) string {
	if c.Len() == 0 {
		return "no CLI is registered"
	}
	if c.Len() <= 8 {
		return "the catalog holds " + strings.Join(c.Names(), ", ")
	}
	if search {
		return fmt.Sprintf("the catalog holds %d entries: search it, or call one the prompt lists", c.Len())
	}
	return fmt.Sprintf("the catalog holds %d entries: call one the prompt lists", c.Len())
}

// Plan validates an argument object and builds the command a call runs.
//
// The plan is the command and nothing else: no environment is read and no
// process is started until the caller runs it. A validation failure comes
// back as the library's *ValidationError, whose issues are the model's to
// correct.
func (c *Catalog) Plan(name string, args map[string]any) (*cliregistry.Plan, error) {
	tool, ok := c.tools[name]
	if !ok {
		return nil, fmt.Errorf("no CLI named %q", name)
	}
	return c.registry.Plan(tool, args)
}

//go:embed catalog.md
var catalogText string

// catalogData is the data the catalog's section template renders with.
//
// Search says the catalog is behind a search rather than listed whole.
// Count is the number of registered CLIs, Call and Find are the tools
// that run one and that search the catalog, and Entries are the CLIs, in
// name order.
type catalogData struct {
	Search  bool
	Count   int
	Call    string
	Find    string
	Entries []Entry
}

// Template returns the catalog's section of the system prompt as a
// template.
//
// The full catalog is one line per entry: the name the model calls the
// CLI by, and what it does. A session that searches the catalog instead
// is told what the search is and how to use it, and the entries stay out
// of the prompt.
//
// The template renders with the catalog's data, so a prompt directory
// that replaces the section renders with it too.
func (c *Catalog) Template(search bool) *prompt.Template {
	return prompt.MustCompileTrimmed("catalog", catalogText, func(prompt.Env) any {
		return catalogData{
			Search:  search,
			Count:   c.Len(),
			Call:    CallToolName,
			Find:    FindToolName,
			Entries: c.entries,
		}
	})
}

// Section renders the catalog's section of the system prompt.
func (c *Catalog) Section(search bool) string {
	return c.Template(search).Render(prompt.Env{})
}
