package history

import (
	"strings"
	"testing"
)

// TestTheCutHonorsTheBudgetAndTheCharacters covers the truncation
// arithmetic: a line over its character budget is cut on a character
// boundary with the marker, a line that fits is returned whole however
// many bytes its characters take.
func TestTheCutHonorsTheBudgetAndTheCharacters(t *testing.T) {
	multibyte := strings.Repeat("héllo", 600) // 3000 characters, 3600 bytes
	long := strings.Repeat("x", 3500)

	if got := cut(long, 2000); !strings.HasSuffix(got, "…") || len(got) > 2003 {
		t.Errorf("the cut line is %d bytes with marker %v, want the cut at the budget", len(got), strings.HasSuffix(got, "…"))
	}
	if got := cut(multibyte, 3000); strings.Contains(got, "…") {
		t.Errorf("the multi-byte line was cut though its %d characters fit the budget", len([]rune(multibyte)))
	}
	if got := cut("short", 2000); got != "short" {
		t.Errorf("the short line is %q, want it whole", got)
	}
}
