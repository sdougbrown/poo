package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A task's repo may name a commit that is not the tip of the default
// branch. The checkout has to reach it.

func TestAGitRepoIsCheckedOutAtItsPinnedSha(t *testing.T) {
	// A local repository with two commits. The task pins the first one,
	// which a shallow clone does not hold.
	repo := t.TempDir()
	git(t, repo, "init", "--quiet", "-b", "main")
	git(t, repo, "config", "user.email", "harness@example.invalid")
	git(t, repo, "config", "user.name", "harness")
	writeFile(t, filepath.Join(repo, "a.txt"), "first\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "--quiet", "-m", "first")
	first := git(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "a.txt"), "second\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "--quiet", "-m", "second")

	work := t.TempDir()
	runner := NewRunner(RunOptions{})
	// The url is a file:// one, so the clone honours --depth: a local
	// path clone ignores it.
	task := Task{ID: "t", Repo: "file://" + repo + "@" + first}
	got, err := runner.checkout(context.Background(), task, filepath.Join(work, "w"))
	if err != nil {
		t.Fatalf("checking out: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(got, "a.txt"))
	if err != nil {
		t.Fatalf("reading the checked out file: %v", err)
	}
	if string(data) != "first\n" {
		t.Errorf("the checked out file is %q, want the first commit's", string(data))
	}
}

func TestAnSSHRepoKeepsItsAt(t *testing.T) {
	// The sha is the part after the last "@", so an SSH url keeps the
	// one it owns.
	url, sha, ok := splitRepoSHA("git@host.invalid:org/repo.git@abc123")
	if !ok {
		t.Fatal("the repo was not split")
	}
	if url != "git@host.invalid:org/repo.git" {
		t.Errorf("the url is %q, want the whole url", url)
	}
	if sha != "abc123" {
		t.Errorf("the sha is %q, want abc123", sha)
	}
}

func TestARepoWithoutAShaIsRefused(t *testing.T) {
	runner := NewRunner(RunOptions{})
	task := Task{ID: "t", Repo: "https://example.invalid/repo.git"}
	if _, err := runner.checkout(context.Background(), task, filepath.Join(t.TempDir(), "w")); err == nil {
		t.Error("the repo without a sha was accepted, want a refusal")
	}
}

// git runs a git command in a directory and returns its output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeFile writes a file in a directory.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making the directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
}
