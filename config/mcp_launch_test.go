package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/mcp"
	"github.com/sdougbrown/poo/mcp/manifest"
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

// fixtureServer builds the mcp package's fixture server and returns its
// path. The test asks a real server what environment it runs with, so
// the server is a process the client can start.
func fixtureServer(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "server")
	cmd := osexec.Command("go", "build", "-o", binary, "github.com/sdougbrown/poo/mcp/testdata/server")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the fixture server: %v\n%s", err, out)
	}
	return binary
}

// clientLauncher is the launcher a test links when it wants the real
// client behind the session: it starts the selected server the way the
// binary does, with the session's own values and the scrub the build
// handed it, and keeps the group so the test can call the server it
// started.
//
// The binary's own launcher lives in package main and cannot be linked
// from here; this is the same wiring over the same client.
func clientLauncher(group **mcp.Group) config.MCPLauncher {
	hostEnv := exec.Lookup(os.Environ())
	return func(ctx context.Context, values config.MCPLaunchValues) (config.MCPRunning, error) {
		g, err := mcp.Launch(ctx, mcp.LaunchOptions{
			SessionID: values.SessionID,
			Dir:       values.WorkingDir,
			HostEnv:   hostEnv,
			Scrub:     values.Scrub,
			Servers:   []manifest.Server{values.Server},
		})
		if err != nil {
			return nil, err
		}
		*group = g
		return g, nil
	}
}

// TestMCPSelectedServerDoesNotInheritTheDeclaredCredential covers the
// environment a server starts with: the name a launcher declared is
// scrubbed from the host environment the server inherits, and the
// manifest's explicit reference still resolves it — the operator's
// choice, the same way a feed reaches one only through a binding.
func TestMCPSelectedServerDoesNotInheritTheDeclaredCredential(t *testing.T) {
	t.Setenv("POO_TEST_CREDENTIAL", "launched-by-the-parent")
	t.Setenv("POO_CREDENTIAL_ENV", "POO_TEST_CREDENTIAL")
	binary := fixtureServer(t)
	path := mcpManifest(t, map[string]any{"fixture": map[string]any{
		"command": binary,
		"env":     map[string]any{"CRED": "${POO_TEST_CREDENTIAL}"},
	}}, nil)
	settings, err := config.Parse([]byte(theSelectingSettings(t, path, "picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	var group *mcp.Group
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		Overrides:   config.EnvOverrides(config.Overrides{}),
		MCPLauncher: clientLauncher(&group),
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()
	if group == nil {
		t.Fatal("the session started no server")
	}

	res, err := group.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{"text":"hi","env":["POO_TEST_CREDENTIAL","CRED"]}`))
	if err != nil {
		t.Fatalf("calling the echo tool: %v", err)
	}
	if len(res.Text) != 1 {
		t.Fatalf("the echo tool answered with %d text contents, want 1", len(res.Text))
	}
	var answer struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(res.Text[0]), &answer); err != nil {
		t.Fatalf("the echo tool's answer is not a report: %v\n%s", err, res.Text[0])
	}
	if got := answer.Env["POO_TEST_CREDENTIAL"]; got != "<unset>" {
		t.Errorf("the declared credential reached the server as %q, want it scrubbed from the host environment", got)
	}
	if got := answer.Env["CRED"]; got != "launched-by-the-parent" {
		t.Errorf("the manifest's explicit reference resolved to %q, want the credential the operator asked for", got)
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
		child := osexec.Command("sleep", "60")
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
