package perm_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
)

func TestACallOutsideTheWorkingDirectoryIsAsked(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{
		AskOutside: map[string]string{"read": "path"},
		WorkingDir: dir,
		Askers:     []perm.Asker{asker},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	relative, _ := json.Marshal(map[string]string{"path": "../outside.txt"})
	absolute, _ := json.Marshal(map[string]string{"path": outside})
	inside, _ := json.Marshal(map[string]string{"path": "notes.md"})
	for _, tc := range []struct {
		name string
		args json.RawMessage
		ask  bool
	}{
		{"relative escape", relative, true},
		{"absolute escape", absolute, true},
		{"inside", inside, false},
		{"no path", json.RawMessage(`{}`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asker.mu.Lock()
			asker.asked = 0
			asker.mu.Unlock()
			decision, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
				Spec:      poo.ToolSpec{Name: "read", Class: poo.ClassReadOnly},
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("asking the policy: %v", err)
			}
			if decision.Denied {
				t.Fatalf("the call was denied: %s", decision.Detail)
			}
			asker.mu.Lock()
			asked := asker.asked
			asker.mu.Unlock()
			if tc.ask && asked == 0 {
				t.Fatal("the call outside the working directory was not asked")
			}
			if !tc.ask && asked > 0 {
				t.Fatal("the call inside the working directory was asked")
			}
		})
	}
}

func TestAPrepareFailedCallIsNotAskedAboutItsPath(t *testing.T) {
	dir := t.TempDir()
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{
		AskOutside: map[string]string{"read": "path"},
		WorkingDir: dir,
		Askers:     []perm.Asker{asker},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	args, _ := json.Marshal(map[string]string{"path": "../outside.txt"})
	if _, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec:          poo.ToolSpec{Name: "read", Class: poo.ClassReadOnly},
		Arguments:     args,
		PrepareFailed: true,
	}); err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if asker.asked != 0 {
		t.Fatalf("the unprepared call was asked %d time(s), want 0", asker.asked)
	}
}

func TestTheUnmatchedAllowDoesNotSwallowTheOutsideAsk(t *testing.T) {
	dir := t.TempDir()
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{
		Unmatched:  perm.ActionAllow,
		AskOutside: map[string]string{"read": "path"},
		WorkingDir: dir,
		Askers:     []perm.Asker{asker},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	args, _ := json.Marshal(map[string]string{"path": "../outside.txt"})
	if _, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec:      poo.ToolSpec{Name: "read", Class: poo.ClassReadOnly},
		Arguments: args,
	}); err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if asker.asked != 1 {
		t.Fatalf("the outside call was asked %d time(s), want 1", asker.asked)
	}
}

func TestTheCallOwnWorkingDirectoryDecidesTheBoundary(t *testing.T) {
	policyDir := t.TempDir()
	sessionDir := t.TempDir()
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{
		AskOutside: map[string]string{"read": "path"},
		WorkingDir: policyDir,
		Askers:     []perm.Asker{asker},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	// The file sits inside the policy's build directory but outside the
	// directory the session works in.
	insidePolicyDir := filepath.Join(policyDir, "sibling.txt")
	args, _ := json.Marshal(map[string]string{"path": insidePolicyDir})
	if _, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec:       poo.ToolSpec{Name: "read", Class: poo.ClassReadOnly},
		Arguments:  args,
		WorkingDir: sessionDir,
	}); err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if asker.asked != 1 {
		t.Fatalf("the call outside the session directory was asked %d time(s), want 1", asker.asked)
	}
	if asker.lastAsk.WorkingDir != sessionDir {
		t.Fatalf("the ask's working directory is %q, want the session's %q", asker.lastAsk.WorkingDir, sessionDir)
	}
}

func TestAPolicyWithoutAWorkingDirectoryDoesNotAsk(t *testing.T) {
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{
		AskOutside: map[string]string{"read": "path"},
		Askers:     []perm.Asker{asker},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	args, _ := json.Marshal(map[string]string{"path": "/etc/passwd"})
	if _, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec:      poo.ToolSpec{Name: "read", Class: poo.ClassReadOnly},
		Arguments: args,
	}); err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if asker.asked != 0 {
		t.Fatalf("the call was asked %d time(s), want 0: a policy with no directory to judge paths against lets the read-only auto-allow stand", asker.asked)
	}
}

func TestAnAskOutsidePolicyCountsAsAnAskSource(t *testing.T) {
	policy, err := perm.New(perm.Options{
		AskOutside: map[string]string{"read": "path"},
		WorkingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	warning := perm.Warning(policy, nil)
	if warning == "" {
		t.Fatal("an ask-outside policy with no answering asker drew no warning")
	}
	if !strings.Contains(warning, "falls back") {
		t.Fatalf("the warning %q does not name the fallback", warning)
	}
}

func TestARuleMatchesBeforeTheOutsideAsk(t *testing.T) {
	dir := t.TempDir()
	asker := &fakeAsker{decision: perm.DecisionDeny, available: true}
	policy, err := perm.New(perm.Options{
		Rules:      []perm.Rule{{Tool: "read", Action: perm.ActionDeny}},
		AskOutside: map[string]string{"read": "path"},
		WorkingDir: dir,
		Askers:     []perm.Asker{asker},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	args, _ := json.Marshal(map[string]string{"path": "../outside.txt"})
	decision, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec:      poo.ToolSpec{Name: "read", Class: poo.ClassReadOnly},
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if !decision.Denied {
		t.Fatal("the rule's denial did not apply")
	}
	if asker.asked != 0 {
		t.Fatalf("the matched rule asked the chain %d time(s), want 0", asker.asked)
	}
}
