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

// offeredRegistry returns the registry entries a profile offering tools may
// see, or nil when it may see none.
//
// A registry whose manifests declare no conditions is offered to a profile
// that already offers a side-effecting tool, as it always was: a registered CLI
// can change anything, so a registry that gates nothing offers everything. Once
// the manifests declare conditions, the rules decide entry by entry, which is
// how a read-only profile is offered the entries that only read.
func (r *Runtime) offeredRegistry(tools []poo.ToolInfo) (*cli.Catalog, error) {
	if r.loaded == nil {
		return nil, nil
	}
	declared := r.loaded.AvailabilityInputs()
	sideEffecting := offersSideEffects(tools)
	if !sideEffecting && len(declared) == 0 {
		return nil, nil
	}
	inputs := map[string]any{ProfileSideEffectingCondition: sideEffecting}
	if err := checkRegistryConditions(declared); err != nil {
		return nil, err
	}
	offered, err := r.loaded.Filter(inputs)
	if err != nil {
		return nil, &Error{
			Code:    CodeInvalid,
			Field:   "cli_registry.paths",
			Message: "the registry's availability rules reject the session",
			Err:     err,
		}
	}
	if offered.Len() == 0 {
		return nil, nil
	}
	return offered, nil
}

// checkRegistryConditions reports the conditions the manifests declare and poo
// cannot supply.
//
// The conditions are checked against the whole declared set rather than against
// one profile's value: what poo can supply is a property of poo, and the answer
// does not change per profile.
func checkRegistryConditions(declared map[string]string) error {
	var unsupplied []string
	for name := range declared {
		if name != ProfileSideEffectingCondition {
			unsupplied = append(unsupplied, name)
		}
	}
	if len(unsupplied) == 0 {
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
