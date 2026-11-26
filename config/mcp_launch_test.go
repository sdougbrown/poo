package config_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// recordingLauncher is the launcher a test links when it wants to see
// what a session asked it to start: every launch is recorded with the
// session's own id and directory, and a named server may be made to
// fail.
type recordingLauncher struct {
	mu sync.Mutex
	// starts are the recorded launches, in order: one
	// server@session@directory per entry.
	starts []string
	// fails names the servers that do not start, by the error they
	// return.
	fails map[string]error
	// runnings are the handles the launcher handed out, in order.
	runnings []*stubRunning
}

// launch is the launcher's function.
func (l *recordingLauncher) launch(_ context.Context, values config.MCPLaunchValues) (config.MCPRunning, error) {
	l.mu.Lock()
	l.starts = append(l.starts, values.Server.Name+"@"+values.SessionID+"@"+values.WorkingDir)
	err := l.fails[values.Server.Name]
	if err == nil {
		running := &stubRunning{}
		l.runnings = append(l.runnings, running)
		l.mu.Unlock()
		return running, nil
	}
	l.mu.Unlock()
	return nil, err
}

// recorded returns the launches the launcher saw.
func (l *recordingLauncher) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.starts...)
}

// theSelectingSettings are the settings a session that selects a server
// runs under: a model, a store the session's records go to, and a
// profile that names the manifest's servers.
func theSelectingSettings(t *testing.T, path, profile, servers string) string {
	t.Helper()
	return theMCPSettings(path) + "session:\n  dir: " + t.TempDir() + "\nprofiles:\n  " + profile + ":\n    base: code\n    mcp_servers: [" + servers + "]\nprofile: " + profile + "\n"
}

// theCreatedRecord is the session header a created session runs from.
func theCreatedRecord(profile, workingDir string) *poo.SessionCreatedRecord {
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-11-26T12:00:00Z",
			WorkingDir: workingDir,
			Profile:    profile, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
}

// TestMCPNoSelectionStartsNoServer covers the rule the manifests
// answer to: loading declares servers, and nothing starts until a
// profile selects one — a session of a profile that names none
// launches nothing.
func TestMCPNoSelectionStartsNoServer(t *testing.T) {
	path := mcpManifest(t, map[string]any{"one": stdioServer()}, nil)
	document := theMCPSettings(path) + "session:\n  dir: " + t.TempDir() + "\nprofiles:\n  plain:\n    base: code\nprofile: plain\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	launcher := &recordingLauncher{}
	runtime := buildWith(t, config.BuildOptions{Settings: settings, MCPLauncher: launcher.launch, WorkingDir: t.TempDir()})
	defer runtime.Store.Close()

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("plain", t.TempDir()))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if starts := launcher.recorded(); len(starts) != 0 {
		t.Errorf("a session with no selection started %q, want nothing", starts)
	}
}

