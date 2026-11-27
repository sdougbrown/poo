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
// A server whose process ends is brought back. One goroutine per
// server owns its transitions — ready when the process runs, down
// while a relaunch is due, failed once the relaunch budget is spent,
// closed for good when the session ends — and every reader takes a
// snapshot under the server's lock. A successful relaunch swaps the
// session, the tools and the notification routing in one move and
// counts a new generation, so a call, a search and a feed all reach the
// process the session runs now; the feed's own restart policy brings
// its next attempt on that generation.
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
	"io"
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
	"github.com/sdougbrown/poo/mcp/wire"
)

// ClientName is the name the client introduces itself with.
const ClientName = "poo"

// DefaultStartupTimeout bounds one server's startup: the launch, the
// handshake and the tool listing, together. A server that takes
// longer than this never becomes the session's.
const DefaultStartupTimeout = 30 * time.Second

// DefaultRelaunchBackoff is the wait before a server's first relaunch
// attempt. Each further failure doubles the wait, up to
// relaunchBackoffMax.
const DefaultRelaunchBackoff = time.Second

// relaunchBackoffMax caps the doubling wait between a server's
// relaunch attempts.
const relaunchBackoffMax = 30 * time.Second

// DefaultRelaunchStability is how long a generation must run before
// its exit stops counting against the relaunch budget. A generation
// that ends sooner is a server that starts and dies at once, and its
// exit spends the budget the way a launch that failed does, so a crash
// loop reaches failed instead of relaunching for the session's whole
// life; one that runs past the window clears the count, so a server
// that worked for a while is given the whole budget again.
const DefaultRelaunchStability = time.Minute

// relaunchBudget is how many consecutive failures a down server may
// spend before it is failed for good, a failed launch and a generation
// that ended inside the stability window alike: a server that cannot
// come back in five tries is one the session runs without, and the
// feeds and calls that need it read it down.
const relaunchBudget = 5

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
	// RelaunchBackoff is the wait before a server's first relaunch
	// attempt after its process ends, doubling on each failure up to
	// the cap. Zero or less means the DefaultRelaunchBackoff.
	RelaunchBackoff time.Duration
	// RelaunchStability is how long a generation must run before its
	// exit stops counting against the relaunch budget. Zero or less
	// means the DefaultRelaunchStability.
	RelaunchStability time.Duration
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
	// flag. A server's session is swapped under the server's own lock,
	// and a server's mutable state belongs to its watcher.
	mu      sync.Mutex
	closed  bool
	servers []*serverState
	byName  map[string]*serverState

	// ctx is the lifetime every generation's context derives from.
	// Close cancels it, so a startup in flight ends with the close.
	ctx    context.Context
	cancel context.CancelFunc
	// closeCh is closed once, on Close: it wakes every watcher that is
	// waiting out a relaunch backoff, so no relaunch follows the close.
	closeCh chan struct{}
	// backoff is the wait before a server's first relaunch attempt.
	backoff time.Duration
	// stability is how long a generation must run before its exit
	// stops counting against the relaunch budget.
	stability time.Duration
	// done is closed when the close finishes, and closeErr is the
	// result it recorded first: a caller that arrives while the close
	// runs waits on done and reads the same answer, rather than
	// reporting servers that are still stopping as stopped.
	done     chan struct{}
	closeErr error
}

// serverPhase is one server's state in the relaunch cycle: ready while
// its process runs, down while a relaunch is due, failed once its
// budget of relaunches is spent, and closed for good when the group
// closes. Closed is terminal — the session is ending, and nothing
// brings a closed server back.
type serverPhase int

// The phases a server moves through. A group's close is the one
// transition outside the watcher: it moves a server straight to
// closed, whatever the phase was.
const (
	phaseReady serverPhase = iota
	phaseDown
	phaseFailed
	phaseClosed
)

// String names the phase, so a test's failure says what it read.
func (p serverPhase) String() string {
	switch p {
	case phaseReady:
		return "ready"
	case phaseDown:
		return "down"
	case phaseFailed:
		return "failed"
	case phaseClosed:
		return "closed"
	}
	return "unknown"
}

