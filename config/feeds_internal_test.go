package config

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/feed"
)

// TestTheFeedIdlePoliciesMapEveryValue covers the mapping the manifest's
// on_idle goes through: every value the loader accepts has a policy in
// the core, and a value neither side names is a failure.
func TestTheFeedIdlePoliciesMapEveryValue(t *testing.T) {
	cases := []struct {
		// value is what the manifest says.
		value string
		// want is the policy it maps to.
		want poo.OnIdle
		// wantErr reports that the value maps to nothing.
		wantErr bool
	}{
		{value: "", want: ""},
		{value: feed.OnIdleWake, want: poo.OnIdleWake},
		{value: feed.OnIdleHold, want: poo.OnIdleHold},
		{value: "sometimes", wantErr: true},
	}
	for _, c := range cases {
		got, err := onIdleOf(c.value)
		if c.wantErr {
			if err == nil {
				t.Errorf("the on_idle %q mapped to %q, want a failure", c.value, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("the on_idle %q failed: %v", c.value, err)
			continue
		}
		if got != c.want {
			t.Errorf("the on_idle %q maps to %q, want %q", c.value, got, c.want)
		}
	}
}
