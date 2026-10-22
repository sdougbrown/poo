package bash_test

import (
	"testing"

	"github.com/sdougbrown/poo/tool/bash"
)

// TestTheEnvironmentLosesTheScrubbedNames covers the plan's
// environment rules: the names and the globs are gone, and a
// passthrough limits the environment to the names on it.
func TestTheEnvironmentLosesTheScrubbedNames(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"POO_TEST_API_KEY=secret",
		"POO_TEST_SECRET_ONE=secret",
		"POO_TEST_PLAIN=visible",
	}

	got := bash.ScrubEnv(base, bash.Scrub{
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

// TestAPassthroughLimitsTheEnvironment covers the plan's passthrough:
// the environment holds exactly the names on it.
func TestAPassthroughLimitsTheEnvironment(t *testing.T) {
	base := []string{"POO_TEST_KEEP=kept", "POO_TEST_OTHER=dropped"}

	got := bash.ScrubEnv(base, bash.Scrub{Passthrough: []string{"POO_TEST_KEEP"}})
	if len(got) != 1 || got[0] != "POO_TEST_KEEP=kept" {
		t.Errorf("the environment is %v, want only the passthrough name", got)
	}
}

// TestAScrubbedNameStaysGoneThroughAPassthrough covers the order of
// the rules: a passthrough name that is scrubbed as well is still
// gone.
func TestAScrubbedNameStaysGoneThroughAPassthrough(t *testing.T) {
	base := []string{"POO_TEST_KEEP=kept", "POO_TEST_API_KEY=secret"}

	got := bash.ScrubEnv(base, bash.Scrub{
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
	if got := bash.ChildEnv(nil, nil); got != nil {
		t.Errorf("the environment is %v, want nil so the child inherits", got)
	}
	got := bash.ChildEnv([]string{"A=1", "B=2"}, []string{"A"})
	if len(got) != 1 || got[0] != "B=2" {
		t.Errorf("the environment is %v, want only B", got)
	}
}
