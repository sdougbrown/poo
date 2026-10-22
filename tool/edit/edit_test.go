package edit_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/edit"
)

// call runs an edit through the tool and returns the result.
func call(t *testing.T, tool *edit.Tool, args map[string]any, dir string) poo.ToolResult {
	t.Helper()
	return callWith(t, tool, args, dir, nil)
}

// callWith runs an edit with protected paths and returns the result.
func callWith(t *testing.T, tool *edit.Tool, args map[string]any, dir string, protected []string) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	env := poo.PrepareEnv{WorkingDir: dir, ProtectedPaths: protected}
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

// writeFile writes a file and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// readFile reads a file.
func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

// TestTheOneMatchIsReplaced covers the ordinary edit.
func TestTheOneMatchIsReplaced(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "one\ntwo\nthree\n")

	res := call(t, edit.New(), map[string]any{
		"path": "a.txt",
		"old":  "two",
		"new":  "TWO",
	}, dir)
	if res.IsError {
		t.Fatalf("the edit failed: %s", res.Text)
	}
	if got := readFile(t, path); got != "one\nTWO\nthree\n" {
		t.Errorf("the file holds %q, want the replaced text", got)
	}
}

// TestAMissingOldTextFailsLoudly covers the edit that changes nothing.
func TestAMissingOldTextFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "one\ntwo\n")

	res := call(t, edit.New(), map[string]any{
		"path": "a.txt",
		"old":  "three",
		"new":  "THREE",
	}, dir)
	if !res.IsError {
		t.Fatalf("the edit succeeded: %s", res.Text)
	}
	if !strings.Contains(res.Text, "not found") {
		t.Errorf("the error is %q, want it to say the text was not found", res.Text)
	}
	if got := readFile(t, path); got != "one\ntwo\n" {
		t.Errorf("the file holds %q, want it unchanged", got)
	}
}

// TestManyMatchesFailLoudly covers the edit that would change more than
// the model meant.
func TestManyMatchesFailLoudly(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "x\nx\nx\n")

	res := call(t, edit.New(), map[string]any{
		"path": "a.txt",
		"old":  "x",
		"new":  "y",
	}, dir)
	if !res.IsError {
		t.Fatalf("the edit succeeded: %s", res.Text)
	}
	if !strings.Contains(res.Text, "3 times") {
		t.Errorf("the error is %q, want it to name the number of matches", res.Text)
	}
	if got := readFile(t, path); got != "x\nx\nx\n" {
		t.Errorf("the file holds %q, want it unchanged", got)
	}
}

// TestReplaceAllReplacesEveryMatch covers the flag that says so.
func TestReplaceAllReplacesEveryMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "x\nx\nx\n")

	res := call(t, edit.New(), map[string]any{
		"path":        "a.txt",
		"old":         "x",
		"new":         "y",
		"replace_all": true,
	}, dir)
	if res.IsError {
		t.Fatalf("the edit failed: %s", res.Text)
	}
	if got := readFile(t, path); got != "y\ny\ny\n" {
		t.Errorf("the file holds %q, want every match replaced", got)
	}
}

// TestAMissingFileIsCreated covers the edit with an empty old text.
func TestAMissingFileIsCreated(t *testing.T) {
	dir := t.TempDir()

	res := call(t, edit.New(), map[string]any{
		"path": "new.txt",
		"old":  "",
		"new":  "hello\n",
	}, dir)
	if res.IsError {
		t.Fatalf("the edit failed: %s", res.Text)
	}
	path := filepath.Join(dir, "new.txt")
	if got := readFile(t, path); got != "hello\n" {
		t.Errorf("the file holds %q, want the new text", got)
	}
}

// TestAMissingFileWithAnOldTextFails covers the edit of a file that is
// not there.
func TestAMissingFileWithAnOldTextFails(t *testing.T) {
	dir := t.TempDir()

	res := call(t, edit.New(), map[string]any{
		"path": "missing.txt",
		"old":  "x",
		"new":  "y",
	}, dir)
	if !res.IsError {
		t.Fatalf("the edit succeeded: %s", res.Text)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.txt")); !os.IsNotExist(err) {
		t.Errorf("the file was created: %v", err)
	}
}

// TestAnExistingFileWithAnEmptyOldTextFails covers the edit that would
// have no old text to replace.
func TestAnExistingFileWithAnEmptyOldTextFails(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "one\n")

	res := call(t, edit.New(), map[string]any{
		"path": "a.txt",
		"old":  "",
		"new":  "two\n",
	}, dir)
	if !res.IsError {
		t.Fatalf("the edit succeeded: %s", res.Text)
	}
	if got := readFile(t, path); got != "one\n" {
		t.Errorf("the file holds %q, want it unchanged", got)
	}
}

// TestTheProtectedPathIsRefused covers the protected paths: the call
// fails before the file is touched.
func TestTheProtectedPathIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "secret.txt", "one\n")

	raw, err := json.Marshal(map[string]any{"path": "secret.txt", "old": "one", "new": "two"})
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	_, err = edit.New().Prepare(raw, poo.PrepareEnv{
		WorkingDir:     dir,
		ProtectedPaths: []string{path},
	})
	if err == nil {
		t.Fatal("the protected path was accepted")
	}
	if !strings.Contains(err.Error(), "protected by configuration") {
		t.Errorf("the error is %q, want it to name the protection", err)
	}
	if got := readFile(t, path); got != "one\n" {
		t.Errorf("the file holds %q, want it unchanged", got)
	}
}

// TestASymlinkToAProtectedPathIsRefused covers the protected path that
// is reached through a symlink.
func TestASymlinkToAProtectedPathIsRefused(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "secret.txt", "one\n")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	raw, err := json.Marshal(map[string]any{"path": "link.txt", "old": "one", "new": "two"})
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	_, err = edit.New().Prepare(raw, poo.PrepareEnv{
		WorkingDir:     dir,
		ProtectedPaths: []string{target},
	})
	if err == nil {
		t.Fatal("the protected path was accepted")
	}
	if got := readFile(t, target); got != "one\n" {
		t.Errorf("the file holds %q, want it unchanged", got)
	}
}

// TestTheModeOfTheFileIsKept covers the change of a file that has a
// mode of its own.
func TestTheModeOfTheFileIsKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.sh")
	if err := os.WriteFile(path, []byte("one\n"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	res := call(t, edit.New(), map[string]any{"path": "a.sh", "old": "one", "new": "two"}, dir)
	if res.IsError {
		t.Fatalf("the edit failed: %s", res.Text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("the mode is %v, want 0755", info.Mode().Perm())
	}
}

// TestAnUnknownArgumentIsRefused covers the arguments that are not the
// tool's.
func TestAnUnknownArgumentIsRefused(t *testing.T) {
	if _, err := edit.New().Prepare(json.RawMessage(`{"path":"a.txt","old":"x","new":"y","extra":1}`), poo.PrepareEnv{}); err == nil {
		t.Error("the unknown argument was accepted")
	}
}
