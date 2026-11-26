package config

import (
	"slices"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/profile"
)

// generatedToolset is the seam a tool over the cli catalog implements when it
// offers one native tool per entry.
//
// config does not import tool/cliregistry, so the seam is structural: the tool
// the binary's factory builds is asked for its entries, and a binary that links
// no such tool never answers. A binary that links cliregistry.NewCall answers
// through the tool it already builds, so the catalog and the tools over it stay
// one adapter, and no new import or registration reaches this package.
type generatedToolset interface {
	// Tools returns one tool per catalog entry, in name order.
	Tools() []poo.Tool
}

// reservedToolNames returns the names a catalog entry may not take as a
// native tool: the tools the binary links, the core's own, and the two
// tools over the catalog itself.
//
// A registry that names one of them keeps the call tool, so an existing
// manifest never changes the meaning of a tool the model was already
// offered. The names come from the source of truth rather than a second
// hand-maintained list: the binary's own linked factories, and the core
// name constants poo adds by itself, so a tool the binary gains is
// reserved with it.
func reservedToolNames(opts BuildOptions) []string {
	names := make([]string, 0, len(opts.Tools)+6)
	for name := range opts.Tools {
		names = append(names, name)
	}
	return append(names,
		profile.JobsToolName, profile.CancelJobToolName,
		poo.SubscribeFeedToolName, poo.FinishToolName,
		cli.CallToolName, cli.FindToolName)
}

// elevatedRegistryTools returns the catalog's entries as tools of their own,
// or nil when the session offers the call tool over the catalog instead.
//
// A catalog is short enough to show when it holds no more entries than
// cli.FindThreshold. Such a catalog is offered entry by entry: the model reads
// each tool's name, description and argument schema in the tool list, so the
// call tool and the catalog's prompt section would say the same thing twice.
//
// The offering is all or nothing, and several things keep the call tool:
//
//   - a catalog longer than the threshold, which the session lists or searches;
//   - a disabled call_cli, which disables the whole offering;
//   - a profile that names call_cli or find_tool itself, whose choice is the
//     profile's and outranks the shape the build would pick;
//   - an entry whose name a reserved or already-offered tool answers to, so a
//     manifest never shadows the meaning of a tool the session already has.
//
// The tools are the ones the binary's own call tool generates: a binary that
// links no call tool, or one whose tool offers no entries, gets nil and keeps
// whatever the ordinary path gives it.
func elevatedRegistryTools(opts BuildOptions, catalog *cli.Catalog, existing []poo.ToolInfo, disabled []string) []poo.ToolInfo {
	if catalog == nil || catalog.Len() == 0 || catalog.Len() > cli.FindThreshold {
		return nil
	}
	if slices.Contains(disabled, cli.CallToolName) {
		return nil
	}
	for _, name := range []string{cli.CallToolName, cli.FindToolName} {
		if offersToolName(existing, name) {
			return nil
		}
	}
	factory := opts.Tools[cli.CallToolName]
	if factory == nil {
		return nil
	}
	set, ok := factory(ToolOptions{Registry: catalog}).(generatedToolset)
	if !ok {
		return nil
	}
	tools := set.Tools()
	if len(tools) == 0 {
		return nil
	}
	elevated := make([]poo.ToolInfo, 0, len(tools))
	reserved := reservedToolNames(opts)
	for _, tool := range tools {
		spec := tool.Spec()
		if spec.Name == "" || slices.Contains(reserved, spec.Name) || offersToolName(existing, spec.Name) {
			return nil
		}
		elevated = append(elevated, poo.ToolInfo{Name: spec.Name, Class: spec.Class, Background: spec.Background, Idempotent: spec.Idempotent})
	}
	return elevated
}

// offersToolName reports whether a set of tools already answers to a name.
func offersToolName(tools []poo.ToolInfo, name string) bool {
	return slices.ContainsFunc(tools, func(tool poo.ToolInfo) bool { return tool.Name == name })
}
