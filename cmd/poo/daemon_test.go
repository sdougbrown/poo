package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The daemon hosts a long-lived feed-driven session: it starts the
// session's feeds, and waits for a signal or a lost lease without
// reading stdin. The tests spawn the binary, drive its feeds, and
// verify the session's records and its exit code.

// daemonHandle is a daemon process the test drives.
type daemonHandle struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin *os.File
	mu    sync.Mutex
	lines []string
}

// startDaemon starts the daemon with the given arguments and returns
// its handle. Its stdin is /dev/null, so a daemon that reads it blocks
// or ends, and the test proves it does neither.
func startDaemon(t *testing.T, args ...string) *daemonHandle {
	t.Helper()
	binary := buildTheBinary(t, nil)
	h := &daemonHandle{t: t}
	h.cmd = exec.Command(binary, args...)
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening /dev/null: %v", err)
	}
	h.stdin = null
	h.cmd.Stdin = null
	h.cmd.Stdout = io.Discard
	stderrPipe, err := h.cmd.StderrPipe()
	if err != nil {
		t.Fatalf("opening the daemon's stderr: %v", err)
	}
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			h.mu.Lock()
			h.lines = append(h.lines, scanner.Text())
			h.mu.Unlock()
		}
	}()
	if err := h.cmd.Start(); err != nil {
		t.Fatalf("starting the daemon: %v", err)
	}
	t.Cleanup(func() {
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		_ = h.cmd.Wait()
		_ = h.stdin.Close()
	})
	return h
}

// waitLine waits for a line on the daemon's stderr.
func (h *daemonHandle) waitLine(needle string) string {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, line := range h.lines {
			if strings.Contains(line, needle) {
				h.mu.Unlock()
				return line
			}
		}
		h.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatalf("the daemon's stderr did not say %q in time: %v", needle, h.allLines())
	return ""
}

// allLines returns the daemon's stderr lines, in order.
func (h *daemonHandle) allLines() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.lines))
	copy(out, h.lines)
	return out
}

// waitExit waits for the daemon to exit and returns its exit code.
func (h *daemonHandle) waitExit() int {
	h.t.Helper()
	done := make(chan error, 1)
	go func() { done <- h.cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		h.t.Fatalf("waiting for the daemon: %v", err)
	case <-time.After(30 * time.Second):
		h.t.Fatal("the daemon did not exit in time")
	}
	return 0
}

