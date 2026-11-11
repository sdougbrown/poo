package perm_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
)

// The load-time warning names the fallback that will apply when the
// policy can ask and no asker can ever answer.

// TestTheWarningFiresWhenNoAskerCanEverAnswer covers the rule: a
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

	warning := perm.Warning(policy, askers)

	if warning == "" {
		t.Fatal("the load printed no warning, want one: the doom loop is set to ask")
	}
	if !strings.Contains(warning, "deny") {
		t.Errorf("the warning is %q, want it to name the fallback", warning)
	}
}

// The wire client warning is the session-time counterpart of the load
// warning: a client that is connected but cannot answer is told, with
// its session, why every ask falls back.

// TestTheWireClientWarningFiresForAClientWithoutTheCapability covers
// the rule: a policy that can ask, a chain that asks the wire,
// and a client that did not declare the permission_answers capability
// are warned about when the session is built. The warning names the
// cause and the capability that fixes it.
func TestTheWireClientWarningFiresForAClientWithoutTheCapability(t *testing.T) {
	client := &fakeClient{connected: true}
	askers := []perm.Asker{perm.NewWireAsker(client, 0)}
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAsk, Askers: askers})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	warning := perm.WireClientWarning(policy, policy.Askers(), client)

	if warning == "" {
		t.Fatal("the session carried no warning, want one: the client cannot answer")
	}
	if !strings.Contains(warning, "permission_answers") {
		t.Errorf("the warning is %q, want it to name the capability", warning)
	}
	if !strings.Contains(warning, "deny") {
		t.Errorf("the warning is %q, want it to name the fallback", warning)
	}
}

// TestTheWireClientWarningIsSilentWhenTheClientDeclaredTheCapability
// covers the other side: a client that declared the capability is not
// warned about.
func TestTheWireClientWarningIsSilentWhenTheClientDeclaredTheCapability(t *testing.T) {
	client := &fakeClient{connected: true, answers: true}
	askers := []perm.Asker{perm.NewWireAsker(client, 0)}
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAsk, Askers: askers})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	if warning := perm.WireClientWarning(policy, policy.Askers(), client); warning != "" {
		t.Errorf("the session warned %q, want no warning", warning)
	}
}

// TestTheWireClientWarningIsSilentWhenThePolicyCannotAsk covers a
// policy that never asks: there is nothing to warn about.
func TestTheWireClientWarningIsSilentWhenThePolicyCannotAsk(t *testing.T) {
	client := &fakeClient{connected: true}
	askers := []perm.Asker{perm.NewWireAsker(client, 0)}
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAllow, Askers: askers})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	if warning := perm.WireClientWarning(policy, policy.Askers(), client); warning != "" {
		t.Errorf("the session warned %q, want no warning: the policy cannot ask", warning)
	}
}

// TestTheWireClientWarningIsSilentWithoutAWireAsker covers a chain that
// does not ask the wire: the client's capability is not what answers
// the asks, so there is nothing to warn about.
func TestTheWireClientWarningIsSilentWithoutAWireAsker(t *testing.T) {
	client := &fakeClient{connected: true}
	askers := []perm.Asker{&fakeAsker{available: true}}
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAsk, Askers: askers})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	if warning := perm.WireClientWarning(policy, policy.Askers(), client); warning != "" {
		t.Errorf("the session warned %q, want no warning: the chain does not ask the wire", warning)
	}
}

// TestTheWireClientWarningIsSilentWhenAnotherAskerCanAnswer covers a
// chain that asks the wire and another asker that can answer: the ask
// does not fall back, so the client is not warned about it.
func TestTheWireClientWarningIsSilentWhenAnotherAskerCanAnswer(t *testing.T) {
	client := &fakeClient{connected: true}
	askers := []perm.Asker{perm.NewWireAsker(client, 0), &fakeAsker{available: true}}
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAsk, Askers: askers})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	if warning := perm.WireClientWarning(policy, policy.Askers(), client); warning != "" {
		t.Errorf("the session warned %q, want no warning: another asker answers", warning)
	}
}

// TestTheWarningFiresForAnAskRule covers the rule: a rule that
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

