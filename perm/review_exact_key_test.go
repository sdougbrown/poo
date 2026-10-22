package perm_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// rawCall is a scripted call with the exact arguments the model wrote.
func rawCall(id, name, args string) pootest.Call {
	return pootest.Call{Name: name, ID: id, Arguments: json.RawMessage(args)}
}

// TestReviewExactKeyUsesThePreparedValue covers the plan's exact key: the
// tool plus its canonical prepared input. Two calls that prepare to the
// same value share the key, even when the model spells one of them with
// an explicit default.
func TestReviewExactKeyUsesThePreparedValue(t *testing.T) {
	policy, err := perm.New(perm.Options{DoomLoop: perm.DoomLoop{Action: perm.ActionDeny}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{
			rawCall("c1", "read", `{"path":"a.go"}`),
			rawCall("c2", "read", `{"path":"a.go","offset":0}`),
			rawCall("c3", "read", `{"path":"a.go"}`),
		}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	// The three calls prepare to the same readPrepared{path:"a.go", offset:0},
	// so the plan's exact key reaches its threshold on the third call.
	if tool.Calls() != 2 {
		t.Errorf("the tool ran %d time(s), want 2: the third call is the same prepared call", tool.Calls())
	}
	if got := s.lastResult(t); got != "Blocked: this exact call was repeated 3 times. Try a different approach." {
		t.Errorf("the model saw %q, want the doom-loop refusal on the third call", got)
	}
}
