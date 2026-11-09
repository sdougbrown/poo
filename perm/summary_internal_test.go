package perm

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The ask's summary cap: a bash command or a script's arguments can be
// as long as the call itself, and the ask reaches the client as one
// line, so the ask's summary holds to the cap.

type capPrepared struct{ summary string }

func (p capPrepared) Summary() string { return p.summary }

type capAsker struct{ summary string }

func (p capAsker) Summary() string    { return p.summary }
func (p capAsker) AskSummary() string { return p.summary }

// TestTheAskSummaryIsCutToTheCap covers the cap on the ask's summary:
// the ask shows the call itself, so a summary over the cap is cut with
// the note, and a summary within the cap is unchanged.
func TestTheAskSummaryIsCutToTheCap(t *testing.T) {
	big := strings.Repeat("a", poo.SummaryCapChars+100)
	// The summaries are ASCII, so the head cuts at the byte.
	want := big[:poo.SummaryCapChars] + "\n… 100 more characters"

	if got := summaryOf(poo.ToolCallInfo{Prepared: capAsker{summary: big}}); got != want {
		t.Errorf("the asker's summary is %d characters, want the capped text", len(got))
	}
	if got := summaryOf(poo.ToolCallInfo{Prepared: capPrepared{summary: big}}); got != want {
		t.Errorf("the plain summary is %d characters, want the capped text", len(got))
	}
	if got := summaryOf(poo.ToolCallInfo{Prepared: capPrepared{summary: "short"}}); got != "short" {
		t.Errorf("the short summary is %q, want it unchanged", got)
	}
}
