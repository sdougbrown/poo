package eval

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runPython runs a python snippet in a directory and returns its
// output.
func runPython(t *testing.T, dir, snippet string) string {
	t.Helper()
	cmd := exec.Command("python3", "-c", snippet)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running python in %s: %v: %s", dir, err, out)
	}
	return string(out)
}

// readFixtureFile reads a file of a fixture.
func readFixtureFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// fixtureField reads a version out of a file.
func fixtureField(t *testing.T, content, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(name + `\s*=?\s*["']?([0-9][^"'\s]*)["']?`)
	match := pattern.FindStringSubmatch(content)
	if match == nil {
		t.Fatalf("the file holds no %s", name)
	}
	return match[1]
}

// The read-only tasks ask a question and check the answer.
//
// The question has to be answerable: the answer must not be in the
// prompt, the check must verify every fact the prompt asks for, and
// the check must not be reachable from the working dir.

// The read-only prompts must not hand the answer to the model.
//
// A prompt that names the function, the variable or the handler makes
// the task a copy exercise, and a model that reads the prompt gets the
// answer for free.
func TestTheReadOnlyPromptsDoNotNameTheAnswer(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}

	// The words that would give a read-only task's answer away.
	forbidden := map[string][]string{
		"find-secret-in-config":        {"APP_TOKEN"},
		"find-dead-code":               {"handle_legacy_ping"},
		"find-off-by-one-loop":         {"chunks"},
		"find-slow-loop":               {"find_all"},
		"find-version-mismatch":        {"__version__"},
		"long-find-error-in-log":       {"req-8f2a41c9"},
		"long-find-definition-in-tree": {"src/pipeline/ingest/handlers/loader.py"},
	}

	for _, task := range tasks {
		if !task.HasTag("readonly") {
			continue
		}
		t.Run(task.ID, func(t *testing.T) {
			words, ok := forbidden[task.ID]
			if !ok {
				t.Fatalf("the task %q has no forbidden words in the test's map", task.ID)
			}
			for _, word := range words {
				if strings.Contains(task.Prompt, word) {
					t.Errorf("the prompt names %q, which is the answer", word)
				}
			}
		})
	}
}

// The read-only check verifies every fact the prompt asks for.
//
// An answer that names one of the facts is not enough: the check has to
// reject it.
func TestTheReadOnlyChecksWantEveryFact(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}

	// The answers that name only some of the facts.
	partial := map[string]string{
		"find-secret-in-config":        "The token comes from APP_TOKEN.",
		"find-dead-code":               "The unreachable handler is handle_legacy_ping.",
		"find-off-by-one-loop":         "The helper is chunks.",
		"find-slow-loop":               "The slow method is get.",
		"find-version-mismatch":        "The package declares 1.3.0.",
		"long-find-definition-in-tree": "The definition is somewhere under src/pipeline.",
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

			// An answer that names only some of the facts fails.
			writeSessionLog(t, log, partial[task.ID])
			if code := runCheck(t, task.Dir, workdir, log, ""); code == 0 {
				t.Errorf("the check passed an answer that named only some of the facts: %q", partial[task.ID])
			}
		})
	}
}

// The read-only checks pass the right answer.
//
// The right answer is the one a careful model would give, and the check
// has to accept it.
func TestTheReadOnlyChecksAcceptTheRightAnswer(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}

	answers := map[string]string{
		"find-secret-in-config":        "The token is read by the read_api_token() function in app/secrets.py, and it comes from the APP_TOKEN environment variable.",
		"find-dead-code":               "The unreachable handler is handle_legacy_ping in src/handlers.py. src/routes.py would have to change to point a route at it.",
		"find-off-by-one-loop":         "The helper is chunks in lib/pipeline.py. Its loop condition compares index < len(items) - 1, so the last item is never included.",
		"find-slow-loop":               "The slow method is get in svc/store.py. A dict keyed by the record id would replace the scan.",
		"find-version-mismatch":        "pyproject.toml declares version 1.4.0 and src/shop/__init__.py declares __version__ = 1.3.0, so src/shop/__init__.py is out of step with the changelog.",
		"long-find-error-in-log":       "The one ERROR line carries request id req-8f2a41c9.",
		"long-find-definition-in-tree": "The definition is in src/pipeline/ingest/handlers/loader.py.",
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

			writeSessionLog(t, log, answers[task.ID])
			if code := runCheck(t, task.Dir, workdir, log, ""); code != 0 {
				t.Errorf("the check rejected the right answer: %q", answers[task.ID])
			}
		})
	}
}

