package config_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/mcp"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/callmcp"
)

// relaunchLauncher is the client launcher a relaunch test links: the
// same wiring the binary's own launcher is, with the backoff the test
// wants to observe instead of the second-long one a session runs with.
func relaunchLauncher(group **mcp.Group, backoff time.Duration) config.MCPLauncher {
	hostEnv := exec.Lookup(os.Environ())
	return func(ctx context.Context, values config.MCPLaunchSelection) (config.MCPRunning, error) {
		g, err := mcp.Launch(ctx, mcp.LaunchOptions{
			SessionID:       values.SessionID,
			Dir:             values.WorkingDir,
			HostEnv:         hostEnv,
			Scrub:           values.Scrub,
			RelaunchBackoff: backoff,
			Servers:         values.Servers,
		})
		if err != nil {
			return nil, err
		}
		*group = g
		servers := make([]callmcp.Server, 0, len(values.Servers))
		for _, server := range values.Servers {
			servers = append(servers, &testMCPServer{group: g, name: server.Name})
		}
		return &listedSession{views: servers, group: g, stop: g.Close}, nil
	}
}

// relaunchSession starts a session whose profile selects the fixture
// server with its declared inbox feed, over the client the test links
// with the backoff it names. The fixture writes its pid to the path
// the manifest names, so the test can tell a relaunched process from
// the one it killed, and it fails the startups the counter path says.
func relaunchSession(t *testing.T, backoff time.Duration, failStarts bool, turns ...pootest.Turn) (*poo.Session, *pootest.Transport, *mcp.Group, string, string) {
	t.Helper()
	binary := fixtureServer(t)
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "fixture.pid")
	env := map[string]any{"POO_FIXTURE_PIDFILE": pidfile}
	counts := ""
	if failStarts {
		counts = filepath.Join(dir, "fail-starts")
		env["POO_FIXTURE_FAIL_STARTS"] = counts
	}
	manifestPath := mcpManifest(t,
		map[string]any{"fixture": map[string]any{"command": binary, "env": env}},
		map[string]any{"servers": map[string]any{"fixture": map[string]any{"feeds": map[string]any{
			"inbox": resourceFeedDecl(theInboxURI, "The fixture's queued messages.", "wake", nil),
		}}}},
	)
	settings, err := config.Parse([]byte(theFeedSettings(t, manifestPath)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	var group *mcp.Group
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		Overrides:   config.EnvOverrides(config.Overrides{}),
		MCPLauncher: relaunchLauncher(&group, backoff),
		WorkingDir:  t.TempDir(),
	})
	t.Cleanup(func() { _ = runtime.Store.Close() })
	transport := pootest.NewTransport(turns...)
	runtime.Options.Transport = transport

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	if group == nil {
		t.Fatal("the session started no server")
	}
	return sess, transport, group, pidfile, counts
}

// killTheFixture ends the fixture's process without answering the
// call, so the client's watcher is the one that sees the exit.
func killTheFixture(t *testing.T, group *mcp.Group) {
	t.Helper()
	_, _ = group.CallTool(context.Background(), "fixture/exit", []byte(`{}`))
}

// waitFixtureBack waits until a call on the fixture's echo tool
// succeeds, so a test that just waited for a new process does not race
// the handshake that makes it the session's.
func waitFixtureBack(t *testing.T, group *mcp.Group) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := group.CallTool(context.Background(), "fixture/echo", []byte(`{"text":"hi"}`))
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the fixture's call never succeeded again: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitFixtureDown waits until a call on the fixture reports the server
// down, so a test reads the down state the session's calls read.
func waitFixtureDown(t *testing.T, group *mcp.Group) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := group.CallTool(context.Background(), "fixture/echo", []byte(`{"text":"hi"}`))
		if errors.Is(err, mcp.ErrServerDown) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the fixture's call never reported the server down: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRelaunchDeliversAFeedUpdateMadeAfterTheRelaunch covers the whole
// cycle a session survives: the fixture's process ends mid-session, the
// client brings it back on a new generation — the old process reaped,
// so nothing the session started survives it — the feed's own restart
// policy subscribes again on the new generation, and a message pushed
// after the relaunch reaches the model.
func TestRelaunchDeliversAFeedUpdateMadeAfterTheRelaunch(t *testing.T) {
	sess, transport, group, pidfile, _ := relaunchSession(t, 20*time.Millisecond, false,
		pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})
	_ = sess

	push(t, group, "before the crash")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "before the crash"))

	old := pootest.FixturePid(t, pidfile)
	killTheFixture(t, group)
	pootest.WaitGone(t, "killed fixture", old)
	fresh := pootest.WaitNewPid(t, pidfile, old)
	if fresh == old {
		t.Error("the relaunch kept the killed process's pid")
	}
	waitFixtureBack(t, group)

	// The feed's next attempt subscribes on the generation the session
	// runs now, and the update made after the relaunch is delivered.
	push(t, group, "after the crash")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "after the crash"))
}

// TestRelaunchStopsAfterFiveFailuresAndLeavesTheSessionUsable covers
// the bounded failure: a server that spends the budget of five
// consecutive failures — the process that ended at once, and the
// relaunches that never spoke — is failed for good, the feed that
// depended on it suspends with the fault that says why, and the session
// keeps working: a call names the server down instead of failing the
// turn.
func TestRelaunchStopsAfterFiveFailuresAndLeavesTheSessionUsable(t *testing.T) {
	// The scripted turns, in the order the session asks for them: the
	// delivery before the crash, the suspension report, and the call the
	// test prompts for after both.
	sess, transport, group, pidfile, counts := relaunchSession(t, 10*time.Millisecond, true,
		pootest.Turn{Text: "ack"},
		pootest.Turn{Text: "ack"},
		pootest.Turn{Calls: []pootest.Call{{
			Name:      config.ToolCallMCP,
			ID:        "down",
			Arguments: []byte(`{"name":"fixture/echo","args":{}}`),
		}},
		}, pootest.Turn{Text: "done"})
	_ = sess
	push(t, group, "before the crash")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "before the crash"))

	// The counter is seeded after the session was built, so the server
	// it started is the one the session keeps, and it holds more
	// failures than the budget does: every relaunch from here fails its
	// startup, and none comes back.
	old := pootest.FixturePid(t, pidfile)
	if err := os.WriteFile(counts, []byte("5"), 0o600); err != nil {
		t.Fatalf("seeding the startup failures: %v", err)
	}
	killTheFixture(t, group)
	pootest.WaitGone(t, "killed fixture", old)

	// The failures spend the budget, and the session's calls read the
	// server down.
	waitFixtureDown(t, group)

	// The feed suspends with the fault that says why, and the report
	// is what the model reads — the feed does not restart against a
	// server that will not come back.
	waitUntil(t, "the feed's suspension report", func() bool {
		return strings.Contains(requestText(transport), "server fixture stopped after 5 relaunches")
	})

	// The session is usable: a call on the failed server comes back as
	// a result naming it down, and the turn ends. The report's own run
	// is waited out first — a prompt that lands while the session is
	// answering it is refused, and the refusal is the session's, not
	// the test's.
	waitNoRun(t, transport, 2, "the suspension report's run")
	asked := len(transport.Requests())
	if _, err := sess.Prompt(context.Background(), "call the echo tool"); err != nil {
		t.Fatalf("prompting the session: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	text := requestText(transport)
	if !strings.Contains(text, "server fixture is down") {
		t.Errorf("the model reads %q, want the server named as down", text)
	}
	// The suspended feed delivers nothing further and asks for no run.
	waitNoRun(t, transport, asked+2, "the session after the suspension")
}
