package session

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// TestADeliveredMessageSettlesItsInput covers a crash between an input's
// message and its delivered record: the message names the input, so the
// recovery does not report the input as never delivered.
func TestADeliveredMessageSettlesItsInput(t *testing.T) {
	pend := pendingOf("s1", []poo.Record{
		createdRecord("s1"),
		inputQueued("in1", poo.Steer),
		&poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), Role: poo.RoleUser, InputID: "in1"},
		inputQueued("in2", poo.FollowUp),
	})
	if len(pend.Inputs) != 1 || pend.Inputs[0].InputID != "in2" {
		t.Fatalf("pending inputs = %+v, want only in2", pend.Inputs)
	}
}