// serverState is one server the group holds: its identity, the
// declaration it relaunches from, and the generation it runs now.
//
// One goroutine — the watcher — owns every phase and every generation
// install; the group's close is the one other transition, and it is
// terminal. Every reader takes a snapshot under the lock, and the
// generation a snapshot holds is immutable from its install to its
// retirement, so the reader needs no lock of its own.
type serverState struct {
	// name is the server's declared name.
	name string
	// decl is the server's declaration, its templates unresolved: every
	// launch resolves them from the session's own values, the way the
	// first one did.
	decl manifest.Server
	// info is the launch's own values, the ones every generation of
	// this server starts with.
	info launchInfo
	// watchDone is closed when the watcher exits, so a close can wait
	// for the last transition to finish.
	watchDone chan struct{}

	// stateMu guards the server's mutable state: the phase and the
	// generation the server runs.
	stateMu sync.Mutex
	// phase is the server's state in the relaunch cycle.
	phase serverPhase
	// generation is how many generations the server has installed: a
	// relaunch that succeeds increments it, and a reader's snapshot
	// carries it, so a reader can tell which generation it holds.
	generation int
	// current is the generation the server runs. It is nil only before
	// the first install, which the group's build completes before any
	// reader exists.
	current *generation
	// noRelaunch marks a server the group cannot bring back: Connect
	// builds one over a transport the caller owns, so the group holds
	// no process and no declaration to launch again. The watcher
	// treats its end as terminal — the generation retires and the
	// phase is failed — instead of launching from a zero declaration.
	noRelaunch bool
	// failNoRelaunch records why a failed server failed: a no-relaunch
	// server lost the transport it ran on and no relaunch ever ran, so
	// a suspended feed's report names the server gone instead of a
	// crash loop it never ran. The budget-spent failure clears it.
	failNoRelaunch bool
	// failures is how many consecutive failures the server has spent:
	// a launch that failed, or a generation that ended inside the
	// stability window. The watcher is its only reader and writer, and
	// a generation that ran past the window clears it.
	failures int
}

// launchInfo is the launch's own values: the ones every generation of
// one server starts with, held once so a relaunch launches the way the
// first launch did.
type launchInfo struct {
	// sessionID is the session's own identifier.
	sessionID string
	// dir is the directory the process runs in.
	dir string
	// hostEnv resolves a manifest's ${NAME} references.
	hostEnv func(string) (string, bool)
	// scrub is the rule the host environment is scrubbed with.
	scrub exec.Scrub
	// version is the version the client introduces itself with.
	version string
	// timeout bounds one generation's startup.
	timeout time.Duration
}

// generation is one process's client side: the session the group speaks
// on, the notifications that process routes, the tools it listed, and
// the context that ends when the generation is retired.
//
// A generation is immutable from its install to its retirement: the
// swap that retires one installs the next under the server's lock, so
// a reader that holds one reads its values without taking that lock,
// and a late notification on a retired generation's connection reaches
// only that generation's waiters — waiters whose attempts the
// generation's cancelled context has already ended.
type generation struct {
	// session is the client session on the process's transport. Its
	// Close is the only reaper of the process it owns: the go-sdk
	// waits for the child there and nowhere else, so a generation that
	// ends is closed by whoever ends it, and Close is safe to call
	// again.
	session *sdkmcp.ClientSession
	// waiters routes the process's resource notifications to the
	// per-URI waiters the resource feeds register.
	waiters *resourceWaiters
	// result is the handshake's answer: the capabilities the process
	// advertised. The resource feeds read it before they subscribe.
	result *sdkmcp.InitializeResult
	// tools are the tools the process listed at startup.
	tools []Tool
	// started is when the process came up: its handshake answered. The
	// stability window runs from here, so a server that takes its whole
	// startup bound to speak is not credited with that time as life.
	started time.Time
	// ctx ends when the generation is retired — its process exited, or
	// the group closed. The calls and the feed attempts that started
	// on the generation end with it: a call reports the server down
	// instead of leaving again, and a feed's restart policy brings its
	// next attempt on the generation the group runs then.
	ctx context.Context
	// cancel ends ctx. It belongs to whoever retires the generation.
	cancel context.CancelFunc
}

