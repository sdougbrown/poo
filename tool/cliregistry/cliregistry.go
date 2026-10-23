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
// A registered CLI runs where bash runs. cli-registry's Plan validates an
// argument object and builds the command without touching a process, and
// poo runs the plan through the session's executor: the sandbox the
// settings name, with the protected paths it masks, in the session's
// working directory, with an environment resolved against the session's
// scrubbed one, and under the run's context, so an interrupt, a cancel or
// a close stops the CLI and everything it started.
//
// The plan's own timeout is poo's to enforce, and the call shows the
// model the same observation bash shows it: the two streams, the exit
// status, the partial output of a timeout, and null for a killed process.
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
