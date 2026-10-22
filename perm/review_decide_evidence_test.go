package perm_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	permissive "github.com/sdougbrown/poo/perm/decide"
	"github.com/sdougbrown/poo/pootest"
)

// TestReviewTheDecideAskerShowsTheCall covers the call under question:
// the decision model must be shown the call it is asked to approve. The
// live evaluation puts the command in the ask's summary.
func TestReviewTheDecideAskerShowsTheCall(t *testing.T) {
	decider := pootest.NewDecider(0.9)
	asker := permissive.New(decider, permissive.Options{})
	history := poo.NewHistory([]poo.Message{
		poo.NewTextMessage(poo.RoleUser, "clean up the disk space"),
	}, nil, nil)
	if _, err := asker.Ask(t.Context(), perm.Ask{
		Tool:    "bash",
		Summary: "rm -rf ~",
		Class:   poo.ClassSideEffecting,
		Reason:  poo.ReasonPolicy,
		History: history,
	}); err != nil {
		t.Fatalf("asking: %v", err)
	}
	raw, err := json.Marshal(decider.States()[0])
	if err != nil {
		t.Fatalf("encoding the state: %v", err)
	}
	if !strings.Contains(string(raw), "rm -rf") {
		t.Fatalf("the decision model was never shown the call: %s", raw)
	}
}
