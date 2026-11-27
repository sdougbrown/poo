package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/mcp/manifest"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildFixture builds one of the package's fixture servers and
// returns its path. The fixture is a binary because the client's
// whole job is to own a process: the test proves what a process does,
// not what a function calls.
func buildFixture(t *testing.T, name string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), name)
	cmd := osexec.Command("go", "build", "-o", binary, "./testdata/"+name)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the %s fixture: %v\n%s", name, err, out)
	}
	return binary
}

// template parses one manifest value, and fails the test when it does
// not parse: a fixture that cannot say what it means is a broken
// fixture, not a case the launch covers.
func template(t *testing.T, source string) manifest.Template {
	t.Helper()
	parsed, err := manifest.ParseTemplate(source)
	if err != nil {
		t.Fatalf("parsing the template %q: %v", source, err)
	}
	return parsed
}

// counterURI is the resource the fixture server updates on a bump.
const counterURI = "fixture://counter"

// fixtureServer returns the declared fixture server: the command the
// host environment resolves, the session's own id as an argument and an
// environment value, and the pid file the close asserts through.
func fixtureServer(t *testing.T, binary, pidfile string) manifest.Server {
	t.Helper()
	return manifest.Server{
		Name:    "fixture",
		Command: template(t, "${FIXTURE}"),
		Args:    []manifest.Template{template(t, "--session"), template(t, "${session:id}")},
		Env: map[string]manifest.Template{
			"POO_FIXTURE_VALUE":   template(t, "${session:id}"),
			"POO_FIXTURE_PIDFILE": template(t, pidfile),
			"LEAK":                template(t, "${SECRET}"),
			"POO_ENV_SENTINEL":    template(t, "overridden"),
		},
	}
}

