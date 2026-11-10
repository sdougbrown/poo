package urlnorm

import "testing"

// TestCanonicalKeepsTheQueryAndTheFragment covers the parts of a URL
// that are not the host: the query and the fragment survive the
// canonical form, so two URLs that differ in them stay two URLs.
func TestCanonicalKeepsTheQueryAndTheFragment(t *testing.T) {
	canonical, ok := Canonical("HTTPS://Docs.Example.COM/search?q=1&lang=en#results")
	if !ok {
		t.Fatal("the URL did not canonicalize")
	}
	want := "https://docs.example.com/search?q=1&lang=en#results"
	if canonical != want {
		t.Errorf("the canonical form is %q, want %q", canonical, want)
	}
	other, ok := Canonical("https://docs.example.com/search?q=2&lang=en#results")
	if !ok {
		t.Fatal("the second URL did not canonicalize")
	}
	if other == canonical {
		t.Errorf("two URLs that differ only in the query share one canonical form: %q", other)
	}
	// A host that differs only in case or in the default port is one
	// host spelled two ways, and the canonical form merges it.
	same, ok := Canonical("https://docs.example.com:443/search?q=1&lang=en#results")
	if !ok || same != canonical {
		t.Errorf("the canonical form is %q, want %q for the same URL respelled", same, canonical)
	}
}
