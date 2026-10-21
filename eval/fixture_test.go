package eval

import (
	"testing"
)

// TestAFixturePathCannotEscapeTheRoot covers the containment check: a
// task's repo field cannot walk out of the fixtures root.
func TestAFixturePathCannotEscapeTheRoot(t *testing.T) {
	for _, repo := range []string{
		"fixture:../../outside",
		"fixture:a/../../outside",
		"fixture:/etc/passwd",
	} {
		task := Task{ID: "t", Repo: repo}
		if _, err := task.FixtureDir(t.TempDir()); err == nil {
			t.Errorf("the fixture path %q escaped the root", repo)
		}
	}
	// A repo inside the root still resolves.
	task := Task{ID: "t", Repo: "fixture:fix-a"}
	dir, err := task.FixtureDir("root/fixtures/../tasks")
	if err != nil {
		t.Fatalf("the in-root fixture was refused: %v", err)
	}
	if dir == "" {
		t.Error("the fixture directory is empty")
	}
}

// TestASourceGuardRefusesAnEscapingFixture covers the snapshot's own
// containment check: the work-tree copy cannot land outside the work
// root, whatever the task's repo says.
func TestASourceGuardRefusesAnEscapingFixture(t *testing.T) {
	task := Task{ID: "t", Repo: "fixture:../../outside"}
	if _, err := newSourceGuard([]Task{task}, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("the source guard accepted an escaping fixture path")
	}
}