// fixturePid returns the pid the fixture process wrote, once it has
// written one.
func fixturePid(t *testing.T, pidfile string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(pidfile)
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("the pid file holds %q: %v", data, err)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("the fixture never wrote its pid to %s", pidfile)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitGone waits until the process is fully gone — not a zombie
// either, but reaped, so nothing the session started survives it.
func waitGone(t *testing.T, what string, pid int) {
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

// TestLaunchStartsTheSelectedServersWithTheSessionValues covers the
// launch: a selected server starts in the session's directory, with
// the session's own id resolved into its arguments and environment,
// the host environment scrubbed by the given rule, and a manifest's
// explicit reference reintroducing what the rule scrubbed — and the
// close leaves no process behind.
func TestLaunchStartsTheSelectedServersWithTheSessionValues(t *testing.T) {
	t.Setenv("SECRET", "s3cret")
	// The sentinel is the test's own, so the assertion does not depend
	// on the runner's environment carrying a name like HOME. The
	// manifest declares the same name below with a different value, so
	// the sentinel also proves the merge replaces a base entry in
	// place: the server must see the declared value, not the base's.
	t.Setenv("POO_ENV_SENTINEL", "present")
	binary := buildFixture(t, "server")
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "fixture.pid")

	hostEnv := exec.Lookup(os.Environ())
	lookup := func(name string) (string, bool) {
		if name == "FIXTURE" {
			return binary, true
		}
		return hostEnv(name)
	}
	g, err := Launch(context.Background(), LaunchOptions{
		SessionID: "s-42",
		Dir:       dir,
		HostEnv:   lookup,
		Scrub:     exec.Scrub{Names: []string{"SECRET"}},
		Servers:   []manifest.Server{fixtureServer(t, binary, pidfile)},
	})
	if err != nil {
		t.Fatalf("launching the fixture: %v", err)
	}

	tools, ok := g.Tools("fixture")
	if !ok {
		t.Fatal("the group holds no server named fixture")
	}
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"echo", "fail", "bump", "exit"} {
		if !slices.Contains(names, want) {
			t.Errorf("the server listed no %q tool: %q", want, names)
		}
	}

	res, err := g.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{"text":"hi","env":["POO_FIXTURE_VALUE","SECRET","LEAK","POO_ENV_SENTINEL"]}`))
	if err != nil {
		t.Fatalf("calling the echo tool: %v", err)
	}
	if len(res.Text) != 1 {
		t.Fatalf("the echo tool answered with %d text contents, want 1", len(res.Text))
	}
	var answer struct {
		Text string            `json:"text"`
		Args []string          `json:"args"`
		Cwd  string            `json:"cwd"`
		Env  map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(res.Text[0]), &answer); err != nil {
		t.Fatalf("the echo tool's answer is not a report: %v\n%s", err, res.Text[0])
	}
	if answer.Text != "hi" {
		t.Errorf("the echo tool answered with %q, want hi", answer.Text)
	}
	if answer.Cwd != dir {
		t.Errorf("the server runs in %q, want %q", answer.Cwd, dir)
	}
	if !slices.Contains(answer.Args, "s-42") {
		t.Errorf("the server's arguments are %q, want the session id among them", answer.Args)
	}
	if got := answer.Env["POO_FIXTURE_VALUE"]; got != "s-42" {
		t.Errorf("the session id resolved to %q in the environment, want s-42", got)
	}
	if got := answer.Env["SECRET"]; got != "<unset>" {
		t.Errorf("the scrubbed SECRET reached the server as %q", got)
	}
	if got := answer.Env["LEAK"]; got != "s3cret" {
		t.Errorf("the manifest's explicit ${SECRET} resolved to %q, want the scrubbed value back", got)
	}
	if got := answer.Env["POO_ENV_SENTINEL"]; got != "overridden" {
		t.Errorf("the declared override of a base name reached the server as %q, want the declared value in the base entry's place", got)
	}

	if err := g.Close(); err != nil {
		t.Fatalf("closing the group: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Errorf("the second close returned %v, want none", err)
	}
	waitGone(t, "fixture", fixturePid(t, pidfile))
}

// TestLaunchTimesOutAServerThatNeverSpeaks covers the startup bound: a
// server that hangs before its handshake fails the launch within the
// timeout, the failure names the server, and no process survives it.
func TestLaunchTimesOutAServerThatNeverSpeaks(t *testing.T) {
	binary := buildFixture(t, "hang")
	pidfile := filepath.Join(t.TempDir(), "hang.pid")

	g, err := Launch(context.Background(), LaunchOptions{
		SessionID: "s-42",
		Dir:       t.TempDir(),
		HostEnv: func(name string) (string, bool) {
			if name == "FIXTURE" {
				return binary, true
			}
			return "", false
		},
		Scrub: exec.Scrub{},
		Servers: []manifest.Server{{
			Name:    "hang",
			Command: template(t, "${FIXTURE}"),
			Env:     map[string]manifest.Template{"POO_FIXTURE_PIDFILE": template(t, pidfile)},
		}},
		StartupTimeout: 300 * time.Millisecond,
	})
	if err == nil {
		g.Close()
		t.Fatal("the launch accepted a server that never spoke")
	}
	var launchErr *LaunchError
	if !errors.As(err, &launchErr) || launchErr.Server != "hang" {
		t.Fatalf("the failure is %v, want the server hang named", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the failure is %v, want the startup timeout", err)
	}
	waitGone(t, "hung fixture", fixturePid(t, pidfile))
}

// TestCallToolRoutesItsAnswersAndErrors covers the call contract: an
// unknown server, tool or shape is ErrUnknownTool; a server that is
// down is ErrServerDown; a transport failure mid-call is returned, not
// retried; and the server's own error flag is a successful return.
func TestCallToolRoutesItsAnswersAndErrors(t *testing.T) {
	t.Setenv("SECRET", "s3cret")
	binary := buildFixture(t, "server")
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "fixture.pid")
	g, err := Launch(context.Background(), LaunchOptions{
		SessionID: "s-42",
		Dir:       dir,
		HostEnv: func(name string) (string, bool) {
			if name == "FIXTURE" {
				return binary, true
			}
			return exec.Lookup(os.Environ())(name)
		},
		Servers: []manifest.Server{fixtureServer(t, binary, pidfile)},
	})
	if err != nil {
		t.Fatalf("launching the fixture: %v", err)
	}
	defer g.Close()

	for _, qualified := range []string{"nope/echo", "fixture/nope", "fixture"} {
		if _, err := g.CallTool(context.Background(), qualified, json.RawMessage(`{}`)); !errors.Is(err, ErrUnknownTool) {
			t.Errorf("calling %q returned %v, want ErrUnknownTool", qualified, err)
		}
	}

	res, err := g.CallTool(context.Background(), "fixture/fail", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("calling the failing tool: %v", err)
	}
	if !res.IsError {
		t.Error("the failing tool's answer is not an error answer")
	}
	if len(res.Text) == 0 || res.Text[0] != "the tool refused" {
		t.Errorf("the failing tool's answer is %q", res.Text)
	}

	// The exit tool ends the process without answering, so the call
	// fails with the transport's own failure. Once the session has
	// seen the end, every later call reports the server down instead
	// of leaving again.
	if _, err := g.CallTool(context.Background(), "fixture/exit", json.RawMessage(`{}`)); err == nil {
		t.Fatal("the call to the exiting tool succeeded")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := g.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{}`))
		if errors.Is(err, ErrServerDown) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the call after the server's end returned %v, want ErrServerDown", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestConnectBuildsAGroupOverAnyTransport covers the connect: a
