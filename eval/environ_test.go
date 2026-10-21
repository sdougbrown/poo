package eval

import (
	"strings"
	"testing"
)

// TestTheChildEnvironmentHoldsOnlyTheRunningModelsKey covers the
// scrub: every key variable is removed except the running model's own,
// whose value reaches the subprocess by name.
func TestTheChildEnvironmentHoldsOnlyTheRunningModelsKey(t *testing.T) {
	base := []string{
		"HOME=/home/tester",
		"POO_API_KEY_A=secret-a",
		"POO_API_KEY_B=secret-b",
	}

	got := strings.Join(scrubbedEnv(base, []string{"POO_API_KEY_A", "POO_API_KEY_B"}, "POO_API_KEY_B"), "\n")

	if strings.Contains(got, "secret-a") {
		t.Error("the idle model's key value is still in the child's environment")
	}
	if strings.Contains(got, "POO_API_KEY_A=") {
		t.Error("the idle model's key name is still in the child's environment")
	}
	if !strings.Contains(got, "POO_API_KEY_B=secret-b") {
		t.Error("the running model's key did not reach the child's environment")
	}
	if !strings.Contains(got, "HOME=/home/tester") {
		t.Error("the rest of the environment was dropped")
	}
}
