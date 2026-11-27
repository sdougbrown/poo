package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
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
// The manifest names the session's own id as an argument, so a test
// reads back what every generation was launched with, and the settings
// name a decider role so the session is offered the search over the
// tools its server lists.
func relaunchSession(t *testing.T, backoff time.Duration, failStarts bool, turns ...pootest.Turn) (*poo.Session, *pootest.Transport, *mcp.Group, string, string) {
	t.Helper()
	binary := fixtureServer(t)
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "fixture.pid")
	env := map[string]any{
		"POO_FIXTURE_PIDFILE": pidfile,
		// The fixture counts its own starts, so the tool it lists says
		// which generation a listing came from.
		"POO_FIXTURE_GENERATION": filepath.Join(dir, "generation"),
	}
	counts := ""
	if failStarts {
		counts = filepath.Join(dir, "fail-starts")
		env["POO_FIXTURE_FAIL_STARTS"] = counts
	}
	manifestPath := mcpManifest(t,
		map[string]any{"fixture": map[string]any{
			"command": binary,
			"args":    []string{"--session", "${session:id}"},
			"env":     env,
		}},
		map[string]any{"servers": map[string]any{"fixture": map[string]any{"feeds": map[string]any{
			"inbox": resourceFeedDecl(theInboxURI, "The fixture's queued messages.", "wake", nil),
		}}}},
	)
	settings, err := config.Parse([]byte(withDecider+theSessionDir(t)+theMCPPaths(manifestPath)+theMCPProfile("picky", "fixture")), config.FormatYAML)
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
	// The search answers through the session's decider, and a test names
	// the answer rather than running a decision endpoint.
	runtime.Options.Decider = yesDecider{}
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

// theFixtureSessionArg reads the session id the fixture's process was
// launched with: the manifest names `--session ${session:id}`, and the
// fixture's echo tool answers with its own arguments, so the value is
// the one this generation's launch resolved.
func theFixtureSessionArg(t *testing.T, group *mcp.Group) string {
	t.Helper()
	res, err := group.CallTool(context.Background(), "fixture/echo", []byte(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("asking the fixture what it runs with: %v", err)
	}
	if len(res.Text) == 0 {
		t.Fatal("the fixture's echo tool answered with no text")
	}
	var report struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal([]byte(res.Text[0]), &report); err != nil {
		t.Fatalf("the fixture's answer is not a report: %v\n%s", err, res.Text[0])
	}
	for i, arg := range report.Args {
		if arg == "--session" && i+1 < len(report.Args) {
			return report.Args[i+1]
		}
	}
	t.Fatalf("the fixture started with the arguments %q, and none names the session", report.Args)
	return ""
}

// TestRelaunchDeliversAFeedUpdateMadeAfterTheRelaunch covers the whole
// cycle a session survives: the fixture's process ends mid-session, the
// client brings it back on a new generation — the old process reaped,
// so nothing the session started survives it — the feed's own restart
// policy subscribes again on the new generation, and a message pushed
// after the relaunch reaches the model. The relaunched generation is
// launched the way the first was, its templates resolved from the
// session's own values, and the search reads the listing it installed.
func TestRelaunchDeliversAFeedUpdateMadeAfterTheRelaunch(t *testing.T) {
	sess, transport, group, pidfile, _ := relaunchSession(t, 20*time.Millisecond, false,
		pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"},
		pootest.Turn{Calls: []pootest.Call{{
			Name:      cli.FindToolName,
			ID:        "find",
			Arguments: []byte(`{"intent":"the fixture's own name"}`),
		}}},
		pootest.Turn{Text: "done"})

	push(t, group, "before the crash")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "before the crash"))

	// The first launch resolved the manifest's argument from the
	// session's own values: the process runs with the session's id.
	before := theFixtureSessionArg(t, group)
	if before != sess.ID() {
		t.Fatalf("the fixture started with the session id %q, want %q", before, sess.ID())
	}

	old := pootest.FixturePid(t, pidfile)
	killTheFixture(t, group)
	pootest.WaitGone(t, "killed fixture", old)
	fresh := pootest.WaitNewPid(t, pidfile, old)
	if fresh == old {
		t.Error("the relaunch kept the killed process's pid")
	}
	waitFixtureBack(t, group)
	// Every launch resolves the manifest's templates from the session's
	// own values, so the generation the relaunch started runs with the
	// same session id the first did.
	if got := theFixtureSessionArg(t, group); got != before {
		t.Errorf("the relaunched fixture started with the session id %q, want %q", got, before)
	}

	// The feed's next attempt subscribes on the generation the session
	// runs now, and the update made after the relaunch is delivered.
	push(t, group, "after the crash")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "after the crash"))
	// The delivery's run is waited out: a prompt that lands while the
	// session is answering it is refused, and the refusal is the
	// session's, not the test's.
	waitUntil(t, "the delivery run to end", func() bool { return !sess.Running() })

	// A search after the relaunch reads the offering the session runs
	// now, so the hit names the tools this process listed and carries
	// the description the new generation gave them.
	if _, err := sess.Prompt(context.Background(), "find the fixture's stamp tool"); err != nil {
		t.Fatalf("prompting the session: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	text := requestText(transport)
	for _, want := range []string{"fixture/stamp", "generation 2 of this fixture server"} {
		if !strings.Contains(text, want) {
			t.Errorf("the search's answer does not carry %q, so it names another generation:\n%s", want, text)
		}
	}
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
