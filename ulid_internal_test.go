package poo

import (
	"bytes"
	"testing"
	"time"
)

// TestTheULIDEncodingIsCrockfordBase32 covers the layout: 48 bits of
// milliseconds then 80 bits of entropy, 26 characters, the time first.
func TestTheULIDEncodingIsCrockfordBase32(t *testing.T) {
	zero := newULID(time.UnixMilli(0), bytes.NewReader(make([]byte, 10)))
	if zero != "00000000000000000000000000" {
		t.Errorf("the zero ULID is %q", zero)
	}
	ones := bytes.Repeat([]byte{0xff}, 10)
	max := newULID(time.UnixMilli(1<<48-1), bytes.NewReader(ones))
	if max != "7ZZZZZZZZZZZZZZZZZZZZZZZZZ" {
		t.Errorf("the largest ULID is %q", max)
	}
	// 1 ms with entropy 1: the time ends at the 10th character, the
	// entropy at the 26th.
	one := newULID(time.UnixMilli(1), bytes.NewReader([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1}))
	if one != "00000000010000000000000001" {
		t.Errorf("the ULID for 1 ms and entropy 1 is %q", one)
	}
}
