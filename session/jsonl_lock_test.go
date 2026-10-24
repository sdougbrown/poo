package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The environment variables that tell the test binary to act as a
// helper process.
const (
	// helperMode names the helper's job.
	helperMode = "POO_SESSION_HELPER"
	// helperDir is the session directory the helper works in.
	helperDir = "POO_SESSION_DIR"
	// helperID is the session the helper works on.
	helperID = "POO_SESSION_ID"
	// helperSignal is the file the helper waits for.
	helperSignal = "POO_SESSION_SIGNAL"
)

// The helper's lines.
const (
	// helperReady is printed when the helper is set up.
	helperReady = "ready"
	// helperLoaded is printed when the helper loaded the session.
	helperLoaded = "loaded"
	// helperGone is printed when the helper could not load the
	// session.
	helperGone = "gone"
)

// TestHelperProcess is the entry point of a helper process.
//
// The parent starts the test binary with the helper variable set, and
// the helper does what the variable says. The helper prints "ready"
// when it is set up, and the parent reads that line before it goes on.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperMode)
	if mode == "" {
		return
	}
	os.Exit(runHelper(mode))
}

// runHelper does what the helper mode says.
//
// The helper holds the session's lock and waits to be killed, so the
// parent can see what a lock does across a process boundary.
func runHelper(mode string) int {
	dir := os.Getenv(helperDir)
	id := os.Getenv(helperID)
	store, err := NewStore(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "poo session helper:", err)
		return 1
	}
	ctx := context.Background()
	switch mode {
	case "hold":
		if _, err := store.Load(ctx, id); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
	case "job-started":
		if _, err := store.Create(ctx, createdRecord(id)); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		if err := store.Append(ctx, jobStarted("j1", "r1", "bash", "r1")); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
	case "job-ended":
		if _, err := store.Create(ctx, createdRecord(id)); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		if err := store.Append(ctx, jobStarted("j1", "r1", "bash", "r1")); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		if err := store.Append(ctx, jobEnded("j1", "r1", "bash", "r1", "s1-ji1", "the job's result")); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
	case "job-delivered":
		if _, err := store.Create(ctx, createdRecord(id)); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		if err := store.Append(ctx, jobStarted("j1", "r1", "bash", "r1")); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		if err := store.Append(ctx, jobEnded("j1", "r1", "bash", "r1", "s1-ji1", "the job's result")); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		if err := store.Append(ctx, inputDelivered("s1-ji1", poo.InputModeJob)); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
	case "load-race":
		if _, err := store.Load(ctx, id); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		fmt.Println(helperReady)
		waitForFile(os.Getenv(helperSignal))
		if _, err := store.Load(ctx, id); err != nil {
			fmt.Println(helperGone)
			return 0
		}
		fmt.Println(helperLoaded)
		return 0
	case "load-loop":
		// The helper loads and releases the session again and again,
		// so a prune that runs at the same time either skips the
		// session or deletes it between two loads.
		for i := 0; i < 2000; i++ {
			loaded, err := store.Load(ctx, id)
			if err != nil {
				if errors.Is(err, ErrSessionNotFound) {
					fmt.Println(helperGone)
					return 0
				}
				fmt.Fprintln(os.Stderr, "poo session helper: loading:", err)
				return 1
			}
			if len(loaded.Records) == 0 {
				fmt.Fprintln(os.Stderr, "poo session helper: the load holds no records")
				return 1
			}
			if err := store.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "poo session helper: closing:", err)
				return 1
			}
		}
		fmt.Println(helperLoaded)
		return 0
	case "run":
		// The helper creates a session, runs a turn and stops in the
		// middle of a tool call. The call never finishes, so the log
		// holds the call and no result.
		if err := runBlockedTurn(ctx, store, id); err != nil {
			fmt.Fprintln(os.Stderr, "poo session helper:", err)
			return 1
		}
		fmt.Println(helperReady)
		for {
			time.Sleep(time.Hour)
		}
	default:
		fmt.Fprintln(os.Stderr, "poo session helper: unknown mode", mode)
		return 1
	}
	fmt.Println(helperReady)
	// The helper waits to be killed: it holds the session's lock until
	// the process dies.
	for {
		time.Sleep(time.Hour)
	}
}