// one-server group over a transport the caller owns, with the tools
// listed and the calls routed, and nothing launched of its own.
func TestConnectBuildsAGroupOverAnyTransport(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "inline", Version: "test"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: args.Text}}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = server.Run(context.Background(), serverTransport) }()

	g, err := Connect(context.Background(), "inline", clientTransport)
	if err != nil {
		t.Fatalf("connecting the inline server: %v", err)
	}
	tools, ok := g.Tools("inline")
	if !ok || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("the group listed %v, want the inline server's echo", tools)
	}
	res, err := g.CallTool(context.Background(), "inline/echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("calling the inline echo: %v", err)
	}
	if len(res.Text) != 1 || res.Text[0] != "hi" {
		t.Errorf("the inline echo answered %q, want hi", res.Text)
	}
	if err := g.Close(); err != nil {
		t.Errorf("closing the inline group: %v", err)
	}

	// A launch with no server starts none: the group is empty and
	// closes cleanly.
	empty, err := Launch(context.Background(), LaunchOptions{SessionID: "s-42"})
	if err != nil {
		t.Fatalf("launching no server: %v", err)
	}
	if tools, ok := empty.Tools("fixture"); ok {
		t.Errorf("an empty launch listed %v", tools)
	}
	if err := empty.Close(); err != nil {
		t.Errorf("closing an empty group: %v", err)
	}
}

// TestAHintTheServerOmittedReadsTheProtocolDefault covers the
// annotations' defaults: a server that names some hints and omits
// others reads the omitted ones as the protocol documents them —
// destructive and open-world true, read-only and idempotent false —
// while the hints it named keep the values it gave them.
func TestAHintTheServerOmittedReadsTheProtocolDefault(t *testing.T) {
	no := false
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "inline", Version: "test"}, nil)
	server.AddTool(&sdkmcp.Tool{
		Name:        "partial",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Annotations: &sdkmcp.ToolAnnotations{Title: "Partial", DestructiveHint: &no},
	}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "hi"}}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = server.Run(context.Background(), serverTransport) }()

	g, err := Connect(context.Background(), "inline", clientTransport)
	if err != nil {
		t.Fatalf("connecting the inline server: %v", err)
	}
	defer g.Close()
	tools, ok := g.Tools("inline")
	if !ok || len(tools) != 1 || tools[0].Name != "partial" {
		t.Fatalf("the group listed %v, want the inline server's partial", tools)
	}
	ann := tools[0].Annotations
	if ann.Title != "Partial" {
		t.Errorf("the tool's title is %q, want Partial", ann.Title)
	}
	if ann.DestructiveHint {
		t.Error("the named destructive hint read as true, want the false the server sent")
	}
	if !ann.OpenWorldHint {
		t.Error("the omitted open-world hint read as false, want the protocol's true")
	}
	if ann.ReadOnlyHint {
		t.Error("the omitted read-only hint read as true, want the protocol's false")
	}
	if ann.IdempotentHint {
		t.Error("the omitted idempotent hint read as true, want the protocol's false")
	}
}

