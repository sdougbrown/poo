// Package cliregistry offers the registered tools to the model.
//
// The catalog itself lives in poo/cli: it loads the manifests, compiles
// the argument schemas and renders the prompt's section. This package
// holds the two tools over it:
//
//   - call_cli runs one registered command, naming it and passing its
//     arguments. It is side-effecting, so it goes through the permission
//     policy like any command.
//   - find_tool searches a large catalog with a decision model, because a
//     catalog of hundreds of entries does not fit in a prompt.
//
// # Direct tools
//
// A catalog short enough to show is offered entry by entry rather than behind
// a call: *CallTool.Tools returns one tool per entry, each with the entry's own
// name and argument schema, and a host that links this package registers them
// directly. The tools run through the same code as a call, so an entry means
// the same thing however the model reaches it.
//
// # Execution
//
// A registered tool runs where bash runs. cli-registry's Plan validates an
// argument object and builds the command without touching a process, and
// poo runs the plan through the session's executor: the sandbox the
// settings name, with the protected paths it masks, in the session's
// working directory, with an environment resolved against the session's
// scrubbed one — a manifest's session:id resolving to the session's own
// ID — and under the run's context, so an interrupt, a cancel or a
// close stops the tool and everything it started.
//
// The plan's own timeout is poo's to enforce, and the call shows the
// model the same observation bash shows it: the two streams, the exit
// status, the partial output of a timeout, and null for a killed
// process. The one exception: a tool whose manifest decodes JSON
// returns that JSON instead of the envelope, with the run's stderr
// after a newline when there is any.
//
// # Validation
//
// What the arguments may be is the library's to decide, and poo adds no
// rule of its own: Plan refuses a value that does not fit the tool's
// schema, and a dash-leading value a separate-value binding would put
// where the command parses options. Both come back as a *ValidationError,
// which poo reports as an invalid call before any process exists. A value
// bound with the equals or short style stays inside one argv element and
// is not refused, nor is a positional one, which the library escapes with
// "--".
package cliregistry

import (
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
)

// The catalog is cli's: a binary that reads settings links the catalog
// without linking the tools over it, and the names below keep the
// tools' API whole.
type (
	// Catalog is the set of registered tools a session offers.
	Catalog = cli.Catalog
	// Entry is one registered tool.
	Entry = cli.Entry
)

// The names the catalog and its tools answer to.
const (
	// CallToolName is the name of the tool that runs a catalog entry.
	CallToolName = cli.CallToolName
	// FindToolName is the name of the tool that searches the catalog.
	FindToolName = cli.FindToolName
	// SectionName is the name of the prompt section that lists the
	// catalog.
	SectionName = cli.SectionName
	// CommandArgument is the call tool's argument that names a registered
	// command.
	CommandArgument = cli.CommandArgument
	// FindThreshold is the number of entries above which a session with
	// a decision model searches the catalog instead of reading it.
	FindThreshold = cli.FindThreshold
)

// Load reads the registries reachable from paths and compiles the
// catalog.
func Load(paths ...string) (*Catalog, error) { return cli.Load(paths...) }

// GeneratedToolset is the seam for the one-tool-per-entry shape.
//
// A catalog short enough to show is offered entry by entry, generated from its
// manifests: the model sees each one in the tool list, with its arguments as
// that tool's schema, and there is no call name to wrap them in. *CallTool
// implements it, so the catalog and the tools over it stay one adapter.
//
// A host that builds the tools asks the tool it built whether it offers them,
// which is why the interface is exported: the host need not link this package
// to offer what it returns.
type GeneratedToolset interface {
	// Tools returns one tool per catalog entry, in name order.
	Tools() []poo.Tool
}
