package grep_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/grep"
)

// reviewGrep runs a grep call and returns the result text.
func reviewGrep(t *testing.T, rawArgs map[string]any, dir string) string {
	t.Helper()
	raw, err := json.Marshal(rawArgs)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	tool := grep.New()
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	return res.Text
}

// TestASymlinkedSearchRootFindsTheFiles covers the search root: a root
// that is a symlink to a directory is the directory, and the search
// covers it.
func TestASymlinkedSearchRootFindsTheFiles(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("making the directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("the needle\n"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	// The real directory is the control.
	if got := reviewGrep(t, map[string]any{"pattern": "needle", "path": "real"}, dir); !strings.Contains(got, "a.txt") {
		t.Fatalf("the control search found nothing: %q", got)
	}
	got := reviewGrep(t, map[string]any{"pattern": "needle", "path": "link"}, dir)
	if !strings.Contains(got, "a.txt") {
		t.Errorf("the search of a symlinked root found nothing: %q", got)
	}
}
