package session

import "time"

// StaleTmpAge is how old a temporary file must be before a prune removes
// it.
//
// A temporary file is a session's construction that was cut short. The
// construction that made it is long gone once it is this old, and the
// file is only clutter.
const StaleTmpAge = time.Hour

// Policy says which sessions a prune deletes.
//
// Retention is opt-in: a policy with neither an age nor a count deletes
// nothing, and the prune fails with ErrNoPolicy. A caller that has no
// policy and wants nothing deleted says so with the empty policy.
type Policy struct {
	// MaxAge is how old a session's last activity must be for the
	// prune to delete it. Zero means the rule does not apply.
	//
	// The clock is the session's last record, not its creation: a
	// session that is still being written is not old.
	MaxAge time.Duration
	// Keep is how many sessions the prune keeps, by last activity.
	// Zero means the rule does not apply.
	//
	// The locked sessions are counted: a session another process is
	// using is one of the sessions that are kept, and it is not
	// deleted.
	Keep int
	// DryRun lists the sessions the prune would delete and deletes
	// nothing.
	DryRun bool
	// Now is the prune's clock. The default is the store's clock.
	Now time.Time
}

// PruneResult is what a prune did.
//
// The result is what a command line prints: every deletion is listed,
// and a dry run lists the same set.
type PruneResult struct {
	// Deleted are the sessions the prune deleted, in the order it
	// deleted them. A dry run lists the sessions it would have
	// deleted.
	Deleted []Summary
	// Skipped are the sessions the prune skipped because another
	// process held their lock.
	Skipped []Summary
	// Removed are the stale temporary files the prune removed.
	Removed []string
}
