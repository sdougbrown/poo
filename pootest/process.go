package pootest

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ReadPid reads the pid the file holds, when it holds one: a path that
// is not there yet, or holds something that is not a pid, reads as not
// ok, so a caller can poll a pid file a process is still writing.
func ReadPid(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, true
}

// FixturePid returns the pid a fixture's process wrote to its pid file,
// once it has written one.
func FixturePid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if pid, ok := ReadPid(path); ok {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("the fixture never wrote its pid to %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitNewPid waits until the pid file holds a process's pid other than
// the one it held, and returns the new one.
func WaitNewPid(t *testing.T, path string, old int) int {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if pid, ok := ReadPid(path); ok && pid != old {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("no process replaced the fixture's pid %d", old)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitGone waits until the process is fully gone — not a zombie either,
// but reaped, so nothing the session started survives it.
func WaitGone(t *testing.T, what string, pid int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the %s process %d is still there", what, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