// waitForFile waits until the path exists.
func waitForFile(path string) {
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// startHelper starts a helper process and waits until it is ready.
//
// The helper's output is read in the background, so the parent does not
// block on a full pipe.
func startHelper(t *testing.T, mode, dir, id string) (*exec.Cmd, *bufio.Reader) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(),
		helperMode+"="+mode,
		helperDir+"="+dir,
		helperID+"="+id,
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("opening the helper's output: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the helper: %v", err)
	}
	reader := bufio.NewReader(out)
	line, err := reader.ReadString('\n')
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("reading the helper's readiness: %v", err)
	}
	if strings.TrimSpace(line) != helperReady {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("the helper said %q, want %q", line, helperReady)
	}
	return cmd, reader
}

// killHelper kills the helper process and waits for it.
//
// The helper's pid is the one the parent started: the parent kills only
// what it started.
func killHelper(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("killing the helper %d: %v", pid, err)
	}
	_ = cmd.Wait()
}

// TestASecondProcessIsRefused covers the authority: a session another
// process holds is refused.
func TestASecondProcessIsRefused(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	helper, _ := startHelper(t, "hold", dir, "s1")
	defer killHelper(t, helper)

	other, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer other.Close()
	_, err = other.Load(ctx, "s1")
	if err == nil {
		t.Fatal("the load succeeded, want the session locked")
	}
	if !errors.Is(err, ErrSessionLocked) {
		t.Fatalf("the load returned %v, want the session locked", err)
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeSessionLocked {
		t.Errorf("the failure's code is %q, want %q", code, CodeSessionLocked)
	}
}

// TestTheLockIsReleasedWhenTheHolderDies covers the kernel's part of
// the authority: a process that dies leaves the lock free.
func TestTheLockIsReleasedWhenTheHolderDies(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	helper, _ := startHelper(t, "hold", dir, "s1")
	killHelper(t, helper)

	other, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer other.Close()
	if _, err := other.Load(ctx, "s1"); err != nil {
		t.Fatalf("loading the session: %v", err)
	}
}

// TestTheLockIsReleasedAfterAFailedLoad covers the authority's promise:
// a lock that was taken for a load that failed is given up.
func TestTheLockIsReleasedAfterAFailedLoad(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	// A log that is not a session: the load fails.
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte("not a session\n"), 0o600); err != nil {
		t.Fatalf("writing the log: %v", err)
	}
	if _, err := store.Load(ctx, "s1"); err == nil {
		t.Fatal("the load succeeded, want a failure")
	}
	locked, err := Locked(LockPath(dir, "s1"))
	if err != nil {
		t.Fatalf("checking the lock: %v", err)
	}
	if locked {
		t.Error("the session is still locked, want the lock released")
	}
	// The second load fails on the log, not on the lock.
	if _, err := store.Load(ctx, "s1"); err == nil {
		t.Fatal("the second load succeeded, want a failure")
	} else if errors.Is(err, ErrSessionLocked) {
		t.Errorf("the second load returned %v, want the log's failure", err)
	}
}

// TestAJobThatWasKilledIsRecovered covers the job boundaries: a
// process that was killed after the job started leaves a job that is
// ended and queued again.
func TestAJobThatWasKilledIsRecovered(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	helper, _ := startHelper(t, "job-started", dir, "s1")
	killHelper(t, helper)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	ended, ok := loaded.Records[len(loaded.Records)-1].(*poo.JobEnded)
	if !ok {
		t.Fatalf("the last record is %T, want a job's end", loaded.Records[len(loaded.Records)-1])
	}
	if ended.Outcome != poo.OutcomeInterrupted {
		t.Errorf("the job's outcome is %q, want %q", ended.Outcome, poo.OutcomeInterrupted)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("the load queues %d job results, want 1", len(loaded.Jobs))
	}
}

