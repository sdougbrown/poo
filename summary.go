package poo

import (
	"fmt"
	"unicode/utf8"
)

// The cap on the summary text the core puts in its events and records.
//
// A summary is a human-facing line: the model's own description of a
// call, a bash command, a permission ask's command. A script can make
// one as long as its own arguments, and the event goes out before the
// permission check. The cap keeps one line's payload far under the
// wire's output queue line budget.

// SummaryCapChars is the cap on a call's summary, in characters.
const SummaryCapChars = 2000

// SummaryElisionNote ends a cut summary. The verb takes the number of
// characters left out.
const SummaryElisionNote = "\n… %d more characters"

// cutRuneHead returns the head of the text that holds limit characters
// on a character boundary, and the number of characters left out. A
// text within the limit comes back whole, with nothing left out.
func cutRuneHead(text string, limit int) (string, int) {
	total := utf8.RuneCountInString(text)
	if total <= limit {
		return text, 0
	}
	i := 0
	for n := 0; n < limit; n++ {
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	return text[:i], total - limit
}

// CapSummary returns the head of the summary that the cap holds.
//
// A summary within the cap comes back unchanged. A longer one is cut
// on a character boundary and ends with SummaryElisionNote. A reader
// of the text alone sees that it is not whole.
func CapSummary(text string) string {
	head, elided := cutRuneHead(text, SummaryCapChars)
	if elided == 0 {
		return head
	}
	return head + fmt.Sprintf(SummaryElisionNote, elided)
}
