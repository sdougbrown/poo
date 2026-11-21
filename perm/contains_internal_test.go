package perm

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The contains matcher's own tests: the matching, the any-one-substring
// rule, and the rules a malformed contains rule breaks.

// TestAContainsSubstringMatchesAnywhereInTheCommand covers the matcher:
// the substring matches wherever it sits in the command, and the command's
// leading whitespace does not hide it.
func TestAContainsSubstringMatchesAnywhereInTheCommand(t *testing.T) {
	rule := Rule{Tool: "bash", CommandContains: []string{"rm -rf"}, Action: ActionDeny}
	spec := poo.ToolSpec{Name: "bash", PrimaryArg: "command"}
	matching := []string{
		"rm -rf build",
		"cd /work/x && rm -rf build",
		"git status; rm -rf /",
		"  cd /work/x && rm -rf build",
	}
	for _, command := range matching {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"command": "` + command + `"}`)}
		if !rule.matchesContains(info) {
			t.Errorf("the substring does not match %q, want the command to carry it", command)
		}
	}
	for _, command := range []string{
		"ls -la",
		"cd /work/x && go test ./...",
		"rm build",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"command": "` + command + `"}`)}
		if rule.matchesContains(info) {
			t.Errorf("the substring matches %q, want no substring in the command", command)
		}
	}
}

// TestAContainsRuleMatchesAnyOneOfItsSubstrings covers the list: one
// substring matching is enough, whatever the others do.
func TestAContainsRuleMatchesAnyOneOfItsSubstrings(t *testing.T) {
	rule := Rule{Tool: "bash", CommandContains: []string{"rm -rf", "git push --force"}, Action: ActionAsk}
	spec := poo.ToolSpec{Name: "bash", PrimaryArg: "command"}
	for _, command := range []string{
		"cd /work/x && rm -rf build",
		"git push --force origin main",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"command": "` + command + `"}`)}
		if !rule.matchesContains(info) {
			t.Errorf("no substring matches %q, want one of them to", command)
		}
	}
}

// TestAMalformedContainsRuleIsAConstructionError covers the rules a
// contains rule has to hold: it names a tool, it carries no prefix
// beside it, and its substrings are not empty.
func TestAMalformedContainsRuleIsAConstructionError(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
	}{
		{
			name: "a contains substring on a class rule",
			rule: Rule{Class: poo.ClassSideEffecting, CommandContains: []string{"rm -rf"}, Action: ActionAsk},
		},
		{
			name: "a contains rule beside a prefix rule",
			rule: Rule{Tool: "bash", CommandPrefix: "git", CommandContains: []string{"rm -rf"}, Action: ActionAsk},
		},
		{
			name: "an empty substring",
			rule: Rule{Tool: "bash", CommandContains: []string{""}, Action: ActionAsk},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := New(Options{Rules: []Rule{tt.rule}})
			if err == nil {
				t.Fatalf("the policy was built, want a construction error")
			}
			if policy != nil {
				t.Errorf("the policy is %v, want nil", policy)
			}
			if !strings.Contains(err.Error(), "perm:") {
				t.Errorf("the error is %q, want it to name the package", err)
			}
		})
	}
}
