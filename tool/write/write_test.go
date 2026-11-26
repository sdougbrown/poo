package write_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/internal/fileio"
	writetool "github.com/sdougbrown/poo/tool/write"
)

// call runs a write through the tool and returns the result.
func call(t *testing.T, args map[string]any, dir string, protected []string) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	tool := writetool.New()
	env := poo.PrepareEnv{WorkingDir: dir, ProtectedPaths: protected}
	prepared, err := tool.Prepare(raw, env)
	if err != nil {
		return poo.ToolResult{Text: err.Error(), IsError: true}
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{PrepareEnv: env})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	return res
}

// mustRead reads a file the test expects to be there.
func mustRead(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

// TestAWriteCreatesTheFileAndItsDirectories covers the ordinary call:
// the file holds the content, and missing parents arrive with it.
func TestAWriteCreatesTheFileAndItsDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "deep", "file.txt")
	res := call(t, map[string]any{"path": path, "content": "hello\n"}, dir, nil)
	if res.IsError {
		t.Fatalf("the write failed: %s", res.Text)
	}
	if got := mustRead(t, path); got != "hello\n" {
		t.Errorf("the file holds %q, want the written content", got)
	}
}

// TestAWriteOverAnExistingFileIsRefused covers the tool's boundary: an
// existing file comes back as a refusal that names Edit, and the file is
// untouched.
func TestAWriteOverAnExistingFileIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "there.txt")
	if err := os.WriteFile(path, []byte("kept\n"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	res := call(t, map[string]any{"path": path, "content": "gone\n"}, dir, nil)
	if !res.IsError {
		t.Fatal("the write over an existing file succeeded, want a refusal")
	}
	if !strings.Contains(res.Text, "already exists") || !strings.Contains(res.Text, "edit") {
		t.Errorf("the refusal says %q, want it to name the existing file and Edit", res.Text)
	}
	if got := mustRead(t, path); got != "kept\n" {
		t.Errorf("the file holds %q, want it untouched by the refusal", got)
	}
}

// TestAWriteOfAProtectedPathIsRefused covers the configuration boundary:
// a path under a protected directory is refused before anything is
// touched.
func TestAWriteOfAProtectedPathIsRefused(t *testing.T) {
	dir := t.TempDir()
	res := call(t, map[string]any{"path": filepath.Join(dir, "secrets", "x.txt"), "content": "v"}, dir, []string{filepath.Join(dir, "secrets")})
	if !res.IsError {
		t.Fatal("the write to a protected path succeeded, want a refusal")
	}
	if !strings.Contains(res.Text, "protected") {
		t.Errorf("the refusal says %q, want it to name the protection", res.Text)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets", "x.txt")); !os.IsNotExist(err) {
		t.Error("the protected path was written anyway")
	}
}

// TestAWriteOutsideTheWorkingDirectoryIsRefused covers the confinement
// the settings can turn on: the path is refused before the file is
// touched.
func TestAWriteOutsideTheWorkingDirectoryIsRefused(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	raw, err := json.Marshal(map[string]any{"path": filepath.Join(outside, "f.txt"), "content": "x"})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	tool := writetool.New()
	_, err = tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir, RestrictWrite: true})
	if err == nil {
		t.Fatal("the write outside the working directory was prepared, want a refusal")
	}
	if !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("the refusal says %q, want it to name the boundary", err.Error())
	}
}

// TestAWriteWhileAnotherCallHoldsThePathConflicts covers the shared
// reservation: while another call holds a path, a write for it comes
// back as a recoverable conflict and touches nothing; once the path is
// released, the same write lands.
func TestAWriteWhileAnotherCallHoldsThePathConflicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	key := fileio.Resolve(path, dir)
	if !fileio.Reserve(key) {
		t.Fatal("reserving the path: it is already held")
	}

	res := call(t, map[string]any{"path": path, "content": "two\n"}, dir, nil)
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

	fileio.Release(key)
	res = call(t, map[string]any{"path": path, "content": "two\n"}, dir, nil)
	if res.IsError {
		t.Fatalf("the retried write failed after the path was released: %s", res.Text)
	}
	if got := mustRead(t, path); got != "two\n" {
		t.Errorf("the file holds %q, want the written content", got)
	}
}
