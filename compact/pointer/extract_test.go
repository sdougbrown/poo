package pointer

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
)

// fakeHistory is a history whose search answers with the matches it
// was built with.
type fakeHistory struct {
	matches []poo.HistoryMatch
}

func (f fakeHistory) Turns(context.Context) ([]int, error) {
	return nil, nil
}

func (f fakeHistory) Turn(context.Context, int) (string, error) {
	return "", nil
}

func (f fakeHistory) Search(context.Context, string) ([]poo.HistoryMatch, error) {
	return f.matches, nil
}

// TestTheSnippetBudgetIsAByteBudget covers the cap the digest's
// matched lines live under: the budget is a byte count, so a line that
// does not fit is cut to the bytes that remain, on a rune boundary -
// a multi-byte line comes back as text, and the bytes it adds stay the
// budget's.
func TestTheSnippetBudgetIsAByteBudget(t *testing.T) {
	c := New(Options{SnippetBudget: 32768})
	// One match whose text is one multi-byte rune repeated past the
	// whole budget: the cut has a rune boundary to find.
	matches := []poo.HistoryMatch{{Turn: 1, Text: strings.Repeat("世", 40000)}}
	out := c.extract(context.Background(), fakeHistory{matches: matches}, []string{"TURN"})
	used := 0
	for _, m := range out {
		used += len(m.Text)
		if !utf8.ValidString(m.Text) {
			t.Error("the matches carry text that is not valid UTF-8")
		}
	}
	if used > 32768 {
		t.Errorf("the matches carry %d bytes, over the %d-byte budget", used, 32768)
	}
	if len(out) == 0 || !strings.Contains(out[0].Text, "世") {
		t.Errorf("the matches are %v, want the line's text kept", out)
	}
}
