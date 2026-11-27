// Package mcp is poo's MCP client: it starts the stdio servers a
// session's profile selected, owns them for the length of that
// session, and routes the calls a session makes into them.
//
// A server is a session's own process. It starts when the session is
// built, in the directory the session works in, with the session's
// own id resolved into the manifest's templates, and it stops when the
// session ends. Nothing here starts a server on its own: the caller
// hands over the declarations a profile selected, and the launch is
// the caller's word.
//
// The servers run as the operator's trusted host processes, the way
// the session's feed commands do: not through an executor or a
// sandbox, because the transport needs the process handle itself, and
// with the host environment scrubbed by the rules the session's
// commands are scrubbed with, plus the manifest's own environment
// entries. A manifest's explicit ${NAME} resolves against the host
// environment, so it may reintroduce a scrubbed variable; that is the
// operator's choice.
//
// The package is the one place the go-sdk is imported. The settings
// and the core never see it: a binary links this package through the
// launcher it hands the build, and a binary that links no launcher
// never links the SDK at all.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	osexec "os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/mcp/manifest"

	// The go-sdk's mcp package is aliased everywhere: this package is
	// an mcp too, and its own name would read as its own.
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClientName is the name the client introduces itself with.
const ClientName = "poo"

// DefaultStartupTimeout bounds one server's startup: the launch, the
// handshake and the tool listing, together. A server that takes
// longer than this never becomes the session's.
const DefaultStartupTimeout = 30 * time.Second

// defaultClientVersion stands in when the caller named none. The
// launcher the binary links always names the build's own version.
const defaultClientVersion = "dev"

// The errors a call reports. A caller tells them apart with errors.Is.
var (
	// ErrUnknownTool means the qualified name names no server the
	// session started, or no tool that server listed.
	ErrUnknownTool = errors.New("mcp: no such tool")
	// ErrServerDown means the server's process has ended, so the call
	// never left the session.
	ErrServerDown = errors.New("mcp: the server is not running")
)

// LaunchError is the failure of one server's launch.
//
// The launch is bounded and transactional: a failure leaves no process
// behind, whatever the step that failed, and the name says which
// selected server did not start.
type LaunchError struct {
	// Server is the name the manifest declared.
	Server string
	// Err is what went wrong.
	Err error
}

// Error names the server and says what went wrong.
func (e *LaunchError) Error() string {
	return fmt.Sprintf("the mcp server %q did not start: %v", e.Server, e.Err)
}

// Unwrap returns what went wrong.
func (e *LaunchError) Unwrap() error { return e.Err }

// LaunchOptions are the values a session's servers launch with.
type LaunchOptions struct {
	// SessionID is the session's own identifier: the value a manifest's
	// ${session:id} resolves to.
	SessionID string
	// Dir is the directory the servers run in, the session's own
	// working directory.
	Dir string
	// HostEnv looks a variable up in the host environment, for a
	// manifest's ${NAME} references. A nil HostEnv resolves no
	// reference.
	HostEnv func(string) (string, bool)
	// Scrub is the rule the host environment is scrubbed with. The
	// servers run with the scrubbed environment plus the manifest's own
	// entries. Explicit ${NAME} references resolve through HostEnv, so a
	// manifest may reintroduce a name the rule scrubbed.
	Scrub exec.Scrub
	// Servers are the selected servers, in selection order. Nothing
	// starts without one.
	Servers []manifest.Server
	// StartupTimeout bounds one server's startup. Zero or less means
	// the DefaultStartupTimeout.
	StartupTimeout time.Duration
	// ClientVersion is the build version the client introduces itself
	// with. The empty value means the default.
	ClientVersion string
}

// Group is the MCP servers one session started, by name.
//
// A group is built by Launch or Connect and owned by one session: the
// session closes it when it ends, and no other writer touches it. The
// methods are safe to call from any goroutine, so a feed's runner and
// a tool call may reach the same group at once.
type Group struct {
	// mu guards the group's own state: the server table and the closed
	// flag. A server's session is fixed when the group is built, and a
	// server's own mutable state belongs to the server.
	mu      sync.Mutex
	closed  bool
	servers []*serverState
	byName  map[string]*serverState
}