// daemonSettings writes the settings a daemon test runs under: the
// endpoint, the session's directory, the feed manifest, and a tiny
// lease TTL, so a dead owner is taken over in about a second.
func daemonSettings(t *testing.T, dir, endpointURL, manifestDir string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.json")
	body := `{"version":1,
		"endpoints":{"test":{"endpoint":"` + endpointURL + `/v1"}},
		"models":{"test-model":{"endpoint":"test","model_id":"test-model","api":"chat_completions","tools_mode":"native","ops":["chat"],"context_limit":32768}},
		"roles":{"chat":"test-model"},
		"session":{"durable":{"lease_ttl_s":1}},
		"feeds":{"paths":["` + manifestDir + `"],"defaults":false}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// feedManifest writes a feed manifest whose command waits for a trigger
// file and then emits a line.
func feedManifest(t *testing.T, dir, trigger string) string {
	t.Helper()
	path := filepath.Join(dir, "feed.json")
	body := `{"version":1,"feeds":{"test-feed":{"description":"A test feed.","command":["sh","-c","while [ ! -f ` + trigger + ` ]; do sleep 0.1; done; echo '{\"text\":\"the feed line\"}'"],"on_idle":"wake"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the feed manifest: %v", err)
	}
	return path
}

// sessionLog returns the path of a session's log.
func sessionLog(dir, sessionID string) string {
	return filepath.Join(dir, "sessions", sessionID+".jsonl")
}

// leasePath returns the path of a session's lease file.
func leasePath(dir, sessionID string) string {
	return filepath.Join(dir, "sessions", sessionID+".lease")
}

// waitRecord waits for a record name in a session's log.
func waitRecord(t *testing.T, path, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		recs := readLog(t, path)
		for _, rec := range recs {
			if rec["record"] == name {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the log did not hold %q in time: %v", name, names(readLog(t, path)))
}

// waitCounter waits for a counter file to hold a value.
func waitCounter(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("the counter is %q, want %q, in time", string(data), want)
}

// TestTheDaemonStaysAliveOnAFeed covers the headless life: a daemon
// with /dev/null as its stdin stays alive on a feed, wakes when the
// feed delivers a line, settles the wake, and keeps running.
func TestTheDaemonStaysAliveOnAFeed(t *testing.T) {
	endpoint := newFakeEndpoint(t, doneAnswer())
	dir := t.TempDir()
	manifestDir := t.TempDir()
	trigger := filepath.Join(dir, "trigger")
	feedManifest(t, manifestDir, trigger)
	settings := daemonSettings(t, dir, endpoint.URL, manifestDir)

	daemon := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", filepath.Join(dir, "sessions"),
	)
	daemon.waitLine("poo daemon: started")
	daemon.waitLine("poo daemon: session ")

	// The feed delivers a line, and the wake settles.
	if err := os.WriteFile(trigger, []byte("go"), 0o600); err != nil {
		t.Fatalf("writing the trigger: %v", err)
	}
	// The session id is on the second line.
	var sessionID string
	for _, line := range daemon.allLines() {
		if id, ok := strings.CutPrefix(line, "poo daemon: session "); ok {
			sessionID = id
		}
	}
	if sessionID == "" {
		t.Fatal("the daemon did not print its session id")
	}
	waitRecord(t, sessionLog(dir, sessionID), "run.settled")

	// The daemon is still alive: it has not exited.
	time.Sleep(500 * time.Millisecond)
	if err := daemon.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the daemon exited before the check: %v", err)
	}
	if err := daemon.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling the daemon: %v", err)
	}
	daemon.waitLine("poo daemon: shutdown")
	if code := daemon.waitExit(); code != 0 {
		t.Errorf("the daemon exited %d, want 0", code)
	}
}

// TestTheDaemonEphemeralFlagOpensAnEphemeralSession covers the mode
// resolution: --ephemeral wins over the settings and the default, and
// the session opens in the memory store, which writes no file.
func TestTheDaemonEphemeralFlagOpensAnEphemeralSession(t *testing.T) {
	endpoint := newFakeEndpoint(t, doneAnswer())
	dir := t.TempDir()
	manifestDir := t.TempDir()
	trigger := filepath.Join(dir, "trigger")
	feedManifest(t, manifestDir, trigger)
	settings := daemonSettings(t, dir, endpoint.URL, manifestDir)
	sessionDir := filepath.Join(dir, "sessions")

	daemon := startDaemon(t,
		"daemon",
		"--ephemeral",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
	)
	daemon.waitLine("poo daemon: started")
	// The store is the memory: the session wrote no file.
	entries, err := os.ReadDir(sessionDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading the session dir: %v", err)
	}
	if err == nil {
		for _, entry := range entries {
			t.Errorf("the ephemeral session wrote %s to disk", entry.Name())
		}
	}
	_ = daemon.cmd.Process.Kill()
	daemon.waitExit()
}

// TestTheDaemonRefusesABadSessionID covers the refused argument: a
// session id that names outside the session directory is refused with
// the usage exit code.
func TestTheDaemonRefusesABadSessionID(t *testing.T) {
	endpoint := newFakeEndpoint(t, doneAnswer())
	dir := t.TempDir()
	manifestDir := t.TempDir()
	trigger := filepath.Join(dir, "trigger")
	feedManifest(t, manifestDir, trigger)
	settings := daemonSettings(t, dir, endpoint.URL, manifestDir)

	daemon := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", filepath.Join(dir, "sessions"),
		"--session", "../outside",
	)
	if code := daemon.waitExit(); code != 2 {
		t.Errorf("the daemon exited %d, want 2: %v", code, daemon.allLines())
	}
}

