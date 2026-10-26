package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A coding task's check runs the fixture's own tests.
//
// A model that rewrote the tests would make the check pass without
// fixing the bug, so the harness puts the pristine tests back before the
// check runs.

func TestTheCheckRunsTheFixtureOwnTests(t *testing.T) {
	// A run that rewrites the test to pass without fixing the bug
	// must not pass the check.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	// The fixture has a tests directory, and the check runs it.
	fixture := filepath.Join(taskRoot, "..", "fixtures", "fix-a")
	tests := filepath.Join(fixture, "tests")
	if err := os.MkdirAll(tests, 0o755); err != nil {
		t.Fatalf("making the tests directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tests, "test_a.py"), []byte("def test_ok():\n    assert 'fixed' in open('a.txt').read()\n"), 0o644); err != nil {
		t.Fatalf("writing the test: %v", err)
	}

	// The check runs the fixture's tests.
	dir := filepath.Join(taskRoot, "tasks", "fix-a")
	check := "#!/usr/bin/env bash\nset -uo pipefail\ncd \"${POO_WORKDIR:-.}\" || exit 1\npython3 -m unittest discover -s tests -t . >/dev/null\n"
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(check), 0o755); err != nil {
		t.Fatalf("writing the check: %v", err)
	}
	reloaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("loading the task: %v", err)
	}

	// The fake poo rewrites the test so that it passes, and leaves the
	// source broken.
	binary := fakePoo(t, `
dir=""
log=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --working-dir) dir="$2"; shift 2 ;;
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cat > "$dir/tests/test_a.py" <<'PY'
def test_ok():
    assert True
PY
cat > "$log" <<'JSON'
{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}
{"record":"run.settled","seq":2,"ts":"2026-10-20T00:00:01Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}
JSON
`)

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{reloaded},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(out, "work"),
		PooBinary: binary,
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	r := report.Results[0]
	// The test file the model wrote is in the diff, and it is
	// unexpected.
	if !containsAny(r.Metrics.ChangedFiles, "tests/test_a.py") {
		t.Errorf("the diff holds %v, want the rewritten test", r.Metrics.ChangedFiles)
	}
	if !containsAny(r.Metrics.UnexpectedFiles, "tests/test_a.py") {
		t.Errorf("the unexpected files are %v, want the rewritten test", r.Metrics.UnexpectedFiles)
	}
}