// serverState is one running server: its session, the tools it listed
// at startup, and the notifications it sends.
type serverState struct {
	name    string
	session *sdkmcp.ClientSession
	tools   []Tool
	// result is the handshake's answer: the capabilities the server
	// advertised. The resource feeds read it before they subscribe.
	result *sdkmcp.InitializeResult
	// waiters routes the server's resource notifications to the
	// per-URI waiters the resource feeds register.
	waiters resourceWaiters

	// stateMu guards the server's mutable state.
	stateMu sync.Mutex
	// down reports that the server's process ended. A call that sees it
	// never leaves the session; a call that does not may still find it
	// gone, and the failure it gets is the transport's own.
	down bool
}

// Tool is one tool a server listed, as the session sees it. The values
// are copies: a caller may read them after the server is gone.
type Tool struct {
	// Name is the tool's own name, unqualified. A call qualifies it
	// with the server's.
	Name string
	// Description is what the server said the tool does.
	Description string
	// InputSchema is the tool's input schema, as the server sent it.
	InputSchema json.RawMessage
	// Annotations are the hints the server attached to the tool. They
	// are the server's own claims about itself, never a permission.
	Annotations ToolAnnotations
}

// ToolAnnotations are the hints a server attached to a tool, in the
// shape the callers read. The protocol's omitted booleans become the
// protocol's own defaults, and the protocol documents them: a hint the
// server did not name reads as the protocol says — destructive and
// open-world default to true, read-only and idempotent to false.
type ToolAnnotations struct {
	// Title is the tool's human name.
	Title string
	// ReadOnlyHint says the tool does not modify its environment.
	ReadOnlyHint bool
	// DestructiveHint says the tool may perform destructive updates.
	DestructiveHint bool
	// IdempotentHint says repeated calls have no further effect.
	IdempotentHint bool
	// OpenWorldHint says the tool interacts with outside entities.
	OpenWorldHint bool
}

// CallResult is what a tool call returned.
type CallResult struct {
	// IsError reports the server's own error: the call reached the
	// server and the tool ran, and the tool's answer is that it failed.
	// The call itself is a success, so the error text travels in the
	// contents like any other answer.
	IsError bool
	// Text holds the text contents the server returned, in order.
	Text []string
	// Images holds the image contents the server returned, in order.
	Images []CallImage
	// Structured is the structured content the server returned, or nil
	// when it returned none.
	Structured json.RawMessage
}

// CallImage is one image content a tool call returned.
type CallImage struct {
	// MediaType is the image's MIME type.
	MediaType string
	// Data is the image's bytes.
	Data []byte
}

// Launch starts the selected servers as this session's own processes.
//
// Each server's templates resolve here, never at load: the values a
// launch resolves are the session's. A server starts with the host
// environment scrubbed by the options' rule, plus the manifest's own
// environment entries, in the directory the options name, and its
// startup — the launch, the handshake and the tool listing — is
// bounded by the options' timeout under the context the session
// handed over, never by a request's own context.
//
// The launch is transactional: a server that fails names itself in the
// returned LaunchError, and every server that started before it stops,
// so a failed launch owns no process. A launch that succeeds owns
// them all, and the caller closes the group when the session ends.
func Launch(ctx context.Context, opts LaunchOptions) (*Group, error) {
	timeout := opts.StartupTimeout
	if timeout <= 0 {
		timeout = DefaultStartupTimeout
	}
	g := &Group{byName: map[string]*serverState{}}
	for _, server := range opts.Servers {
		state, err := launchServer(ctx, opts, server, timeout)
		if err != nil {
			_ = g.Close()
			return nil, err
		}
		g.add(state)
	}
	return g, nil
}

// Connect builds a one-server group over a transport the caller owns.
//
// The transport is any the SDK speaks — the in-memory pair a test
// uses, or a stream a consumer drives itself. The handshake runs under
// the caller's context, and the group is closed by the caller, the way
// a launched one is.
func Connect(ctx context.Context, name string, t sdkmcp.Transport) (*Group, error) {
	state, err := connectServer(ctx, name, defaultClientVersion, t)
	if err != nil {
		return nil, err
	}
	g := &Group{byName: map[string]*serverState{name: state}}
	g.add(state)
	return g, nil
}

