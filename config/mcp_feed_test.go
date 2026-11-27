package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/mcp"
	"github.com/sdougbrown/poo/pootest"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// theInboxURI is the fixture's drain queue: a resource whose read
// returns what the push tool queued, one content each.
const theInboxURI = "fixture://inbox"

// resourceFeedDecl is one `poo` block feed declaration, as the
// manifest's JSON spells it.
func resourceFeedDecl(resource, description, onIdle string, filter map[string]any) map[string]any {
	decl := map[string]any{"resource": resource, "description": description}
	if onIdle != "" {
		decl["on_idle"] = onIdle
	}
	if filter != nil {
		decl["filter"] = filter
	}
	return decl
}

// feedManifest writes an mcp.json whose fixture server starts as the
// given command and declares the given resource feeds, and returns its
// path.
func mcpFeedManifest(t *testing.T, command string, feeds map[string]any) string {
	t.Helper()
	return mcpManifest(t, map[string]any{"fixture": map[string]any{"command": command}},
		map[string]any{"servers": map[string]any{"fixture": map[string]any{"feeds": feeds}}})
}

// theFeedSettings are the settings a session with the fixture's feed
// runs under.
func theFeedSettings(t *testing.T, path string) string {
	t.Helper()
	return theMCPSettings(path) + theSessionDir(t) + theMCPProfile("picky", "fixture")
}

// feedSession starts a session whose profile selects the fixture
// server with its declared feed, over the client the test links. The
// session's chat model answers with the scripted turns, and the
// started group comes back so the test can push to the resource.
func feedSession(t *testing.T, manifest string, turns ...pootest.Turn) (*poo.Session, *pootest.Transport, *config.Runtime, *mcp.Group) {
	t.Helper()
	settings, err := config.Parse([]byte(theFeedSettings(t, manifest)), config.FormatYAML)
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
	t.Cleanup(func() { _ = runtime.Store.Close() })
	transport := pootest.NewTransport(turns...)
	runtime.Options.Transport = transport

	workDir := t.TempDir()
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", workDir))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	if group == nil {
		t.Fatal("the session started no server")
	}
	// The records a session's sinks write go to the session's store,
	// which CreateSession sets itself: the store is what a test reads
	// back, not the sink the options held.
	return sess, transport, runtime, group
}

// push queues one message on the fixture's inbox.
func push(t *testing.T, group *mcp.Group, text string) {
	t.Helper()
	if _, err := group.CallTool(context.Background(), "fixture/push", []byte(`{"text":`+quoteJSON(text)+`}`)); err != nil {
		t.Fatalf("pushing %q to the fixture's inbox: %v", text, err)
	}
}

// quoteJSON renders text as a JSON string.
func quoteJSON(text string) string {
	data, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// theFeedQueued answers how many messages the fixture's inbox still
// holds, so a test waits on the read that drained them rather than on a
// delivery the read happened not to make.
func theFeedQueued(t *testing.T, group *mcp.Group) int64 {
	t.Helper()
	res, err := group.CallTool(context.Background(), "fixture/stats", []byte(`{}`))
	if err != nil {
		t.Fatalf("reading the fixture's stats: %v", err)
	}
	var counters struct {
		Queued int64 `json:"queued"`
	}
	if err := json.Unmarshal([]byte(res.Text[0]), &counters); err != nil {
		t.Fatalf("the fixture's stats are not counters: %v\n%s", err, res.Text[0])
	}
	return counters.Queued
}

// waitNoRun waits out the bound a run would land in and reports how
// many runs the model was asked for: the session starts a run
// asynchronously, so a check made the moment the input is queued could
// pass before the run's own request arrived.
func waitNoRun(t *testing.T, transport *pootest.Transport, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if reads := transport.Requests(); len(reads) != want {
			t.Fatalf("%s started %d runs, want %d", what, len(reads), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// theFeedMessage is how the model reads one delivered record.
func theFeedMessage(name, text string) string {
	return `<feed name="` + name + `" from="fixture">` + text + `</feed>`
}

// TestMCPResourceFeedWakesAnIdleSession covers the declared feed's
// delivery: what the resource queued before the session's first
// subscription is delivered under the feed's origin, and the wake is
// the declared one — a feed that says wake starts a run an idle
// session answers.
func TestMCPFeedWakesAnIdleSession(t *testing.T) {
	binary := fixtureServer(t)
	manifest := mcpFeedManifest(t, binary, map[string]any{
		"inbox": resourceFeedDecl(theInboxURI, "The fixture's queued messages.", "wake", nil),
	})

	// The message is pushed once the session is running, so the
	// update the push sends is what the feed reads.
	sess, transport, _, group := feedSession(t, manifest,
		pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})
	_ = sess
	push(t, group, "hello from the fixture")

	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "hello from the fixture"))
	// A later update is delivered too: the feed reads on the updates
	// the server sends.
	push(t, group, "and a second one")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "and a second one"))
}

