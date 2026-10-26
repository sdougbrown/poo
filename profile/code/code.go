// Package code holds the code profile.
//
// The code profile is the profile a coding session runs under: bash is
// the primary tool, and the file tools are the conveniences around it.
//
// The profile allows every side-effecting call no rule covers. That is
// the mini-swe-agent stance: the isolation is the operator's job, and
// it is done with a sandbox or a container, not with a question at
// every call. An operator who wants the questions asks for them, with
// the settings or with the command line.
package code

import "github.com/sdougbrown/poo/profile"

// Name is the profile's name.
const Name = "code"

// Profile returns the code profile.
//
// The job tools are not named: the core adds them on its own whenever
// a linked tool may run in the background, and bash does.
func Profile() profile.Profile {
	return profile.Profile{
		Name:      Name,
		Tools:     []string{"bash", "read", "grep", "edit"},
		Unmatched: "allow",
	}
}
