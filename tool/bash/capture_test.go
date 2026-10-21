package bash

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTheCaptureCapNeverSplitsARune covers the capture cap: a cut that
// lands in the middle of a character drops the partial one rather than
// turning it into a replacement character.
func TestTheCaptureCapNeverSplitsARune(t *testing.T) {
	// The euro sign is three bytes, so a cap that is not a multiple of
	// three lands in the middle of one.
	for _, limit := range []int{1, 2, 4, 5, 7, 8, 10} {
		b := &cappedBuffer{limit: limit}
		_, _ = b.Write([]byte(strings.Repeat("€", 10)))
		got := b.String()
		if !utf8.ValidString(got) {
			t.Errorf("the capture at %d bytes is not valid UTF-8: %q", limit, got)
		}
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Errorf("the capture at %d bytes holds a replacement character: %q", limit, got)
		}
		want := limit - limit%3
		if utf8.RuneCountInString(got) != want/3 {
			t.Errorf("the capture at %d bytes holds %d characters, want %d",
				limit, utf8.RuneCountInString(got), want/3)
		}
	}
}

// TestTheCaptureCapKeepsWholeCharacters covers the same cap with a
// limit that lands on a character boundary.
func TestTheCaptureCapKeepsWholeCharacters(t *testing.T) {
	b := &cappedBuffer{limit: 9}
	_, _ = b.Write([]byte(strings.Repeat("€", 10)))
	if got := b.String(); got != strings.Repeat("€", 3) {
		t.Errorf("the capture is %q, want three euro signs", got)
	}
}

// TestTheCaptureKeepsInvalidBytesItDidNotCut covers output that ends in a
// byte that is not a character: with no cut, the capture holds what the
// command printed.
func TestTheCaptureKeepsInvalidBytesItDidNotCut(t *testing.T) {
	for _, out := range []string{"ok\xff", "\xff", "a\xe2\x82"} {
		b := &cappedBuffer{limit: 1 << 20}
		_, _ = b.Write([]byte(out))
		if got := b.String(); got != out {
			t.Errorf("the capture of %q is %q, want it unchanged", out, got)
		}
	}
}
