package wire_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// harness drives a server over a pair of pipes.
//
// The client writes its requests into one pipe and reads the server's
// answers from the other, exactly as a client of `poo serve` does.
type harness struct {
	t *testing.T
	// factory is the factory the server runs with.
	factory *testFactory
	// writer is the client's end of the request stream.
	writer *io.PipeWriter
	// reader reads the server's answers.
	reader *bufio.Reader
	// events are the notifications the server sent.
	events chan event
	// asks are the requests the server sent to the client.
	asks chan *protocol.Request
	// finished is closed when the server returns.
	finished chan struct{}
	// err is why the server returned.
	err error
	// mu guards the fields below it.
	mu sync.Mutex
	// pending are the outstanding requests, by id.
	pending map[string]chan *protocol.Response
	// ids counts the requests the client made.
	ids int
	// stopped reports that the harness was stopped.
	stopped bool
	// readMu guards paused.
	readMu sync.Mutex
	// readCond wakes the reader when the gate changes.
	readCond *sync.Cond
	// paused reports that the client stopped reading.
	paused bool
	// stray holds the messages that answered no request.
	stray chan []byte
	// all holds every event the server sent, in order.
	all []event
}

// event is one notification the server sent.
type event struct {
	// Seq is the event's sequence number.
	Seq int64 `json:"seq"`
	// Type names the event.
	Type string `json:"type"`
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run, when the event names one.
	RunID string `json:"run_id"`
	// Reason is the settle reason, for a settled run.
	Reason string `json:"reason"`
	// Fields is the whole notification.
	Fields map[string]any `json:"-"`
}

// newHarness starts a server and returns the client that drives it.
func newHarness(t *testing.T, factory *testFactory, bounds wire.Bounds) *harness {
	t.Helper()
	toServerReader, toServerWriter := io.Pipe()
	toClientReader, toClientWriter := io.Pipe()
	h := &harness{
		t:        t,
		factory:  factory,
		writer:   toServerWriter,
		reader:   bufio.NewReader(toClientReader),
		events:   make(chan event, 1024),
		asks:     make(chan *protocol.Request, 16),
		finished: make(chan struct{}),
		pending:  map[string]chan *protocol.Response{},
		stray:    make(chan []byte, 16),
	}
	h.readCond = sync.NewCond(&h.readMu)
	go h.read(toClientReader)
	go func() {
		err := wire.Serve(wire.Options{
			In:      toServerReader,
			Out:     toClientWriter,
			Factory: factory,
			Build: protocol.Build{
				Name:       "poo-test",
				Version:    "test",
				PooVersion: "test",
			},
			Bounds: bounds,
		})
		_ = toClientWriter.Close()
		h.mu.Lock()
		h.err = err
		h.mu.Unlock()
		close(h.finished)
	}()
	t.Cleanup(h.stop)
	return h
}

// read reads the server's messages until the stream ends.
func (h *harness) read(r io.Reader) {
	for {
		h.readMu.Lock()
		for h.paused {
			h.readCond.Wait()
		}
		h.readMu.Unlock()
		line, err := h.reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			h.dispatch(bytes.TrimSpace(line))
		}
		if err != nil {
			return
		}
	}
}

// pause stops the client from reading.
//
// The server's writes block while the client is not reading, which is
// what a slow client looks like.
func (h *harness) pause() {
	h.t.Helper()
	h.readMu.Lock()
	h.paused = true
	h.readMu.Unlock()
	h.readCond.Broadcast()
	// The reader may be inside a read already: the pause takes effect
	// on its next turn, and the test needs it to have taken effect.
	time.Sleep(50 * time.Millisecond)
}

