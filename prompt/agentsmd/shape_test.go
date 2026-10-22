package agentsmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/prompt/agentsmd"
)

// TestASymlinkedFileIsRead covers the file shapes: a symlink to a
// regular file is a regular file, and its text is the instructions.
func TestASymlinkedFileIsRead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-instructions.md")
	if err := os.WriteFile(target, []byte("the instructions"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	text, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if !strings.Contains(text, "the instructions") {
		t.Errorf("the text is %q, want the instructions", text)
	}
}

// TestAHugeFileIsNotHeldWhole covers the cap: the loader keeps at most
// MaxBytes, and it does not read a runaway file into memory to find
// that out.
func TestAHugeFileIsNotHeldWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	// 64 MiB, far over the cap.
	chunk := []byte(strings.Repeat("a", 1<<20))
	for range 64 {
		if _, err := f.Write(chunk); err != nil {
			t.Fatalf("writing: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	var before, after os.FileInfo
	before, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	text, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	after, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if before.Size() != after.Size() {
		t.Fatalf("the file changed size")
	}
	if len(text) > agentsmd.MaxBytes+200 {
		t.Errorf("the text is %d bytes, want it within the cap plus the notice", len(text))
	}
}
