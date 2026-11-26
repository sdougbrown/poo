package write

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/internal/fileio"
	"github.com/sdougbrown/poo/protect"
)

// reserve holds a path the way a concurrent call does, from its Reserve
// to its Release. The key goes through the same resolution the tool's
// own Prepare applies, so the test cannot drift from the real key.
func reserve(t *testing.T, dir, path string) {
	t.Helper()
	if !fileio.Reserve(protect.Resolve(fileio.Resolve(path, dir))) {
		t.Fatalf("reserving %s: the path is already held", path)
	}
}

// runCall prepares and runs a write through the tool.
func runCall(t *testing.T, args map[string]any, dir string) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	tool := New()
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	return res
}

// TestAWriteWhileAnotherCallHoldsThePathConflicts covers the shared
// reservation: while another call holds a path, a write for it comes
// back as a recoverable conflict and touches nothing; once the path is
// released, the same write lands.
func TestAWriteWhileAnotherCallHoldsThePathConflicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	reserve(t, dir, path)

	res := runCall(t, map[string]any{"path": path, "content": "two\n"}, dir)
	if !res.IsError {
		t.Fatal("the write succeeded while the path was held, want a conflict")
	}
	if !strings.Contains(res.Text, "another write is in progress") {
		t.Errorf("the conflict says %q, want it to name the busy path", res.Text)
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(res.Structured, &payload); err != nil {
		t.Fatalf("reading the structured payload %s: %v", res.Structured, err)
	}
	if payload.Code != "edit_conflict" {
		t.Errorf("the conflict reports code %q, want edit_conflict", payload.Code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the write created the file while the path was held")
	}

	fileio.Release(protect.Resolve(fileio.Resolve(path, dir)))
	res = runCall(t, map[string]any{"path": path, "content": "two\n"}, dir)
	if res.IsError {
		t.Fatalf("the retried write failed after the path was released: %s", res.Text)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(content) != "two\n" {
		t.Errorf("the file holds %q, want the written content", content)
	}
}
