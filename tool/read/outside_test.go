package read_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/read"
)

// TestAPathOutsideTheWorkingDirectoryIsRefused covers the boundary the
// file tools share: a relative escape, an absolute path and a symlinked
// parent all resolve outside the working directory and are refused.
func TestAPathOutsideTheWorkingDirectoryIsRefused(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("the secret\n"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	for _, path := range []string{
		"..",
		filepath.Join("..", filepath.Base(target)),
		target,
		filepath.Join("link", "secret.txt"),
	} {
		raw, err := json.Marshal(map[string]any{"path": path})
		if err != nil {
			t.Fatalf("marshalling: %v", err)
		}
		_, err = read.New().Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
		if err == nil {
			t.Errorf("the path %q was accepted", path)
			continue
		}
		if !strings.Contains(err.Error(), "outside the working directory") {
			t.Errorf("the error for %q is %q, want it to name the boundary", path, err)
		}
	}
}

// TestAPathInAReadRootIsAccepted covers the root a session offers: a
// skill lives outside the tree the session works in, and the prompt
// tells the model to read it.
func TestAPathInAReadRootIsAccepted(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	skillPath := filepath.Join(root, "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("making the skill directory: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("Read the diff first.\n"), 0o644); err != nil {
		t.Fatalf("writing the skill: %v", err)
	}
	raw, err := json.Marshal(map[string]any{"path": skillPath})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if _, err := read.New().Prepare(raw, poo.PrepareEnv{WorkingDir: dir, ReadRoots: []string{root}}); err != nil {
		t.Fatalf("the skill path was refused: %v", err)
	}
	// The root is the directory it names, not everything beside it.
	sibling := filepath.Join(filepath.Dir(root), "elsewhere.txt")
	if err := os.WriteFile(sibling, []byte("not a skill\n"), 0o644); err != nil {
		t.Fatalf("writing the sibling: %v", err)
	}
	raw, err = json.Marshal(map[string]any{"path": sibling})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if _, err := read.New().Prepare(raw, poo.PrepareEnv{WorkingDir: dir, ReadRoots: []string{root}}); err == nil {
		t.Errorf("the path %q beside the root was accepted", sibling)
	}
}
