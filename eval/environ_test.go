package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points POO_SETTINGS at the smallest settings file that
// parses, so a run the tests execute cannot discover the operator's
// own settings — whose models and askers a test cannot serve.
//
// The variable is set, not unset, because an empty value is no value to
// the discovery: FindSettings reads an empty POO_SETTINGS as absent and
// walks on to the user's config directory, which is the leak this
// closes. The exec'd binary inherits the environment, so this covers
// the runs the runner starts.
func TestMain(m *testing.M) {
	path := filepath.Join(os.TempDir(), "poo-test-settings.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o644); err != nil {
		println("poo tests: writing the test settings:", err.Error())
		os.Exit(1)
	}
	os.Setenv("POO_SETTINGS", path)
	os.Exit(m.Run())
}

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
