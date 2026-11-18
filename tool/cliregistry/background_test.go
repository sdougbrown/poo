package cliregistry_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// TestTheCallToolTakesBackgroundJobs pins the opt-in: call_cli is a
// background-capable tool the way bash is, so a registered CLI the
// model wants off the critical path becomes a job.
func TestTheCallToolTakesBackgroundJobs(t *testing.T) {
	catalog := fixtureCatalog(t)
	if got := cliregistry.NewCall(catalog).Spec().Background; got != poo.BackgroundAllowed {
		t.Errorf("call_cli's background mode is %q, want %q", got, poo.BackgroundAllowed)
	}
}
