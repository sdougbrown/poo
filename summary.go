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
// permission check, so the cap keeps one line's payload far under the
// wire's output queue line budget.

// SummaryCapChars is the cap on a call's summary, in characters.
const SummaryCapChars = 2000

// SummaryElisionNote is the note that ends a cut summary. The verb
// takes the number of characters left out.
const SummaryElisionNote = "\n… %d more characters"

// CapSummary returns the head of the summary that the cap holds.
//
// A summary within the cap comes back unchanged. A longer one is cut on
// a character boundary and ends with SummaryElisionNote, so a reader of
// the text alone sees that it is not whole.
func CapSummary(text string) string {
	total := utf8.RuneCountInString(text)
	if total <= SummaryCapChars {
		return text
	}
	i := 0
	for n := 0; n < SummaryCapChars; n++ {
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	return text[:i] + fmt.Sprintf(SummaryElisionNote, total-SummaryCapChars)
}