// TestTheDaemonTakesOverACrashedSession covers the crash and the
// restart: a daemon that dies mid-run leaves its lease to expire, and
// a restart with --session takes it over and completes the interrupted
// wake through the auto-resume.
func TestTheDaemonTakesOverACrashedSession(t *testing.T) {
	// The model calls a blocking tool, then answers.
	toolCall := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"sleep 30\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	answer := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpointSequence(t, []string{toolCall, answer})
	dir := t.TempDir()
	manifestDir := t.TempDir()
	trigger := filepath.Join(dir, "trigger")
	feedManifest(t, manifestDir, trigger)
	settings := daemonSettings(t, dir, endpoint.URL, manifestDir)
	sessionDir := filepath.Join(dir, "sessions")
	sessionID := "crashed-session"

	// The first daemon drives the run mid-flight, and dies.
	daemon := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"--session", sessionID,
	)
	daemon.waitLine("poo daemon: started")
	if err := os.WriteFile(trigger, []byte("go"), 0o600); err != nil {
		t.Fatalf("writing the trigger: %v", err)
	}
	// The run is mid-flight: the blocking tool has started.
	waitRecord(t, sessionLog(dir, sessionID), "tool.started")
	// The daemon dies mid-run: the lease is not released.
	_ = daemon.cmd.Process.Kill()
	daemon.waitExit()

	// The lease expires in about a second, and the restart takes it
	// over.
	time.Sleep(2 * time.Second)
	restart := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"--session", sessionID,
	)
	defer func() { _ = restart.cmd.Process.Kill() }()
	restart.waitLine("poo daemon: started")
	// The auto-resume completes the interrupted wake.
	waitRecord(t, sessionLog(dir, sessionID), "run.settled")
	_ = restart.cmd.Process.Kill()
	restart.waitExit()
}

// TestTheDaemonDoesNotReplayASideEffect covers the no-replay rule: a
// side-effecting tool that had begun is not re-executed on a restart;
// the model sees the interrupted result instead, and the effect ran
// once.
func TestTheDaemonDoesNotReplayASideEffect(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	// The model calls a side-effecting tool that increments a counter
	// and then blocks, then answers.
	toolCall := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"n=$(cat ` + counter + ` 2>/dev/null || echo 0); n=$((n+1)); echo $n > ` + counter + `; sleep 30\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	answer := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpointSequence(t, []string{toolCall, answer})
	manifestDir := t.TempDir()
	trigger := filepath.Join(dir, "trigger")
	feedManifest(t, manifestDir, trigger)
	settings := daemonSettings(t, dir, endpoint.URL, manifestDir)
	sessionDir := filepath.Join(dir, "sessions")
	sessionID := "side-effect-session"

	// The first daemon drives the run mid-flight, and dies.
	daemon := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"--session", sessionID,
	)
	daemon.waitLine("poo daemon: started")
	if err := os.WriteFile(trigger, []byte("go"), 0o600); err != nil {
		t.Fatalf("writing the trigger: %v", err)
	}
	waitRecord(t, sessionLog(dir, sessionID), "tool.started")
	// The side effect ran: the counter is 1.
	waitCounter(t, counter, "1")
	_ = daemon.cmd.Process.Kill()
	daemon.waitExit()

	// The effect ran once: the counter is 1.
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("reading the counter: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "1" {
		t.Fatalf("the counter is %q, want 1: the side effect ran more than once", got)
	}

	// The lease expires, and the restart takes it over.
	time.Sleep(2 * time.Second)
	restart := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"--session", sessionID,
	)
	defer func() { _ = restart.cmd.Process.Kill() }()
	restart.waitLine("poo daemon: started")
	waitRecord(t, sessionLog(dir, sessionID), "run.settled")
	_ = restart.cmd.Process.Kill()
	restart.waitExit()

	// The effect ran once, not twice: the counter is still 1.
	data, err = os.ReadFile(counter)
	if err != nil {
		t.Fatalf("reading the counter: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "1" {
		t.Errorf("the counter is %q, want 1: the side effect was replayed", got)
	}
}

