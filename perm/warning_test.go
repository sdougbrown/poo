package perm_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
)

// The load-time warning names the fallback that will apply when the
// policy can ask and no asker can ever answer.

// TestTheWarningFiresWhenNoAskerCanEverAnswer covers the plan's rule: a
// policy that can ask, in a build whose chain can never answer, is
// warned about at load.
func TestTheWarningFiresWhenNoAskerCanEverAnswer(t *testing.T) {
	askers := []perm.Asker{perm.NewWireAsker(nil, 0)}
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAsk, Askers: askers})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	warning := perm.Warning(policy, policy.Askers())

	if warning == "" {
		t.Fatal("the load printed no warning, want one: no asker can ever answer")
	}
	if !strings.Contains(warning, "deny") {
		t.Errorf("the warning is %q, want it to name the fallback", warning)
	}
}

// TestTheWarningIsSilentWhenAnAskerCanAnswer covers the other side: a
// chain with an asker that may answer is not warned about.
func TestTheWarningIsSilentWhenAnAskerCanAnswer(t *testing.T) {
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAsk})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	askers := []perm.Asker{perm.NewWireAsker(&fakeClient{connected: true, answers: true}, 0)}

	if warning := perm.Warning(policy, askers); warning != "" {
		t.Errorf("the load warned %q, want no warning", warning)
	}
}

// TestTheWarningIsSilentWhenThePolicyCannotAsk covers a policy that
// never asks: there is nothing to warn about.
func TestTheWarningIsSilentWhenThePolicyCannotAsk(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Unmatched: perm.ActionAllow,
		DoomLoop:  perm.DoomLoop{Action: perm.ActionAllow},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	askers := []perm.Asker{perm.NewWireAsker(nil, 0)}

	if warning := perm.Warning(policy, askers); warning != "" {
		t.Errorf("the load warned %q, want no warning", warning)
	}
}

// TestTheWarningIsSilentWhenOnlyTheDefaultDoomLoopAsks covers the
// default path: the doom loop's default ask is not one an operator
// configured, so a chain that cannot answer it is not warned about at
// load.
func TestTheWarningIsSilentWhenOnlyTheDefaultDoomLoopAsks(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Unmatched: perm.ActionAllow,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	askers := []perm.Asker{perm.NewWireAsker(nil, 0)}

	if warning := perm.Warning(policy, askers); warning != "" {
		t.Errorf("the load warned %q, want no warning: only the doom loop asks", warning)
	}
}

// TestTheWarningFiresForADoomLoopSetToAsk covers an operator who sets
// the doom loop's action to ask: that ask is configured, and a chain
// that cannot answer it is warned about.
func TestTheWarningFiresForADoomLoopSetToAsk(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Unmatched: perm.ActionAllow,
		DoomLoop:  perm.DoomLoop{Action: perm.ActionAsk},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	askers := []perm.Asker{perm.NewWireAsker(nil, 0)}

	if warning := perm.Warning(policy, askers); !strings.Contains(warning, "deny") {
		t.Errorf("the load warned %q, want a warning that names the fallback", warning)
	}
}

// TestTheWarningFiresForAnAskRule covers the plan's rule: a rule that
// asks is an ask too.
func TestTheWarningFiresForAnAskRule(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Unmatched: perm.ActionAllow,
		Rules:     []perm.Rule{{Class: poo.ClassSideEffecting, Action: perm.ActionAsk}},
		DoomLoop:  perm.DoomLoop{Action: perm.ActionAllow},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	askers := []perm.Asker{perm.NewWireAsker(nil, 0)}

	if warning := perm.Warning(policy, askers); warning == "" {
		t.Fatal("the load printed no warning, want one: a rule asks")
	}
}
