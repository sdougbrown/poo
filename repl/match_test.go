package repl

import "testing"

// TestAFragmentRankstheNamesItHolds covers the order the catalog is
// offered in: the name that holds the whole fragment in one piece first,
// then the names that only hold its characters.
func TestAFragmentRankstheNamesItHolds(t *testing.T) {
	names := []string{"gemini-maxima", "gemma4", "meta/llama-3-70b"}

	got := rankModels(names, "gemma", "")
	if want := []string{"gemma4", "gemini-maxima"}; !sameNames(got, want) {
		t.Errorf("the fragment ranked %v, want %v", got, want)
	}
}

// TestARunDecidesBetweenTwoSubstringMatches covers the run as the
// deciding points: both names hold the fragment whole and both open it
// at the same boundary, so only the adjacency of the matched characters
// separates them. Without the run's points the two would tie and the
// name order would put "aab" first.
func TestARunDecidesBetweenTwoSubstringMatches(t *testing.T) {
	got := rankModels([]string{"aab", "abx"}, "ab", "")
	if want := []string{"abx", "aab"}; !sameNames(got, want) {
		t.Errorf("the fragment ranked %v, want %v", got, want)
	}
}

// TestAWordBoundaryRanksAboveTheMiddleOfAWord covers the boundary: the
// same character scores higher where a word starts.
func TestAWordBoundaryRanksAboveTheMiddleOfAWord(t *testing.T) {
	// "q" opens qwen's word and hides inside torque's.
	got := rankModels([]string{"torque-7b", "qwen"}, "q", "")
	if want := []string{"qwen", "torque-7b"}; !sameNames(got, want) {
		t.Errorf("the fragment %q ranked %v, want %v", "q", got, want)
	}
}

// TestTheCurrentModelSortsLastOnATie covers the tie: two names that match
// a fragment equally well offer the one the session is not already on
// first.
func TestTheCurrentModelSortsLastOnATie(t *testing.T) {
	names := []string{"gemma-3-27b", "gemma4"}

	got := rankModels(names, "gemma", "gemma4")
	if want := []string{"gemma-3-27b", "gemma4"}; !sameNames(got, want) {
		t.Errorf("the fragment ranked %v, want %v", got, want)
	}
	// The other way round: the tie is the current model's, not the name's.
	got = rankModels(names, "gemma", "gemma-3-27b")
	if want := []string{"gemma4", "gemma-3-27b"}; !sameNames(got, want) {
		t.Errorf("the fragment ranked %v, want %v", got, want)
	}
}

// TestATieWithoutTheCurrentModelIsInNameOrder covers the rest of the tie:
// the order does not depend on the catalog's.
func TestATieWithoutTheCurrentModelIsInNameOrder(t *testing.T) {
	names := []string{"qwen-max", "qwen-plus"}

	got := rankModels(names, "qwen", "")
	if want := []string{"qwen-max", "qwen-plus"}; !sameNames(got, want) {
		t.Errorf("the fragment ranked %v, want %v", got, want)
	}
}

// TestTheFragmentIgnoresCaseAndAlphabet covers the names a pi fleet
// carries: the match folds case, unicode included.
func TestTheFragmentIgnoresCaseAndAlphabet(t *testing.T) {
	names := []string{"gemma-Ωmega", "Qwen"}

	if got := rankModels(names, "ωM", ""); !sameNames(got, []string{"gemma-Ωmega"}) {
		t.Errorf("the fragment %q ranked %v, want gemma-Ωmega", "ωM", got)
	}
	if got := rankModels(names, "QW", ""); !sameNames(got, []string{"Qwen"}) {
		t.Errorf("the fragment %q ranked %v, want Qwen", "QW", got)
	}
}

// TestTheFragmentHoldsTheCharactersInOrder covers the subsequence: a
// fragment's characters that only come in the wrong order match nothing.
func TestTheFragmentHoldsTheCharactersInOrder(t *testing.T) {
	if got := rankModels([]string{"gemma4"}, "4ammeg", ""); len(got) != 0 {
		t.Errorf("the fragment ranked %v, want nothing", got)
	}
	if got := rankModels([]string{"gemma4"}, "mga", ""); len(got) != 0 {
		t.Errorf("the fragment ranked %v, want nothing", got)
	}
}

// TestAFragmentNoNameHoldsRanksNothing covers the empty answer.
func TestAFragmentNoNameHoldsRanksNothing(t *testing.T) {
	if got := rankModels([]string{"gemma4", "qwen"}, "zzz", ""); len(got) != 0 {
		t.Errorf("the fragment ranked %v, want nothing", got)
	}
	if got := rankModels([]string{"gemma4", "qwen"}, "", ""); len(got) != 0 {
		t.Errorf("the empty fragment ranked %v, want nothing", got)
	}
}

// sameNames compares two name lists.
func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
