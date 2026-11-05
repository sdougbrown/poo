package bash

import (
	"testing"
	"time"
)

// TestResolveTimeout covers the per-call ceiling and the fallback order.
func TestResolveTimeout(t *testing.T) {
	for _, tc := range []struct {
		name     string
		call     *float64
		settings int
		tool     time.Duration
		want     time.Duration
	}{
		{"ceiling", ms(600001), 3, time.Second, 10 * time.Minute},
		{"huge", ms(1e100), 3, time.Second, 10 * time.Minute},
		{"fraction", ms(1500.5), 3, time.Second, 1500*time.Millisecond + 500*time.Microsecond},
		{"configured", nil, 3, time.Second, 3 * time.Second},
		{"zero", ms(0), 3, time.Second, 3 * time.Second},
		{"negative", ms(-1), 3, time.Second, 3 * time.Second},
		{"tool", nil, 0, time.Second, time.Second},
		{"default", nil, 0, 0, DefaultTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveTimeout(tc.call, tc.settings, tc.tool); got != tc.want {
				t.Errorf("timeout = %v, want %v", got, tc.want)
			}
		})
	}
}

// ms returns a requested timeout in milliseconds.
func ms(v float64) *float64 { return &v }
