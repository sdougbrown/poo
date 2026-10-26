package bwrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
)

// reviewTimeoutCall runs a command through the tool with a timeout and
// returns the observation.
func reviewTimeoutCall(t *testing.T, executor *bwrap.Executor, command, dir string, timeout time.Duration) (map[string]any, time.Duration) {
	t.Helper()
	tool := &bash.Tool{Timeout: timeout, Executor: executor}
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	start := time.Now()
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	var obs map[string]any
	if err := json.Unmarshal(res.Structured, &obs); err != nil {
		t.Fatalf("decoding %q: %v", res.Structured, err)
	}
	return obs, elapsed
}

// TestTheSandboxIsGoneWhenWaitReturns checks the moment the call
// returns: the plan says Wait returns after everything the command
// started has exited.
func TestTheSandboxIsGoneWhenWaitReturns(t *testing.T) {
	executor, err := bwrap.New(bwrap.Options{})
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	dir := t.TempDir()
	obs, elapsed := reviewTimeoutCall(t, executor,
		"readlink /proc/self/ns/pid > ns.txt; sleep 300 & echo $! > pid.txt; echo started; wait",
		dir, 300*time.Millisecond)
	t.Logf("elapsed %v, observation %v", elapsed, obs)

	raw, err := os.ReadFile(filepath.Join(dir, "ns.txt"))
	if err != nil {
		t.Fatalf("reading the namespace: %v", err)
	}
	ns := strings.TrimSpace(string(raw))
	if ns == "" {
		t.Fatal("the command wrote no namespace")
	}
	// No polling: the question is what is still there when the call
	// returns.
	if pids := reviewPIDsInNamespace(ns); len(pids) > 0 {
		t.Errorf("the call returned %v after it started and left %v in %s", elapsed, pids, ns)
	}
}

// reviewPIDsInNamespace returns the pids whose pid namespace is ns.
func reviewPIDsInNamespace(ns string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		link, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
		if err != nil {
			continue
		}
		if link == ns {
			pids = append(pids, pid)
		}
	}
	return pids
}
