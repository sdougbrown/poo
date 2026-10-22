package grep_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/grep"
)

// TestASearchRootOutsideTheWorkingDirectoryIsRefused covers the boundary
// the file tools share: a relative escape, an absolute path and a
// symlinked parent all resolve outside the working directory and are
// refused before the search runs.
func TestASearchRootOutsideTheWorkingDirectoryIsRefused(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "a.txt"), []byte("the needle\n"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	for _, path := range []string{
		"..",
		filepath.Join("..", filepath.Base(outside)),
		outside,
		link,
	} {
		raw, err := json.Marshal(map[string]any{"pattern": "needle", "path": path})
		if err != nil {
			t.Fatalf("marshalling: %v", err)
		}
		_, err = grep.New().Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
		if err == nil {
			t.Errorf("the root %q was accepted", path)
			continue
		}
		if !strings.Contains(err.Error(), "outside the working directory") {
			t.Errorf("the error for %q is %q, want it to name the boundary", path, err)
		}
	}
}
