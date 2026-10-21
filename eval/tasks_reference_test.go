package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every coding fixture's bug is real: the fixture's own tests fail
// before the fix and pass after it.
//
// The reference fixes live outside the task tree, so a task never
// ships with its answer next to it. A missing reference fix is not a
// failure of this test: the fix is a development aid, and the task
// tree is what ships.

func TestTheCodingChecksFailBeforeTheFixAndPassAfterIt(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}
	fixesDir := filepath.Join(root, ".cache", "reference-fixes")
	if _, err := os.ReadDir(fixesDir); err != nil {
		t.Skipf("the reference fixes are not reachable: %v", err)
	}

	for _, task := range tasks {
		if !task.HasTag("bugfix") {
			continue
		}
		t.Run(task.ID, func(t *testing.T) {
			fixture, err := task.FixtureDir(filepath.Join(root, "evals", "fixtures"))
			if err != nil {
				t.Fatalf("resolving the fixture: %v", err)
			}

			// Before the fix, the check fails.
			workdir := t.TempDir()
			copyDir(t, fixture, workdir)
			if code := runCheck(t, task.Dir, workdir, "", ""); code == 0 {
				t.Error("the check passed before the fix")
			}

			// After the fix, the check passes.
			fix := filepath.Join(fixesDir, task.ID+".py")
			source, err := os.ReadFile(fix)
			if err != nil {
				t.Skipf("the reference fix is not reachable: %v", err)
			}
			workdir = t.TempDir()
			copyDir(t, fixture, workdir)
			target := packageInit(t, workdir)
			if err := os.WriteFile(target, source, 0o644); err != nil {
				t.Fatalf("writing the fix: %v", err)
			}
			if code := runCheck(t, task.Dir, workdir, "", ""); code != 0 {
				t.Errorf("the check rejected the reference fix")
			}
		})
	}
}

// packageInit returns the package's __init__.py in a workdir.
//
// The package is the directory beside the tests directory.
func packageInit(t *testing.T, workdir string) string {
	t.Helper()
	entries, err := os.ReadDir(workdir)
	if err != nil {
		t.Fatalf("reading the workdir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "tests" {
			continue
		}
		path := filepath.Join(workdir, e.Name(), "__init__.py")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Fatalf("the workdir holds no package: %s", strings.Join(dirNames(t, workdir), ", "))
	return ""
}

// dirNames returns a directory's subdirectory names.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
