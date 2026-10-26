// Package wire serves poo's public protocol over a byte stream.
//
// The protocol is JSON-RPC 2.0, one JSON object per line. The server
// takes a factory: it never reads settings and never builds a session
// itself, so the binary that links it decides what it can do, and the
// contract in the protocol package stays the only thing a client
// depends on.
//
// The server is written for a client that may go away at any moment.
// Every failure of the transport takes the same path: the sessions are
// closed, the locks are released, and the process reports the failure.
package wire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/wire/protocol"
)

// The bounds the transport enforces.
//
// The defaults are the plan's: a queue of ten thousand messages or
// sixteen mebibytes, a write that may take ten seconds, a teardown that
// may take five, and a permission ask that may take the asker chain's
// own budget.
const (
	// DefaultQueueMessages is how many messages the output queue holds.
	DefaultQueueMessages = 10000
	// DefaultQueueBytes is how many bytes the output queue holds.
	DefaultQueueBytes = 16 << 20
	// DefaultWriteStall is how long one write to the client may take.
	DefaultWriteStall = 10 * time.Second
	// DefaultDrain is how long the teardown waits for the output queue
	// to drain.
	DefaultDrain = 5 * time.Second
)

// Bounds are the limits the transport enforces.
//
// A test shortens them: a queue that holds a hundred messages and a
// write that may take a millisecond make the failure paths reachable
// without waiting.
type Bounds struct {
	// QueueMessages is how many messages the output queue holds. Zero
	// takes the default.
	QueueMessages int
	// QueueBytes is how many bytes the output queue holds. Zero takes
	// the default.
	QueueBytes int
	// WriteStall is how long one write to the client may take. Zero
	// takes the default.
	WriteStall time.Duration
	// Drain is how long the teardown waits for the output queue to
	// drain. Zero takes the default.
	Drain time.Duration
	// AskBudget is how long a client may take to answer a permission
	// ask. Zero takes the asker chain's default.
	AskBudget time.Duration
}

// withDefaults returns the bounds with the defaults filled in.
func (b Bounds) withDefaults() Bounds {
	if b.QueueMessages <= 0 {
		b.QueueMessages = DefaultQueueMessages
	}
	if b.QueueBytes <= 0 {
		b.QueueBytes = DefaultQueueBytes
	}
	if b.WriteStall <= 0 {
		b.WriteStall = DefaultWriteStall
	}
	if b.Drain <= 0 {
		b.Drain = DefaultDrain
	}
	if b.AskBudget <= 0 {
		b.AskBudget = perm.DefaultWireBudget
	}
	return b
}

// Options configure the server.
type Options struct {
	// In is the client's request stream. The server reads one JSON
	// object per line from it.
	In io.Reader
	// Out is the server's response stream. The server writes one JSON
	// object per line to it.
	Out io.Writer
	// Factory builds the sessions the server serves.
	Factory Factory
	// Build names the binary the server runs in.
	Build protocol.Build
	// Bounds are the limits the transport enforces.
	Bounds Bounds
}

// Env is what the server gives the factory when it builds a session.
type Env struct {
	// Observer sees the session's events. The server sends them to the
	// client as notifications.
	Observer poo.Observer
	// Client is the wire client. The session's policy asks it about
	// the calls the policy wants to ask about, and a client that did
	// not declare the permission_answers capability is never asked.
	Client perm.Client
}

// Factory builds the sessions the server serves.
//
// The factory is the seam between the protocol and the binary: it knows
// the settings, the profiles and the models, and the server knows
// neither.
type Factory interface {
	// Profiles returns the profiles the binary offers.
	Profiles() []protocol.ProfileInfo
	// Capabilities returns what this binary can do.
	Capabilities() protocol.Capabilities
	// Roles returns the models this binary talks to.
	Roles() protocol.ModelRoles
	// Create builds a session.
	Create(ctx context.Context, params protocol.CreateParams, env Env) (*poo.Session, *Info, error)
	// Load opens a recorded session.
	Load(ctx context.Context, params protocol.LoadParams, env Env) (*poo.Session, *Info, error)
	// Configure applies a configuration change to a session between
	// its runs.
	Configure(ctx context.Context, sess *poo.Session, params protocol.ConfigureParams) (protocol.ModelRoles, error)
	// Models returns the effective model catalog.
	Models(ctx context.Context, refresh bool) ([]protocol.ModelInfo, error)
	// Config returns the effective settings, with the credentials
	// redacted.
	Config(ctx context.Context) (map[string]any, error)
}

