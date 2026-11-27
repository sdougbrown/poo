package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/config"
	pooexec "github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/mcp/manifest"
	"github.com/sdougbrown/poo/mcp/wire"
)

// The shipped examples are settings and manifests an operator runs as they
// are, so the tests load them the way poo does: nothing is rewritten into a
// temporary directory first.

// theAvenorMCPExample returns the example's directory, absolute.
func theAvenorMCPExample(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "examples", "avenor-mcp"))
	if err != nil {
		t.Fatalf("resolving the example's directory: %v", err)
	}
	return dir
}

// theCheckout returns the repository root the example's settings resolve
// their relative path against.
func theCheckout(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(".."))
	if err != nil {
		t.Fatalf("resolving the checkout: %v", err)
	}
	return root
}

// theExampleSettings loads the settings file an example ships.
func theExampleSettings(t *testing.T, dir string) *config.Settings {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatalf("reading the example's settings: %v", err)
	}
	settings, err := config.Parse(data, config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the example's settings: %v", err)
	}
	return settings
}

// profileToolNames returns the names a profile's tool list offers.
func profileToolNames(tools []poo.ToolInfo) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// exampleRunning is a launcher handle for a server that starts no process: it
// advertises the resource subscriptions a declared feed needs, and the feed's
// runner waits for the session to end.
type exampleRunning struct {
	mu     sync.Mutex
	closed int
	feeds  []string
}

// Close counts.
func (r *exampleRunning) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}

// Servers are the per-server views the tools read: the example's own
// server, listed with no tools of its own.
func (r *exampleRunning) Servers() []config.MCPServer {
	return []config.MCPServer{exampleServer{name: "avenor"}}
}

// Offered returns no entry: the example's server lists no tools, so the
// derivation over its view carries none.
func (r *exampleRunning) Offered() []cli.Offered { return nil }

// exampleServer is the example's started server, as the tools over it
// read it: named, with no tools, and a call that answers nothing — the
// example's manifest declares the server alone.
type exampleServer struct{ name string }

func (s exampleServer) Name() string { return s.name }

func (s exampleServer) Tools() []wire.Tool { return nil }

func (s exampleServer) CallTool(context.Context, string, json.RawMessage) (wire.CallResult, error) {
	return wire.CallResult{}, nil
}

// Subscribes reports the capability the example's feed needs.
func (r *exampleRunning) Subscribes(string) bool { return true }

// ResourceFeed records the resource the feed subscribed to, and holds the
// attempt open until the session ends it.
func (r *exampleRunning) ResourceFeed(server, uri string) poo.FeedRunner {
	r.mu.Lock()
	r.feeds = append(r.feeds, uri)
	r.mu.Unlock()
	return func(ctx context.Context, io poo.FeedIO) error {
		<-ctx.Done()
		return ctx.Err()
	}
}

// startedFeeds returns the resources the session subscribed.
func (r *exampleRunning) startedFeeds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.feeds...)
}

