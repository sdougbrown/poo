package grep_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheGitFileSelectionRunsNoRepositoryCode covers the host-tool
// boundary: the grep tool runs git in poo's own process, on a tree the
// model can write, so the tree's own configuration must not get a
// command run.
func TestTheGitFileSelectionRunsNoRepositoryCode(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a needle here\n"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	// The working directory is the model's: this is what a model can
	// write into it from inside the sandbox.
	marker := filepath.Join(t.TempDir(), "ran-on-the-host")
	script := filepath.Join(t.TempDir(), "hook.sh")
	// poo's own environment holds the endpoint's key: the provider read
	// it from there.
	t.Setenv("POO_REVIEW_API_KEY", "sk-the-endpoint-key")
	hook := "#!/bin/sh\nprintf '%s' \"$POO_REVIEW_API_KEY\" > " + marker + "\necho\nexit 0\n"
	if err := os.WriteFile(script, []byte(hook), 0o755); err != nil {
		t.Fatalf("writing the hook: %v", err)
	}
	config := filepath.Join(dir, ".git", "config")
	existing, err := os.ReadFile(config)
	if err != nil {
		t.Fatalf("reading the config: %v", err)
	}
	poisoned := string(existing) + "[core]\n\tfsmonitor = " + script + "\n"
	if err := os.WriteFile(config, []byte(poisoned), 0o644); err != nil {
		t.Fatalf("writing the config: %v", err)
	}

	res := reviewGrep(t, map[string]any{"pattern": "needle"}, dir)
	if !strings.Contains(res, "a.txt") {
		t.Fatalf("the search found nothing: %q", res)
	}
	got, err := os.ReadFile(marker)
	if err == nil {
		t.Errorf("the search ran the repository's own command on the host, and it read %q from poo's environment", got)
	}
}

// TestTheGitInvocationIsTheOnlyOne keeps the helper above honest: the
// repository really is a work tree, so the git path is the one taken.
func TestTheGitInvocationIsTheOnlyOne(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git: %v", err)
	}
	if strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("the fixture is not a work tree: %q", out)
	}
}
