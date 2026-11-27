package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
)

// ProfileSideEffectingCondition is the availability condition poo supplies
// itself: whether the profile offers a tool that can change anything.
//
// A manifest reads it to gate its write entries, so a profile with no
// side-effecting tool is offered the entries that only read. poo supplies no
// other condition. A registry that declares one fails the build naming it,
// because a value poo does not have is not one poo can invent.
const ProfileSideEffectingCondition = "profile_side_effecting"

// conditionBoolean is the declared type poo supplies for that condition: the
// library's condition vocabulary spells a boolean "boolean", and poo supplies
// nothing else. A manifest that declares the condition as any other type would
// judge entries against a value poo never sends.
const conditionBoolean = "boolean"

// offeredCatalog returns the catalog the session's tools read, or nil when the
// profile earns none. The tools are built from this, never from Registry, so a
// profile that names a registry tool itself reads no entry the rules withhold.
func (r *Runtime) offeredCatalog() *cli.Catalog {
	if !r.registryOffered {
		return nil
	}
	return r.Registry
}

// offeredView returns the session's offering: the entries the model
// reaches through a tool that calls them, read as they are now.
//
// The catalog's entries come first when the search covers the catalog — the
// same rule that lists it in the prompt and points call_cli's refusal at the
// search — then the tools of the servers the session started, each naming the
// call that runs it. One value serves the search, the prompt's catalogs and
// the refusals, so none of them can name a dispatcher another does not offer.
// A search the servers brought does not cover a catalog the prompt already
// lists: below the threshold the entries are the model's to read, and a
// decision-model round trip over them is what the threshold rule exists to
// avoid.
//
// One read is one snapshot — every handle reads the generation its group
// runs, under the group's lock — so a search after a relaunch names the
// tools of the server the session runs, and two searches over an offering
// that changed between them disagree without either being wrong.
//
// An elevated catalog contributes nothing: its entries are tools of their own
// by now, so a hit for one would name a call the session does not offer, and
// the model already reads each entry in the tool list.
func offeredView(catalog *cli.Catalog, elevated []poo.ToolInfo, started []MCPRunning, searched bool) cli.Offerings {
	return func() []cli.Offered {
		var entries []cli.Offered
		if len(elevated) == 0 && searched {
			entries = catalog.Offered()
		}
		for _, running := range started {
			entries = append(entries, running.Offered()...)
		}
		return entries
	}
}

// offeredRegistry returns the registry entries a profile offering tools may
// see, or nil when it may see none.
//
// A registry whose manifests declare no conditions is offered to a profile
// that already offers a side-effecting tool, as it always was: a registered tool
// can change anything, so a registry that gates nothing offers everything. Once
// the manifests declare conditions, the rules decide entry by entry, which is
// how a read-only profile is offered the entries that only read.
//
// A profile that offers no tools earns none of it.
func offeredRegistry(loaded *cli.Catalog, tools []poo.ToolInfo) (*cli.Catalog, error) {
	if loaded == nil || len(tools) == 0 {
		return nil, nil
	}
	declared := loaded.AvailabilityInputs()
	sideEffecting := offersSideEffects(tools)
	if !sideEffecting && len(declared) == 0 {
		return nil, nil
	}
	inputs := map[string]any{ProfileSideEffectingCondition: sideEffecting}
	offered, err := loaded.Filter(inputs)
	if err != nil {
		return nil, &Error{
			Code:    CodeInvalid,
			Field:   "cli_registry.paths",
			Message: "the registry's availability rules reject the profile",
			Err:     err,
		}
	}
	if offered.Len() == 0 {
		return nil, nil
	}
	return offered, nil
}

// checkRegistryConditions reports the conditions the manifests declare and poo
// cannot supply, and a supplied condition declared as something other than the
// type poo sends.
//
// It runs once, when the registry loads: the declared set is a property of the
// manifests, not of the profile being judged, so a manifest poo cannot supply
// fails every build that loads it, whatever profiles the binary offers.
func checkRegistryConditions(declared map[string]string) error {
	var unsupplied []string
	for name := range declared {
		if name != ProfileSideEffectingCondition {
			unsupplied = append(unsupplied, name)
		}
	}
	if len(unsupplied) == 0 {
		if typ, ok := declared[ProfileSideEffectingCondition]; ok && typ != conditionBoolean {
			return &Error{
				Code:  CodeUnsupported,
				Field: "cli_registry.paths",
				Message: fmt.Sprintf(
					"the registry declares the availability condition %s as %q, and poo supplies a boolean",
					ProfileSideEffectingCondition, typ,
				),
			}
		}
		return nil
	}
	sort.Strings(unsupplied)
	return &Error{
		Code:  CodeUnsupported,
		Field: "cli_registry.paths",
		Message: fmt.Sprintf(
			"the registry declares the availability condition(s) %s, and poo supplies only %s",
			strings.Join(unsupplied, ", "), ProfileSideEffectingCondition,
		),
	}
}