func TestARestoredTestIsTheFixtureOwn(t *testing.T) {
	// The harness's restore puts the fixture's tests back.
	root := t.TempDir()
	pristine := filepath.Join(root, "pristine")
	workdir := filepath.Join(root, "work")
	for _, dir := range []string{pristine, workdir} {
		if err := os.MkdirAll(filepath.Join(dir, "tests"), 0o755); err != nil {
			t.Fatalf("making the tests directory: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(pristine, "tests", "test_a.py"), []byte("original\n"), 0o644); err != nil {
		t.Fatalf("writing the pristine test: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "tests", "test_a.py"), []byte("rewritten\n"), 0o644); err != nil {
		t.Fatalf("writing the rewritten test: %v", err)
	}

	if err := restoreTests(pristine, workdir); err != nil {
		t.Fatalf("restoring the tests: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workdir, "tests", "test_a.py"))
	if err != nil {
		t.Fatalf("reading the restored test: %v", err)
	}
	if string(data) != "original\n" {
		t.Errorf("the restored test is %q, want the fixture's", string(data))
	}
}

func TestATaskWithNoTestsIsLeftAlone(t *testing.T) {
	// A task with no tests has nothing to restore.
	root := t.TempDir()
	pristine := filepath.Join(root, "pristine")
	workdir := filepath.Join(root, "work")
	for _, dir := range []string{pristine, workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("making the directory: %v", err)
		}
	}
	if err := restoreTests(pristine, workdir); err != nil {
		t.Fatalf("restoring the tests: %v", err)
	}
}

// The long change task's check wants every timeout to be 45.
//
// A check that only rejects the literal 30 lets a timeout of 46
// through, and the task is then a search-and-replace rather than the
// many-edit task it claims to be.
func TestTheLongChangeCheckWantsEveryTimeout(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}
	var task Task
	for _, t := range tasks {
		if t.ID == "long-change-timeout-everywhere" {
			task = t
		}
	}
	if task.ID == "" {
		t.Fatal("the long change task is missing")
	}

	fixture, err := task.FixtureDir(filepath.Join(root, "evals", "fixtures"))
	if err != nil {
		t.Fatalf("resolving the fixture: %v", err)
	}

	// The fixture as it stands: every timeout is 30, so the check
	// fails.
	workdir := t.TempDir()
	if err := copyTree(fixture, workdir); err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}
	if code := runCheck(t, task.Dir, workdir, "", ""); code == 0 {
		t.Error("the check passed before the change")
	}

	// Every timeout raised to 45, with the tests updated: the check
	// passes.
	workdir = t.TempDir()
	if err := copyTree(fixture, workdir); err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}
	raiseTimeouts(t, workdir, 45)
	updateTests(t, workdir, 45)
	if code := runCheck(t, task.Dir, workdir, "", ""); code != 0 {
		t.Error("the check rejected the finished change")
	}

	// One timeout at 46 and the rest at 45: the check fails, because
	// every request timeout has to be exactly 45.
	workdir = t.TempDir()
	if err := copyTree(fixture, workdir); err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}
	raiseTimeouts(t, workdir, 45)
	updateTests(t, workdir, 45)
	setTimeout(t, filepath.Join(workdir, "shop", "api", "api_1.py"), 46)
	if code := runCheck(t, task.Dir, workdir, "", ""); code == 0 {
		t.Error("the check passed a timeout that is not 45")
	}

	// The decoys must not move.
	workdir = t.TempDir()
	if err := copyTree(fixture, workdir); err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}
	raiseTimeouts(t, workdir, 45)
	updateTests(t, workdir, 45)
	settings := filepath.Join(workdir, "shop", "settings.py")
	data, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("reading the settings: %v", err)
	}
	if err := os.WriteFile(settings, []byte(strings.ReplaceAll(string(data), "PAYMENT_TIMEOUT_SECONDS = 30", "PAYMENT_TIMEOUT_SECONDS = 45")), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	if code := runCheck(t, task.Dir, workdir, "", ""); code == 0 {
		t.Error("the check passed a moved decoy")
	}
}

// raiseTimeouts raises every request timeout in a working dir.
func raiseTimeouts(t *testing.T, workdir string, seconds int) {
	t.Helper()
	err := filepath.Walk(filepath.Join(workdir, "shop"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".py") {
			return nil
		}
		if strings.HasSuffix(path, filepath.Join("shop", "settings.py")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := strings.ReplaceAll(string(data), "_TIMEOUT_SECONDS = 30", "_TIMEOUT_SECONDS = "+itoa(seconds))
		return os.WriteFile(path, []byte(updated), 0o644)
	})
	if err != nil {
		t.Fatalf("raising the timeouts: %v", err)
	}
}

// setTimeout sets one file's timeout constant.
func setTimeout(t *testing.T, path string, seconds int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	updated := strings.ReplaceAll(string(data), "_TIMEOUT_SECONDS = 30", "_TIMEOUT_SECONDS = "+itoa(seconds))
	updated = strings.ReplaceAll(updated, "_TIMEOUT_SECONDS = 45", "_TIMEOUT_SECONDS = "+itoa(seconds))
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// updateTests updates the fixture's own tests to the new value.
func updateTests(t *testing.T, workdir string, seconds int) {
	t.Helper()
	path := filepath.Join(workdir, "tests", "test_timeouts.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the tests: %v", err)
	}
	updated := strings.ReplaceAll(string(data), "SECONDS, 30)", "SECONDS, "+itoa(seconds)+")")
	updated = strings.ReplaceAll(updated, "is_thirty", "is_forty_five")
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("writing the tests: %v", err)
	}
}

// containsAny reports whether a list holds a value.
func containsAny(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
