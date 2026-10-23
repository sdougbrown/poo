package bwrap_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/protect"
	"github.com/sdougbrown/poo/tool/bash"
)

// reviewMasker expands to a fixed list of paths.
type reviewMasker struct{ paths []string }

func (m reviewMasker) Expand(string) ([]string, error) { return m.paths, nil }

// reviewCall runs a command through the tool and returns the whole
// observation.
func reviewCall(t *testing.T, exec *bwrap.Executor, command, dir string) map[string]any {
	t.Helper()
	tool := bash.New(bash.WithExecutor(exec))
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	var obs map[string]any
	if err := json.Unmarshal(res.Structured, &obs); err != nil {
		t.Fatalf("decoding %q: %v", res.Structured, err)
	}
	return obs
}

// TestAProtectedPathInsideAReadOnlyBindIsMasked covers the plan's rule
// that a protected path inside a bound directory is masked. /etc is one
// of the default read-only binds, and the plan's own example config
// protects /etc/**.
func TestAProtectedPathInsideAReadOnlyBindIsMasked(t *testing.T) {
	exec, err := bwrap.New(bwrap.Options{Masker: reviewMasker{paths: []string{"/etc/hostname"}}})
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	workdir := t.TempDir()
	obs := reviewCall(t, exec, "cat /etc/hostname; echo rc=$?", workdir)
	got, _ := obs["output"].(string)
	if strings.TrimSpace(got) != "rc=0" {
		t.Errorf("the protected path inside the read-only bind is readable: %q", got)
	}
}

// TestAProtectedPathInsideAReadWriteBindIsMasked is the control: a path
// inside a read-write bind outside the working directory is masked.
func TestAProtectedPathInsideAReadWriteBindIsMasked(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("the protected text"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	exec, err := bwrap.New(bwrap.Options{
		RWBinds: []string{outside},
		Masker:  reviewMasker{paths: []string{secret}},
	})
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	obs := reviewCall(t, exec, "cat "+secret, t.TempDir())
	got, _ := obs["output"].(string)
	if strings.Contains(got, "the protected text") {
		t.Errorf("a path inside a read-write bind is not masked: %q", got)
	}
}

// newReviewExecutor returns a bwrap executor.
func newReviewExecutor(t *testing.T) (*bwrap.Executor, error) {
	t.Helper()
	return bwrap.New(bwrap.Options{})
}

// reviewReadFile returns a file's contents.
func reviewReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return strings.TrimSpace(string(raw))
}

// TestAProtectedPathInsideAReadWriteBindIsMaskedWithTheRealMasker is
// the same control with the session's own matcher: the masker a session
// hands the executor only ever expands the paths under the root it is
// given (the working directory), so a protected path inside a read-write
// bind outside the working directory is never named.
func TestAProtectedPathInsideAReadWriteBindIsMaskedWithTheRealMasker(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("the protected text"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	set, err := protect.Compile([]string{filepath.Join(outside, "**")})
	if err != nil {
		t.Fatalf("compiling the globs: %v", err)
	}
	exec, err := bwrap.New(bwrap.Options{RWBinds: []string{outside}, Masker: set})
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	obs := reviewCall(t, exec, "cat "+secret, t.TempDir())
	got, _ := obs["output"].(string)
	if strings.Contains(got, "the protected text") {
		t.Errorf("a protected path inside a read-write bind is not masked: %q", got)
	}
}

// TestALiteralProtectedFileIsMaskedWithTheRealMasker covers the matcher
// expansion a literal protected file needs: the file sits in the
// sandbox's working directory, and its content is not readable there.
func TestALiteralProtectedFileIsMaskedWithTheRealMasker(t *testing.T) {
	workdir := t.TempDir()
	private := filepath.Join(workdir, "private.txt")
	if err := os.WriteFile(private, []byte("fixture-protected-content"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	set, err := protect.Compile([]string{private})
	if err != nil {
		t.Fatalf("compiling the globs: %v", err)
	}
	exec, err := bwrap.New(bwrap.Options{Masker: set})
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	obs := reviewCall(t, exec, "cat private.txt", workdir)
	got, _ := obs["output"].(string)
	if strings.Contains(got, "fixture-protected-content") {
		t.Errorf("a literal protected file is readable in the sandbox: %q", got)
	}
}
