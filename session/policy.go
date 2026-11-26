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

// Policy says which sessions a prune is willing to delete.
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

// MarkedBy returns the listings ranked by last activity, newest first —
// every listing, marked or not — and a predicate that reports whether the
// policy marks a listing.
//
// It is the prune policy's selection, and every store that prunes shares
// it: the count rule's kept set and boundary are computed once, from
// listings, so a session that moved while a prune ran is judged by the
// same rule the listing was. A session is marked when either rule marks
// it. The predicate reads a listing's ID and LastActivity, nothing else.
//
// now is the prune's clock: MarkedBy reads only this argument, never
// Policy.Now. A zero now makes the age rule mark nothing.
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
	// The count rule keeps the newest sessions. A count that exceeds the
	// listings keeps everything: it marks nothing, so a session that was
	// not in the listing (one born mid-prune) is not marked.
	kept := map[string]bool{}
	var boundary time.Time
	countRule := false
	if policy.Keep > 0 && policy.Keep < len(ranked) {
		for i, listing := range ranked {
			if i >= policy.Keep {
				break
			}
			kept[listing.ID] = true
		}
		boundary = ranked[policy.Keep-1].LastActivity
		countRule = true
	}
	return ranked, func(listing Listing) bool {
		if policy.MaxAge > 0 && now.Sub(listing.LastActivity) > policy.MaxAge {
			return true
		}
		if countRule && !kept[listing.ID] {
			// A listing that became newer than the sessions that are
			// kept is kept too: the count rule is about the newest
			// sessions.
			if !listing.LastActivity.After(boundary) {
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