// dispatch hands one message to the client.
func (h *harness) dispatch(line []byte) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *protocol.Error `json:"error"`
		Params json.RawMessage `json:"params"`
		Fields map[string]any  `json:"-"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return
	}
	switch {
	case msg.Method != "" && len(msg.ID) > 0:
		// The server asks the client something.
		select {
		case h.asks <- &protocol.Request{ID: msg.ID, Method: msg.Method, Params: msg.Params}:
		default:
		}
	case msg.Method != "":
		var evt event
		_ = json.Unmarshal(msg.Params, &evt)
		_ = json.Unmarshal(msg.Params, &evt.Fields)
		h.mu.Lock()
		h.all = append(h.all, evt)
		h.mu.Unlock()
		select {
		case h.events <- evt:
		default:
		}
	default:
		key := string(msg.ID)
		var text string
		if err := json.Unmarshal(msg.ID, &text); err == nil {
			key = text
		}
		h.mu.Lock()
		ch, ok := h.pending[key]
		if ok {
			delete(h.pending, key)
		}
		h.mu.Unlock()
		if ok {
			ch <- &protocol.Response{ID: msg.ID, Result: msg.Result, Error: msg.Error}
			return
		}
		// The message answered no request: the client holds it, so a
		// test can read it.
		select {
		case h.stray <- line:
		default:
		}
	}
}

// write sends a message to the server.
func (h *harness) write(msg any) {
	h.t.Helper()
	line, err := json.Marshal(msg)
	if err != nil {
		h.t.Fatalf("encoding the message: %v", err)
	}
	if _, err := h.writer.Write(append(line, '\n')); err != nil {
		h.t.Fatalf("writing to the server: %v", err)
	}
}

// call makes a request and waits for its answer.
func (h *harness) call(method string, params any) (json.RawMessage, *protocol.Error) {
	h.t.Helper()
	h.mu.Lock()
	h.ids++
	id := strconv.Itoa(h.ids)
	ch := make(chan *protocol.Response, 1)
	h.pending[id] = ch
	h.mu.Unlock()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	h.write(req)
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-h.finished:
		h.t.Fatalf("the server stopped before it answered %s: %v", method, h.serverErr())
	case <-time.After(20 * time.Second):
		h.t.Fatalf("the server did not answer %s", method)
	}
	return nil, nil
}

// callAsync makes a request and returns a channel for its answer.
//
// A client that waits for a run and closes the session while it waits
// needs both answers, and the second one comes first.
func (h *harness) callAsync(method string, params any) <-chan *protocol.Response {
	h.t.Helper()
	h.mu.Lock()
	h.ids++
	id := strconv.Itoa(h.ids)
	ch := make(chan *protocol.Response, 1)
	h.pending[id] = ch
	h.mu.Unlock()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	h.write(req)
	return ch
}

// send makes a request without waiting for its answer.
//
// A client that stopped reading never gets the answer, so the test
// sends the request and waits for the server to fail instead.
func (h *harness) send(method string, params any) {
	h.t.Helper()
	h.mu.Lock()
	h.ids++
	id := strconv.Itoa(h.ids)
	h.mu.Unlock()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	h.write(req)
}

// callRaw makes a request with a raw params object.
func (h *harness) callRaw(method string, params string) (json.RawMessage, *protocol.Error) {
	h.t.Helper()
	h.mu.Lock()
	h.ids++
	id := strconv.Itoa(h.ids)
	ch := make(chan *protocol.Response, 1)
	h.pending[id] = ch
	h.mu.Unlock()
	h.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": json.RawMessage(params)})
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-h.finished:
		h.t.Fatalf("the server stopped before it answered %s: %v", method, h.serverErr())
	case <-time.After(20 * time.Second):
		h.t.Fatalf("the server did not answer %s", method)
	}
	return nil, nil
}

// writeRaw sends a raw line to the server.
func (h *harness) writeRaw(line string) {
	h.t.Helper()
	if _, err := h.writer.Write([]byte(line + "\n")); err != nil {
		h.t.Fatalf("writing to the server: %v", err)
	}
}

// initialize performs the handshake.
func (h *harness) initialize() protocol.InitializeResult {
	h.t.Helper()
	result, rerr := h.call(protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersions: []int{protocol.Version},
		Client: protocol.Client{
			Name:    "test",
			Version: "test",
			Capabilities: protocol.ClientCapabilities{
				PermissionAnswers: true,
			},
		},
	})
	if rerr != nil {
		h.t.Fatalf("the handshake failed: %v", rerr)
	}
	var out protocol.InitializeResult
	if err := json.Unmarshal(result, &out); err != nil {
		h.t.Fatalf("decoding the handshake result: %v", err)
	}
	return out
}

// event waits for a notification of a type.
//
// The events that already arrived are searched first, so a test may ask
// for an event that came before the one it waited for.
func (h *harness) event(kind string) event {
	h.t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		h.mu.Lock()
		for _, evt := range h.all {
			if evt.Type == kind {
				h.mu.Unlock()
				return evt
			}
		}
		h.mu.Unlock()
		select {
		case <-h.events:
		case <-deadline:
			h.t.Fatalf("the server sent no %s event", kind)
		}
	}
}

// ask waits for a permission ask and returns it.
func (h *harness) ask() *protocol.Request {
	h.t.Helper()
	select {
	case req := <-h.asks:
		return req
	case <-time.After(20 * time.Second):
		h.t.Fatal("the server sent no permission ask")
	}
	return nil
}

// answer answers a permission ask.
func (h *harness) answer(req *protocol.Request, decision string) {
	h.t.Helper()
	h.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(req.ID),
		"result":  map[string]any{"decision": decision},
	})
}

// closeInput closes the client's end of the request stream, which is
// the server's cue to tear down.
func (h *harness) closeInput() {
	h.t.Helper()
	_ = h.writer.Close()
}

// wait waits for the server to return.
func (h *harness) wait() error {
	h.t.Helper()
	select {
	case <-h.finished:
		return h.serverErr()
	case <-time.After(30 * time.Second):
		h.t.Fatal("the server did not stop")
	}
	return nil
}

// serverErr returns why the server returned.
func (h *harness) serverErr() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// stop ends the harness.
func (h *harness) stop() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	h.mu.Unlock()
	// The reader starts again, so a write the server is holding ends
	// and no goroutine is left behind.
	h.readMu.Lock()
	h.paused = false
	h.readMu.Unlock()
	h.readCond.Broadcast()
	_ = h.writer.Close()
	select {
	case <-h.finished:
	case <-time.After(30 * time.Second):
		h.t.Error("the server did not stop")
	}
}

// testFactory builds the sessions the tests serve.
//
// The factory is the seam the server has: it builds the sessions over
// a scripted transport and a store in a temporary directory.
type testFactory struct {
	t *testing.T
	// dir is where the session logs live.
	dir string
	// transport builds the transport of a session.
	transport func() poo.ChatTransport
	// unmatched is the action for a side-effecting call no rule covers.
	unmatched perm.Action
	// mu guards the fields below it.
	mu sync.Mutex
	// sessions are the sessions the factory built.
	sessions []*poo.Session
	// released counts the sessions that were released.
	released int
	// ids numbers the sessions.
	ids int
	// flushErr is the failure a session's flush returns, when it is set.
	flushErr error
	// prompt are the prompt changes the session is built with.
	prompt []func(*prompt.Builder)
}

// newTestFactory returns a factory over a temporary directory.
func newTestFactory(t *testing.T, transport func() poo.ChatTransport) *testFactory {
	t.Helper()
	return &testFactory{
		t:         t,
		dir:       t.TempDir(),
		transport: transport,
		unmatched: perm.ActionAllow,
	}
}

// Profiles returns the profiles the binary offers.
func (f *testFactory) Profiles() []protocol.ProfileInfo {
	return []protocol.ProfileInfo{{
		Name:      "code",
		Tools:     f.toolInfos(),
		Unmatched: string(f.unmatched),
	}}
}

// Capabilities returns what this binary can do.
func (f *testFactory) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		Decider:     false,
		Compaction:  true,
		Persistence: []string{"ephemeral", "logged"},
		Jobs:        false,
	}
}

// Roles returns the models this binary talks to.
func (f *testFactory) Roles() protocol.ModelRoles {
	return protocol.ModelRoles{Chat: "test-model"}
}

// Create builds a session.
func (f *testFactory) Create(ctx context.Context, params protocol.CreateParams, env wire.FactoryEnv) (*poo.Session, *wire.Info, error) {
	mode := params.Mode
	if mode == "" {
		mode = "logged"
	}
	store, err := f.openStore(mode)
	if err != nil {
		return nil, nil, err
	}
	recorder := &session.Recording{Store: store}
	f.mu.Lock()
	f.ids++
	id := "sess-" + strconv.Itoa(f.ids)
	f.mu.Unlock()
	model := f.model()
	if _, err := store.Create(ctx, &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    id,
		Mode:         mode,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		WorkingDir:   f.dir,
		Model:        poo.SessionModel{ModelID: "test-model"},
		Tools:        f.pooTools(),
		ContextLimit: 8192,
		ToolsMode:    "native",
		Profile:      "code",
		Roles:        poo.ModelRoles{Chat: &model},
		Build:        poo.BuildInfo{Name: "poo-test", Version: "test"},
	}); err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	policy, err := f.policy(env)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	var sink poo.RecordSink = recorder
	if f.flushErr != nil {
		sink = &flushErrorSink{RecordSink: recorder, err: f.flushErr}
	}
	sess, err := poo.New(poo.Options{
		Transport:  f.transport(),
		Tools:      map[string]poo.Tool{"test": testTool{}},
		ToolOrder:  []string{"test"},
		Sink:       sink,
		SessionID:  id,
		Model:      f.model(),
		Profile:    "code",
		Profiles:   []poo.Profile{{Name: "code", Tools: f.pooTools()}},
		Build:      poo.BuildInfo{Name: "poo-test", Version: "test"},
		WorkingDir: f.dir,
		ToolsMode:  "native",
		Prompt:     f.prompt,
	}, poo.WithObserver(env.Observer), poo.WithPolicy(policy))
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	f.mu.Lock()
	f.sessions = append(f.sessions, sess)
	f.mu.Unlock()
	info := &wire.Info{
		Mode:    mode,
		Tools:   f.toolInfos(),
		Models:  f.Roles(),
		Records: recorder.Records,
		Release: func() error {
			f.mu.Lock()
			f.released++
			f.mu.Unlock()
			return store.Close()
		},
	}
	return sess, info, nil
}

// Load opens a recorded session.
func (f *testFactory) Load(ctx context.Context, params protocol.LoadParams, env wire.FactoryEnv) (*poo.Session, *wire.Info, error) {
	store, err := session.NewStore(f.dir, session.WithBuild(poo.BuildInfo{Name: "poo-test", Version: "test"}))
	if err != nil {
		return nil, nil, err
	}
	recorder := &session.Recording{Store: store}
	policy, err := f.policy(env)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	resumed, err := session.Resume(ctx, recorder, params.SessionID, poo.Options{
		Transport:  f.transport(),
		Tools:      map[string]poo.Tool{"test": testTool{}},
		ToolOrder:  []string{"test"},
		Model:      f.model(),
		Profiles:   []poo.Profile{{Name: "code", Tools: f.pooTools()}},
		Build:      poo.BuildInfo{Name: "poo-test", Version: "test"},
		WorkingDir: f.dir,
		ToolsMode:  "native",
	}, poo.WithObserver(env.Observer), poo.WithPolicy(policy))
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	f.mu.Lock()
	f.sessions = append(f.sessions, resumed.Session)
	f.mu.Unlock()
	loaded := resumed.Loaded
	records := recorder.RecordsWith(loaded.Records)
	info := &wire.Info{
		Mode:            string(loaded.Metadata.Mode),
		SessionDir:      f.dir,
		Tools:           f.toolInfos(),
		Models:          f.Roles(),
		RecoveredTail:   loaded.RecoveredTail,
		InterruptedRuns: resumed.Restored.Interrupted,
		Warnings:        resumed.Restored.Warnings,
		Records:         func() []poo.Record { return records },
		Release: func() error {
			f.mu.Lock()
			f.released++
			f.mu.Unlock()
			return store.Close()
		},
	}
	return resumed.Session, info, nil
}

// Configure applies a configuration change to a session.
func (f *testFactory) Configure(ctx context.Context, sess *poo.Session, params protocol.ConfigureParams) (protocol.ModelRoles, error) {
	if params.Models != nil && params.Models.Chat != "" && params.Models.Chat != "test-model" {
		return protocol.ModelRoles{}, poo.ErrUnknownModel
	}
	return f.Roles(), nil
}

// Models returns the effective model catalog.
func (f *testFactory) Models(ctx context.Context, refresh bool) ([]protocol.ModelInfo, error) {
	return []protocol.ModelInfo{{
		Name:         "test-model",
		ModelID:      "test-model",
		API:          string(poo.APIChatCompletions),
		Ops:          []string{string(poo.OpChat)},
		ContextLimit: 8192,
		ToolsMode:    "native",
	}}, nil
}

// Config returns the effective settings.
func (f *testFactory) Config(ctx context.Context) (map[string]any, error) {
	return map[string]any{"version": 1}, nil
}

// openStore opens the store a session's records live in.
func (f *testFactory) openStore(mode string) (session.Store, error) {
	if mode == string(session.ModeEphemeral) {
		return session.NewMemory(), nil
	}
	return session.NewStore(f.dir, session.WithBuild(poo.BuildInfo{Name: "poo-test", Version: "test"}))
}

// policy builds the permission policy of a session.
//
// The wire asker is in the chain, so a client that declared the
// permission_answers capability is asked about the calls the policy
// wants to ask about.
func (f *testFactory) policy(env wire.FactoryEnv) (*perm.Policy, error) {
	opts := perm.Options{Unmatched: f.unmatched, WorkingDir: f.dir}
	if env.Client != nil {
		opts.Askers = []perm.Asker{perm.NewWireAsker(env.Client, 0)}
	}
	return perm.New(opts)
}

// model returns the descriptor of the chat model.
func (f *testFactory) model() poo.ModelInfo {
	return poo.ModelInfo{
		ModelID:      "test-model",
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: 8192,
		Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
	}
}

// toolInfos returns the tools the sessions offer.
func (f *testFactory) toolInfos() []protocol.ToolInfo {
	return []protocol.ToolInfo{{
		Name:       "test",
		Class:      string(poo.ClassSideEffecting),
		Background: string(poo.BackgroundNever),
	}}
}

// pooTools returns the tools the sessions link.
func (f *testFactory) pooTools() []poo.ToolInfo {
	return []poo.ToolInfo{{
		Name:       "test",
		Class:      poo.ClassSideEffecting,
		Background: poo.BackgroundNever,
	}}
}

// lastSession returns the session the factory built last.
func (f *testFactory) lastSession() *poo.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sessions) == 0 {
		return nil
	}
	return f.sessions[len(f.sessions)-1]
}

// releasedSessions returns how many sessions were released.
func (f *testFactory) releasedSessions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.released
}

// flushErrorSink wraps a sink whose flush fails.
type flushErrorSink struct {
	poo.RecordSink
	err error
}

// Flush returns the configured failure.
func (s *flushErrorSink) Flush(ctx context.Context) error { return s.err }

// testTool is a side-effecting tool the scripted model calls.
type testTool struct{}

// Spec describes the tool.
func (testTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "test",
		Description: "a tool the tests call",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Class:       poo.ClassSideEffecting,
	}
}

// Prepare validates the call's arguments.
func (testTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return testPrepared{}, nil
}

// Call runs the tool.
func (testTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "the tool ran"}, nil
}

// testPrepared is a validated call of the test tool.
type testPrepared struct{}

// Summary describes the call.
func (testPrepared) Summary() string { return "the test tool" }