// TestAJobThatEndedIsQueuedAgainAfterAKill covers the boundary after a
// job's end and before its delivery: the recorded payload is queued
// again, and the job is not run again.
func TestAJobThatEndedIsQueuedAgainAfterAKill(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	helper, _ := startHelper(t, "job-ended", dir, "s1")
	killHelper(t, helper)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("the load queues %d job results, want 1", len(loaded.Jobs))
	}
	queued := loaded.Jobs[0]
	if queued.Text != "the job's result" {
		t.Errorf("the queued result's text is %q, want the recorded text", queued.Text)
	}
	if queued.Outcome != poo.OutcomeOK {
		t.Errorf("the queued result's outcome is %q, want %q", queued.Outcome, poo.OutcomeOK)
	}
	// The job was not run again: the recovery did not end it a second
	// time.
	ends := 0
	for _, rec := range loaded.Records {
		if _, ok := rec.(*poo.JobEnded); ok {
			ends++
		}
	}
	if ends != 1 {
		t.Errorf("the session holds %d job ends, want 1", ends)
	}
}

// TestADeliveredJobIsNotQueuedAgainAfterAKill covers the boundary after
// the delivery: there is nothing to deliver twice.
func TestADeliveredJobIsNotQueuedAgainAfterAKill(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	helper, _ := startHelper(t, "job-delivered", dir, "s1")
	killHelper(t, helper)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if len(loaded.Jobs) != 0 {
		t.Errorf("the load queues %d job results, want none", len(loaded.Jobs))
	}
}

// TestARepeatedRecoveryAddsNothing covers the recovery's idempotence
// after a crash: the second load adds no record.
func TestARepeatedRecoveryAddsNothing(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	helper, _ := startHelper(t, "job-started", dir, "s1")
	killHelper(t, helper)

	first, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	second, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session again: %v", err)
	}
	if len(second.Records) != len(first.Records) {
		t.Errorf("the second load holds %d records, want %d", len(second.Records), len(first.Records))
	}
}

// TestALoadLoopRacingAPrune covers the race the serial test cannot: a
// helper loads and releases the same session while the parent prunes it,
// so a load either sees the whole session or gets no such session.
func TestALoadLoopRacingAPrune(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	sessionAt(t, dir, "s1", pruneNow.Add(-48*time.Hour))

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(),
		helperMode+"=load-loop",
		helperDir+"="+dir,
		helperID+"=s1",
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("opening the helper's output: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the helper: %v", err)
	}

	stop := make(chan struct{})
	pruned := make(chan struct{})
	go func() {
		defer close(pruned)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow}); err != nil {
				t.Errorf("pruning: %v", err)
				return
			}
		}
	}()
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != helperLoaded && line != helperGone {
			t.Errorf("the helper said %q, want %q or %q", line, helperLoaded, helperGone)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading the helper's output: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("waiting for the helper: %v", err)
	}
	close(stop)
	<-pruned
}

// TestALoadRacingAPrune covers the two outcomes of a load that races a
// prune: the load sees the whole session, or the session is gone.
//
// The helper holds the session's lock while the parent prunes: the
// prune skips the session, and the helper's second load sees it whole.
func TestALoadRacingAPrune(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	sessionAt(t, dir, "s1", pruneNow.Add(-48*time.Hour))

	signal := filepath.Join(t.TempDir(), "go")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(),
		helperMode+"=load-race",
		helperDir+"="+dir,
		helperID+"=s1",
		helperSignal+"="+signal,
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("opening the helper's output: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the helper: %v", err)
	}
	defer killHelper(t, cmd)
	reader := bufio.NewReader(out)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the helper's readiness: %v", err)
	}
	if strings.TrimSpace(line) != helperReady {
		t.Fatalf("the helper said %q, want %q", line, helperReady)
	}

	// The prune runs while the helper holds the lock: the session is
	// skipped, and it is not deleted.
	result, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("the prune deleted %v, want nothing", result.Deleted)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("the prune skipped %v, want the locked session", result.Skipped)
	}

	// The helper loads the session again: it is whole.
	if err := os.WriteFile(signal, nil, 0o600); err != nil {
		t.Fatalf("signalling the helper: %v", err)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the helper's answer: %v", err)
	}
	if strings.TrimSpace(line) != helperLoaded {
		t.Fatalf("the helper said %q, want %q", line, helperLoaded)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("waiting for the helper: %v", err)
	}

	// With the lock free, the prune deletes the session, and the load
	// finds nothing.
	if _, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow}); err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if _, err := store.Load(ctx, "s1"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("the load returned %v, want no such session", err)
	}
}
