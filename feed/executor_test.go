package feed_test

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
)

// The fixture feeds are the scripts in testdata/bin: real commands that
// print the lines a feed prints, then wait to be stopped.
//
// The command is the script's absolute path, so the tests do not depend
// on the directory they run in.

// fixtureDir returns the directory the fixture feeds live in.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "bin"))
	if err != nil {
		t.Fatalf("resolving the fixtures: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the fixtures are not there: %v", err)
	}
	return dir
}

// script returns the absolute path of one fixture feed.
func script(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(fixtureDir(t), name)
}

// executorCase is one executor the runner's tests run the fixtures
// through.
type executorCase struct {
	// name is the executor's name.
	name string
	// executor runs the attempt.
	executor exec.Executor
	// local reports that the executor's pids are the host's, so a test
	// may look a process up by the pid the command recorded.
	local bool
}

// executorCases returns the executors the tests run against.
//
// The sandbox is required, not skipped: CI runs this package in the job
// that installs bubblewrap, and a case that quietly disappears there
// proves nothing about the sandbox. A host that cannot create one fails
// here, the way the cli-registry sandbox tests do.
func executorCases(t *testing.T) []executorCase {
	t.Helper()
	cases, err := executorCasesFor(fixtureDir(t), newSandbox)
	if err != nil {
		t.Fatalf("the bwrap executor is unavailable: %v", err)
	}
	return cases
}

// newSandbox builds the sandbox the feed tests run in.
//
// The sandbox sees the system directories and nothing else of the host,
// so the fixtures are mounted read-only beside them.
func newSandbox(fixtures string) (exec.Executor, error) {
	binds := append(append([]string{}, bwrap.DefaultROBinds...), fixtures)
	return bwrap.New(bwrap.Options{ROBinds: binds})
}

// executorCasesFor returns the executors the tests run against, with the
// sandbox the constructor builds. A sandbox that cannot be built is an
// error rather than a case left out.
func executorCasesFor(fixtures string, sandbox func(string) (exec.Executor, error)) ([]executorCase, error) {
	cases := []executorCase{{name: "local", executor: bash.NewLocal(), local: true}}
	executor, err := sandbox(fixtures)
	if err != nil {
		return cases, err
	}
	return append(cases, executorCase{name: "bwrap", executor: executor}), nil
}

// TestASandboxThatCannotStartFailsTheCases covers what CI relies on:
// the sandboxed case is required, so a bubblewrap that cannot start is
// the test's failure rather than a case that disappears.
func TestASandboxThatCannotStartFailsTheCases(t *testing.T) {
	errNoSandbox := errors.New("bubblewrap cannot create a sandbox here")
	cases, err := executorCasesFor(t.TempDir(), func(string) (exec.Executor, error) {
		return nil, errNoSandbox
	})
	if !errors.Is(err, errNoSandbox) {
		t.Fatalf("a sandbox that cannot start returned %d cases and %v, want the constructor's failure", len(cases), err)
	}
}

// envLookup returns a lookup over the environment, with extra entries
// ahead of it.
//
// The runner resolves a manifest's `env:NAME` references through it, so
// a test sees the same rules a session does.
func envLookup(extra ...string) func(string) (string, bool) {
	values := map[string]string{}
	for _, entry := range append(os.Environ(), extra...) {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[name] = value
	}
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

// logLines is a logger that keeps the lines it was given, for a test
// that reads what a command's stderr said.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

// logger returns the slog logger that records into the sink.
func (l *logLines) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(writerFunc(l.append), &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// append records one log record's message and attributes.
func (l *logLines) append(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, string(p))
	return len(p), nil
}

// all returns every line the log holds.
func (l *logLines) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// text returns the log's lines joined, for a test that searches them.
func (l *logLines) text() string {
	return strings.Join(l.all(), "")
}

// writerFunc is an io.Writer over a function.
type writerFunc func([]byte) (int, error)

// Write calls the function.
func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// waitFor waits for a condition, so a test does not sleep for a guess.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
