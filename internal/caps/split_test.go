package caps

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTheSplitKeepsBothHalvesWhole covers the split's contract: each
// half is within its budget, and neither half splits a rune.
func TestTheSplitKeepsBothHalvesWhole(t *testing.T) {
	cases := []struct {
		name string
		text string
		half int
	}{
		{"short", "hello", 10},
		{"exact", "hello", 5},
		{"long", strings.Repeat("a", 100), 10},
		{"multibyte", strings.Repeat("€", 100), 10},
		{"mixed", strings.Repeat("a€b", 40), 10},
		{"zero", "hello", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			head, tail := SplitRunes(c.text, c.half)
			if c.half <= 0 {
				if head != "" || tail != "" {
					t.Errorf("the split is %q/%q, want two empty halves", head, tail)
				}
				return
			}
			if !utf8.ValidString(head) || !utf8.ValidString(tail) {
				t.Errorf("the split broke a rune: head %q tail %q", head, tail)
			}
			if got := utf8.RuneCountInString(head); got > c.half {
				t.Errorf("the head holds %d characters, want at most %d", got, c.half)
			}
			if got := utf8.RuneCountInString(tail); got > c.half {
				t.Errorf("the tail holds %d characters, want at most %d", got, c.half)
			}
			// The two halves together hold the text's ends, and nothing
			// in the middle.
			wantHead, wantTail := wantSplit(c.text, c.half)
			if head != wantHead || tail != wantTail {
				t.Errorf("the split is %q/%q, want %q/%q", head, tail, wantHead, wantTail)
			}
		})
	}
}

// wantSplit is the split the old rune-slice implementation produced.
func wantSplit(s string, half int) (string, string) {
	if half <= 0 {
		return "", ""
	}
	runes := []rune(s)
	if len(runes) <= half {
		return s, ""
	}
	return string(runes[:half]), string(runes[len(runes)-half:])
}

// TestTheSplitCountsInvalidBytesAsCharacters covers text that is not
// valid UTF-8: each stray byte counts as one character, so the tail stays
// within its budget and never repeats the head.
func TestTheSplitCountsInvalidBytesAsCharacters(t *testing.T) {
	text := "ab" + strings.Repeat("\xff", 20) + "cd"
	head, tail := SplitRunes(text, 4)
	if head != "ab\xff\xff" {
		t.Errorf("the head is %q, want %q", head, "ab\xff\xff")
	}
	if tail != "\xff\xffcd" {
		t.Errorf("the tail is %q, want %q", tail, "\xff\xffcd")
	}
}

// TestRuneCountAgreesWithTheSplit covers the count the callers size a
// cap with: it counts a stray byte as one character, so a body that fits
// the count fits the cut the count feeds.
func TestRuneCountAgreesWithTheSplit(t *testing.T) {
	text := "ab" + strings.Repeat("\xff", 20) + "cd"
	if got, want := RuneCount(text), 24; got != want {
		t.Errorf("RuneCount is %d, want %d", got, want)
	}
	head, _ := SplitRunes(text, RuneCount(text))
	if head != text {
		t.Errorf("a split at the full count lost text: %q", head)
	}
}
