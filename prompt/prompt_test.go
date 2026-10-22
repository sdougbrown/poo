package prompt_test

import (
	"reflect"
	"testing"

	"github.com/sdougbrown/poo/prompt"
)

// TestTheOrderIsReturnedAsACopy covers the copy protection: a caller
// that changes the slice Order returns does not change the order the
// next caller sees.
func TestTheOrderIsReturnedAsACopy(t *testing.T) {
	before := prompt.Order()
	after := make([]string, len(before))
	copy(after, before)

	mutated := prompt.Order()
	mutated[0] = "changed"
	mutated = append(mutated, "extra")
	if len(mutated) != len(after)+1 {
		t.Fatalf("the mutated slice holds %d entries, want %d", len(mutated), len(after)+1)
	}

	if got := prompt.Order(); !reflect.DeepEqual(got, after) {
		t.Errorf("the order after a caller edit is %v, want %v", got, after)
	}
}
