package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireBwrap skips a test on a machine where bubblewrap is missing or
// cannot create namespaces.
func requireBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap is not installed")
	}
	if out, err := exec.Command("bwrap", "--ro-bind", "/", "/", "--unshare-all", "true").CombinedOutput(); err != nil {
		t.Skipf("bwrap cannot create a sandbox here: %v: %s", err, out)
	}
}

func TestTheBwrapSandboxHidesEverythingButTheWorkdir(t *testing.T) {
	requireBwrap(t)
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "answer.txt")
	if err := os.WriteFile(secret, []byte("the-answer"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	// The repository and the real home directory are where the task set
	// and the user's files live on a real machine, so the probe lists
	// them too. A listing creates nothing.
	repo := repositoryRootOfTests(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	// The fake run writes its log as the real one does, then records what
	// it could see outside the working dir.
	poo := fakePoo(t, strings.Replace(logWritingPoo, "exit 0",
		`{ cat `+secret+` 2>&1; ls `+taskRoot+` 2>&1; echo "home=$HOME pwd=$(pwd)"
if ls '`+repo+`' >/dev/null 2>&1; then echo repo=readable; else echo repo=hidden; fi
if ls '`+home+`' >/dev/null 2>&1; then echo realhome=readable; else echo realhome=hidden; fi
} > "$(dirname "$log")/seen.txt"
mkdir -p "$HOME/.local/bin" && touch "$HOME/.local/bin/pytest"
exit 0`, 1))

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(t.TempDir(), "work"),
		PooBinary: poo,
		Sandbox:   SandboxBwrap,
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	res := report.Results[0]
	if !res.Success {
		t.Fatalf("the sandboxed run did not succeed: %+v", res)
	}
	if res.Metrics.SettleReason != "end_turn" {
		t.Errorf("the settle reason is %q, want the log read back from the sandbox", res.Metrics.SettleReason)
	}
	seen, err := os.ReadFile(filepath.Join(filepath.Dir(res.LogPath), "seen.txt"))
	if err != nil {
		t.Fatalf("reading what the run saw: %v", err)
	}
	if strings.Contains(string(seen), "the-answer") {
		t.Errorf("the run read a file outside its workdir: %q", seen)
	}
	if strings.Contains(string(seen), "fix-a") {
		t.Errorf("the run listed the task set: %q", seen)
	}
	if !strings.Contains(string(seen), "home=/tmp pwd=/work") {
		t.Errorf("the run saw %q, want its home in /tmp and its working directory /work", seen)
	}
	// A tool that installs into the home directory, as pip --user does,
	// leaves nothing in the working dir's diff.
	if len(res.Metrics.UnexpectedFiles) != 0 {
		t.Errorf("the run's home directory reached the diff: %v", res.Metrics.UnexpectedFiles)
	}
	if !strings.Contains(string(seen), "repo=hidden") {
		t.Errorf("the run could read the repository %s: %q", repo, seen)
	}
	if !strings.Contains(string(seen), "realhome=hidden") {
		t.Errorf("the run could read the real home %s: %q", home, seen)
	}
	if report.Sandbox != SandboxBwrap {
		t.Errorf("the report's sandbox is %q, want %q", report.Sandbox, SandboxBwrap)
	}
}

func TestASandboxThatFailsToStartSaysSo(t *testing.T) {
	requireBwrap(t)
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(t.TempDir(), "work"),
		PooBinary: "/nonexistent-dir/poo",
		Sandbox:   SandboxBwrap,
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	got := report.Results[0].Error
	if !strings.Contains(got, "bwrap sandbox failed to start") || !strings.Contains(got, "/nonexistent-dir/poo") {
		t.Errorf("error = %q, want it to say the sandbox failed and what bwrap printed", got)
	}
}

// repositoryRootOfTests returns the directory above the test's working
// directory that holds go.mod.
func repositoryRootOfTests(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

func TestAPooThatExitsWithoutALogSaysSoWhateverTheSandbox(t *testing.T) {
	for _, mode := range []string{SandboxNone, SandboxBwrap} {
		t.Run(mode, func(t *testing.T) {
			if mode == SandboxBwrap {
				requireBwrap(t)
			}
			fixtureRoot := fakeFixture(t, "fix-a", "old\n")
			taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
			task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
			out := t.TempDir()
			runner := NewRunner(RunOptions{
				Models:    []Model{{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768}},
				Tasks:     []Task{task},
				TaskRoot:  taskRoot,
				Seeds:     1,
				Out:       out,
				RunsDir:   filepath.Join(t.TempDir(), "work"),
				PooBinary: fakePoo(t, "echo boom >&2\nexit 3\n"),
				Sandbox:   mode,
			})
			report, err := runner.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := report.Results[0].Error
			if got != "poo exited 3 without writing a log: boom" {
				t.Errorf("error = %q, want poo's exit code and output", got)
			}
		})
	}
}

func TestTheWaitDelayOutlastsTheKillGrace(t *testing.T) {
	// The kill sleeps KillGrace between SIGTERM and SIGKILL, and Wait
	// must still be waiting when the SIGKILL lands.
	if KillWaitDelay < KillGrace+time.Second {
		t.Errorf("KillWaitDelay is %s, want it at least a second over KillGrace (%s)", KillWaitDelay, KillGrace)
	}
}
