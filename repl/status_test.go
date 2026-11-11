package repl

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

// TestTheStatuslineNamesWhatTheSessionKnows covers the line: the
// folder, the branch, the model and the context's fill.
func TestTheStatuslineNamesWhatTheSessionKnows(t *testing.T) {
	s := &status{dir: "/home/me/Code/poo", branch: "main", model: "gemma4"}
	s.setUsage(12300, 262144)

	line := s.line()
	for _, want := range []string{"⌂ poo", "⎇ main", "gemma4", "ctx 12.3k/262.1k"} {
		if !strings.Contains(line, want) {
			t.Errorf("the statusline lacks %q:\n%s", want, line)
		}
	}
}

// TestTheStatuslineLeavesOutWhatItDoesNotKnow covers the gaps: a
// session outside a repository has no branch, and a model with no
// window shows the fill alone.
func TestTheStatuslineLeavesOutWhatItDoesNotKnow(t *testing.T) {
	s := &status{dir: "/tmp"}
	s.setUsage(500, 0)

	line := s.line()
	if strings.Contains(line, "⎇") {
		t.Errorf("the statusline names a branch it does not know:\n%s", line)
	}
	if !strings.Contains(line, "ctx 500") {
		t.Errorf("the context fill is missing:\n%s", line)
	}
	if strings.Contains(line, "ctx 500/") {
		t.Errorf("the context fill shows a window it does not know:\n%s", line)
	}
}

// TestThePromptReadsBehindTheStatusline covers the prompt: the status
// is the line above, and the mark is the line the typing goes to.
// TestAWindowSwitchClearsTheWindowItDoesNotKnow covers a move to a
// model whose window the catalog does not know: the line drops the
// old window instead of keeping the last model's.
func TestAWindowSwitchClearsTheWindowItDoesNotKnow(t *testing.T) {
	s := &status{dir: "/tmp", model: "gemma4"}
	s.setWindow(262144)
	s.setUsage(12300, 262144)

	s.model = "mystery"
	s.setWindow(0)

	line := s.line()
	if strings.Contains(line, "/262.1k") {
		t.Errorf("the statusline keeps the last model's window:\n%s", line)
	}
	if !strings.Contains(line, "ctx 12.3k") {
		t.Errorf("the context fill went missing:\n%s", line)
	}

	s.setWindow(4096)
	if !strings.Contains(s.line(), "ctx 12.3k/4.1k") {
		t.Errorf("the statusline misses the new window:\n%s", s.line())
	}
}

func TestThePromptReadsBehindTheStatusline(t *testing.T) {
	s := &status{dir: "/home/me/Code/poo", model: "gemma4"}
	prompt := s.prompt()
	if !strings.HasSuffix(prompt, "\n💩❯ ") {
		t.Errorf("the prompt does not end at the mark: %q", prompt)
	}
	if !strings.Contains(prompt, "poo") {
		t.Errorf("the prompt carries no status: %q", prompt)
	}
}

// TestThePromptIsTheMarkAloneWithoutAStatus covers a session with
// nothing to say: the mark stands alone.
func TestThePromptIsTheMarkAloneWithoutAStatus(t *testing.T) {
	s := &status{}
	if got := s.prompt(); got != promptMark {
		t.Errorf("the prompt is %q, want %q", got, promptMark)
	}
}

// TestTheBranchReadsFromARepository covers the branch segment: a
// repository names its branch, and a plain directory has none.
func TestTheBranchReadsFromARepository(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	// A rename works on an unborn branch: symbolic-ref still names
	// it, and no commit is needed.
	runGit(t, dir, "checkout", "-b", "trunk")
	branch, ok := gitBranch(dir)
	if !ok || branch != "trunk" {
		t.Errorf("the repository named the branch %q (found %v)", branch, ok)
	}

	if _, ok := gitBranch(t.TempDir()); ok {
		t.Error("a plain directory named a branch")
	}
}

// TestTheBranchReadsAtADetachedHead covers the fallback: a head with
// no symbolic ref names its commit.
func TestTheBranchReadsAtADetachedHead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit",
		"--allow-empty", "-m", "one")
	runGit(t, dir, "checkout", "--detach", "HEAD")
	branch, ok := gitBranch(dir)
	if !ok {
		t.Fatal("a detached head named no branch")
	}
	// The branch is the commit the head points at, short form.
	short := strings.TrimSpace(runGitOut(t, dir, "rev-parse", "--short", "HEAD"))
	if branch != short {
		t.Errorf("the detached head named %q, want %q", branch, short)
	}
}

// runGit runs one git command in dir.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	runGitOut(t, dir, args...)
}

// runGitOut runs one git command in dir and returns its output.
func runGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