// TestTheResourceUpdateReachesItsWaiter covers the notification routing
// the client installs before it connects: a subscribed resource's
// update reaches the waiter registered for its URI.
func TestTheResourceUpdateReachesItsWaiter(t *testing.T) {
	t.Setenv("SECRET", "s3cret")
	binary := buildFixture(t, "server")
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "fixture.pid")
	g, err := Launch(context.Background(), LaunchOptions{
		SessionID: "s-42",
		Dir:       dir,
		HostEnv: func(name string) (string, bool) {
			if name == "FIXTURE" {
				return binary, true
			}
			return exec.Lookup(os.Environ())(name)
		},
		Servers: []manifest.Server{fixtureServer(t, binary, pidfile)},
	})
	if err != nil {
		t.Fatalf("launching the fixture: %v", err)
	}
	defer g.Close()

	g.mu.Lock()
	state := g.byName["fixture"]
	g.mu.Unlock()
	if state.result.Capabilities.Resources == nil {
		t.Fatal("the fixture advertised no resources capability")
	}

	// The update reaches a subscribed session, so the subscription is
	// the test's own: the waiter answers the notification the bump
	// sends.
	if err := state.session.Subscribe(context.Background(), &sdkmcp.SubscribeParams{URI: counterURI}); err != nil {
		t.Fatalf("subscribing to the counter: %v", err)
	}
	waiter, stop := state.waiters.waitUpdate(counterURI)
	defer stop()

	if _, err := g.CallTool(context.Background(), "fixture/bump", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("bumping the counter: %v", err)
	}
	select {
	case <-waiter:
	case <-time.After(5 * time.Second):
		t.Fatal("the resource update never reached its waiter")
	}
}

// TestTheSubscriptionsAcknowledgementReachesItsWaiter covers the other
// notification the routing carries: the acknowledgement the SDK delivers
// for a granted subscription reaches the waiter registered for its URI,
// so a flow that waits on it is not left hanging on a shape the routing
// read wrong.
func TestTheSubscriptionsAcknowledgementReachesItsWaiter(t *testing.T) {
	t.Setenv("SECRET", "s3cret")
	binary := buildFixture(t, "server")
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "fixture.pid")
	g, err := Launch(context.Background(), LaunchOptions{
		SessionID: "s-42",
		Dir:       dir,
		HostEnv: func(name string) (string, bool) {
			if name == "FIXTURE" {
				return binary, true
			}
			return exec.Lookup(os.Environ())(name)
		},
		Servers: []manifest.Server{fixtureServer(t, binary, pidfile)},
	})
	if err != nil {
		t.Fatalf("launching the fixture: %v", err)
	}
	defer g.Close()

	g.mu.Lock()
	state := g.byName["fixture"]
	g.mu.Unlock()

	// The waiter is registered before the subscribe round trip, so the
	// acknowledgement lands in a waiter rather than nowhere.
	waiter, stop := state.waiters.waitAck(counterURI)
	defer stop()
	if err := state.session.Subscribe(context.Background(), &sdkmcp.SubscribeParams{URI: counterURI}); err != nil {
		t.Fatalf("subscribing to the counter: %v", err)
	}
	select {
	case granted := <-waiter:
		if !granted {
			t.Fatalf("the acknowledgement refused the URI the server granted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription acknowledgement never reached its waiter")
	}
}