// TestTheDaemonReleasesTheLeaseOnShutdown covers the clean shutdown:
// a SIGTERM releases the lease, and the daemon exits 0.
func TestTheDaemonReleasesTheLeaseOnShutdown(t *testing.T) {
	endpoint := newFakeEndpoint(t, doneAnswer())
	dir := t.TempDir()
	manifestDir := t.TempDir()
	trigger := filepath.Join(dir, "trigger")
	feedManifest(t, manifestDir, trigger)
	settings := daemonSettings(t, dir, endpoint.URL, manifestDir)
	sessionDir := filepath.Join(dir, "sessions")
	sessionID := "shutdown-session"

	daemon := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"--session", sessionID,
	)
	daemon.waitLine("poo daemon: started")
	// The lease is held.
	if _, err := os.Stat(leasePath(dir, sessionID)); err != nil {
		t.Fatalf("the lease is not held: %v", err)
	}
	if err := daemon.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling the daemon: %v", err)
	}
	daemon.waitLine("poo daemon: shutdown")
	if code := daemon.waitExit(); code != 0 {
		t.Errorf("the daemon exited %d, want 0", code)
	}
	// The lease is released.
	if _, err := os.Stat(leasePath(dir, sessionID)); !os.IsNotExist(err) {
		t.Errorf("the lease is still held after the shutdown: %v", err)
	}
}

// daemonSqliteSettings writes the settings a daemon test runs under with
// the durable store's sqlite backend: the endpoint, the session's
// directory, the feed manifest, and a tiny lease TTL, so a dead owner is
// taken over in about a second.
func daemonSqliteSettings(t *testing.T, dir, endpointURL, manifestDir string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.json")
	body := `{"version":1,
		"endpoints":{"test":{"endpoint":"` + endpointURL + `/v1"}},
		"models":{"test-model":{"endpoint":"test","model_id":"test-model","api":"chat_completions","tools_mode":"native","ops":["chat"],"context_limit":32768}},
		"roles":{"chat":"test-model"},
		"session":{"durable":{"lease_ttl_s":1,"store":"sqlite"}},
		"feeds":{"paths":["` + manifestDir + `"],"defaults":false}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestTheDaemonResumesADurableSqliteSession covers the existence probe: a
// sqlite session keeps no log, so the daemon finds the session it created
// through the store on the next start and resumes it, instead of taking
// the create path and failing that the session already exists.
func TestTheDaemonResumesADurableSqliteSession(t *testing.T) {
	endpoint := newFakeEndpoint(t, doneAnswer())
	dir := t.TempDir()
	manifestDir := t.TempDir()
	settings := daemonSqliteSettings(t, dir, endpoint.URL, manifestDir)
	sessionDir := filepath.Join(dir, "sessions")
	sessionID := "durable-session"

	first := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"--session", sessionID,
	)
	first.waitLine("poo daemon: started")
	first.waitLine("poo daemon: session " + sessionID)
	// The session lives in the sqlite database, and no log holds it.
	if _, err := os.Stat(filepath.Join(sessionDir, sessionID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("the sqlite session wrote a log: %v", err)
	}
	// The daemon dies without releasing its lease; the restart takes the
	// session over once the lease expires.
	_ = first.cmd.Process.Kill()
	first.waitExit()
	time.Sleep(2 * time.Second)

	restart := startDaemon(t,
		"daemon",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"--session", sessionID,
	)
	defer func() { _ = restart.cmd.Process.Kill() }()
	restart.waitLine("poo daemon: started")
	restart.waitLine("poo daemon: session " + sessionID)
	_ = restart.cmd.Process.Kill()
	restart.waitExit()
}
