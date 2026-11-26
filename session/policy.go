package session

import (
	"sort"
	"time"
)

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

// MarkedBy returns the listings ranked by last activity, newest first,
// and the policy's mark over them.
//
// It is the prune policy's selection, and every store shares it: the
// count rule's kept set and boundary are computed once, from listings,
// so a session that moved while a prune ran is judged by the same rule
// the listing was. A session is marked when either rule marks it.
//
// now is the prune's clock. A caller that leaves Policy.Now zero
// resolves it to its own clock before it calls.
func MarkedBy(policy Policy, listings []Listing, now time.Time) ([]Listing, func(Listing) bool) {
	ranked := make([]Listing, len(listings))
	copy(ranked, listings)
	sort.Slice(ranked, func(i, j int) bool {
		if !ranked[i].LastActivity.Equal(ranked[j].LastActivity) {
			return ranked[i].LastActivity.After(ranked[j].LastActivity)
		}
		// Ties are broken by id, so the same sessions are kept on every
		// run.
		return ranked[i].ID < ranked[j].ID
	})
	// The count rule keeps the newest sessions, and it counts the
	// locked ones: a locked session is one of the sessions that are
	// kept.
	kept := map[string]bool{}
	var boundary time.Time
	if policy.Keep > 0 {
		for i, sum := range ranked {
			if i >= policy.Keep {
				break
			}
			kept[sum.ID] = true
		}
		if policy.Keep <= len(ranked) {
			boundary = ranked[policy.Keep-1].LastActivity
		}
	}
	return ranked, func(sum Listing) bool {
		if policy.MaxAge > 0 && now.Sub(sum.LastActivity) > policy.MaxAge {
			return true
		}
		if policy.Keep > 0 && !kept[sum.ID] {
			// A session that became newer than the sessions that are
			// kept is kept too: the count rule is about the newest
			// sessions.
			if boundary.IsZero() || !sum.LastActivity.After(boundary) {
				return true
			}
		}
		return false
	}
}

// PruneResult is what a prune did.
//
// The result is what a command line prints: every deletion is listed,
// and a dry run lists the same set.
type PruneResult struct {
	// Deleted are the sessions the prune deleted, in the order it
	// deleted them. A dry run lists the sessions it would have
	// deleted.
	Deleted []Listing
	// Skipped are the sessions the prune skipped because another
	// process held their lock.
	Skipped []Listing
	// Removed are the stale temporary files the prune removed.
	Removed []string
}