// add puts a started server in the group. The group is being built,
// so no lock is taken: nothing else has seen it yet.
func (g *Group) add(s *serverState) {
	g.servers = append(g.servers, s)
	g.byName[s.name] = s
}

// launchServer starts one declared server and lists its tools.
func launchServer(ctx context.Context, opts LaunchOptions, server manifest.Server, timeout time.Duration) (*serverState, error) {
	name := server.Name
	fail := func(err error) (*serverState, error) {
		return nil, &LaunchError{Server: name, Err: err}
	}

	// The templates resolve here, from the session's own values: a
	// command that runs with an empty argument or an empty environment
	// value is worse than a server that does not launch, so a reference
	// that does not resolve fails the launch.
	command, err := server.Command.Resolve(opts.HostEnv, opts.SessionID)
	if err != nil {
		return fail(err)
	}
	argv := make([]string, 0, len(server.Args)+1)
	argv = append(argv, command)
	for _, arg := range server.Args {
		resolved, err := arg.Resolve(opts.HostEnv, opts.SessionID)
		if err != nil {
			return fail(err)
		}
		argv = append(argv, resolved)
	}
	declared := make(map[string]string, len(server.Env))
	for key, template := range server.Env {
		value, err := template.Resolve(opts.HostEnv, opts.SessionID)
		if err != nil {
			return fail(err)
		}
		declared[key] = value
	}

	cmd := osexec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = mergeEnv(exec.ScrubEnv(nil, opts.Scrub), declared)

	// The startup runs under its own bound, so a server that never
	// finishes its handshake cannot hold the session's build hostage.
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	state, err := connectServer(sctx, name, opts.ClientVersion, &sdkmcp.CommandTransport{Command: cmd})
	if err != nil {
		return fail(err)
	}
	return state, nil
}

// connectServer connects one server over a transport, lists its tools
// and starts watching its process.
//
// The client's notification routing is installed before the connection
// is made, so a notification that races the subscribe round trip still
// reaches the waiter it belongs to.
func connectServer(ctx context.Context, name, version string, t sdkmcp.Transport) (*serverState, error) {
	state := &serverState{name: name}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: ClientName, Version: clientVersion(version)}, &sdkmcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *sdkmcp.ResourceUpdatedNotificationRequest) {
			if req.Params != nil {
				state.waiters.updated(req.Params.URI)
			}
		},
	})
	// The acknowledged notification has no handler of its own — the
	// SDK's default discards it — so the middleware is the seam it
	// reaches the waiters through. The middleware passes every message
	// on, so the SDK's own handling, the read cache's invalidation
	// among them, still runs.
	client.AddReceivingMiddleware(routeAcknowledged(&state.waiters))

	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, err
	}
	state.session = session
	state.result = session.InitializeResult()
	if err := state.listTools(ctx); err != nil {
		_ = session.Close()
		return nil, err
	}
	// The watcher reports the server's own end. A call that arrives
	// after it never left the session; one that raced it gets the
	// transport's own failure. Closing the group ends the watcher too,
	// and a late report on a closed group tells no one anything.
	go func() {
		_ = session.Wait()
		state.stateMu.Lock()
		state.down = true
		state.stateMu.Unlock()
	}()
	return state, nil
}

// clientVersion returns the version the client introduces itself with.
func clientVersion(version string) string {
	if version == "" {
		return defaultClientVersion
	}
	return version
}

// listTools pages the server's tools and keeps what a caller reads:
// the name, the description, the input schema and the annotations.
func (s *serverState) listTools(ctx context.Context) error {
	var tools []Tool
	cursor := ""
	for {
		res, err := s.session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return err
		}
		for _, tool := range res.Tools {
			converted, err := convertTool(tool)
			if err != nil {
				return err
			}
			tools = append(tools, converted)
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	s.tools = tools
	return nil
}

// convertTool keeps one listed tool's caller-facing values.
func convertTool(tool *sdkmcp.Tool) (Tool, error) {
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return Tool{}, fmt.Errorf("the tool %q: %w", tool.Name, err)
	}
	out := Tool{Name: tool.Name, Description: tool.Description, InputSchema: schema}
	if tool.Annotations != nil {
		// The protocol's omitted booleans are its own defaults, and the
		// protocol documents them: destructive and open-world default
		// to true, so a destructive tool that said nothing reads as
		// destructive.
		out.Annotations = ToolAnnotations{
			Title:           tool.Annotations.Title,
			ReadOnlyHint:    tool.Annotations.ReadOnlyHint,
			DestructiveHint: tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint,
			IdempotentHint:  tool.Annotations.IdempotentHint,
			OpenWorldHint:   tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint,
		}
	}
	return out, nil
}

