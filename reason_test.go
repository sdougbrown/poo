package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// The v1 exit-code table: the codes the poo run CLI reports, one per
// settle reason, with 1 for anything unheard of so a bug never reports
// success.
func TestExitCodeMapsTheV1Table(t *testing.T) {
	cases := []struct {
		reason poo.Reason
		want   int
	}{
		{poo.ReasonEndTurn, 0},
		{poo.ReasonMaxTurns, 11},
		{poo.ReasonContextExhausted, 13},
		{poo.ReasonProviderError, 20},
		{poo.ReasonCancelled, 130},
		{poo.Reason("unheard_of"), 1},
	}
	for _, c := range cases {
		if got := poo.ExitCode(c.reason); got != c.want {
			t.Errorf("ExitCode(%q) is %d, want %d", c.reason, got, c.want)
		}
	}
}