// TestMCPResourceFeedHoldsAnIdleSession covers the other idle word: a
// feed that says hold queues what it delivered and starts no run, so
// the model is not woken for the message.
func TestMCPFeedHoldsAnIdleSession(t *testing.T) {
	binary := fixtureServer(t)
	manifest := mcpFeedManifest(t, binary, map[string]any{
		"inbox": resourceFeedDecl(theInboxURI, "The fixture's queued messages.", "hold", nil),
	})
	sess, transport, runtime, group := feedSession(t, manifest,
		pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})
	push(t, group, "not worth waking for")

	// The input is queued and the model is not asked: the hold is the
	// operator's word about when the feed may start a run. The queued
	// input is in the session's store, which is the session's own sink.
	waitUntil(t, "the held input to be queued", func() bool {
		return theStoreHolds(t, runtime, theFeedMessage("inbox", "not worth waking for"))
	})
	waitNoRun(t, transport, 0, "the held feed")
	_ = sess
}

// TestMCPResourceFeedIsAJob covers the job the feed is: the session
// lists it with its jobs under its origin, and cancel_job stops it, so
// a later update delivers nothing.
func TestMCPFeedIsAJob(t *testing.T) {
	binary := fixtureServer(t)
	manifest := mcpFeedManifest(t, binary, map[string]any{
		"inbox": resourceFeedDecl(theInboxURI, "The fixture's queued messages.", "wake", nil),
	})
	sess, transport, _, group := feedSession(t, manifest,
		pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})

	push(t, group, "the first message")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "the first message"))

	// The feed is listed under its origin, as the feed tool's own.
	listed := false
	for _, job := range sess.Jobs() {
		if job.JobID != "feed:inbox" {
			continue
		}
		listed = true
		if job.Tool != poo.FeedToolName {
			t.Errorf("the feed is listed as the tool %q, want %q", job.Tool, poo.FeedToolName)
		}
	}
	if !listed {
		t.Fatalf("the session does not list the feed: %v", sess.Jobs())
	}

	// Stopping the job ends the subscription: a later push delivers
	// nothing, for no one is reading the resource.
	if err := sess.CancelJob("feed:inbox"); err != nil {
		t.Fatalf("stopping the feed: %v", err)
	}
	waitUntil(t, "the feed to leave the listing", func() bool {
		for _, job := range sess.Jobs() {
			if job.JobID == "feed:inbox" {
				return false
			}
		}
		return true
	})
	push(t, group, "after the stop")
	if theFeedWasRead(transport, theFeedMessage("inbox", "after the stop")) {
		t.Error("the stopped feed delivered a message")
	}
}

// TestMCPResourceFeedFansOutThroughAScriptFilter covers the filter a
// declared feed carries: one read becomes the records the filter wrote
// — two inputs for the message it shapes — and an output of no records
// drops the batch, so a message the filter says nothing about delivers
// nothing.
func TestMCPFeedFansOutThroughAScriptFilter(t *testing.T) {
	// The filter reads the record and answers by its text: the fanned
	// message becomes two inputs, and the quiet one becomes none.
	script := filepath.Join(t.TempDir(), "shape.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
input=$(cat)
case "$input" in
	*fan*)
		printf '%s\n' '{"text":"first of two","from":"fixture"}' '{"text":"second of two","from":"fixture"}'
		;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := fixtureServer(t)
	manifest := mcpFeedManifest(t, binary, map[string]any{
		"inbox": resourceFeedDecl(theInboxURI, "The fixture's queued messages, shaped.", "wake",
			map[string]any{"command": []string{script}}),
	})
	_, transport, _, group := feedSession(t, manifest,
		pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"}, pootest.Turn{Text: "ack"})

	push(t, group, "fan this one")
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "first of two"))
	waitUntilFeedRead(t, transport, theFeedMessage("inbox", "second of two"))

	// The empty batch: the filter writes nothing for the quiet message,
	// and no input is delivered. The read that carried it happened all
	// the same, so the assertion is about the read's empty answer rather
	// than about a read still to come.
	asked := len(transport.Requests())
	push(t, group, "quiet")
	waitUntil(t, "the quiet message's read", func() bool { return theFeedQueued(t, group) == 0 })
	waitNoRun(t, transport, asked, "the filter's empty batch")
}

