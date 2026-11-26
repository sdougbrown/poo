// Command server is the MCP server the client's tests launch: it
// answers with the world it runs in, so a test can tell which session
// started it, in which directory, with which arguments and
// environment.
//
// It offers an echo tool that reports its text, the process's own
// arguments, its working directory and the environment values it is
// asked for; a tool that answers with the protocol's own error flag;
// a tool that bumps a counter and updates a resource; and a tool that
// ends the process, so a test can watch a server go down mid-session.
//
// The process writes its own pid to the file the environment names,
// so a test that closes the session can prove no process was left.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// counterURI is the resource a bump updates.
const counterURI = "fixture://counter"

// count is the counter the bump tool advances.
var count atomic.Int64

// noArgsSchema is the input schema of a tool that takes nothing.
var noArgsSchema = json.RawMessage(`{"type":"object"}`)

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
	// The pid file is how a test proves the process ended: the file is
	// written before the server speaks, and read by the test that
	// started the process.
	if path := os.Getenv("POO_FIXTURE_PIDFILE"); path != "" {
		_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
	}

	// The subscription handlers are the server's own bookkeeping: the
	// SDK tracks the subscriptions itself, and a resource update
	// reaches only the sessions that subscribed.
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "test"}, &sdkmcp.ServerOptions{
		SubscribeHandler:   func(context.Context, *sdkmcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *sdkmcp.UnsubscribeRequest) error { return nil },
	})
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
	server.AddTool(&sdkmcp.Tool{Name: "exit", Description: "ends the process without answering", InputSchema: noArgsSchema}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		os.Exit(3)
		return nil, nil
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

	if err := server.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil {
		os.Exit(1)
	}
}
