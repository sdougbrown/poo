package poo

import (
	"testing"
	"time"
)

// TestTheFeedBackoffDoublesUpToACap covers the restart rule: the wait
// doubles from one second, stops at a minute, and an attempt that ran a
// minute resets it.
func TestTheFeedBackoffDoublesUpToACap(t *testing.T) {
	for name, tc := range map[string]struct {
		ran      time.Duration
		previous time.Duration
		want     time.Duration
	}{
		"first attempt":        {ran: 0, previous: 0, want: time.Second},
		"no previous wait":     {ran: 0, previous: -time.Second, want: time.Second},
		"doubles":              {ran: 0, previous: time.Second, want: 2 * time.Second},
		"caps":                 {ran: 0, previous: 40 * time.Second, want: feedMaxRestartDelay},
		"stays at the cap":     {ran: 0, previous: feedMaxRestartDelay, want: feedMaxRestartDelay},
		"a long attempt reset": {ran: feedLongAttempt, previous: feedMaxRestartDelay, want: feedFirstRestartDelay},
		"past the reset":       {ran: 2 * feedLongAttempt, previous: 20 * time.Second, want: feedFirstRestartDelay},
	} {
		if got := feedRestartDelay(tc.ran, tc.previous); got != tc.want {
			t.Errorf("%s: the wait is %s, want %s", name, got, tc.want)
		}
	}
}
