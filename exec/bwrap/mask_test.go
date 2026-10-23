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

// writeMaskFixture writes a fixture file and returns its path.
func writeMaskFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// linkFixture writes a symlink in dir and returns its path.
func linkFixture(t *testing.T, dir, name, target string) string {
	t.Helper()
	link := filepath.Join(dir, name)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	return link
}

// maskedOutput runs a command in a sandbox with the masker's masks and
// returns its output.
//
// Every masking test needs the same three facts: the sandbox started, a
// public sibling is readable, and the protected text is gone. The first
// two are checked here, so a sandbox that fails at startup cannot pass for
// a mask.
func maskedOutput(t *testing.T, masker bwrap.Masker, workdir, command, public string) string {
	t.Helper()
	exec, err := bwrap.New(bwrap.Options{Masker: masker})
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	obs := reviewCall(t, exec, command, workdir)
	got, _ := obs["output"].(string)
	code, _ := obs["returncode"].(float64)
	if code != 0 {
		t.Errorf("the sandbox did not start: returncode %v, output %q", code, got)
	}
	if !strings.Contains(got, public) {
		t.Errorf("the public sibling is not readable: %q", got)
	}
	return got
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
	private := writeMaskFixture(t, workdir, "private.txt", "fixture-protected-content")
	public := writeMaskFixture(t, workdir, "keep.txt", "fixture-public-sibling")
	set, err := protect.Compile([]string{private})
	if err != nil {
		t.Fatalf("compiling the globs: %v", err)
	}
	got := maskedOutput(t, set, workdir, "cat private.txt; cat "+public, "fixture-public-sibling")
	if strings.Contains(got, "fixture-protected-content") {
		t.Errorf("a literal protected file is readable in the sandbox: %q", got)
	}
}

// TestASymlinkReachingAProtectedPathIsMasked covers the paths a glob
// reaches through a symlink: a symlinked working directory, a symlinked
// prefix, a protected file's own link, and a link between the two.
func TestASymlinkReachingAProtectedPathIsMasked(t *testing.T) {
	const (
		private = "fixture-protected-content"
		public  = "fixture-public-sibling"
	)
	// literalUnderLinkedRoot protects a literal file in a working
	// directory that is a symlink.
	literalUnderLinkedRoot := func(t *testing.T) (*protect.Set, string, string) {
		t.Helper()
		root := t.TempDir()
		real := filepath.Join(root, "real")
		writeMaskFixture(t, real, "private.txt", private)
		writeMaskFixture(t, real, "keep.txt", public)
		link := linkFixture(t, root, "linked-root", real)
		set, err := protect.Compile([]string{filepath.Join(link, "private.txt")})
		if err != nil {
			t.Fatalf("compiling the globs: %v", err)
		}
		return set, link, "cat private.txt; cat keep.txt"
	}
	// globUnderLinkedPrefix protects a wildcard whose fixed prefix is a
	// symlinked directory beside the working directory.
	globUnderLinkedPrefix := func(t *testing.T) (*protect.Set, string, string) {
		t.Helper()
		root := t.TempDir()
		real := filepath.Join(root, "real")
		secret := writeMaskFixture(t, real, "private.txt", private)
		keep := writeMaskFixture(t, real, "keep.txt", public)
		link := linkFixture(t, root, "linked-root", real)
		set, err := protect.Compile([]string{filepath.Join(link, "private.*")})
		if err != nil {
			t.Fatalf("compiling the globs: %v", err)
		}
		return set, root, "cat " + secret + "; cat " + keep
	}
	// fileLinkInsideBinds protects a link whose target the sandbox
	// mounts, so the link is masked at the target.
	fileLinkInsideBinds := func(t *testing.T) (*protect.Set, string, string) {
		t.Helper()
		root := t.TempDir()
		target := writeMaskFixture(t, root, "private.txt", private)
		writeMaskFixture(t, root, "keep.txt", public)
		link := linkFixture(t, root, "alias.txt", target)
		set, err := protect.Compile([]string{link})
		if err != nil {
			t.Fatalf("compiling the globs: %v", err)
		}
		return set, root, "cat alias.txt; cat keep.txt"
	}
	// fileLinkOutsideBinds protects a link whose target the sandbox does
	// not mount: there is nothing to mask, and nothing to fail on.
	fileLinkOutsideBinds := func(t *testing.T) (*protect.Set, string, string) {
		t.Helper()
		root := t.TempDir()
		secret := writeMaskFixture(t, t.TempDir(), "secret.txt", private)
		writeMaskFixture(t, root, "keep.txt", public)
		link := linkFixture(t, root, "alias.txt", secret)
		set, err := protect.Compile([]string{link})
		if err != nil {
			t.Fatalf("compiling the globs: %v", err)
		}
		return set, root, "cat alias.txt 2>/dev/null; cat keep.txt"
	}
	// wildcardMatchesFileLink protects a wildcard that matches a link:
	// the link is masked at the target it reaches.
	wildcardMatchesFileLink := func(t *testing.T) (*protect.Set, string, string) {
		t.Helper()
		root := t.TempDir()
		target := writeMaskFixture(t, root, "private.txt", private)
		writeMaskFixture(t, root, "keep.md", public)
		linkFixture(t, root, "alias.txt", target)
		set, err := protect.Compile([]string{filepath.Join(root, "*.txt")})
		if err != nil {
			t.Fatalf("compiling the globs: %v", err)
		}
		return set, root, "cat alias.txt; cat keep.md"
	}
	// intermediateDirLink protects a literal file whose prefix holds a
	// symlinked directory.
	intermediateDirLink := func(t *testing.T) (*protect.Set, string, string) {
		t.Helper()
		root := t.TempDir()
		real := filepath.Join(root, "nested", "deeper")
		writeMaskFixture(t, real, "private.txt", private)
		writeMaskFixture(t, real, "keep.txt", public)
		link := linkFixture(t, root, "linked-dir", real)
		set, err := protect.Compile([]string{filepath.Join(link, "private.txt")})
		if err != nil {
			t.Fatalf("compiling the globs: %v", err)
		}
		return set, root, "cat linked-dir/private.txt; cat linked-dir/keep.txt"
	}
	// globThroughIntermediateDirLink is the same with a wildcard in the
	// last segment.
	globThroughIntermediateDirLink := func(t *testing.T) (*protect.Set, string, string) {
		t.Helper()
		root := t.TempDir()
		real := filepath.Join(root, "nested", "deeper")
		writeMaskFixture(t, real, "private.txt", private)
		writeMaskFixture(t, real, "keep.txt", public)
		linkFixture(t, root, "linked-dir", real)
		set, err := protect.Compile([]string{filepath.Join(root, "linked-dir", "private.*")})
		if err != nil {
			t.Fatalf("compiling the globs: %v", err)
		}
		return set, root, "cat linked-dir/private.txt; cat linked-dir/keep.txt"
	}

	cases := []struct {
		name  string
		setup func(t *testing.T) (*protect.Set, string, string)
	}{
		{"literal under a symlinked working directory", literalUnderLinkedRoot},
		{"glob under a symlinked prefix", globUnderLinkedPrefix},
		{"file link inside the binds", fileLinkInsideBinds},
		{"file link outside the binds", fileLinkOutsideBinds},
		{"wildcard matching a file link", wildcardMatchesFileLink},
		{"literal under an intermediate directory link", intermediateDirLink},
		{"glob under an intermediate directory link", globThroughIntermediateDirLink},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, workdir, command := tc.setup(t)
			got := maskedOutput(t, set, workdir, command, public)
			if strings.Contains(got, private) {
				t.Errorf("a protected path reached through a symlink is readable: %q", got)
			}
		})
	}
}

