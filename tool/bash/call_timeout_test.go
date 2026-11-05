package bash_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestACallTimeoutBoundsTheCall covers a model-supplied deadline: it ends a
// command that runs longer, with no configured timeout to beat.
func TestACallTimeoutBoundsTheCall(t *testing.T) {
	tool := bash.New()
	p, err := tool.Prepare([]byte(`{"command":"sleep 2","timeout":50}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Call(context.Background(), p, poo.ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || structured(t, res)["timed_out"] != true {
		t.Fatalf("expected a timeout error, got %s", res.Text)
	}
}

// TestTheConfiguredTimeoutCapsACallTimeout covers the operator's bound: a
// call that asks for longer than tools.bash.timeout_s still ends there.
func TestTheConfiguredTimeoutCapsACallTimeout(t *testing.T) {
	tool := bash.New()
	p, err := tool.Prepare([]byte(`{"command":"sleep 3","timeout":600000}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatal(err)
	}
	env := poo.ToolEnv{}
	env.Settings.BashTimeoutS = 1
	res, err := tool.Call(context.Background(), p, env)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || structured(t, res)["timed_out"] != true {
		t.Fatalf("the configured 1s timeout did not end the call: %s", res.Text)
	}
}

// TestATimeoutLeavesThePermissionKeyAlone covers identity: a rule, a
// decision and the summary see the command and never the timeout.
func TestATimeoutLeavesThePermissionKeyAlone(t *testing.T) {
	tool := bash.New()
	plain, err := tool.Prepare([]byte(`{"command":"true"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatal(err)
	}
	timed, err := tool.Prepare([]byte(`{"command":"true","timeout":1000}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if a, b := plain.(poo.CanonicalKeyer).CanonicalKey(), timed.(poo.CanonicalKeyer).CanonicalKey(); string(a) != string(b) {
		t.Errorf("the timeout changed the canonical key: %s vs %s", a, b)
	}
	if a, b := plain.(poo.EvidenceArgumenter).EvidenceArguments(), timed.(poo.EvidenceArgumenter).EvidenceArguments(); string(a) != string(b) {
		t.Errorf("the timeout changed the evidence: %s vs %s", a, b)
	}
	if timed.Summary() != plain.Summary() {
		t.Errorf("the timeout changed the summary: %q", timed.Summary())
	}
}

// TestAnyJSONNumberIsATimeout covers decoding: a huge value and an
// exponent form are accepted, so a call never fails over its timeout.
func TestAnyJSONNumberIsATimeout(t *testing.T) {
	tool := bash.New()
	for _, raw := range []string{
		`{"command":"true","timeout":999999999999999999999999999999999999}`,
		`{"command":"true","timeout":3e5}`,
		`{"command":"true","timeout":1500.5}`,
	} {
		if _, err := tool.Prepare([]byte(raw), poo.PrepareEnv{}); err != nil {
			t.Errorf("%s was rejected: %v", raw, err)
		}
	}
}
