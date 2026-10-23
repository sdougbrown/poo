package read_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/read"
)

// TestTheProtectedPathIsRefused covers the protected paths: the call
// fails before the file is opened.
func TestTheProtectedPathIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "secret.txt", "the secret\n")

	raw, err := json.Marshal(map[string]any{"path": "secret.txt"})
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	_, err = read.New().Prepare(raw, poo.PrepareEnv{
		WorkingDir:     dir,
		ProtectedPaths: []string{filepath.Join(dir, "secret.txt")},
	})
	if err == nil {
		t.Fatal("the protected path was accepted")
	}
	if !strings.Contains(err.Error(), "protected by configuration") {
		t.Errorf("the error is %q, want it to name the protection", err)
	}
	if _, ok := err.(*poo.ErrInvalidArgs); !ok {
		t.Errorf("the error is %T, want an ErrInvalidArgs", err)
	}
}

// TestASymlinkToAProtectedPathIsRefused covers the protected path that
// is reached through a symlink.
func TestASymlinkToAProtectedPathIsRefused(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "secret.txt", "the secret\n")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	raw, err := json.Marshal(map[string]any{"path": "link.txt"})
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	_, err = read.New().Prepare(raw, poo.PrepareEnv{
		WorkingDir:     dir,
		ProtectedPaths: []string{target},
	})
	if err == nil {
		t.Fatal("the protected path was accepted")
	}
	if !strings.Contains(err.Error(), "protected by configuration") {
		t.Errorf("the error is %q, want it to name the protection", err)
	}
}

// TestAnEmptyProtectedPathListHasNoEffect covers the default: a run
// with no protected paths configured reads what it asks for.
func TestAnEmptyProtectedPathListHasNoEffect(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\n")

	res := call(t, read.New(), map[string]any{"path": "a.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "one") {
		t.Errorf("the output is %q, want the file", res.Text)
	}
}

// TestThePromptSectionIsThere covers the guidance the tool brings.
func TestThePromptSectionIsThere(t *testing.T) {
	section := read.New().PromptSection(prompt.Env{})
	if !strings.Contains(section, "Read:") {
		t.Errorf("the section is %q, want it to name the tool", section)
	}
	if !strings.Contains(section, "offset") {
		t.Errorf("the section is %q, want it to talk about the ranges", section)
	}
}
