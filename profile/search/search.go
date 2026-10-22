// Package search holds the search profile.
//
// The search profile is the profile a read-only session runs under: it
// offers the file tools and nothing that can change anything.
//
// A build that cannot link bash uses the profile: the tools are the
// ones that read, and the model's answers are the ones the consumer
// acts on.
package search

import "github.com/sdougbrown/poo/profile"

// Name is the profile's name.
const Name = "search"

// Profile returns the search profile.
//
// The profile allows every side-effecting call no rule covers. The
// tools it offers are read-only, so the action hardly shows: it is
// there for the tool a consumer adds to the profile.
func Profile() profile.Profile {
	return profile.Profile{
		Name:      Name,
		Tools:     []string{"read", "grep"},
		Unmatched: "allow",
	}
}
