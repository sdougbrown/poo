package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// PR_SET_CHILD_SUBREAPER makes orphans of this process's descendants
// reparent to it, so a test can reap the exact fixture it started.
const prSetChildSubreaper = 36

// TestARunReapsTheCLIsChildren covers the end-to-end lifetime: a
// registered CLI that leaves a child in its own process group leaves
// nothing behind when the run returns, in either mode of the one-shot's
// job handling.
func TestARunReapsTheCLIsChildren(t *testing.T) {
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0, 0, 0, 0); errno != 0 {
		t.Fatalf("becoming a subreaper: %v", errno)
	}
	defer syscall.Syscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 0, 0, 0, 0, 0)

	for _, noWait := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-wait-jobs=%v", noWait), func(t *testing.T) {
			dir := t.TempDir()
			pidPath := filepath.Join(dir, "pid")
			manifest := map[string]any{"version": 1, "tools": map[string]any{"probe": map[string]any{
				"description": "A CLI with a same-group child",
				"command": []string{"/bin/sh", "-c",
					`sleep 30 </dev/null >/dev/null 2>&1 & echo $! > "$1"; printf parent-done`, "probe", pidPath},
				"inputSchema": map[string]any{
					"type":                 "object",
					"properties":           map[string]any{},
					"additionalProperties": false,
				},
			}}}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatalf("encoding the registry: %v", err)
			}
			registry := filepath.Join(dir, "registry.json")
			if err := os.WriteFile(registry, encoded, 0o600); err != nil {
				t.Fatalf("writing the registry: %v", err)
			}
			settings := filepath.Join(dir, "settings.yaml")
			document := fmt.Sprintf("version: 1\nsession:\n  mode: ephemeral\ncliregistry:\n  paths: [%q]\n", registry)
			if err := os.WriteFile(settings, []byte(document), 0o600); err != nil {
				t.Fatalf("writing the settings: %v", err)
			}

			call := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"call_cli","arguments":"{\"name\":\"probe\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
			answer := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
			endpoint := newFakeEndpointSequence(t, []string{call, answer})

			args := []string{
				"run",
				"--settings", settings,
				"--base-url", endpoint.URL + "/v1",
				"--model-id", "test-model",
				"--context-limit", "32768",
				"--working-dir", dir,
				"--permission-unmatched", "allow",
				"--log", filepath.Join(dir, "session.jsonl"),
			}
			if noWait {
				args = append(args, "--no-wait-jobs")
			}
			args = append(args, "run the fixture")
			code, stderr, _ := runCLIWithStdout(t, args...)
			if code != 0 {
				t.Fatalf("the run exited %d: %s", code, stderr)
			}

			data, err := os.ReadFile(pidPath)
			if err != nil {
				t.Fatalf("the CLI's child recorded no pid: %v", err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("decoding the pid: %v", err)
			}
			defer func() {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				var status syscall.WaitStatus
				_, _ = syscall.Wait4(pid, &status, 0, nil)
			}()
			if !reaped(pid, 5*time.Second) {
				t.Fatalf("the run returned and left the CLI's child (pid %d) running", pid)
			}
		})
	}
}

// reaped reports whether a process has exited and been reaped by this
// one within the deadline.
//
// The child is this process's own, because the test made itself a
// subreaper, so a successful wait is the proof that it exited: kill with
// signal 0 still succeeds for a zombie nothing has reaped.
func reaped(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		var status syscall.WaitStatus
		waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if err == nil && waited == pid {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
