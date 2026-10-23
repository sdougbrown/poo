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
