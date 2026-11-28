package session

import (
	"testing"
	"time"
)

// TestTheCountRuleKeepsEverythingWhenTheCountExceedsTheListings covers
// the keep-all boundary: a count larger than the listings marks nothing,
// so a session that was born after the listing is not marked.
func TestTheCountRuleKeepsEverythingWhenTheCountExceedsTheListings(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	listings := []Listing{
		{ID: "a", LastActivity: at},
		{ID: "b", LastActivity: at.Add(-time.Hour)},
	}

	_, marked := MarkedBy(Policy{Keep: len(listings) + 3}, listings, at)
	for _, listing := range listings {
		if marked(listing) {
			t.Errorf("the count rule marked %s, want every listing kept", listing.ID)
		}
	}
	born := Listing{ID: "later", LastActivity: at}
	if marked(born) {
		t.Error("the count rule marked a listing it never ranked, want it kept")
	}
}

// TestTheCountRulesBoundaryMarksAnOlderListingOutsideTheSet covers the
// count rule inside the count, where the keep count is smaller than the
// listings: a listing that was not ranked is still marked when it is no
// newer than the boundary, so a session that was re-summarized during a
// prune is judged by the listing's own rule.
//
// It pins the within-count rule alone, not the exact-count case, which
// TestTheCountRuleAtTheExactCountKeepsATiedListing covers.
func TestTheCountRulesBoundaryMarksAnOlderListingOutsideTheSet(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	listings := []Listing{
		{ID: "newest", LastActivity: at},
		{ID: "middle", LastActivity: at.Add(-time.Hour)},
		{ID: "oldest", LastActivity: at.Add(-2 * time.Hour)},
	}

	_, marked := MarkedBy(Policy{Keep: 2}, listings, at)
	old := Listing{ID: "moved", LastActivity: at.Add(-time.Hour)}
	if !marked(old) {
		t.Error("the count rule kept a listing no newer than the boundary, want it marked")
	}
	fresh := Listing{ID: "moved", LastActivity: at.Add(time.Minute)}
	if marked(fresh) {
		t.Error("the count rule marked a listing newer than the boundary, want it kept")
	}
}

// TestTheCountRuleAtTheExactCountKeepsATiedListing covers the boundary
// of the keep-all rule: a keep count equal to the listings marks nothing,
// so a listing that was not ranked is kept even when its last activity
// ties the oldest listing's.
func TestTheCountRuleAtTheExactCountKeepsATiedListing(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	listings := []Listing{
		{ID: "newest", LastActivity: at},
		{ID: "middle", LastActivity: at.Add(-time.Hour)},
		{ID: "oldest", LastActivity: at.Add(-2 * time.Hour)},
	}

	ranked, marked := MarkedBy(Policy{Keep: len(listings)}, listings, at)
	tied := Listing{ID: "regrown", LastActivity: ranked[len(ranked)-1].LastActivity}
	if marked(tied) {
		t.Error("the count rule marked a listing tied with the oldest listing, want it kept")
	}
}
