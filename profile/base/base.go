// Package base holds the base profile.
//
// The base profile is the profile with no tools: a run that offers the
// model nothing but the contract it works under. It is the profile the
// other profiles are built on, and the one a consumer composes its own
// tool set from.
package base

import "github.com/sdougbrown/poo/profile"

// Name is the profile's name.
const Name = "base"

// Profile returns the base profile.
//
// The profile offers no tools, so the prompt carries the base section
// and nothing else.
func Profile() profile.Profile {
	return profile.Profile{
		Name:      Name,
		Tools:     nil,
		Unmatched: "",
	}
}
