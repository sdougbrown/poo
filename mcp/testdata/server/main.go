// Command server is the MCP server the client's tests launch: it
// answers with the world it runs in, so a test can tell which session
// started it, in which directory, with which arguments and
// environment.
//
// It offers an echo tool that reports its text, the process's own
// arguments, its working directory and the environment values it is
// asked for; a tool that answers with the protocol's own error flag;
// a tool that bumps a counter and updates a resource; a tool that
// ends the process, so a test can watch a server go down mid-session;
// a tool that holds its call until a file appears, so a test can end
// the process under a call in flight; a tool whose description
// carries how many times this fixture has started, so a test can tell
// the listing a relaunch installed from the one it replaced; and a
// tool that pushes a message onto a drain queue and updates it, so a
// resource feed has something to deliver. The queue's resource reads
// destructively — everything the pushes queued comes back as one
// content each — so a test can tell a delivered message from a read
// one, and a blob resource stands beside it for a feed that reads
// text alone.
//
// The process writes its own pid to the file the environment names,
// so a test that closes the session can prove no process was left,
// and counts its own starts in the file another name points at, so a
// relaunch is visible in what the process lists. The environment may
// force the protocol revision the server negotiates down to the legacy
// one, so a test can run the same conversation on both revisions.
//
// The environment may also name a startup-failure counter: the process
// exits before it speaks, as many times as the counter says, and
// decrements it each time, so a test can force the relaunches that fail
// and the one that recovers.
//
// The environment may also name a generation from which the server
// advertises no resource subscriptions: the subscription handlers
// drop, and the initialize result lacks the capability, so a test can
// put a feed on a relaunch that cannot grant the subscription.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// counterURI is the resource a bump updates.
const counterURI = "fixture://counter"

// inboxURI is the drain queue a push appends to.
const inboxURI = "fixture://inbox"

// blobURI returns one blob content: a resource a feed that reads text
// skips.
const blobURI = "fixture://blob"

// legacyRevision is the protocol revision the environment's word
// "legacy" restricts the server to, so the client falls back to the
// initialize handshake and the subscription answer is the subscribe
// call's own.
const legacyRevision = "2025-11-25"

// count is the counter the bump tool advances.
var count atomic.Int64

// inbox is the queue the push tool appends to. A read drains it: the
// messages come back as one content each and leave, so a second read
// of the same batch answers with nothing.
var (
	inboxMu           sync.Mutex
	inbox             []string
	inboxReads        atomic.Int64
	inboxUnsubscribes atomic.Int64
	blobReads         atomic.Int64
)

// noArgsSchema is the input schema of a tool that takes nothing.
var noArgsSchema = json.RawMessage(`{"type":"object"}`)

// stampTool is the name of the tool whose description carries the
// generation, so a test can read which process listed it.
const stampTool = "stamp"

// holdTimeout bounds how long the hold tool waits for its release, so
// a test that never releases it leaves no handler behind; holdPoll is
// how often it looks.
const (
	holdTimeout = 30 * time.Second
	holdPoll    = 5 * time.Millisecond
)

// echoArgs are what the echo tool is called with.
type echoArgs struct {
	// Text is the message to answer with.
	Text string `json:"text"`
	// Env names the environment values to report.
	Env []string `json:"env,omitempty"`
}

// report is what the echo tool answers with.
type report struct {
	// Text is the message it was called with.
	Text string `json:"text"`
	// Args are the process's own arguments, as the launch resolved
	// them.
	Args []string `json:"args"`
	// Cwd is the directory the process runs in.
	Cwd string `json:"cwd"`
	// Env holds the named environment values, and a name the
	// environment does not hold reports as missing.
	Env map[string]string `json:"env"`
}