// TestExamplesAvenorMCPSelectsTheServer covers the MCP example: its settings
// load its manifest and no other, and the profile's selection is what starts
// the server and runs the feed the server's manifest declares.
func TestExamplesAvenorMCPSelectsTheServer(t *testing.T) {
	dir := theAvenorMCPExample(t)
	settings := theExampleSettings(t, dir)
	running := &exampleRunning{}
	runtime := buildWith(t, config.BuildOptions{
		Settings: settings,
		// The settings name the example's directory relative to the
		// checkout, so the build runs from there the way the README says.
		// The session directory is the test's own: the shipped settings
		// name no store, and the checkout's real one must not hold a test
		// session (and a rerun would collide with the first's `s1`).
		WorkingDir: theCheckout(t),
		Overrides:  config.Overrides{SessionDir: t.TempDir()},
		MCPLauncher: func(context.Context, config.MCPLaunchSelection) (config.MCPRunning, error) {
			return running, nil
		},
	})
	t.Cleanup(func() { _ = runtime.Store.Close() })

	if names := runtime.MCPServerNames(); !slices.Equal(names, []string{"avenor"}) {
		t.Fatalf("the build loaded the mcp servers %q, want the example's avenor alone", names)
	}
	if !slices.Contains(runtime.MCPServers, "avenor") {
		t.Fatalf("the profile selects the mcp servers %q, want avenor", runtime.MCPServers)
	}
	// A selection is what offers the call: the servers are the session's own
	// processes, so the model reaches their tools through it and the prompt's
	// catalog.
	if !offersTool(profileToolNames(runtime.Profile.Tools), config.ToolCallMCP) {
		t.Fatalf("the profile offers %q, and the selection is what earns the %s tool", profileToolNames(runtime.Profile.Tools), config.ToolCallMCP)
	}

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("avenor", t.TempDir()))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	// The declared resource feed is a feed the session runs: it is listed
	// with the session's jobs under its own name, whatever the manifest that
	// declared it is.
	waitUntil(t, "the session to run the example's inbox feed", func() bool {
		for _, job := range sess.Jobs() {
			if job.JobID == "feed:avenor-inbox" {
				return true
			}
		}
		return false
	})
	if feeds := running.startedFeeds(); !slices.Equal(feeds, []string{"avenor://inbox"}) {
		t.Fatalf("the session subscribed the resources %q, want the example's inbox alone", feeds)
	}
}

// TestExamplesAvenorMCPManifestResolves covers the manifest the example
// ships: one server whose command resolves per session, and one resource feed
// over the bound server's inbox.
func TestExamplesAvenorMCPManifestResolves(t *testing.T) {
	dir := theAvenorMCPExample(t)
	set, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("loading the example's manifest: %v", err)
	}
	if names := set.Names(); !slices.Equal(names, []string{"avenor"}) {
		t.Fatalf("the manifest declares the servers %q, want avenor alone", names)
	}
	if warnings := set.Warnings(); len(warnings) != 0 {
		t.Errorf("the manifest warns %q, want nothing left out", warnings)
	}
	server, ok := set.Lookup("avenor")
	if !ok {
		t.Fatal("the manifest does not declare the server avenor")
	}
	host := pooexec.Lookup(os.Environ())
	command, err := server.Command.Resolve(host, "session-7")
	if err != nil {
		t.Fatalf("resolving the server's command: %v", err)
	}
	argv := []string{command}
	for _, arg := range server.Args {
		resolved, err := arg.Resolve(host, "session-7")
		if err != nil {
			t.Fatalf("resolving the server's arguments: %v", err)
		}
		argv = append(argv, resolved)
	}
	want := []string{"avenor", "mcp", "--bind-host", "poo", "--bind-session", "session-7"}
	if !slices.Equal(argv, want) {
		t.Errorf("the server starts as %q, want %q", argv, want)
	}

	feeds := set.Feeds("avenor")
	if len(feeds) != 1 {
		t.Fatalf("the server declares %d resource feeds, want the inbox alone", len(feeds))
	}
	inbox := feeds[0]
	if inbox.Name != "avenor-inbox" || inbox.Resource != "avenor://inbox" {
		t.Errorf("the feed is %q over %q, want avenor-inbox over avenor://inbox", inbox.Name, inbox.Resource)
	}
	if inbox.OnIdle != "wake" {
		t.Errorf("the feed's on_idle is %q, want wake", inbox.OnIdle)
	}
	if inbox.Filter == nil || !inbox.Filter.Script() {
		t.Fatalf("the feed's filter is %+v, want the jq program that shapes an inbox read", inbox.Filter)
	}
	if len(inbox.Filter.Command) < 2 || inbox.Filter.Command[0] != "jq" {
		t.Errorf("the feed's filter runs %q, want jq", inbox.Filter.Command)
	}
}

// theAvenorInboxPayload is one read of a bound server's inbox as it answers:
// three messages, one tell and two asks, one of which this run cannot answer.
const theAvenorInboxPayload = `{"messages":[` +
	`{"id":"msg-1","from":"child-alpha","text":"the build is green","expects_reply":false,"answerable":false},` +
	`{"id":"msg-2","from":"child-beta","text":"which branch should I use?","expects_reply":true,"answerable":true},` +
	`{"id":"msg-3","from":"child-gamma","text":"may I have the token?","expects_reply":true,"answerable":false}` +
	`]}`