// Info is what the factory says about a session it built.
type Info struct {
	// Mode is the session's persistence mode: "logged" or "ephemeral".
	Mode string
	// SessionDir is the session's directory. It is set for a logged
	// session.
	SessionDir string
	// Tools are the tools the session offers.
	Tools []protocol.ToolInfo
	// Models are the models the session talks to.
	Models protocol.ModelRoles
	// RecoveredTail says the load truncated a torn tail.
	RecoveredTail bool
	// InterruptedRuns are the runs the load marked interrupted.
	InterruptedRuns []string
	// Warnings are the things the client should know about.
	Warnings []string
	// Records returns the session's records, oldest first.
	//
	// The server projects them into the session's history, so a
	// factory that keeps its records anywhere can serve it.
	Records func() []poo.Record
	// Release releases what holds the session: the store that took
	// its lock, and the lock itself.
	//
	// The server calls it at the end of the close ordering, after the
	// session's records are flushed.
	Release func() error
}

// Serve runs the server until the client goes away.
//
// The server reads the client's requests from the options' reader and
// writes its answers to the options' writer. It returns when the client
// closes the stream, or when the transport fails.
//
// A clean teardown returns nil: the sessions are closed, the locks are
// released, and the process exits zero. A transport failure returns an
// error: the sessions are closed, the locks are released, and the
// process exits one.
func Serve(opts Options) error {
	if opts.In == nil || opts.Out == nil {
		return errors.New("wire: the server needs a reader and a writer")
	}
	if opts.Factory == nil {
		return errors.New("wire: the server needs a factory")
	}
	s := newServer(opts)
	return s.run()
}

// server is the running server.
type server struct {
	// opts are the server's configuration.
	opts Options
	// bounds are the limits the transport enforces, with the defaults
	// filled in.
	bounds Bounds
	// factory builds the sessions.
	factory Factory
	// out is the output queue.
	out *output
	// client is the connected client and the permission asker's client.
	client *wireClient

	// mu guards the fields below it.
	mu sync.Mutex
	// sessions are the sessions the server serves, by id.
	sessions map[string]*session
	// pending are the outstanding permission asks, by ask id.
	pending map[string]chan *protocol.Response
	// initialized reports that the handshake is done.
	initialized bool
	// clientInfo is what the handshake said about the client.
	clientInfo protocol.Client

	// failed is closed when the transport fails.
	failed chan struct{}
	// failOnce guards the failure.
	failOnce sync.Once
	// failErr is why the transport failed.
	failErr error
	// writerDone is closed when the writer goroutine has ended.
	writerDone chan struct{}
}

// newServer builds a server.
func newServer(opts Options) *server {
	bounds := opts.Bounds.withDefaults()
	s := &server{
		opts:       opts,
		bounds:     bounds,
		factory:    opts.Factory,
		out:        newOutput(bounds.QueueMessages, bounds.QueueBytes),
		sessions:   map[string]*session{},
		pending:    map[string]chan *protocol.Response{},
		failed:     make(chan struct{}),
		writerDone: make(chan struct{}),
	}
	s.client = &wireClient{srv: s}
	return s
}