// The read-only checks accept a right answer written a different way.
//
// A check that only accepts one phrasing measures the phrasing, not
// the answer.
func TestTheReadOnlyChecksAcceptAnotherPhrasing(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}

	answers := map[string]string{
		"find-off-by-one-loop":  "The helper is chunks. Its loop condition is `index < len(items) - 1`, and it should be `index < len(items)`.",
		"find-slow-loop":        "Store.get is the slow one. Build a dict of the records keyed by id and look the key up in it.",
		"find-version-mismatch": "The packaging metadata says 1.4.0 and the package says 1.3.0, so src/shop/__init__.py is the one out of step.",
		"find-dead-code":        "handle_legacy_ping is the orphan. Adding it to ROUTES in routes.py would make it reachable.",
		"find-secret-in-config": "read_api_token() reads it, and the variable is APP_TOKEN.",
	}

	for _, task := range tasks {
		if !task.HasTag("readonly") {
			continue
		}
		answer, ok := answers[task.ID]
		if !ok {
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

			writeSessionLog(t, log, answer)
			if code := runCheck(t, task.Dir, workdir, log, ""); code != 0 {
				t.Errorf("the check rejected another phrasing of the right answer: %q", answer)
			}
		})
	}
}

// The two tasks that were invalid have a real defect now.
//
// A task whose premise is false measures nothing, and a check that
// passes a log holding one word measures the check, not the model.
func TestTheReadOnlyFixturesHaveTheDefectTheyClaim(t *testing.T) {
	root := repoRoot(t)

	t.Run("find-off-by-one-loop", func(t *testing.T) {
		fixture := filepath.Join(root, "evals", "fixtures", "find-off-by-one-loop")
		out := runPython(t, fixture, "import sys; sys.path.insert(0, '.'); "+
			"from lib.pipeline import chunks; print(chunks([1,2,3,4,5], 2))")
		if strings.Contains(out, "5") {
			t.Errorf("the chunking helper kept every item: %q", out)
		}
	})

	t.Run("find-slow-loop", func(t *testing.T) {
		fixture := filepath.Join(root, "evals", "fixtures", "find-slow-loop")
		out := runPython(t, fixture, "import sys; sys.path.insert(0, '.'); "+
			"from svc.store import Store; print(Store([{'id':'a'}]).get('a'))")
		if strings.Contains(out, "Error") {
			t.Errorf("the store raised instead of scanning: %q", out)
		}
	})
}

// The version task has one file that is out of step.
//
// Two files out of step make the question ambiguous, and an ambiguous
// question has no right answer.
func TestTheVersionTaskHasOneOutOfStepFile(t *testing.T) {
	root := repoRoot(t)
	fixture := filepath.Join(root, "evals", "fixtures", "find-version-mismatch")
	metadata := readFixtureFile(t, filepath.Join(fixture, "pyproject.toml"))
	pkg := readFixtureFile(t, filepath.Join(fixture, "src", "shop", "__init__.py"))
	changelog := readFixtureFile(t, filepath.Join(fixture, "docs", "CHANGELOG.md"))

	metaVersion := fixtureField(t, metadata, "version")
	pkgVersion := fixtureField(t, pkg, "__version__")
	changelogVersion := fixtureField(t, changelog, "##")

	outOfStep := 0
	if metaVersion != changelogVersion {
		outOfStep++
	}
	if pkgVersion != changelogVersion {
		outOfStep++
	}
	if outOfStep != 1 {
		t.Errorf("%d files are out of step with the changelog, want exactly 1", outOfStep)
	}
}

// The read-only checks tell a named fact from a word that shares its
// spelling, and accept a path however much of it is written.
func TestTheReadOnlyChecksJudgeTheFactNotTheWord(t *testing.T) {
	root := repoRoot(t)
	tasks, err := LoadTasks(filepath.Join(root, "evals", "tasks"))
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}
	byID := map[string]Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}

	cases := []struct {
		task   string
		answer string
		pass   bool
	}{
		{"find-slow-loop", "Store.get scans every record. Use a dict.", true},
		{"find-slow-loop", "Each lookup scans, so you get slow behaviour. Use a dict.", false},
		{"find-slow-loop", "find_all does the scan, and a dict keyed by id would replace it.", true},
		{"find-slow-loop", "Store.put does the scan, and a dict keyed by id would replace it.", false},
		{"find-off-by-one-loop", "chunks() stops early: it should be `index < len(items)`.", true},
		{"find-off-by-one-loop", "chunks() stops early: it should be `index <= len(items) - 1`.", true},
		{"find-off-by-one-loop", "chunks() stops early: it should be `index < len(items) - 2`.", false},
		{"find-version-mismatch", "pyproject says 1.4.0, the package says 1.3.0, and shop/__init__.py is out of step.", true},
		{"long-find-definition-in-tree", "parse_manifest is defined in loader.py.", true},
		{"long-find-definition-in-tree", "parse_manifest is defined in other.py.", false},
	}
	for _, c := range cases {
		task := byID[c.task]
		fixture, err := task.FixtureDir(filepath.Join(root, "evals", "fixtures"))
		if err != nil {
			t.Fatalf("resolving the fixture: %v", err)
		}
		workdir := t.TempDir()
		if err := copyTree(fixture, workdir); err != nil {
			t.Fatalf("copying the fixture: %v", err)
		}
		log := filepath.Join(workdir, "session.jsonl")
		writeSessionLog(t, log, c.answer)
		code := runCheck(t, task.Dir, workdir, log, "")
		if (code == 0) != c.pass {
			t.Errorf("%s: %q: passed=%t, want %t", c.task, c.answer, code == 0, c.pass)
		}
	}
}