// TestMCPDeclaredFeedNeedsASubscribingServer covers the attach's
// refusal: a server that declared a feed but advertised no resource
// subscriptions cannot run it, the session is not built, whatever
// started stops, and the failure names the server and the feed.
func TestMCPFeedNeedsASubscribingServer(t *testing.T) {
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "bare", Version: "test"}, nil)
	// The server holds a resource but no subscribe handler, so its
	// initialize result advertises no resource subscriptions.
	server.AddResource(&sdkmcp.Resource{URI: "bare://inbox", Name: "inbox", MIMEType: "text/plain"},
		func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{}, nil
		})
	go func() { _ = server.Run(context.Background(), serverTransport) }()

	closes := int64(0)
	launcher := func(_ context.Context, values config.MCPLaunchSelection) (config.MCPRunning, error) {
		g, err := mcp.Connect(context.Background(), values.Servers[0].Name, clientTransport)
		if err != nil {
			return nil, err
		}
		return &closingServer{group: g, name: values.Servers[0].Name, closed: &closes}, nil
	}
	manifest := mcpFeedManifest(t, "true", map[string]any{
		"inbox": resourceFeedDecl("bare://inbox", "A feed the server cannot serve.", "", nil),
	})
	settings, err := config.Parse([]byte(theFeedSettings(t, manifest)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		Overrides:   config.EnvOverrides(config.Overrides{}),
		MCPLauncher: launcher,
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()

	_, err = runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()))
	if err == nil {
		t.Fatal("the session was built with a feed its server cannot serve")
	}
	for _, want := range []string{"fixture", "inbox", "resource subscriptions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure %q does not name %q", err, want)
		}
	}
	if closes != 1 {
		t.Errorf("the server was closed %d times, want once — the refused attach owns nothing", closes)
	}
}

// TestMCPResourceFeedOffersNoSubscribeTool covers the offering: a
// resource feed is one the session starts itself, so the tool that
// starts an optional feed is not the model's to call for it.
func TestMCPFeedOffersNoSubscribeTool(t *testing.T) {
	binary := fixtureServer(t)
	manifest := mcpFeedManifest(t, binary, map[string]any{
		"inbox": resourceFeedDecl(theInboxURI, "The fixture's queued messages.", "wake", nil),
	})
	settings, err := config.Parse([]byte(theFeedSettings(t, manifest)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWith(t, config.BuildOptions{
		Settings:   settings,
		Overrides:  config.EnvOverrides(config.Overrides{}),
		WorkingDir: t.TempDir(),
	})
	defer runtime.Store.Close()
	for _, tool := range runtime.Profile.Tools {
		if tool.Name == poo.SubscribeFeedToolName {
			t.Errorf("the profile offers %s for a feed the session starts itself", tool.Name)
		}
	}
	for _, want := range []string{"jobs", "cancel_job"} {
		found := false
		for _, tool := range runtime.Profile.Tools {
			if tool.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the profile offers no %s for the feed the session runs", want)
		}
	}
}

// closingServer is an in-memory handle that counts its closes, so a
// test can prove a refused attach stopped what it started.
type closingServer struct {
	group  *mcp.Group
	name   string
	closed *int64
}

func (s *closingServer) Close() error {
	atomic.AddInt64(s.closed, 1)
	return s.group.Close()
}

func (s *closingServer) Offered() []cli.Offered { return nil }

func (s *closingServer) Subscribes(server string) bool { return s.group.Subscribes(server) }

func (s *closingServer) ResourceFeed(server, uri string) poo.FeedRunner {
	return mcp.ResourceFeed(s.group, server, uri)
}

// Servers are the per-server views the tools read: one view per server
// the handle started, backed by the group's own tools and calls.
func (s *closingServer) Servers() []config.MCPServer {
	tools, _ := s.group.Tools(s.name)
	return []config.MCPServer{&closingView{group: s.group, name: s.name, tools: tools}}
}

// closingView is one started server's view: the group answers its calls.
type closingView struct {
	group *mcp.Group
	name  string
	tools []mcp.Tool
}

func (v *closingView) Name() string { return v.name }

func (v *closingView) Tools() []mcp.Tool { return v.tools }

func (v *closingView) CallTool(ctx context.Context, tool string, args json.RawMessage) (mcp.CallResult, error) {
	return v.group.CallTool(ctx, v.name+"/"+tool, args)
}

// waitUntilFeedRead waits until the model reads the delivered message.
func waitUntilFeedRead(t *testing.T, transport *pootest.Transport, message string) {
	t.Helper()
	waitUntil(t, "the message "+message+" in the model's view", func() bool {
		return theFeedWasRead(transport, message)
	})
}

// theStoreHolds reports whether the session's store holds a queued
// feed input with the message's text. The store is the session's own
// sink, so a held input — one the session was not woken for — is
// read back from it rather than from a sink the session replaced.
func theStoreHolds(t *testing.T, runtime *config.Runtime, message string) bool {
	t.Helper()
	records, err := runtime.Store.ReadRecords(context.Background())
	if err != nil {
		t.Fatalf("reading the session's records: %v", err)
	}
	for _, rec := range records {
		if queued, ok := rec.(*poo.InputQueuedRecord); ok && queued.Mode == poo.InputModeFeed &&
			queued.Text == message {
			return true
		}
	}
	return false
}

// theFeedWasRead reports whether the model's requests carry the
// message.
func theFeedWasRead(transport *pootest.Transport, message string) bool {
	for _, request := range transport.Requests() {
		for _, msg := range request.Messages {
			if len(msg.Content) == 0 {
				continue
			}
			if msg.Origin == poo.FeedOrigin("inbox") && msg.Content[0].Text == message {
				return true
			}
		}
	}
	return false
}
