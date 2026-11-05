package bash_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestCallTimeoutOverridesTheConfiguredTimeout covers a model-supplied deadline.
func TestCallTimeoutOverridesTheConfiguredTimeout(t *testing.T) {
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

// TestTimeoutDoesNotChangePermissionEvidence covers validation and identity.
func TestTimeoutDoesNotChangePermissionEvidence(t *testing.T) {
	tool := bash.New()
	plain, err := tool.Prepare([]byte(`{"command":"true"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatal(err)
	}
	withTimeout, err := tool.Prepare([]byte(`{"command":"true","timeout":1000}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatal(err)
	}
	plainKey := plain.(poo.CanonicalKeyer).CanonicalKey()
	timeoutKey := withTimeout.(poo.CanonicalKeyer).CanonicalKey()
	if string(plainKey) != string(timeoutKey) {
		t.Errorf("timeout changed canonical key: %s vs %s", plainKey, timeoutKey)
	}
	if string(plain.(poo.EvidenceArgumenter).EvidenceArguments()) != string(withTimeout.(poo.EvidenceArgumenter).EvidenceArguments()) || withTimeout.Summary() != plain.Summary() {
		t.Error("timeout changed the evidence or summary")
	}
	if _, err := tool.Prepare([]byte(`{"command":"true","timeout":999999999999999999999999999999999999}`), poo.PrepareEnv{}); err != nil {
		t.Errorf("huge timeout rejected: %v", err)
	}
	if _, err := tool.Prepare([]byte(`{"command":"true","timeout":3e5}`), poo.PrepareEnv{}); err != nil {
		t.Errorf("an exponent-form timeout was rejected: %v", err)
	}
	if _, err := tool.Prepare([]byte(`{"command":"true","timeout":1000,"unknown":1}`), poo.PrepareEnv{}); err == nil {
		t.Error("unknown field accepted")
	}
}