func main() {
	// The startup-failure counter comes first: a process told to fail
	// its startup writes no pid and speaks nothing, so the pid file a
	// test reads is only a server that started.
	if path := os.Getenv("POO_FIXTURE_FAIL_STARTS"); path != "" {
		if left := readStarts(path); left > 0 {
			writeStarts(path, left-1)
			os.Exit(1)
		}
	}

	// The pid file is how a test proves the process ended: the file is
	// written before the server speaks, and read by the test that
	// started the process.
	if path := os.Getenv("POO_FIXTURE_PIDFILE"); path != "" {
		_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
	}

	// The generation is how many times this fixture has started: a
	// relaunch is a new process, so the count is what a test reads back
	// out of the listing to tell one generation's tools from the next.
	generation := 0
	if path := os.Getenv("POO_FIXTURE_GENERATION"); path != "" {
		generation = readStarts(path) + 1
		writeStarts(path, generation)
	}

	// The subscription handlers are the server's own bookkeeping: the
	// SDK tracks the subscriptions itself, and a resource update
	// reaches only the sessions that subscribed. The unsubscribe
	// handler counts, so a test can prove a feed ended its
	// subscription. The protocol revision is the environment's word:
	// a server restricted to the legacy revision answers the modern
	// discovery with the versions it holds, and the client falls back
	// to the legacy handshake. A generation the drop-subscribe word
	// has passed holds neither handler: the SDK advertises no resource
	// subscriptions without them.
	opts := &sdkmcp.ServerOptions{}
	if from, err := strconv.Atoi(os.Getenv("POO_FIXTURE_DROP_SUBSCRIBE")); err != nil || generation < from {
		opts.SubscribeHandler = func(context.Context, *sdkmcp.SubscribeRequest) error { return nil }
		opts.UnsubscribeHandler = func(context.Context, *sdkmcp.UnsubscribeRequest) error { inboxUnsubscribes.Add(1); return nil }
	}
	if os.Getenv("POO_FIXTURE_PROTOCOL") == "legacy" {
		opts.SupportedProtocolVersions = []string{legacyRevision}
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "test"}, opts)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "answers with the world the server runs in"}, func(_ context.Context, req *sdkmcp.CallToolRequest, args echoArgs) (*sdkmcp.CallToolResult, any, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		env := make(map[string]string, len(args.Env))
		for _, name := range args.Env {
			value, ok := os.LookupEnv(name)
			if !ok {
				value = "<unset>"
			}
			env[name] = value
		}
		data, err := json.Marshal(report{Text: args.Text, Args: os.Args, Cwd: cwd, Env: env})
		if err != nil {
			return nil, nil, err
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
	server.AddTool(&sdkmcp.Tool{Name: "fail", Description: "answers with the protocol's own error flag", InputSchema: noArgsSchema}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{
			IsError: true,
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "the tool refused"}},
		}, nil
	})
	server.AddTool(&sdkmcp.Tool{Name: "bump", Description: "advances the counter and updates the resource", InputSchema: noArgsSchema}, func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		n := count.Add(1)
		if err := server.ResourceUpdated(ctx, &sdkmcp.ResourceUpdatedNotificationParams{URI: counterURI}); err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("%d", n)}},
		}, nil
	})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "push", Description: "queues a message on the inbox resource and updates it"}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args pushArgs) (*sdkmcp.CallToolResult, any, error) {
		inboxMu.Lock()
		inbox = append(inbox, args.Text)
		inboxMu.Unlock()
		if err := server.ResourceUpdated(ctx, &sdkmcp.ResourceUpdatedNotificationParams{URI: inboxURI}); err != nil {
			return nil, nil, err
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: strconv.Itoa(len(args.Text))}},
		}, nil, nil
	})
	server.AddTool(&sdkmcp.Tool{Name: "stats", Description: "answers the counters a test synchronizes on", InputSchema: noArgsSchema}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		inboxMu.Lock()
		queued := len(inbox)
		inboxMu.Unlock()
		data, err := json.Marshal(struct {
			Reads        int64 `json:"reads"`
			BlobReads    int64 `json:"blob_reads"`
			Unsubscribes int64 `json:"unsubscribes"`
			Queued       int   `json:"queued"`
		}{Reads: inboxReads.Load(), BlobReads: blobReads.Load(), Unsubscribes: inboxUnsubscribes.Load(), Queued: queued})
		if err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
		}, nil
	})
	server.AddTool(&sdkmcp.Tool{Name: "exit", Description: "ends the process without answering", InputSchema: noArgsSchema}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		os.Exit(3)
		return nil, nil
	})
	server.AddTool(&sdkmcp.Tool{Name: stampTool, Description: fmt.Sprintf("generation %d of this fixture server", generation), InputSchema: noArgsSchema}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: strconv.Itoa(generation)}},
		}, nil
	})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "hold", Description: "holds its call until the release file exists, so a test can end the process under a call in flight"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args holdArgs) (*sdkmcp.CallToolResult, any, error) {
		if args.Entered != "" {
			if err := os.WriteFile(args.Entered, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
				return nil, nil, err
			}
		}
		deadline := time.Now().Add(holdTimeout)
		for {
			if _, err := os.Stat(args.Release); err == nil {
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "released"}},
				}, nil, nil
			}
			if time.Now().After(deadline) {
				return nil, nil, fmt.Errorf("nothing released the call within %v", holdTimeout)
			}
			time.Sleep(holdPoll)
		}
	})
	server.AddResource(&sdkmcp.Resource{
		URI:         counterURI,
		Name:        "counter",
		Description: "the count the bump tool advanced",
		MIMEType:    "text/plain",
	}, func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		return &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{{
				URI:      counterURI,
				MIMEType: "text/plain",
				Text:     fmt.Sprintf("%d", count.Load()),
			}},
		}, nil
	})

	server.AddResource(&sdkmcp.Resource{
		URI:         inboxURI,
		Name:        "inbox",
		Description: "the messages the push tool queued, drained by a read",
		MIMEType:    "text/plain",
	}, func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		inboxMu.Lock()
		items := inbox
		inbox = nil
		inboxMu.Unlock()
		inboxReads.Add(1)
		contents := make([]*sdkmcp.ResourceContents, 0, len(items))
		for _, item := range items {
			contents = append(contents, &sdkmcp.ResourceContents{
				URI:      inboxURI,
				MIMEType: "text/plain",
				Text:     item,
			})
		}
		return &sdkmcp.ReadResourceResult{Contents: contents}, nil
	})
	server.AddResource(&sdkmcp.Resource{
		URI:         blobURI,
		Name:        "blob",
		Description: "one blob content, for a feed that reads text alone",
		MIMEType:    "application/octet-stream",
	}, func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		blobReads.Add(1)
		return &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{{
				URI:      blobURI,
				MIMEType: "application/octet-stream",
				Blob:     []byte{0},
			}},
		}, nil
	})

	if err := server.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil {
		os.Exit(1)
	}
}

// pushArgs are what the push tool is called with.
type pushArgs struct {
	// Text is the message it queues.
	Text string `json:"text"`
}

// holdArgs are what the hold tool is called with.
type holdArgs struct {
	// Entered is the file the tool writes when the call reached it, so
	// a test knows the call is in flight.
	Entered string `json:"entered"`
	// Release is the file the tool waits for.
	Release string `json:"release"`
}

// readStarts reads the startup-failure counter a path holds. A file
// that is not there, or holds no number, leaves nothing to fail.
func readStarts(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	left, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return left
}

// writeStarts stores the counter a path holds, best effort: a counter
// that cannot be written says nothing on a next start, and the test
// that wrote the first word owns the file.
func writeStarts(path string, left int) {
	_ = os.WriteFile(path, []byte(strconv.Itoa(left)), 0o600)
}
