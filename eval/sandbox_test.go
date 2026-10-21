package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheBwrapSandboxHidesEverythingButTheWorkdir(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap is not installed")
	}
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "answer.txt")
	if err := os.WriteFile(secret, []byte("the-answer"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	// The fake run writes its log as the real one does, then records what
	// it could see outside the workdir.
	poo := fakePoo(t, strings.Replace(logWritingPoo, "exit 0",
		`{ cat `+secret+` 2>&1; ls `+taskRoot+` 2>&1; echo "home=$HOME pwd=$(pwd)"; } > "$(dirname "$log")/seen.txt"
exit 0`, 1))

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(t.TempDir(), "work"),
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
	if !strings.Contains(string(seen), "home=/work pwd=/work") {
		t.Errorf("the run saw %q, want its home and working directory to be /work", seen)
	}
	if report.Sandbox != SandboxBwrap {
		t.Errorf("the report's sandbox is %q, want %q", report.Sandbox, SandboxBwrap)
	}
}
