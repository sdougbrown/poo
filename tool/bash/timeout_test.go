package bash

import (
	"testing"
	"time"
)

// TestResolveTimeout covers the bound a call runs under: a call's own
// timeout bounds it, but never past the operator's configured timeout.
// requestedTimeout has already clamped it to MaxCallTimeout.
func TestResolveTimeout(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested time.Duration
		settings  int
		tool      time.Duration
		want      time.Duration
	}{
		{"unconfigured call", 5 * time.Minute, 0, 0, 5 * time.Minute},
		{"unconfigured default", 0, 0, 0, DefaultTimeout},
		{"configured call below", 2 * time.Second, 3, 0, 2 * time.Second},
		{"configured is the ceiling", 5 * time.Minute, 3, 0, 3 * time.Second},
		{"configured default", 0, 3, 0, 3 * time.Second},
		{"tool timeout is the ceiling", 5 * time.Minute, 0, time.Second, time.Second},
		{"settings beat the tool", 0, 3, time.Second, 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveTimeout(tc.requested, tc.settings, tc.tool); got != tc.want {
				t.Errorf("timeout = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRequestedTimeout covers the arguments: any JSON number decodes,
// a value past MaxCallTimeout is clamped, and zero or below names none.
func TestRequestedTimeout(t *testing.T) {
	for _, tc := range []struct {
		name string
		ms   float64
		want time.Duration
	}{
		{"plain", 1500, 1500 * time.Millisecond},
		{"fraction", 1500.5, 1500*time.Millisecond + 500*time.Microsecond},
		{"past the ceiling", 600001, MaxCallTimeout},
		{"huge", 1e100, MaxCallTimeout},
		{"zero", 0, 0},
		{"negative", -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.ms
			if got := requestedTimeout(&v); got != tc.want {
				t.Errorf("requested = %v, want %v", got, tc.want)
			}
		})
	}
	if got := requestedTimeout(nil); got != 0 {
		t.Errorf("no timeout requested = %v, want 0", got)
	}
}