// run serves the client until the stream ends or the transport fails.
func (s *server) run() error {
	go s.writeLoop()
	readDone := make(chan error, 1)
	go func() { readDone <- s.readLoop() }()

	var readErr error
	select {
	case readErr = <-readDone:
	case <-s.failed:
		// The transport failed. The reader may still be waiting for a
		// line that will never come: the teardown does not wait for
		// it.
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		// The stream broke. It is a transport failure, and it takes the
		// same path as a queue that overflowed.
		s.fail(fmt.Errorf("wire: reading the client: %w", readErr))
	}
	// The sessions are closed either way: a client that hung up and a
	// transport that broke both leave the sessions to be closed.
	source := sourceCancelClose
	if s.failedNow() {
		source = sourceTransportFailed
	}
	s.closeAll(source)
	if s.failedNow() {
		// The output is not drained: the client is not reading, and
		// the server never waits on the output to finish its teardown.
		return s.failure()
	}
	// The client hung up. The queue is drained, bounded, and the
	// process exits zero.
	s.out.close()
	select {
	case <-s.writerDone:
	case <-time.After(s.bounds.Drain):
	}
	return nil
}

// failedNow reports that the transport failed.
func (s *server) failedNow() bool {
	select {
	case <-s.failed:
		return true
	default:
		return false
	}
}

// failure returns why the transport failed.
func (s *server) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr == nil {
		return errors.New("wire: the transport failed")
	}
	return s.failErr
}

// fail reports that the transport failed.
//
// The first failure is the one that counts: the queue overflowing, a
// write error and a write stall all take the same path, and only the
// first one is reported.
func (s *server) fail(err error) {
	s.failOnce.Do(func() {
		s.mu.Lock()
		s.failErr = err
		s.mu.Unlock()
		close(s.failed)
	})
}

// writeLoop writes the queued messages to the client.
//
// The writer is the only goroutine that writes, so the messages reach
// the client in the order they were queued.
func (s *server) writeLoop() {
	defer close(s.writerDone)
	for {
		line, ok := s.out.pop()
		if !ok {
			return
		}
		if err := s.writeLine(line); err != nil {
			s.fail(err)
			return
		}
	}
}

// writeLine writes one message, under the stall watchdog.
//
// A write that does not return in time is a transport failure, even
// when the queue is not full: the client is not reading, and the server
// would wait for it forever.
func (s *server) writeLine(line []byte) error {
	stall := s.bounds.WriteStall
	done := make(chan error, 1)
	go func() {
		_, err := s.opts.Out.Write(line)
		done <- err
	}()
	timer := time.NewTimer(stall)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("wire: writing to the client: %w", err)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("wire: the write to the client took more than %s", stall)
	}
}

