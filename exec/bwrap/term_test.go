package bwrap_test

import (
	"strings"
	"testing"
	"time"
)

// TestACommandThatIgnoresSigtermIsKilled covers the kill escalation: a
// command that ignores SIGTERM must still die, and must not hold the
// call for longer than the grace period.
func TestACommandThatIgnoresSigtermIsKilled(t *testing.T) {
	exec, err := newReviewExecutor(t)
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	dir := t.TempDir()
	obs, elapsed := reviewTimeoutCall(t, exec,
		"readlink /proc/self/ns/pid > ns.txt; trap '' TERM; sleep 300 & echo $! > pid.txt; echo started; wait",
		dir, 300*time.Millisecond)
	out, _ := obs["output"].(string)
	if !strings.Contains(out, "started") {
		t.Fatalf("the partial output is missing: %q", out)
	}
	if elapsed > 10*time.Second {
		t.Errorf("the call took %v, want no more than the grace period", elapsed)
	}
	t.Logf("elapsed %v, observation %v", elapsed, obs)
	ns := strings.TrimSpace(reviewReadFile(t, dir+"/ns.txt"))
	if pids := reviewPIDsInNamespace(ns); len(pids) > 0 {
		t.Errorf("the call returned %v after it started and left %v in %s", elapsed, pids, ns)
	}
}
