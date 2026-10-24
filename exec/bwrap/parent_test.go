package bwrap_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
)

// The variables the test binary reads to act as the process that owns a
// sandbox.
const (
	parentHelperEnv = "POO_BWRAP_PARENT_HELPER"
	parentDirEnv    = "POO_BWRAP_PARENT_DIR"
)

// TestTheSandboxOwner starts one sandbox and dies the way a killed poo
// does, without running a teardown.
//
// It is not a test of its own: another test runs this binary with the
// environment set, so the sandbox is abandoned the moment it exists,
// which is the moment a sandbox has not yet arranged to die with its
// parent.
func TestTheSandboxOwner(t *testing.T) {
	if os.Getenv(parentHelperEnv) == "" {
		t.Skip("a helper for TestASandboxDoesNotOutliveTheProcessThatStartedIt")
	}
	dir := os.Getenv(parentDirEnv)
	exec, err := bwrap.New(bwrap.Options{})
	if err != nil {
		fmt.Println("unavailable:", err)
		os.Exit(1)
	}
	if _, err := exec.Start(context.Background(), bash.Command{
		Argv: []string{"/bin/bash", "-c", "sleep 300"},
		Dir:  dir,
	}, io.Discard); err != nil {
		fmt.Println("start failed:", err)
		os.Exit(1)
	}
	fmt.Println("started")
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	for {
		time.Sleep(time.Hour)
	}
}

// TestASandboxDoesNotOutliveTheProcessThatStartedIt covers the owner
// that dies without a teardown: the sandbox is gone with it, and no
// bubblewrap process with the sandbox's own directory survives.
func TestASandboxDoesNotOutliveTheProcessThatStartedIt(t *testing.T) {
	if _, err := newReviewExecutor(t); err != nil {
		t.Skipf("skip: %v", err)
	}
	dir := t.TempDir()
	owner := exec.Command(os.Args[0], "-test.run=^TestTheSandboxOwner$", "-test.v")
	owner.Env = append(os.Environ(), parentHelperEnv+"=1", parentDirEnv+"="+dir)
	output, err := owner.StdoutPipe()
	if err != nil {
		t.Fatalf("reading the owner's output: %v", err)
	}
	owner.Stderr = owner.Stdout
	if err := owner.Start(); err != nil {
		t.Fatalf("starting the sandbox's owner: %v", err)
	}
	defer func() {
		_ = owner.Process.Kill()
		_ = owner.Wait()
	}()

	lines := bufio.NewReader(output)
	for {
		line, err := lines.ReadString('\n')
		if err != nil {
			t.Fatalf("the sandbox's owner never reported that it started: %v", err)
		}
		switch {
		case strings.Contains(line, "started"):
			goto started
		case strings.Contains(line, "unavailable:"), strings.Contains(line, "start failed:"):
			t.Skipf("the sandbox is unavailable: %s", strings.TrimSpace(line))
		}
	}
started:
	// The owner kills itself as soon as the sandbox exists, so nothing
	// gets to tear the sandbox down.
	_ = owner.Wait()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pids := sandboxPIDs(t, dir); len(pids) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	left := sandboxPIDs(t, dir)
	// Name the survivor before the sweep runs: the sweep's own kills
	// would leave zombies behind and hide what was alive.
	described := describe(left)
	// A survivor is the test's own, because it names the test's
	// directory. The sweep runs until the directory names nothing, so a
	// bubblewrap that is still starting up cannot be left behind either.
	for i := 0; i < 100; i++ {
		pids := sandboxPIDs(t, dir)
		if len(pids) == 0 {
			break
		}
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the sandbox outlived the process that started it: %s", described)
}

// describe names each surviving sandbox process: its state, its parent
// and its command line, so a failure says what kind of leftover it
// was.
func describe(pids []int) string {
	var b strings.Builder
	for _, pid := range pids {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			fmt.Fprintf(&b, "pid %d (stat: %v)", pid, err)
			continue
		}
		// The command name can hold a space inside its parentheses, so
		// the fields after it are found from the end of it: the state
		// and the parent process are the first two.
		open := strings.LastIndex(string(raw), ")")
		rest := strings.Fields(string(raw[open+2:]))
		state, ppid := rest[0], rest[1]
		cmd, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		fmt.Fprintf(&b, "pid %d state=%s ppid=%s argv=%q; ", pid, state, ppid, strings.ReplaceAll(strings.TrimRight(string(cmd), "\x00"), "\x00", " "))
	}
	return strings.TrimRight(b.String(), "; ")
}

// sandboxPIDs returns the pids of the sandbox processes that hold a
// directory.
//
// The directory names the sandbox in bubblewrap's own arguments, so a
// process that carries it was started for this directory and no other.
func sandboxPIDs(t *testing.T, dir string) []int {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("reading /proc: %v", err)
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if len(args) == 0 || !strings.Contains(args[0], "bwrap") {
			continue
		}
		if strings.Contains(string(raw), dir) {
			pids = append(pids, pid)
		}
	}
	return pids
}
