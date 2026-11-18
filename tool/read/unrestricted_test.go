package read_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

func TestFileToolsAllowOutsideWorkingDirectoryByDefault(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "file")
	for _, tc := range []struct {
		name string
		tool poo.Tool
		args map[string]any
	}{
		{"read", read.New(), map[string]any{"path": target}},
		{"grep", grep.New(), map[string]any{"path": outside, "pattern": "needle"}},
		{"edit", edit.New(), map[string]any{"path": target, "old": "a", "new": "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.args)
			if _, err := tc.tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir}); err != nil {
				t.Fatalf("outside path refused: %v", err)
			}
		})
	}
}