// TestMCPSelectionLaunchesWithTheSessionValues covers the launch a
// selection produces: a created session starts the servers its profile
// selected with its own id and directory, a resumed one starts them
// with the recorded ones, and closing either stops what it started.
func TestMCPSelectionLaunchesWithTheSessionValues(t *testing.T) {
	path := mcpManifest(t, map[string]any{"one": stdioServer()}, nil)
	settings, err := config.Parse([]byte(theSelectingSettings(t, path, "picky", "one")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	launcher := &recordingLauncher{}
	runtime := buildWith(t, config.BuildOptions{Settings: settings, MCPLauncher: launcher.launch, WorkingDir: t.TempDir()})
	defer runtime.Store.Close()

	workDir := t.TempDir()
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", workDir))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	starts := launcher.recorded()
	if len(starts) != 1 || starts[0] != "one@s1@"+workDir {
		t.Fatalf("the created session launched %q, want one@<the session's id and directory>", starts)
	}
	if closes := launcher.runnings[0].closes(); closes != 1 {
		t.Errorf("the session's server was closed %d times, want once", closes)
	}

	// The resumed session continues in the recorded directory, and its
	// servers start with the recorded values too.
	resumed, err := runtime.ResumeSession(context.Background(), runtime.Store, "s1")
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	if err := resumed.Session.Close(); err != nil {
		t.Fatalf("closing the resumed session: %v", err)
	}
	starts = launcher.recorded()
	if len(starts) != 2 || starts[1] != "one@s1@"+workDir {
		t.Fatalf("the resumed session launched %q, want the recorded id and directory", starts)
	}
	if closes := launcher.runnings[1].closes(); closes != 1 {
		t.Errorf("the resumed session's server was closed %d times, want once", closes)
	}
}

// TestMCPFailedLaunchClosesWhatItStartedAndNamesTheServer covers the
// failed launch: a session that cannot start one of its selected
// servers is never built, the servers that did start stop with the
// failure, and the failure names the server that did not start.
func TestMCPFailedLaunchClosesWhatItStartedAndNamesTheServer(t *testing.T) {
	path := mcpManifest(t, map[string]any{"one": stdioServer(), "two": stdioServer()}, nil)
	settings, err := config.Parse([]byte(theSelectingSettings(t, path, "picky", "one, two")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	failure := errors.New("the process refused")
	launcher := &recordingLauncher{fails: map[string]error{"two": failure}}
	runtime := buildWith(t, config.BuildOptions{Settings: settings, MCPLauncher: launcher.launch, WorkingDir: t.TempDir()})
	defer runtime.Store.Close()

	_, err = runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()))
	if err == nil {
		t.Fatal("the session was built with a server that did not start")
	}
	var settingsErr *config.Error
	if !errors.As(err, &settingsErr) {
		t.Fatalf("the failure is %T, want a settings error", err)
	}
	if settingsErr.Field != "mcp.paths" {
		t.Errorf("the failure is about %q, want mcp.paths", settingsErr.Field)
	}
	if !strings.Contains(settingsErr.Message, `"two"`) {
		t.Errorf("the failure says %q, want the server two named", settingsErr.Message)
	}
	if !errors.Is(settingsErr.Err, failure) {
		t.Errorf("the failure underneath is %v, want the launcher's own", settingsErr.Err)
	}
	starts := launcher.recorded()
	if len(starts) != 2 {
		t.Fatalf("the failed session launched %q, want both selected servers tried", starts)
	}
	// The server that started stopped with the failure: a session that
	// was never built owns no process.
	if closes := launcher.runnings[0].closes(); closes != 1 {
		t.Errorf("the first server was closed %d times, want once with the failure", closes)
	}
}

// childCloser closes a real child process the test started.
type childCloser struct {
	// kill stops the process and waits for it.
	kill func() error
}

// Close stops the child.
func (c childCloser) Close() error { return c.kill() }

// failingSessionStart is a session-start hook that always fails.
type failingSessionStart struct{}

// SessionStart fails.
func (failingSessionStart) SessionStart(poo.HookContext) (poo.StartDecision, error) {
	return poo.StartDecision{}, errors.New("the hook failed")
}

// TestMCPConstructionFailureLeavesNoChild covers the construction's
// rollback: a session that fails after it attached stops what it
// started, so a construction failure leaves no child process behind.
func TestMCPConstructionFailureLeavesNoChild(t *testing.T) {
	path := mcpManifest(t, map[string]any{"one": stdioServer()}, nil)
	settings, err := config.Parse([]byte(theSelectingSettings(t, path, "picky", "one")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	var pids []int
	launcher := func(context.Context, config.MCPLaunchValues) (config.MCPRunning, error) {
		// A real child, because the rollback's promise is about
		// processes: a fake handle cannot prove one is gone.
		child := exec.Command("sleep", "60")
		if err := child.Start(); err != nil {
			return nil, err
		}
		pids = append(pids, child.Process.Pid)
		return childCloser{kill: func() error {
			_ = child.Process.Kill()
			return child.Wait()
		}}, nil
	}
	runtime := buildWith(t, config.BuildOptions{Settings: settings, MCPLauncher: launcher, WorkingDir: t.TempDir()})
	defer runtime.Store.Close()

	_, err = runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()),
		poo.WithSessionStart(failingSessionStart{}))
	if err == nil {
		t.Fatal("the session was built with a failing session_start hook")
	}
	for _, pid := range pids {
		// The rollback ran the closer before the failure was returned,
		// so the child is stopped and reaped already; the poll is the
		// reaping's own bound, not a wait for the failure.
		deadline := time.Now().Add(5 * time.Second)
		for {
			if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the failed construction left the child %d behind", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