// readLoop reads the client's requests.
//
// The reader dispatches the requests concurrently, so a request that
// waits — an inspect that waits for a settle — never holds up the
// answer to a permission ask.
func (s *server) readLoop() error {
	reader := bufio.NewReader(s.opts.In)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			s.handleLine(bytes.TrimRight(line, "\r\n"))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// handleLine acts on one line of the client's stream.
func (s *server) handleLine(line []byte) {
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
		Error   *protocol.Error `json:"error"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		// The line is not JSON, so the request is unknown: the answer
		// carries the null id, as JSON-RPC says.
		s.respondError(json.RawMessage("null"), protocol.Errorf(protocol.CodeParseError, protocol.DataParseError,
			"the line is not JSON: %v", err))
		return
	}
	if msg.Method != "" && msg.JSONRPC != "2.0" {
		s.respondError(msg.ID, protocol.Errorf(protocol.CodeInvalidRequest, protocol.DataInvalidRequest,
			"the request's jsonrpc must be \"2.0\", not %q", msg.JSONRPC))
		return
	}
	if msg.Method == "" {
		if len(msg.ID) == 0 {
			// The line is neither a request nor an answer.
			s.respondError(json.RawMessage("null"), protocol.Errorf(protocol.CodeInvalidRequest, protocol.DataInvalidRequest,
				"the line is neither a request nor an answer"))
			return
		}
		// The line is an answer to one of the server's requests.
		s.deliverResponse(msg.ID, msg.Result, msg.Error)
		return
	}
	req := protocol.Request{JSONRPC: msg.JSONRPC, ID: msg.ID, Method: msg.Method, Params: msg.Params}
	if req.Method == protocol.MethodInitialize {
		// The handshake is answered in the reader's own goroutine, so
		// that every later request sees a finished handshake.
		result, rerr := s.initialize(req.Params)
		s.respond(req, result, rerr)
		return
	}
	if !s.isInitialized() {
		s.respondError(req.ID, protocol.Errorf(protocol.CodeNotInitialized, protocol.DataNotInitialized,
			"the client must call %s first", protocol.MethodInitialize))
		return
	}
	if !knownMethod(req.Method) {
		s.respondError(req.ID, protocol.Errorf(protocol.CodeMethodNotFound, protocol.DataMethodNotFound,
			"there is no method %q", req.Method))
		return
	}
	go func() {
		result, rerr := s.dispatch(req)
		s.respond(req, result, rerr)
	}()
}

// respond writes the answer to a request.
//
// A request without an id is a notification: the server acts on it and
// answers nothing.
func (s *server) respond(req protocol.Request, result any, rerr *protocol.Error) {
	if len(req.ID) == 0 {
		return
	}
	if rerr != nil {
		s.respondError(req.ID, rerr)
		return
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		s.respondError(req.ID, protocol.Errorf(protocol.CodeInternalError, protocol.DataInternalError,
			"the result could not be encoded: %v", err))
		return
	}
	_ = s.send(protocol.Response{JSONRPC: "2.0", ID: req.ID, Result: encoded})
}

// respondError writes a failure to a request.
func (s *server) respondError(id json.RawMessage, rerr *protocol.Error) {
	if len(id) == 0 {
		// A failure of a notification is not reported: the client
		// asked for no answer.
		return
	}
	_ = s.send(protocol.Response{JSONRPC: "2.0", ID: id, Error: rerr})
}

// send queues a message for the client.
//
// A queue that is full is a transport failure: the client stopped
// reading, and the server waits for it no longer.
func (s *server) send(msg any) error {
	line, err := json.Marshal(msg)
	if err != nil {
		err = fmt.Errorf("wire: encoding a message: %w", err)
		s.fail(err)
		return err
	}
	line = append(line, '\n')
	if err := s.out.push(line); err != nil {
		s.fail(err)
		return err
	}
	return nil
}

// sendNotification queues a notification for the client.
func (s *server) sendNotification(method string, params any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		err = fmt.Errorf("wire: encoding a notification: %w", err)
		s.fail(err)
		return err
	}
	return s.send(protocol.Notification{JSONRPC: "2.0", Method: method, Params: encoded})
}

// deliverResponse hands a client's answer to the ask that is waiting
// for it.
func (s *server) deliverResponse(id json.RawMessage, result json.RawMessage, rerr *protocol.Error) {
	if len(id) == 0 {
		return
	}
	key := idKey(id)
	s.mu.Lock()
	ch, ok := s.pending[key]
	if ok {
		delete(s.pending, key)
	}
	s.mu.Unlock()
	if !ok {
		// The answer is for an ask that is gone: the client was too
		// slow, and the ask has already been deferred.
		return
	}
	ch <- &protocol.Response{ID: id, Result: result, Error: rerr}
}

// isInitialized reports that the handshake is done.
func (s *server) isInitialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

// knownMethod reports that the server has the method.
//
// The server answers a method it does not have with a method_not_found
// failure, rather than ignoring it: a client that calls a method this
// binary does not have should hear about it.
func knownMethod(method string) bool {
	switch method {
	case protocol.MethodSessionCreate,
		protocol.MethodSessionLoad,
		protocol.MethodSessionGet,
		protocol.MethodSessionConfigure,
		protocol.MethodSessionClose,
		protocol.MethodSessionHistory,
		protocol.MethodModelsList,
		protocol.MethodAgentPrompt,
		protocol.MethodAgentMessage,
		protocol.MethodAgentInspect,
		protocol.MethodAgentCancel,
		protocol.MethodJobCancel,
		protocol.MethodConfigGet:
		return true
	}
	return false
}