// theEmptyInboxPayload is a read that drained nothing.
const theEmptyInboxPayload = `{"messages":[]}`

// filterFeedRecord is one record the resource feed hands the filter: the
// server's name as the sender, and the resource's content as the text.
func filterFeedRecord(payload string) map[string]string {
	return map[string]string{"from": "avenor", "text": payload}
}

// filterRecord is one record the filter wrote.
type filterRecord struct {
	From string `json:"from"`
	Text string `json:"text"`
}

// runExampleFilter runs the example's filter over one feed record and returns
// the records it wrote, the way the feed reads them: a stream of JSON values.
func runExampleFilter(t *testing.T, argv []string, record map[string]string) []filterRecord {
	t.Helper()
	input, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encoding the feed record: %v", err)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(append(input, '\n'))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the example's filter: %v: %s", err, stderr.String())
	}
	var records []filterRecord
	dec := json.NewDecoder(&stdout)
	for {
		var record filterRecord
		if err := dec.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decoding the filter's output %q: %v", stdout.String(), err)
		}
		records = append(records, record)
	}
	return records
}

// TestExamplesAvenorMCPInboxFilter covers the jq program the example's
// manifest carries: one inbox read becomes one record per message, a tell keeps
// its text, and an ask carries the call that answers it — or, for an ask this
// run cannot route, the line that says so. A read that drained nothing writes
// nothing, so it delivers no input.
func TestExamplesAvenorMCPInboxFilter(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed, and the example's filter is a jq program")
	}
	dir := theAvenorMCPExample(t)
	set, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("loading the example's manifest: %v", err)
	}
	feeds := set.Feeds("avenor")
	if len(feeds) != 1 || feeds[0].Filter == nil {
		t.Fatalf("the manifest declares no inbox filter: %+v", feeds)
	}
	argv := feeds[0].Filter.Command

	records := runExampleFilter(t, argv, filterFeedRecord(theAvenorInboxPayload))
	if len(records) != 3 {
		t.Fatalf("the filter wrote %d records, want one per message: %+v", len(records), records)
	}
	wantFrom := []string{"child-alpha", "child-beta", "child-gamma"}
	for i, from := range wantFrom {
		if records[i].From != from {
			t.Errorf("the record %d is from %q, want %q", i, records[i].From, from)
		}
	}

	// A tell is delivered as it arrived: the filter adds a footer to an ask
	// and nothing to anything else.
	if got := records[0].Text; got != "the build is green" {
		t.Errorf("the tell's text is %q, want it unchanged", got)
	}

	// An answerable ask tells the model which call answers it, by the
	// message's own id: two asks in one batch are answered apart.
	ask := records[1].Text
	if !strings.HasPrefix(ask, "which branch should I use?\n\n") {
		t.Errorf("the answerable ask's text is %q, want the message's own text first", ask)
	}
	reply := `To answer, call call_mcp with {"name": "avenor/message", "args": {"action": "reply", "reply_to": "msg-2", "message": "..."}}.`
	if !strings.HasSuffix(ask, reply) {
		t.Errorf("the answerable ask's text is %q, want it to end with %q", ask, reply)
	}

	// An ask this run cannot route says so, and names no reply call the
	// model would waste: there is nowhere for the answer to go.
	unanswerable := records[2].Text
	if !strings.HasPrefix(unanswerable, "may I have the token?\n\n") {
		t.Errorf("the unanswerable ask's text is %q, want the message's own text first", unanswerable)
	}
	if !strings.HasSuffix(unanswerable, `This message is an ask from "child-gamma", but it cannot be answered here.`) {
		t.Errorf("the unanswerable ask's text is %q, want it to end with the line that says it cannot be answered", unanswerable)
	}

	// An empty read is no input at all: the filter writes nothing, and a
	// feed that emits nothing queues nothing.
	if empty := runExampleFilter(t, argv, filterFeedRecord(theEmptyInboxPayload)); len(empty) != 0 {
		t.Errorf("the empty batch wrote %+v, want nothing", empty)
	}
}