// deadAsker is an asker that can never answer, like a model asker with
// no decider.
type deadAsker struct{ fakeAsker }

// CanEverAnswer reports that the asker never answers.
func (a *deadAsker) CanEverAnswer() bool { return false }

// TestTheWireClientWarningFiresBesideAnAskerThatCannotAnswer covers a
// chain whose other asker can never answer: every ask still falls back,
// so the client is warned.
func TestTheWireClientWarningFiresBesideAnAskerThatCannotAnswer(t *testing.T) {
	client := &fakeClient{connected: true}
	askers := []perm.Asker{perm.NewWireAsker(client, 0), &deadAsker{}}
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionAsk, Askers: askers})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	if warning := perm.WireClientWarning(policy, policy.Askers(), client); !strings.Contains(warning, "permission_answers") {
		t.Errorf("the session warned %q, want the capability named: the other asker never answers", warning)
	}
}

// TestTheConstructionWarningJoinsTheChainWarning covers the join: a
// policy that can ask, with no asker that can answer, and a prefix
// rule the grammar refuses. The warning names both.
func TestTheConstructionWarningJoinsTheChainWarning(t *testing.T) {
	askers := []perm.Asker{perm.NewWireAsker(nil, 0)}
	policy, err := perm.New(perm.Options{
		Rules:       []perm.Rule{{Tool: "fetch", CommandPrefix: "attacker@internal.example.com", Action: perm.ActionDeny}},
		PrimaryArgs: map[string]string{"fetch": "url"},
		Unmatched:   perm.ActionAsk,
		Askers:      askers,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	warning := perm.Warning(policy, policy.Askers())

	if !strings.Contains(warning, "fetch") || !strings.Contains(warning, "falls back") {
		t.Errorf("the warning is %q, want both the refused rule and the fallback named", warning)
	}
}

// TestARefusedPrefixWarnsAtConstruction covers the rule: a prefix
// rule whose prefix the URL grammar refuses is warned about at load.
// The refused spellings are userinfo in the authority and a scheme
// outside http and https. The warning matters because the rule matches
// nothing against the URLs the operator meant.
func TestARefusedPrefixWarnsAtConstruction(t *testing.T) {
	for _, prefix := range []string{
		"attacker@internal.example.com",
		"htps://internal.example.com",
		"ftp://example.com",
		"https://attacker@internal.example.com",
	} {
		policy, err := perm.New(perm.Options{
			Rules:       []perm.Rule{{Tool: "fetch", CommandPrefix: prefix, Action: perm.ActionDeny}},
			PrimaryArgs: map[string]string{"fetch": "url"},
			Unmatched:   perm.ActionAllow,
		})
		if err != nil {
			t.Fatalf("building the policy with the prefix %q: %v", prefix, err)
		}

		warning := perm.Warning(policy, policy.Askers())

		if warning == "" {
			t.Errorf("the prefix %q printed no warning, want one: the grammar refuses it", prefix)
		}
		if !strings.Contains(warning, "fetch") {
			t.Errorf("the warning is %q, want it to name the tool", warning)
		}
	}
}

// TestAPlainPrefixDoesNotWarn covers the rule's edge. A prefix the
// grammar accepts is not warned about, and neither is one that spells
// no URL at all. A command, a path, and a text that is no absolute URL
// all stay silent.
func TestAPlainPrefixDoesNotWarn(t *testing.T) {
	for _, prefix := range []string{
		"https://internal.example.com",
		"internal.example.com",
		"internal.example.com/secret",
		"git status",
		"run tests!",
		"scp file user@remote:/path",
	} {
		policy, err := perm.New(perm.Options{
			Rules:       []perm.Rule{{Tool: "fetch", CommandPrefix: prefix, Action: perm.ActionDeny}},
			PrimaryArgs: map[string]string{"fetch": "url"},
			Unmatched:   perm.ActionAllow,
		})
		if err != nil {
			t.Fatalf("building the policy with the prefix %q: %v", prefix, err)
		}

		if warning := perm.Warning(policy, policy.Askers()); warning != "" {
			t.Errorf("the prefix %q warned %q, want silence: the grammar accepts it or it spells no URL", prefix, warning)
		}
	}
}
