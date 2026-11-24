package exec_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/exec"
)

// TestTheEnvironmentLosesTheScrubbedNames covers the environment rules: the names and the globs are gone, and a
// passthrough limits the environment to the names on it.
func TestTheEnvironmentLosesTheScrubbedNames(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"POO_TEST_API_KEY=secret",
		"POO_TEST_SECRET_ONE=secret",
		"POO_TEST_PLAIN=visible",
	}

	got := exec.ScrubEnv(base, exec.Scrub{
		Names: []string{"POO_TEST_API_KEY"},
		Globs: []string{"POO_TEST_SECRET_*"},
	})
	want := []string{"PATH=/usr/bin", "POO_TEST_PLAIN=visible"}
	if len(got) != len(want) {
		t.Fatalf("the environment is %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the environment is %v, want %v", got, want)
			break
		}
	}
}

// TestAPassthroughLimitsTheEnvironment covers the passthrough:
// the environment holds exactly the names on it.
func TestAPassthroughLimitsTheEnvironment(t *testing.T) {
	base := []string{"POO_TEST_KEEP=kept", "POO_TEST_OTHER=dropped"}

	got := exec.ScrubEnv(base, exec.Scrub{Passthrough: []string{"POO_TEST_KEEP"}})
	if len(got) != 1 || got[0] != "POO_TEST_KEEP=kept" {
		t.Errorf("the environment is %v, want only the passthrough name", got)
	}
}

// TestAScrubbedNameStaysGoneThroughAPassthrough covers the order of
// the rules: a passthrough name that is scrubbed as well is still
// gone.
func TestAScrubbedNameStaysGoneThroughAPassthrough(t *testing.T) {
	base := []string{"POO_TEST_KEEP=kept", "POO_TEST_API_KEY=secret"}

	got := exec.ScrubEnv(base, exec.Scrub{
		Names:       []string{"POO_TEST_API_KEY"},
		Passthrough: []string{"POO_TEST_KEEP", "POO_TEST_API_KEY"},
	})
	if len(got) != 1 || got[0] != "POO_TEST_KEEP=kept" {
		t.Errorf("the environment is %v, want only the name that is not scrubbed", got)
	}
}

// TestTheChildEnvironmentKeepsItsContract covers the helper the
// command uses: a nil base with nothing to scrub inherits the parent's
// environment.
func TestTheChildEnvironmentKeepsItsContract(t *testing.T) {
	if got := exec.ChildEnv(nil, nil); got != nil {
		t.Errorf("the environment is %v, want nil so the child inherits", got)
	}
	got := exec.ChildEnv([]string{"A=1", "B=2"}, []string{"A"})
	if len(got) != 1 || got[0] != "B=2" {
		t.Errorf("the environment is %v, want only B", got)
	}
}

// TestTheSessionScopeResolvesOnlyTheSessionID covers what a command's
// session references may ask for: `session:id` resolves to the ID the
// caller holds, and any other name, or an ID the caller does not have,
// is an error that names the reference.
func TestTheSessionScopeResolvesOnlyTheSessionID(t *testing.T) {
	resolve := exec.SessionScope("sess-01")
	id, err := resolve("session", "id")
	if err != nil || id != "sess-01" {
		t.Errorf("session:id resolved to %q, %v; want the caller's ID", id, err)
	}
	// The resolver is reusable: a second attempt resolves the same way.
	if id, err = resolve("session", "id"); err != nil || id != "sess-01" {
		t.Errorf("a second resolution gave %q, %v", id, err)
	}
	if _, err := resolve("session", "run_id"); err == nil || !strings.Contains(err.Error(), `"session:run_id"`) {
		t.Errorf("session:run_id resolved, want an error naming the reference: %v", err)
	}
	if _, err := exec.SessionScope("")("session", "id"); err == nil {
		t.Error("an empty session ID resolved")
	}
}

// TestTheScopedResolverCarriesTheSubscriptionValues covers the resolver a
// feed instance runs with: `scope:NAME` resolves to the value the
// subscription supplied for that field, `session:id` still resolves to the
// session's own ID, and a name neither scope carries fails.
func TestTheScopedResolverCarriesTheSubscriptionValues(t *testing.T) {
	resolve := exec.ScopedResolver("sess-01", map[string]string{"url": "https://example.test/repo"})
	url, err := resolve("scope", "url")
	if err != nil || url != "https://example.test/repo" {
		t.Errorf("scope:url resolved to %q, %v; want the subscription's value", url, err)
	}
	id, err := resolve("session", "id")
	if err != nil || id != "sess-01" {
		t.Errorf("session:id resolved to %q, %v beside a scope", id, err)
	}
	if _, err := resolve("scope", "repo"); err == nil || !strings.Contains(err.Error(), `"scope:repo"`) {
		t.Errorf("scope:repo resolved, want an error naming the reference: %v", err)
	}
	// A session reference keeps its own refusals, so the two scopes do
	// not blur into one.
	if _, err := resolve("session", "run_id"); err == nil || !strings.Contains(err.Error(), `"session:run_id"`) {
		t.Errorf("session:run_id resolved, want an error naming the reference: %v", err)
	}
	if _, err := exec.ScopedResolver("", nil)("scope", "url"); err == nil {
		t.Error("a scope value resolved with no scope at all")
	}
	if _, err := resolve("", "url"); err == nil {
		t.Error("a reference with no scope resolved")
	}
}