// Tools returns the tools the named server listed at startup, and
// whether the group holds that server.
func (g *Group) Tools(server string) ([]Tool, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.byName[server]
	if s == nil {
		return nil, false
	}
	return append([]Tool(nil), s.tools...), true
}

// CallTool calls one tool on one server.
//
// The name is qualified: <server>/<tool>. An unknown server or tool
// reports ErrUnknownTool, and a server whose process has ended
// reports ErrServerDown, so the call never left the session. A call
// that reached the server is never retried, whatever it returned: the
// call may have had effects, and a repeat is the caller's word. A
// transport failure mid-call is returned wrapped, and the server's own
// error answer is a successful return whose CallResult carries
// IsError.
func (g *Group) CallTool(ctx context.Context, qualified string, args json.RawMessage) (CallResult, error) {
	server, tool, ok := strings.Cut(qualified, "/")
	if !ok {
		return CallResult{}, fmt.Errorf("%w: %q", ErrUnknownTool, qualified)
	}
	g.mu.Lock()
	s := g.byName[server]
	g.mu.Unlock()
	if s == nil || !s.holds(tool) {
		return CallResult{}, fmt.Errorf("%w: %q", ErrUnknownTool, qualified)
	}
	if s.isDown() {
		return CallResult{}, fmt.Errorf("%w: %s", ErrServerDown, server)
	}
	res, err := s.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return CallResult{}, fmt.Errorf("the mcp server %q: %w", server, err)
	}
	return callResult(res), nil
}

// holds reports whether the server listed the tool.
func (s *serverState) holds(tool string) bool {
	for _, held := range s.tools {
		if held.Name == tool {
			return true
		}
	}
	return false
}

// isDown reports whether the server's process has ended.
func (s *serverState) isDown() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.down
}

// callResult keeps a call's answer: the text, the images and the
// structured content, with the server's own error flag.
func callResult(res *sdkmcp.CallToolResult) CallResult {
	out := CallResult{IsError: res.IsError}
	if res.StructuredContent != nil {
		if data, err := json.Marshal(res.StructuredContent); err == nil {
			out.Structured = data
		}
	}
	for _, content := range res.Content {
		switch content := content.(type) {
		case *sdkmcp.TextContent:
			out.Text = append(out.Text, content.Text)
		case *sdkmcp.ImageContent:
			out.Images = append(out.Images, CallImage{MediaType: content.MIMEType, Data: content.Data})
		}
	}
	return out
}

// Close stops every server the group started.
//
// Each server's session closes its own process: the transport owns
// the waiting, the signalling and the killing, so nothing here waits
// on a process a second time. Close is idempotent, and the errors of
// the servers that did not stop are returned joined.
func (g *Group) Close() error {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil
	}
	g.closed = true
	servers := append([]*serverState(nil), g.servers...)
	g.mu.Unlock()
	var errs []error
	for _, s := range servers {
		if err := s.session.Close(); err != nil {
			errs = append(errs, fmt.Errorf("the mcp server %q: %w", s.name, err))
		}
	}
	return errors.Join(errs...)
}

// mergeEnv overlays the manifest's declared entries on a scrubbed
// base. A declared name replaces the base's entry in place, and a
// name the base does not hold is appended, in name order, so the
// environment is the same whatever the map's order was.
func mergeEnv(base []string, declared map[string]string) []string {
	if len(declared) == 0 {
		return base
	}
	seen := make(map[string]bool, len(declared))
	out := make([]string, 0, len(base)+len(declared))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if value, ok := declared[name]; ok {
			out = append(out, name+"="+value)
			seen[name] = true
			continue
		}
		out = append(out, kv)
	}
	for _, name := range slices.Sorted(maps.Keys(declared)) {
		if seen[name] {
			continue
		}
		out = append(out, name+"="+declared[name])
	}
	return out
}
