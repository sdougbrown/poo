package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nothing in a model's workdir may hold the answer.
//
// The workdir is a copy of the fixture, and the model works in it. A
// check script, a task file or an answer derivation in there is
// something the model can read, and a model that reads it learns the
// answer without doing the work.
//
// The harness makes the workdir, so the property is held for every
// task: the workdir is the fixture and nothing else.

// The names a task tree uses for the things that must not be in a
// workdir.
var answerFileNames = []string{
	"check.sh",
	"check_answer.py",
	"task.json",
	"readonly_check.sh",
}

// Every fixture holds nothing that belongs to the task tree.
func TestNoFixtureHoldsAnAnswerFile(t *testing.T) {
	root := repoRoot(t)
	fixtures := filepath.Join(root, "evals", "fixtures")
	entries, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatalf("reading the fixtures: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(fixtures, e.Name())
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			for _, name := range answerFileNames {
				if base == name {
					t.Errorf("the fixture %s holds %q", e.Name(), name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

// The workdir the harness makes holds the fixture and nothing else.
//
// The harness copies the fixture, so the check is on the copy: the
// answer derivation is in the task tree, and the task tree is never in
// the workdir.
func TestTheWorkdirIsTheFixtureAndNothingElse(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			// The workdir is made the way the harness makes it: the
			// fixture is snapshotted into the work root, and the run
			// copies the snapshot.
			fixtures := filepath.Join(root, "evals", "fixtures")
			guard, err := newSourceGuard(tasks, fixtures, t.TempDir())
			if err != nil {
				t.Fatalf("guarding the fixtures: %v", err)
			}
			snap, err := guard.snapshot(task, fixtures)
			if err != nil {
				t.Fatalf("snapshotting the fixture: %v", err)
			}
			workdir := t.TempDir()
			if err := copyTree(snap, workdir); err != nil {
				t.Fatalf("copying the snapshot: %v", err)
			}

			var found []string
			err = filepath.Walk(workdir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(workdir, path)
				if err != nil {
					return err
				}
				base := filepath.Base(path)
				for _, name := range answerFileNames {
					if base == name {
						found = append(found, rel)
					}
				}
				// The task's own directory is never in the workdir.
				if strings.HasPrefix(rel, "tasks") {
					found = append(found, rel)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walking the workdir: %v", err)
			}
			if len(found) > 0 {
				t.Errorf("the workdir holds %v, want nothing that answers the task", found)
			}
		})
	}
}

// The task tree's own files are not named after an answer.
//
// A file called "answer" in the task tree is a file a model can find.
func TestTheTaskTreeHoldsNoAnswerFile(t *testing.T) {
	root := repoRoot(t)
	tasksDir := filepath.Join(root, "evals", "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		t.Fatalf("reading the tasks: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			switch f.Name() {
			case "check.sh", "task.json":
			default:
				t.Errorf("the task %s holds %q, want only check.sh and task.json", e.Name(), f.Name())
			}
		}
	}
}