// serverSnapshot is one server's state as a reader sees it, taken
// under the lock in one move: the phase, the generation count, and the
// generation the phase describes. A reader that holds a snapshot reads
// one generation's values without further locking, for a generation
// never changes after its install.
type serverSnapshot struct {
	// phase is the server's state.
	phase serverPhase
	// generation is how many generations the server has installed.
	generation int
	// gen is the generation the server runs.
	gen *generation
	// failNoRelaunch carries the failed server's cause: whether it
	// failed without a relaunch ever running.
	failNoRelaunch bool
}

// The vocabulary a server's tools and answers travel in is the wire's own:
// the client copies the server's values into these shapes, and the readers
// over them name the same ones without linking this package.
type (
	Tool = wire.Tool
	// ToolAnnotations are the hints a server attached to a tool.
	ToolAnnotations = wire.ToolAnnotations
	// CallResult is what a tool call returned.
	CallResult = wire.CallResult
	// CallImage is one image content a tool call returned.
	CallImage = wire.CallImage
)

// CallResult is what a tool call returned.

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
// so a failed launch owns no process. A launch that succeeds owns them
// all: each server's watcher brings it back when its process ends,
// within the relaunch budget, and the caller closes the group when the
// session ends.
func Launch(ctx context.Context, opts LaunchOptions) (*Group, error) {
	timeout := opts.StartupTimeout
	if timeout <= 0 {
		timeout = DefaultStartupTimeout
	}
	backoff := opts.RelaunchBackoff
	if backoff <= 0 {
		backoff = DefaultRelaunchBackoff
	}
	stability := opts.RelaunchStability
	if stability <= 0 {
		stability = DefaultRelaunchStability
	}
	g := &Group{
		byName:    map[string]*serverState{},
		closeCh:   make(chan struct{}),
		backoff:   backoff,
		stability: stability,
		done:      make(chan struct{}),
	}
	g.ctx, g.cancel = context.WithCancel(ctx)
	for _, server := range opts.Servers {
		state := &serverState{
			name:      server.Name,
			decl:      server,
			info:      launchInfo{opts.SessionID, opts.Dir, opts.HostEnv, opts.Scrub, clientVersion(opts.ClientVersion), timeout},
			watchDone: make(chan struct{}),
		}
		gen, err := g.nextGeneration(state)
		if err != nil {
			_ = g.Close()
			return nil, &LaunchError{Server: server.Name, Err: err}
		}
		if !state.setGeneration(gen) {
			// The group cannot close while it is being built — no one
			// else holds it — but the install answers for the
			// generation it refuses, all the same.
			_ = gen.session.Close()
			gen.cancel()
			_ = g.Close()
			return nil, &LaunchError{Server: server.Name, Err: errors.New("the group closed during the launch")}
		}
		g.add(state)
		go g.watch(state)
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
	g := &Group{
		byName:    map[string]*serverState{},
		closeCh:   make(chan struct{}),
		backoff:   DefaultRelaunchBackoff,
		stability: DefaultRelaunchStability,
		done:      make(chan struct{}),
	}
	g.ctx, g.cancel = context.WithCancel(ctx)
	state := &serverState{
		name:       name,
		info:       launchInfo{version: defaultClientVersion, timeout: DefaultStartupTimeout},
		watchDone:  make(chan struct{}),
		noRelaunch: true,
	}
	// The handshake runs under the group's lifetime, the way a launched
	// server's startup does, but over the transport the caller handed
	// over: nothing here starts a process of its own.
	genCtx, cancel := context.WithCancel(g.ctx)
	gen, err := connectGeneration(genCtx, defaultClientVersion, t)
	if err != nil {
		cancel()
		_ = g.Close()
		return nil, err
	}
	gen.ctx, gen.cancel = genCtx, cancel
	if !state.setGeneration(gen) {
		_ = gen.session.Close()
		gen.cancel()
		_ = g.Close()
		return nil, errors.New("the group closed during the connect")
	}
	g.add(state)
	go g.watch(state)
	return g, nil
}

// add puts a started server in the group. The group is being built,
// so no lock is taken: nothing else has seen it yet.
func (g *Group) add(s *serverState) {
	g.servers = append(g.servers, s)
	g.byName[s.name] = s
}

// nextGeneration starts the server's next process and lists its tools,
// over a context that ends when the generation is retired. The
// startup — the launch, the handshake and the listing — is bounded by
// the launch's own timeout under the group's lifetime, and a failure
// owns no process: the transport closes what it started.
func (g *Group) nextGeneration(s *serverState) (*generation, error) {
	ctx, cancel := context.WithCancel(g.ctx)
	gen, err := startGeneration(ctx, s.info, s.decl)
	if err != nil {
		cancel()
		return nil, err
	}
	gen.ctx, gen.cancel = ctx, cancel
	return gen, nil
}

// startGeneration starts one declared server over a transport built
// from the declaration, and lists its tools.
func startGeneration(ctx context.Context, info launchInfo, server manifest.Server) (*generation, error) {
	// The templates resolve here, from the session's own values: a
	// command that runs with an empty argument or an empty environment
	// value is worse than a server that does not launch, so a reference
	// that does not resolve fails the launch.
	command, err := server.Command.Resolve(info.hostEnv, info.sessionID)
	if err != nil {
		return nil, err
	}
	argv := make([]string, 0, len(server.Args)+1)
	argv = append(argv, command)
	for _, arg := range server.Args {
		resolved, err := arg.Resolve(info.hostEnv, info.sessionID)
		if err != nil {
			return nil, err
		}
		argv = append(argv, resolved)
	}
	declared := make(map[string]string, len(server.Env))
	for key, template := range server.Env {
		value, err := template.Resolve(info.hostEnv, info.sessionID)
		if err != nil {
			return nil, err
		}
		declared[key] = value
	}

	cmd := osexec.Command(argv[0], argv[1:]...)
	cmd.Dir = info.dir
	cmd.Env = mergeEnv(exec.ScrubEnv(nil, info.scrub), declared)

	// The startup runs under its own bound, so a server that never
	// finishes its handshake cannot hold a relaunch hostage either.
	sctx, cancel := context.WithTimeout(ctx, info.timeout)
	defer cancel()
	return connectGeneration(sctx, info.version, &sdkmcp.CommandTransport{Command: cmd})
}

// connectGeneration connects one server over a transport, lists its
// tools, and returns the generation the caller installs.
//
// The client's notification routing is installed before the connection
// is made, so a notification that races the subscribe round trip still
// reaches the waiter it belongs to.
func connectGeneration(ctx context.Context, version string, t sdkmcp.Transport) (*generation, error) {
	waiters := &resourceWaiters{}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: ClientName, Version: version}, &sdkmcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *sdkmcp.ResourceUpdatedNotificationRequest) {
			if req.Params != nil {
				waiters.updated(req.Params.URI)
			}
		},
	})
	// The acknowledged notification has no handler of its own — the
	// SDK's default discards it — so the middleware is the seam it
	// reaches the waiters through. The middleware passes every message
	// on, so the SDK's own handling, the read cache's invalidation
	// among them, still runs. The transport beside them records the
	// listen streams the client opens, so an acknowledgement names the
	// stream it answers.
	client.AddReceivingMiddleware(routeAcknowledged(waiters))

	session, err := client.Connect(ctx, &listenStreamsTransport{transport: t, waiters: waiters}, nil)
	if err != nil {
		return nil, err
	}
	gen := &generation{session: session, waiters: waiters, result: session.InitializeResult(), started: time.Now()}
	if err := gen.listTools(ctx); err != nil {
		_ = session.Close()
		return nil, err
	}
	return gen, nil
}