// TestAMaskerSymlinkMasksItsTarget covers the masker contract for a link:
// the mount lands on the path the link reaches, so the link reads nothing
// and the sandbox starts.
func TestAMaskerSymlinkMasksItsTarget(t *testing.T) {
	workdir := t.TempDir()
	target := writeMaskFixture(t, workdir, "private.txt", "fixture-protected-content")
	writeMaskFixture(t, workdir, "keep.txt", "fixture-public-sibling")
	link := linkFixture(t, workdir, "alias.txt", target)
	got := maskedOutput(t, reviewMasker{paths: []string{link}}, workdir, "cat alias.txt; cat keep.txt", "fixture-public-sibling")
	if strings.Contains(got, "fixture-protected-content") {
		t.Errorf("the target of a masker symlink is readable: %q", got)
	}
}

// TestAMaskerSymlinkOutsideTheBindsMasksNothing covers the other half of
// the contract: a link whose target no bind reaches is not in the sandbox,
// so there is no mount to make and nothing to fail on.
func TestAMaskerSymlinkOutsideTheBindsMasksNothing(t *testing.T) {
	workdir := t.TempDir()
	secret := writeMaskFixture(t, t.TempDir(), "secret.txt", "fixture-protected-content")
	writeMaskFixture(t, workdir, "keep.txt", "fixture-public-sibling")
	link := linkFixture(t, workdir, "alias.txt", secret)
	got := maskedOutput(t, reviewMasker{paths: []string{link}}, workdir, "cat alias.txt 2>/dev/null; cat keep.txt", "fixture-public-sibling")
	if strings.Contains(got, "fixture-protected-content") {
		t.Errorf("the target of a masker symlink is readable: %q", got)
	}
}

// TestAProtectedWorkingDirectoryThatIsASymlinkIsMasked covers a glob that
// protects the whole working directory: the sandbox mounts the directory
// under the link's name, so the whole directory goes, and the command still
// runs.
func TestAProtectedWorkingDirectoryThatIsASymlinkIsMasked(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	writeMaskFixture(t, real, "private.txt", "fixture-protected-content")
	link := linkFixture(t, root, "linked-root", real)
	set, err := protect.Compile([]string{link})
	if err != nil {
		t.Fatalf("compiling the globs: %v", err)
	}
	exec, err := bwrap.New(bwrap.Options{Masker: set})
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	obs := reviewCall(t, exec, "cat private.txt; echo done", link)
	got, _ := obs["output"].(string)
	code, _ := obs["returncode"].(float64)
	if code != 0 {
		t.Errorf("the sandbox did not start: returncode %v, output %q", code, got)
	}
	if strings.Contains(got, "fixture-protected-content") {
		t.Errorf("a protected working directory is readable through its link: %q", got)
	}
}
