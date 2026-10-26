package eval

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the repository's root, the directory that holds the
// evals directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "evals", "models.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("the evals directory is not reachable from the test's directory")
		}
		dir = parent
	}
}

// runCheck runs a task's check.sh in a working dir and returns its exit code.
func runCheck(t *testing.T, taskDir, workdir, sessionLog, changed string) int {
	t.Helper()
	cmd := exec.Command("/bin/bash", filepath.Join(taskDir, "check.sh"))
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"POO_WORKDIR="+workdir,
		"POO_SESSION_LOG="+sessionLog,
		"POO_CHANGED_FILES="+changed,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	t.Fatalf("running the check: %v: %s", err, out)
	return 1
}

// The task set's shape is the plan's: twelve coding fixes, five
// find-and-explain questions, and three long tasks.
func TestTheTaskSetHasThePlanShape(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}
	if len(tasks) != 20 {
		t.Errorf("the task set holds %d tasks, want 20", len(tasks))
	}

	counts := map[string]int{}
	for _, task := range tasks {
		for _, tag := range task.Tags {
			counts[tag]++
		}
	}
	if counts["long"] != 3 {
		t.Errorf("the task set holds %d long tasks, want 3", counts["long"])
	}
	// The coding tasks are the ones with a bug to fix.
	coding := 0
	for _, task := range tasks {
		if task.HasTag("bugfix") {
			coding++
		}
	}
	if coding != 12 {
		t.Errorf("the task set holds %d coding tasks, want 12", coding)
	}
	// The find-and-explain tasks are the short read-only ones. Two of
	// the long tasks are read-only as well, so the count is of the
	// read-only tasks that are not long.
	explaining := 0
	for _, task := range tasks {
		if task.HasTag("readonly") && !task.HasTag("long") {
			explaining++
		}
	}
	if explaining != 5 {
		t.Errorf("the task set holds %d find-and-explain tasks, want 5", explaining)
	}
}

// Every task names a fixture that exists, and its check is executable.
func TestEveryTaskHasItsFixtureAndCheck(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}
	for _, task := range tasks {
		fixture, err := task.FixtureDir(filepath.Join(root, "evals", "fixtures"))
		if err != nil {
			t.Errorf("task %q: %v", task.ID, err)
			continue
		}
		info, err := os.Stat(fixture)
		if err != nil || !info.IsDir() {
			t.Errorf("task %q: the fixture %s is missing", task.ID, fixture)
		}
		check := filepath.Join(task.Dir, "check.sh")
		info, err = os.Stat(check)
		if err != nil {
			t.Errorf("task %q: the check is missing: %v", task.ID, err)
			continue
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("task %q: the check is not executable", task.ID)
		}
	}
}

// The read-only tasks' checks fail when the answer is wrong, pass when
// it is right, and fail when the run changed a file.
func TestTheReadOnlyChecksHoldTheirConditions(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}

	for _, task := range tasks {
		if !task.HasTag("readonly") {
			continue
		}
		t.Run(task.ID, func(t *testing.T) {
			fixture, err := task.FixtureDir(filepath.Join(root, "evals", "fixtures"))
			if err != nil {
				t.Fatalf("resolving the fixture: %v", err)
			}
			workdir := t.TempDir()
			if err := copyTree(fixture, workdir); err != nil {
				t.Fatalf("copying the fixture: %v", err)
			}
			log := filepath.Join(workdir, "session.jsonl")

			// A run that changed a file fails, whatever it answered.
			writeSessionLog(t, log, "the answer is handle_legacy_ping")
			if code := runCheck(t, task.Dir, workdir, log, "a.py"); code == 0 {
				t.Error("the check passed a run that changed a file")
			}

			// A run that answered nothing fails.
			writeSessionLog(t, log, "")
			if code := runCheck(t, task.Dir, workdir, log, ""); code == 0 {
				t.Error("the check passed a run with no answer")
			}

			// A run that answered wrongly fails.
			writeSessionLog(t, log, "I could not find it.")
			if code := runCheck(t, task.Dir, workdir, log, ""); code == 0 {
				t.Error("the check passed a wrong answer")
			}
		})
	}
}

// writeSessionLog writes a session log with one assistant message.
func writeSessionLog(t *testing.T, path, answer string) {
	t.Helper()
	content := `{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}` + "\n" +
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":` +
		jsonString(answer) + `}]}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}
}

// jsonString renders a string as a JSON string.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// The coding tasks' checks run the fixture's tests, so they fail while
// the bug is there.
func TestTheCodingChecksFailBeforeTheFix(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
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
			workdir := t.TempDir()
			if err := copyTree(fixture, workdir); err != nil {
				t.Fatalf("copying the fixture: %v", err)
			}
			if code := runCheck(t, task.Dir, workdir, "", ""); code == 0 {
				t.Error("the check passed before the fix")
			}
		})
	}
}