// TestTheWaitersAnswerOnceAndStop covers the waiters' own rules: a
// waiter is answered once and then gone, an acknowledgement answers the
// waiters whose URI it grants and no others, and a stopped waiter hears
// nothing.
func TestTheWaitersAnswerOnceAndStop(t *testing.T) {
	var waiters resourceWaiters

	first, stopFirst := waiters.waitUpdate("fixture://one")
	second, _ := waiters.waitUpdate("fixture://one")
	waiters.updated("fixture://one")
	for name, waiter := range map[string]<-chan struct{}{"first": first, "second": second} {
		select {
		case <-waiter:
		case <-time.After(5 * time.Second):
			t.Fatalf("the %s waiter never answered the update", name)
		}
	}
	// The waiters answered once: a later update reaches no one.
	waiters.updated("fixture://one")
	select {
	case <-first:
		t.Error("an answered waiter answered a second update")
	default:
	}
	stopFirst()

	// An acknowledgement answers the waiters whose URI it grants, and
	// only those: each subscription is its own listen stream, so another
	// stream's grant is not this waiter's answer.
	acked, _ := waiters.waitAck("fixture://granted")
	refused, _ := waiters.waitAck("fixture://refused")
	waiters.acknowledged([]string{"fixture://granted"})
	select {
	case granted := <-acked:
		if !granted {
			t.Error("the granted waiter was told its URI was refused")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the acknowledged waiter never answered")
	}
	select {
	case granted := <-refused:
		t.Errorf("the waiter of another URI answered a grant with %v", granted)
	default:
	}

	// An acknowledgement that grants no URI is its own stream's
	// refusal, and the one waiter still waiting for an answer hears it.
	waiters.acknowledged(nil)
	select {
	case granted := <-refused:
		if granted {
			t.Error("the refused waiter was told its URI was granted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the refused waiter never answered")
	}

	// A waiter registered after the acknowledgement waits for the
	// next one, and a stopped one never hears it.
	late, stopLate := waiters.waitAck("fixture://late")
	_ = late
	stopLate()
	waiters.acknowledged([]string{"fixture://late"})
	select {
	case <-late:
		t.Error("a stopped waiter answered an acknowledgement")
	default:
	}

	// The routing is concurrency safe: waiters may be registered and
	// answered from the notification goroutines while a reader holds
	// a channel.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			waiter, stop := waiters.waitUpdate("fixture://race")
			defer stop()
			waiters.updated("fixture://race")
			select {
			case <-waiter:
			case <-time.After(5 * time.Second):
				t.Error("the racing waiter never answered")
			}
		}()
	}
	wg.Wait()
}

// TestCallToolTranslatesTheImageAndStructuredContent covers the
// translation of the SDK's content boundary beyond text: a tool that
// answers an image beside a text and a structured payload comes back
// with the image's media type and data on Images and the payload's
// JSON on Structured, and the text the same answer carries is not
// lost to the image branch.
func TestCallToolTranslatesTheImageAndStructuredContent(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "inline", Version: "test"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "picture", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.ImageContent{MIMEType: "image/png", Data: []byte("aGVsbG8=")},
				&sdkmcp.TextContent{Text: "the caption"},
			},
			StructuredContent: map[string]any{"width": 2, "height": 1},
		}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = server.Run(context.Background(), serverTransport) }()

	g, err := Connect(context.Background(), "inline", clientTransport)
	if err != nil {
		t.Fatalf("connecting the inline server: %v", err)
	}
	defer func() {
		if err := g.Close(); err != nil {
			t.Errorf("closing the inline group: %v", err)
		}
	}()

	res, err := g.CallTool(context.Background(), "inline/picture", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("calling the inline picture: %v", err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("the picture answered %d images, want one", len(res.Images))
	}
	if res.Images[0].MediaType != "image/png" || string(res.Images[0].Data) != "aGVsbG8=" {
		t.Errorf("the picture answered %+v, want the png's media type and data", res.Images[0])
	}
	var captioned bool
	for _, text := range res.Text {
		if text == "the caption" {
			captioned = true
		}
	}
	if !captioned {
		t.Errorf("the picture answered %q, want the caption among the texts", res.Text)
	}
	if res.Structured == nil {
		t.Fatal("the picture answered no structured content, want the payload's JSON")
	}
	var structured map[string]any
	if err := json.Unmarshal(res.Structured, &structured); err != nil {
		t.Fatalf("decoding the structured content %s: %v", res.Structured, err)
	}
	if structured["width"] != float64(2) || structured["height"] != float64(1) {
		t.Errorf("the structured content is %s, want the width and the height", res.Structured)
	}
}
