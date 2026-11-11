package poo

import (
	"slices"
	"testing"

	"github.com/sdougbrown/poo/wire/protocol"
)

// TestTheProtocolLevelNamesMatchTheCore pins the two copies of the
// reasoning level vocabulary to each other: the core refuses levels by
// this list, and the wire carries it, so a copy that drifts is a
// refusal that names levels the other side does not know.
func TestTheProtocolLevelNamesMatchTheCore(t *testing.T) {
	if !slices.Equal(ReasoningLevelNames, protocol.ReasoningLevelNames) {
		t.Errorf("the core's levels are %v, the protocol's are %v",
			ReasoningLevelNames, protocol.ReasoningLevelNames)
	}
}