// watch owns one server's transitions.
//
// The watcher is the one goroutine that writes the server's phase and
// installs its generations. It waits on the generation the server
// runs, and a return from it while the group is open retires the
// generation: the phase moves to down, the dead session is closed —
// the go-sdk reaps the child only there, so skipping it would leave a
// zombie — and the generation's context ends the calls and the feed
// attempts that started on it. A relaunch follows, with backoff,
// until the server is ready again, its budget is spent, or the group
// closes; a return the close already retired is ignored, for the
// session is ending and no one acts on it.
func (g *Group) watch(s *serverState) {
	defer close(s.watchDone)
	for {
		s.stateMu.Lock()
		gen := s.current
		s.stateMu.Unlock()
		_ = gen.session.Wait()
		s.stateMu.Lock()
		if s.phase == phaseClosed {
			// The close retired the generation while the wait hung on
			// it: the return is the closed session's own, and it tells
			// no one anything.
			s.stateMu.Unlock()
			return
		}
		s.phase = phaseDown
		s.stateMu.Unlock()
		// The dead generation is closed for its process's sake: the
		// transport owns the waiting and the reaping, so nothing here
		// waits on the process a second time.
		_ = gen.session.Close()
		// The generation's context ends what started on it: a feed's
		// attempt ends and its restart policy brings the next one on
		// the generation the group runs then, and a call that was in
		// flight reports the server down rather than leaving again.
		gen.cancel()
		if s.noRelaunch {
			// The transport is the caller's: the group holds no process
			// and no declaration to launch again, so the end is
			// terminal. The phase is failed — the one whose calls
			// report the server down and whose feeds report the fault —
			// for nothing brings this server back, and the failure's
			// cause says so: no relaunch was spent, and none exists.
			s.stateMu.Lock()
			s.phase = phaseFailed
			s.failNoRelaunch = true
			s.stateMu.Unlock()
			return
		}
		if !g.relaunch(s, gen) {
			return
		}
	}
}

