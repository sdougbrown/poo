package edit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/protect"
)

// reserveEdit marks a target as busy, the way a concurrent edit call
// holds it from its LoadOrStore to its Delete. This stands in for the
// race the guard exists for: a second call for the same file while the
// first is still inside Call.
func reserveEdit(t *testing.T, dir, path string) {
	t.Helper()
	key := protect.Resolve(absolutePath(path, dir))
	activeEdits.Store(key, struct{}{})
	t.Cleanup(func() { activeEdits.Delete(key) })
}

// releaseEdit frees a target reserved by reserveEdit, the way the other
// edit finishing would.
func releaseEdit(t *testing.T, dir, path string) {
	t.Helper()
	activeEdits.Delete(protect.Resolve(absolutePath(path, dir)))
}

// runCall prepares and runs an edit through the tool.
func runCall(t *testing.T, tool *Tool, args map[string]any, dir string) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	env := poo.PrepareEnv{WorkingDir: dir}
	prepared, err := tool.Prepare(raw, env)
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{PrepareEnv: env})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	return res
}

// TestAnEditUnderwayConflictsAndRetries covers the guard end to end:
// while another edit call holds a file, a second call for it — through
// another tool instance and a different spelling of the path — comes
// back as a recoverable conflict and leaves the file alone; an edit of
// a different file still goes through; and once the file is free, the
// same call retries and lands.
func TestAnEditUnderwayConflictsAndRetries(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	for path, content := range map[string]string{a: "one\n", b: "two\n"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	reserveEdit(t, dir, "a.txt")

	// A different instance and a different spelling of the held target.
	res := runCall(t, New(), map[string]any{"path": a, "old": "one", "new": "ONE"}, dir)
	if !res.IsError {
		t.Fatalf("the edit succeeded while another edit held the file: %s", res.Text)
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
	if !strings.Contains(res.Text, "another edit is in progress") {
		t.Errorf("the conflict says %q, want it to name the busy file", res.Text)
	}
	if got, err := os.ReadFile(a); err != nil || string(got) != "one\n" {
		t.Errorf("a.txt holds %q (%v), want it unchanged by the conflict", got, err)
	}

	// A different target is not blocked by the held one.
	res = runCall(t, New(), map[string]any{"path": "b.txt", "old": "two", "new": "TWO"}, dir)
	if res.IsError {
		t.Fatalf("the edit of b.txt failed while only a.txt was busy: %s", res.Text)
	}
	if got, err := os.ReadFile(b); err != nil || string(got) != "TWO\n" {
		t.Errorf("b.txt holds %q (%v), want the edit applied", got, err)
	}

	// The other edit finishes and releases the file; the same call
	// retries and lands.
	releaseEdit(t, dir, "a.txt")
	res = runCall(t, New(), map[string]any{"path": "a.txt", "old": "one", "new": "ONE"}, dir)
	if res.IsError {
		t.Fatalf("the retry failed after the other edit released the file: %s", res.Text)
	}
	if got, err := os.ReadFile(a); err != nil || string(got) != "ONE\n" {
		t.Errorf("a.txt holds %q (%v), want the retried edit applied", got, err)
	}
}
