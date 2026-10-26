// Package profile holds the profiles poo ships.
//
// A profile is a pre-wired tool set plus defaults: the tools a run
// offers, the action the profile takes with a side-effecting call no
// rule covers, and the name the session's metadata carries.
//
// The profiles themselves live in the packages beside this one, so a
// binary links the profiles it wants and no others.
package profile

import "fmt"

// The names of the tools the core adds by itself.
//
// The core adds the job tools whenever a linked tool may run in the
// background, so a profile that links such a tool offers them without
// naming them.
const (
	// JobsToolName is the name of the tool that lists the jobs.
	JobsToolName = "jobs"
	// CancelJobToolName is the name of the tool that stops a job.
	CancelJobToolName = "cancel_job"
)

// Profile is a named tool set with its own defaults.
//
// The tools are the names of the tools the profile offers, in offer
// order. A binary resolves the names against the tools it links: a
// profile that names a tool the binary does not link is a
// configuration error, never a tool the profile quietly drops.
type Profile struct {
	// Name is the profile's name.
	Name string
	// Tools are the names of the tools the profile offers, in offer
	// order.
	Tools []string
	// Unmatched is what the profile does with a side-effecting call
	// no rule covers: allow, ask or deny.
	//
	// An empty unmatched action means the permission policy's own
	// default applies.
	Unmatched string
}

// HasTool reports whether the profile offers the named tool.
func (p Profile) HasTool(name string) bool {
	for _, tool := range p.Tools {
		if tool == name {
			return true
		}
	}
	return false
}

// Find returns the profile with the given name.
//
// A name no profile answers to is an error: the caller asked for a
// profile that does not exist.
func Find(profiles []Profile, name string) (Profile, error) {
	for _, profile := range profiles {
		if profile.Name == name {
			return profile, nil
		}
	}
	return Profile{}, &UnknownError{Name: name, Linked: Names(profiles)}
}

// Names returns the names of the profiles, in the order they were
// given.
func Names(profiles []Profile) []string {
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	return names
}

// UnknownError is the error for a profile name no linked profile
// answers to.
type UnknownError struct {
	// Name is the name that was asked for.
	Name string
	// Linked names the profiles the binary does link.
	Linked []string
}

func (e *UnknownError) Error() string {
	if len(e.Linked) == 0 {
		return fmt.Sprintf("profile: no profile named %q, and this binary links none", e.Name)
	}
	return fmt.Sprintf("profile: no profile named %q; this binary links %v", e.Name, e.Linked)
}
