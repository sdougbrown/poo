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

// TestACanonicalPathIsCleanedOfItsDotSegments covers the path: the
// server normalizes dot segments, so the comparison does too, and a
// protected path spelled with a detour still matches its rule.
func TestACanonicalPathIsCleanedOfItsDotSegments(t *testing.T) {
	canonical, ok := Canonical("https://internal.example.com/x/../secret")
	if !ok {
		t.Fatal("the URL did not canonicalize")
	}
	want := "https://internal.example.com/secret"
	if canonical != want {
		t.Errorf("the canonical form is %q, want %q", canonical, want)
	}
	if !WithinHost(canonical, "https://internal.example.com/secret") {
		t.Errorf("the detoured path does not match the prefix for %q, want it within the host's path", want)
	}
}

// TestCanonicalRefusesWhatIsNotAURL covers the guard clause: a value
// that is not an absolute http(s) URL is reported so, and the caller
// compares lexically instead.
func TestCanonicalRefusesWhatIsNotAURL(t *testing.T) {
	for _, value := range []string{
		"ftp://example.com/file",
		"example.com/page",
		"//example.com/page",
		"https:///page",
	} {
		if canonical, ok := Canonical(value); ok {
			t.Errorf("Canonical(%q) is %q, want it refused", value, canonical)
		}
	}
}