// relaunch brings a down server back: one wait, one attempt, until the
// server is ready again, its budget of consecutive failures is spent,
// or the group closes. It reports whether the server ended ready.
//
// The generation that ended spends the budget or clears it: one that
// ran past the stability window proved the server works, so the
// failures before it are spent, and one that ended sooner never got
// that far, so its exit counts like a launch that failed. A server
// that keeps dying inside the window is failed for good, for a bounded
// budget is what makes the relaunch safe to run at all.
func (g *Group) relaunch(s *serverState, retired *generation) bool {
	if retired.lived(g.stability) {
		s.failures = 0
	} else {
		s.failures++
	}
	backoff := g.backoff
	for s.failures < relaunchBudget {
		select {
		case <-g.closeCh:
			return false
		case <-time.After(backoff):
		}
		gen, err := g.nextGeneration(s)
		if err == nil {
			if s.setGeneration(gen) {
				return true
			}
			// The group closed while the launch ran: the generation it
			// started is this close's to stop, not the server's.
			_ = gen.session.Close()
			gen.cancel()
			return false
		}
		s.failures++
		if backoff < relaunchBackoffMax {
			backoff *= 2
			if backoff > relaunchBackoffMax {
				backoff = relaunchBackoffMax
			}
		}
	}
	// The budget is spent: the server is failed, and the calls and the
	// feeds that need it read it down. A close that arrives with the
	// last failure still owns the phase. The failure's cause is the
	// budget's: the relaunches ran and none of them spoke.
	s.stateMu.Lock()
	if s.phase != phaseClosed {
		s.phase = phaseFailed
		s.failNoRelaunch = false
	}
	s.stateMu.Unlock()
	return false
}

// lived reports whether the generation ran for at least d: whether its
// end is the crash of a server that worked rather than a start that
// never took.
func (gen *generation) lived(d time.Duration) bool {
	return time.Since(gen.started) >= d
}

// setGeneration installs a generation as the server's current one and
// moves the server to ready. It reports whether the install happened:
// a closed server is terminal, and the generation the close refused is
// the caller's to stop.
func (s *serverState) setGeneration(gen *generation) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.phase == phaseClosed {
		return false
	}
	s.generation++
	s.current = gen
	s.phase = phaseReady
	return true
}

