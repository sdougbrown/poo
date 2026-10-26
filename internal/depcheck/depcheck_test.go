package depcheck_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/internal/depcheck"
)

// TestRepoImports checks the poo module itself: the core package may import
// the standard library plus poo/prompt and poo/decide, poo/prompt,
// poo/prompt/agentsmd, poo/decide and poo/wire/protocol may import the
// standard library only, and poo/provider/catalog may import the standard
// library and the core package.
func TestRepoImports(t *testing.T) {
	root := moduleRoot(t)
	modulePath := modulePathOf(t, root)

	touchGoFiles(t, root)
	touchCacheInputs(t, root)
	res, err := depcheck.Checker{
		ModuleRoot: root,
		ModulePath: modulePath,
	}.Check()
	if err != nil {
		t.Fatalf("checking %s: %v", modulePath, err)
	}
	for _, skip := range res.Skipped {
		t.Logf("skip: %s", skip)
	}
	if len(res.Findings) > 0 {
		t.Errorf("%s breaks the dependency rules:\n%s", modulePath, strings.Join(res.Findings, "\n"))
	}
}

// TestFixtures runs the check over each fixture module and compares the
// findings and the skipped packages exactly.
func TestFixtures(t *testing.T) {
	const m = "example.com/poo/depcheck/"
	tests := []struct {
		name string
		want []string
	}{
		{
			name: "direct",
			want: []string{m + "direct imports " + m + "direct/forbidden"},
		},
		{
			name: "internalpkg",
			want: []string{m + "internalpkg imports " + m + "internalpkg/session"},
		},
		{
			// prompt is allowed for core but may not import decide or helper;
			// helper is not a checked package and may not import extra.
			name: "transitive",
			want: []string{
				m + "transitive/helper imports " + m + "transitive/extra (in the closure of " + m + "transitive)",
				m + "transitive/helper imports " + m + "transitive/extra (in the closure of " + m + "transitive/prompt)",
				m + "transitive/prompt imports " + m + "transitive/decide",
				m + "transitive/prompt imports " + m + "transitive/helper",
			},
		},
		{
			name: "cycle",
			want: []string{
				"cannot check " + m + "cycle/session: import cycle not allowed",
				"cannot check " + m + "cycle: import cycle not allowed",
				m + "cycle imports " + m + "cycle/session",
			},
		},
		{
			name: "allowed",
		},
		{
			// The agentsmd loader may import the standard library
			// only, so the helper it reaches for is a finding.
			name: "agentsmd",
			want: []string{
				m + "agentsmd/prompt/agentsmd imports " + m + "agentsmd/helper",
			},
		},
		{
			name: "wireproto",
			want: []string{m + "wireproto/wire/protocol imports " + m + "wireproto/extra"},
		},
		{
			name: "wireconf",
			want: []string{m + "wireconf/wire/conformance imports " + m + "wireconf/extra"},
		},
		{
			name: "unresolvable",
			want: []string{"cannot resolve " + m + "unresolvable/missing (imported by " + m + "unresolvable)"},
		},
		{
			name: "unparseable",
			want: []string{"cannot check " + m + "unparseable/broken: found packages broken (broken.go) and other (other.go) in broken"},
		},
		{
			name: "onlytests",
		},
		{
			// The permission package may import the core package and
			// no more.
			name: "perm",
			want: []string{
				m + "perm/perm imports " + m + "perm/extra",
			},
		},
		{
			// The session package may import the core package and the
			// decide package and no more.
			name: "session",
			want: []string{
				m + "session/session imports " + m + "session/extra",
			},
		},
		{
			name: "ignored",
		},
		{
			name: "ignoreonly",
		},
		{
			name: "ignoreddep",
			want: []string{"cannot resolve " + m + "ignoreddep/gen (imported by " + m + "ignoreddep)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := checkModule(t, filepath.Join("testdata", tt.name), m+tt.name)
			if !slices.Equal(res.Findings, tt.want) {
				t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(res.Findings, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// TestSkippedPackages checks that a checked package whose directory is
// missing, or holds only test files, is skipped rather than reported.
func TestSkippedPackages(t *testing.T) {
	const m = "example.com/poo/depcheck/onlytests"
	res := checkModule(t, filepath.Join("testdata", "onlytests"), m)
	want := []string{
		m + "/decide (no non-test Go files)",
		m + "/perm (does not exist yet)",
		m + "/perm/decide (does not exist yet)",
		m + "/prompt (does not exist yet)",
		m + "/prompt/agentsmd (does not exist yet)",
		m + "/provider/catalog (does not exist yet)",
		m + "/session (does not exist yet)",
		m + "/tool/cliregistry (does not exist yet)",
		m + "/wire (does not exist yet)",
		m + "/wire/conformance (does not exist yet)",
		m + "/wire/protocol (does not exist yet)",
	}
	if !slices.Equal(res.Skipped, want) {
		t.Errorf("skipped %q, want %q", res.Skipped, want)
	}
}

// TestABuildIgnoredFileIsSkipped covers the dot/underscore exemption: a
// package whose only Go file is build-ignored by an underscore prefix is
// skipped, not reported and not treated as an error.
func TestABuildIgnoredFileIsSkipped(t *testing.T) {
	const m = "example.com/poo/depcheck/ignored"
	res := checkModule(t, filepath.Join("testdata", "ignored"), m)
	want := []string{
		m + " (no non-test Go files)",
		m + "/decide (does not exist yet)",
		m + "/perm (does not exist yet)",
		m + "/perm/decide (does not exist yet)",
		m + "/prompt (does not exist yet)",
		m + "/prompt/agentsmd (does not exist yet)",
		m + "/provider/catalog (does not exist yet)",
		m + "/session (does not exist yet)",
		m + "/tool/cliregistry (does not exist yet)",
		m + "/wire (does not exist yet)",
		m + "/wire/conformance (does not exist yet)",
		m + "/wire/protocol (does not exist yet)",
	}
	if !slices.Equal(res.Skipped, want) {
		t.Errorf("skipped %q, want %q", res.Skipped, want)
	}
	if len(res.Findings) != 0 {
		t.Errorf("findings:\n%s\nwant none", strings.Join(res.Findings, "\n"))
	}
}

// TestAFileWithTheIgnoreBuildTagIsNotSeen covers the doc's promise: a
// normal-named file carrying //go:build ignore does not fail the check,
// the way a dot- or underscore-prefixed file does not.
func TestAFileWithTheIgnoreBuildTagIsNotSeen(t *testing.T) {
	const m = "example.com/poo/depcheck/ignoreonly"
	res := checkModule(t, filepath.Join("testdata", "ignoreonly"), m)
	if len(res.Findings) != 0 {
		t.Errorf("findings:\n%s\nwant none", strings.Join(res.Findings, "\n"))
	}
	want := []string{
		m + "/decide (does not exist yet)",
		m + "/perm (does not exist yet)",
		m + "/perm/decide (does not exist yet)",
		m + "/prompt (does not exist yet)",
		m + "/prompt/agentsmd (does not exist yet)",
		m + "/provider/catalog (does not exist yet)",
		m + "/session (does not exist yet)",
		m + "/tool/cliregistry (does not exist yet)",
		m + "/wire (does not exist yet)",
		m + "/wire/conformance (does not exist yet)",
		m + "/wire/protocol (does not exist yet)",
	}
	if !slices.Equal(res.Skipped, want) {
		t.Errorf("skipped %q, want %q", res.Skipped, want)
	}
}

// TestABrokenModuleIsAnError covers the child failure: a module whose
// go.mod does not parse makes Check fail rather than pass silently.
func TestABrokenModuleIsAnError(t *testing.T) {
	_, err := (depcheck.Checker{
		ModuleRoot: filepath.Join("testdata", "brokenmod"),
		ModulePath: "example.com/poo/depcheck/brokenmod",
	}.Check())
	if err == nil {
		t.Error("the broken module returned no error, want one")
	}
}

// touchCacheInputs opens the inputs the Go test cache does not track on
// its own: the inherited GOFLAGS, which changes which files the build
// sees, and the module's go.mod and go.sum, which change what the
// toolchain resolves. Opening them adds them to the test's own cache
// key, so a run after a change to any of them recomputes instead of
// serving a stale result.
func touchCacheInputs(t *testing.T, root string) {
	t.Helper()

	_ = os.Getenv("GOFLAGS")
	for _, name := range []string{"go.mod", "go.sum"} {
		f, err := os.Open(filepath.Join(root, name))
		if err == nil {
			f.Close()
		}
	}
}

// checkModule runs the check over a module and returns the result.
func checkModule(t *testing.T, root, modulePath string) depcheck.Result {
	t.Helper()

	touchGoFiles(t, root)
	touchCacheInputs(t, root)
	res, err := depcheck.Checker{ModuleRoot: root, ModulePath: modulePath}.Check()
	if err != nil {
		t.Fatalf("checking %s: %v", modulePath, err)
	}
	return res
}

// moduleRoot returns the directory that holds the go.mod of the module the
// test runs in. It walks up from the working directory of the test.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// modulePathOf returns the import path of the module in a directory.
func modulePathOf(t *testing.T, root string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if path, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(path)
		}
	}
	t.Fatal("no module path in go.mod")
	return ""
}

// touchGoFiles opens every Go file under a module, so the test cache is
// invalidated by an edit to any of them, however deeply nested. The go
// command only hashes the files and directories a test opens.
func touchGoFiles(t *testing.T, root string) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		return f.Close()
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}
