// Package cliregistry offers the registered command-line tools to the
// model.
//
// The catalog itself lives in poo/cli: it loads the manifests, compiles
// the argument schemas and renders the prompt's section. This package
// holds the two tools over it:
//
//   - call_cli runs one registered CLI, naming it and passing its
//     arguments. It is side-effecting, so it goes through the permission
//     policy like any command.
//   - find_tool searches a large catalog with a decision model, because a
//     catalog of hundreds of entries does not fit in a prompt.
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
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
)

// The catalog is cli's: a binary that reads settings links the catalog
// without linking the tools over it, and the names below keep the
// tools' API whole.
type (
	// Catalog is the set of registered CLIs a session offers.
	Catalog = cli.Catalog
	// Entry is one registered CLI.
	Entry = cli.Entry
)

// The names the catalog and its tools answer to.
const (
	// CallToolName is the name of the tool that runs a registered CLI.
	CallToolName = cli.CallToolName
	// FindToolName is the name of the tool that searches the catalog.
	FindToolName = cli.FindToolName
	// SectionName is the name of the prompt section that lists the
	// catalog.
	SectionName = cli.SectionName
	// FindThreshold is the number of entries above which a session with
	// a decision model searches the catalog instead of reading it.
	FindThreshold = cli.FindThreshold
)

// Load reads the registries reachable from paths and compiles the
// catalog.
func Load(paths ...string) (*Catalog, error) { return cli.Load(paths...) }

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