// snapshot returns the server's state as a reader sees it, under the
// lock in one move.
func (s *serverState) snapshot() serverSnapshot {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return serverSnapshot{phase: s.phase, generation: s.generation, gen: s.current, failNoRelaunch: s.failNoRelaunch}
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
func (gen *generation) listTools(ctx context.Context) error {
	var tools []Tool
	cursor := ""
	for {
		res, err := gen.session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
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
	gen.tools = tools
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

// state returns the named server's state, or nil when the group holds
// no server by that name.
func (g *Group) state(server string) *serverState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.byName[server]
}

// Tools returns the tools the named server listed at its startup — the
// startup of the generation it runs now, for a relaunch lists its own
// — and whether the group holds that server.
//
// Only a ready server offers any. A server that is down, failed or
// closed is one a call refuses as down, so its tools leave the offered
// view until it is ready again, and a search never names a tool the
// call cannot run. The prompt's catalog is the one exception, by
// design: it is rendered once at attach and keeps the tools the
// session started with.
func (g *Group) Tools(server string) ([]Tool, bool) {
	s := g.state(server)
	if s == nil {
		return nil, false
	}
	snap := s.snapshot()
	if snap.phase != phaseReady || snap.gen == nil {
		return nil, true
	}
	return append([]Tool(nil), snap.gen.tools...), true
}

// Subscribes reports whether the named server advertised resource
// subscriptions in the initialize result it answered with.
//
// A declared resource feed needs the capability: a server without it
// could never grant the subscription the feed runs on, so the attach
// refuses the feed before the session is built. A server the group
// does not hold advertises nothing.
func (g *Group) Subscribes(server string) bool {
	s := g.state(server)
	if s == nil {
		return false
	}
	snap := s.snapshot()
	return snap.gen != nil && subscribes(snap.gen.result)
}

// subscribes reports whether an initialize result advertised resource
// subscriptions: the capability a resource feed's subscribe round trip
// needs from the generation it runs on.
func subscribes(result *sdkmcp.InitializeResult) bool {
	return result != nil && result.Capabilities != nil &&
		result.Capabilities.Resources != nil && result.Capabilities.Resources.Subscribe
}

// CallTool calls one tool on one server.
//
// The name is qualified: <server>/<tool>. An unknown server or tool
// reports ErrUnknownTool, and a server whose process has ended —
// down, failed, or the group closing — reports ErrServerDown, so the
// call never left the session. A call that reached the server is never
// repeated, whatever it returned: the call may have had effects, and a
// repeat is the caller's word. A call the server's own end caught in
// flight reports ErrServerDown too, for its generation's context ends
// with the server and the connection that carried the call ends with
// it. The server's own error answer is a successful return whose
// CallResult carries IsError.
func (g *Group) CallTool(ctx context.Context, qualified string, args json.RawMessage) (CallResult, error) {
	server, tool, ok := strings.Cut(qualified, "/")
	if !ok {
		return CallResult{}, fmt.Errorf("%w: %q", ErrUnknownTool, qualified)
	}
	s := g.state(server)
	if s == nil {
		return CallResult{}, fmt.Errorf("%w: %q", ErrUnknownTool, qualified)
	}
	snap := s.snapshot()
	// Readiness comes before the tool list: a generation the server no
	// longer runs — down, failed — is still the one installed, so a tool
	// it never held is the server's own down state, not an unknown tool
	// a search should chase. Only a ready generation's miss is unknown.
	if snap.gen != nil && snap.phase != phaseReady {
		return CallResult{}, fmt.Errorf("%w: %s", ErrServerDown, server)
	}
	if snap.gen == nil || !snap.gen.holds(tool) {
		return CallResult{}, fmt.Errorf("%w: %q", ErrUnknownTool, qualified)
	}
	// The call runs under its own context, ended by the caller's or by
	// the generation's: a server that ends under the call cancels it,
	// and the call reports the server down instead of hanging on a
	// connection that is gone.
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if stop := context.AfterFunc(snap.gen.ctx, cancel); stop != nil {
		defer stop()
	}
	res, err := snap.gen.session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		if ctx.Err() == nil && snap.gen.ended(err) {
			// The generation ended under the call: the process is gone,
			// and the call was cut off rather than answered.
			return CallResult{}, fmt.Errorf("%w: %s", ErrServerDown, server)
		}
		return CallResult{}, fmt.Errorf("the mcp server %q: %w", server, err)
	}
	return callResult(res), nil
}

// ended reports whether the call's failure is the generation's own end
// rather than an answer the server gave: the generation's context is
// done, or the connection that carried the call has ended.
//
// The two arrive in no fixed order. The server's process ends, the
// stream's read fails, the SDK retires every call in flight with that
// read error, and only then does the watcher's wait return and retire
// the generation — so a call that lost its connection is the server
// down, whether or not the generation's context has caught up.
func (gen *generation) ended(err error) bool {
	return gen.ctx.Err() != nil || errors.Is(err, io.EOF) || errors.Is(err, sdkmcp.ErrConnectionClosed)
}

// holds reports whether the generation listed the tool.
func (gen *generation) holds(tool string) bool {
	for _, held := range gen.tools {
		if held.Name == tool {
			return true
		}
	}
	return false
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
// Every server moves to closed first, so no relaunch starts while the
// sessions stop: the phase is terminal, the close signal wakes a
// watcher waiting out a backoff, and the cancelled lifetime fails a
// startup in flight. Each server's session closes its own process —
// the transport owns the waiting, the signalling and the killing, so
// nothing here waits on a process a second time — and the watchers
// exit before Close returns, so none of them can start a relaunch
// after it. The first call does the stopping and records the answer it
// returns; a call that arrives while it runs — and every later one —
// waits for it to finish and returns that same answer, so no caller is
// told the servers stopped while they are still stopping. The errors of
// the servers that did not stop are returned joined — a server whose
// process had already ended stopped on its own, and the exit status its
// reap reports is not a failure of the close.
func (g *Group) Close() error {
	g.mu.Lock()
	if g.closed {
		done := g.done
		g.mu.Unlock()
		// The close records its answer before it closes done, and
		// closing it orders the two: the wait here reads that answer.
		<-done
		return g.closeErr
	}
	g.closed = true
	servers := append([]*serverState(nil), g.servers...)
	g.mu.Unlock()

	close(g.closeCh)
	g.cancel()
	// A server whose process had already ended reaps with the exit's
	// own answer, and that is not a close's failure: the crash was
	// handled, and the session that is ending did not fail to stop the
	// server. Only a server that was running is the close's to stop.
	ended := make(map[*serverState]bool, len(servers))
	for _, s := range servers {
		ended[s] = s.snapshot().phase != phaseReady
		s.beginClose()
	}
	var errs []error
	for _, s := range servers {
		if err := s.stop(); err != nil && !ended[s] {
			errs = append(errs, fmt.Errorf("the mcp server %q: %w", s.name, err))
		}
	}
	for _, s := range servers {
		<-s.watchDone
	}
	err := errors.Join(errs...)
	g.closeErr = err
	close(g.done)
	return err
}

// beginClose moves the server to closed and ends its generation's
// context. The phase is terminal — no relaunch follows it — and the
// calls in flight on the closing generation read the server down.
func (s *serverState) beginClose() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.phase == phaseClosed {
		return
	}
	s.phase = phaseClosed
	if s.current != nil {
		s.current.cancel()
	}
}

// stop closes the generation the server runs, which reaps the process
// it owns. The close is the session's: a watcher that is reaping a
// dead generation of its own races it harmlessly, for the transport's
// close runs once whatever calls it.
func (s *serverState) stop() error {
	s.stateMu.Lock()
	gen := s.current
	s.stateMu.Unlock()
	if gen == nil {
		return nil
	}
	return gen.session.Close()
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
