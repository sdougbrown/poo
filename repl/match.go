package repl

import (
	"sort"
	"strings"
	"unicode"
)

// What a matched fragment earns. A name that holds the whole fragment in
// one piece beats any scattering of its characters; and among the
// scattered ones, a character that starts a word or follows the one
// matched before it beats a character that lands in the middle of a run of
// others.
const (
	// scoreSubstring is what a name earns for holding the fragment whole.
	scoreSubstring = 1000
	// scoreBoundary is what a matched character earns at the start of a
	// word.
	scoreBoundary = 10
	// scoreRun is what a matched character earns when the one before it
	// in the name was matched too.
	scoreRun = 8
	// scoreChar is what every matched character earns.
	scoreChar = 1
)

// match is one catalog name a fragment matched, and how well.
type match struct {
	// name is the catalog name.
	name string
	// score is what the match earned.
	score int
}

// rankModels returns the catalog names a fragment matches, best first.
//
// The fragment is a subsequence: its characters must come in the name, in
// order, with anything between them, and case folded so a fleet's
// provider and model names are typed however the person remembers them.
// Names that match equally well come in name order, except the model the
// session already runs at, which comes last: a search that offers the
// model in use would spend the pick on a switch that changes nothing.
//
// A fragment that names no entry, and the empty fragment, rank nothing.
func rankModels(names []string, fragment, current string) []string {
	matches := make([]match, 0, len(names))
	for _, name := range names {
		if score, ok := scoreModel(name, fragment); ok {
			matches = append(matches, match{name: name, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.score != b.score {
			return a.score > b.score
		}
		aCurrent, bCurrent := a.name == current, b.name == current
		if aCurrent != bCurrent {
			return bCurrent
		}
		return a.name < b.name
	})
	ranked := make([]string, len(matches))
	for i, m := range matches {
		ranked[i] = m.name
	}
	return ranked
}

// scoreModel scores one name against a fragment: the points it earned,
// and whether every character of the fragment was there in order.
//
// The walk is left to right and greedy, so each character of the fragment
// takes the earliest place in the name that holds it. That is the
// alignment a person reading the list sees; a name whose better alignment
// is not the earliest one is offered lower than it could be, which costs
// nothing a picker can act on.
func scoreModel(name, fragment string) (int, bool) {
	if fragment == "" {
		return 0, false
	}
	folded := foldRunes(name)
	need := foldRunes(fragment)
	score := 0
	// prev is the place the character before this fragment's matched, and
	// next is the first place this one may take.
	prev, next := -2, 0
	for _, want := range need {
		at := -1
		for i := next; i < len(folded); i++ {
			if folded[i] == want {
				at = i
				break
			}
		}
		if at < 0 {
			return 0, false
		}
		score += scoreChar
		if at == 0 || !isWordRune(folded[at-1]) {
			score += scoreBoundary
		}
		if at == prev+1 {
			score += scoreRun
		}
		prev, next = at, at+1
	}
	if strings.Contains(string(folded), string(need)) {
		score += scoreSubstring
	}
	return score, true
}

// foldRunes returns a name as the runes the match is walked over, folded
// to lower case one rune at a time: a fleet's names carry characters that
// whole-string folding would widen into several.
func foldRunes(s string) []rune {
	runes := []rune(s)
	for i, r := range runes {
		runes[i] = unicode.ToLower(r)
	}
	return runes
}

// isWordRune reports whether a rune belongs to a word, so the rune after
// it opens one.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
