package wire_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// The order of a session's first messages: the response that names the
// session reaches the client before any event the session sent.

// eventFiringFactory builds sessions whose observer sees an event while
// the factory is still opening the session. A resumed session that a
// job wakes does this: the wake emits run.started before the client
// knows the session's id.
type eventFiringFactory struct {
	*testFactory
	fireCreate bool
	fireLoad   bool
}

// Create builds a session, and the session emits an event while it is
// built when the factory is told to.
func (f *eventFiringFactory) Create(ctx context.Context, params protocol.CreateParams, env wire.FactoryEnv) (*poo.Session, *wire.Info, error) {
	sess, info, err := f.testFactory.Create(ctx, params, env)
	if err != nil {
		return nil, nil, err
	}
	if f.fireCreate {
		f.fire(env.Observer)
	}
	return sess, info, nil
}

// Load opens a recorded session, and the session emits an event while
// it is opened when the factory is told to.
func (f *eventFiringFactory) Load(ctx context.Context, params protocol.LoadParams, env wire.FactoryEnv) (*poo.Session, *wire.Info, error) {
	sess, info, err := f.testFactory.Load(ctx, params, env)
	if err != nil {
		return nil, nil, err
	}
	if f.fireLoad {
		f.fire(env.Observer)
	}
	return sess, info, nil
}

// fire sends the event the session emits while it is being opened.
func (f *eventFiringFactory) fire(observer poo.Observer) {
	_ = observer.Observe(poo.Event{
		Seq:     1,
		Name:    poo.EventRunStarted,
		At:      time.Now().UTC().Format(time.RFC3339Nano),
		Payload: map[string]any{"run_id": "run-woken"},
	})
}

// orderRecorder records the messages the server wrote, in the order the
// ordered writer queued them.
type orderRecorder struct {
	mu     sync.Mutex
	writes []recordedWrite
	out    io.Writer
}

// recordedWrite is one message the server wrote.
type recordedWrite struct {
	// id is the response's id, when the write is a response.
	id string
	// method is the notification's method, when the write is a
	// notification.
	method string
}

// Write records one message and forwards it.
func (r *orderRecorder) Write(p []byte) (int, error) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(p, &msg); err == nil {
		r.mu.Lock()
		if msg.Method != "" {
			r.writes = append(r.writes, recordedWrite{method: msg.Method})
		} else {
			var id string
			_ = json.Unmarshal(msg.ID, &id)
			r.writes = append(r.writes, recordedWrite{id: id})
		}
		r.mu.Unlock()
	}
	return r.out.Write(p)
}

// notificationsBefore returns the notifications the server queued
// before it wrote the response to the request the id names.
func (r *orderRecorder) notificationsBefore(id string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, write := range r.writes {
		if write.id == id {
			return out
		}
		if write.method != "" {
			out = append(out, write.method)
		}
	}
	return nil
}

// orderedServer is a server whose writes are recorded in queue order.
type orderedServer struct {
	t *testing.T
	// in is the client's end of the request stream.
	in io.WriteCloser
	// lines carries the server's output, one line at a time.
	lines <-chan string
	// recorder records the messages the server wrote.
	recorder *orderRecorder
}

// newOrderedServer starts a server that records the order it writes in.
func newOrderedServer(t *testing.T, factory wire.Factory) *orderedServer {
	t.Helper()
	toServerReader, toServerWriter := io.Pipe()
	toClientReader, toClientWriter := io.Pipe()
	recorder := &orderRecorder{out: toClientWriter}
	lines := make(chan string, 64)
	go func() {
		_ = wire.Serve(wire.Options{
			In:      toServerReader,
			Out:     recorder,
			Factory: factory,
			Build:   protocol.Build{Name: "poo-test", Version: "test", PooVersion: "test"},
		})
		_ = toClientWriter.Close()
	}()
	go func() {
		reader := bufio.NewReader(toClientReader)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				lines <- line
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = toServerWriter.Close()
		for range lines {
		}
	})
	return &orderedServer{t: t, in: toServerWriter, lines: lines, recorder: recorder}
}

// request sends a request to the server.
func (s *orderedServer) request(id, method string, params any) {
	s.t.Helper()
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		s.t.Fatalf("encoding the request: %v", err)
	}
	if _, err := s.in.Write(append(line, '\n')); err != nil {
		s.t.Fatalf("writing to the server: %v", err)
	}
}

// response waits for the server's answer to the request the id names.
func (s *orderedServer) response(id string) json.RawMessage {
	s.t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				s.t.Fatalf("the server stopped before it answered %s", id)
			}
			var msg struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}
			if msg.Method == "" && string(msg.ID) == strconv.Quote(id) {
				return msg.Result
			}
		case <-deadline:
			s.t.Fatalf("the server did not answer %s", id)
		}
	}
}

// notification waits for a notification of a method.
func (s *orderedServer) notification(method string) {
	s.t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				s.t.Fatalf("the server stopped before it sent %s", method)
			}
			var msg struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal([]byte(line), &msg); err == nil && msg.Method == method {
				return
			}
		case <-deadline:
			s.t.Fatalf("the server sent no %s notification", method)
		}
	}
}

// TestCreateAnswersBeforeTheSessionEvents covers the create whose
// session emits an event while it is built: the response that names the
// session reaches the client before the event does.
func TestCreateAnswersBeforeTheSessionEvents(t *testing.T) {
	factory := &eventFiringFactory{testFactory: newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	}), fireCreate: true}
	s := newOrderedServer(t, factory)
	s.request("h", protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersions: []int{protocol.Version},
		Client: protocol.Client{
			Name:         "test",
			Version:      "test",
			Capabilities: protocol.ClientCapabilities{PermissionAnswers: true},
		},
	})
	s.response("h")

	s.request("s", protocol.MethodSessionCreate, protocol.CreateParams{WorkingDir: factory.dir})
	result := s.response("s")
	var info protocol.SessionInfo
	if err := json.Unmarshal(result, &info); err != nil {
		t.Fatalf("decoding the session info: %v", err)
	}
	if info.SessionID == "" {
		t.Fatal("the session has no id")
	}
	// The event was not lost, and it did not run ahead: the response
	// that names the session is on the wire before the event is.
	if early := s.recorder.notificationsBefore("s"); len(early) > 0 {
		t.Fatalf("the server sent %v before the response to the create", early)
	}
	s.notification(string(protocol.MethodEvent))
}

// TestLoadAnswersBeforeTheSessionEvents covers the load whose session
// emits an event while it is opened — the resumed session a job wakes:
// the response that names the session reaches the client before the
// event does.
func TestLoadAnswersBeforeTheSessionEvents(t *testing.T) {
	factory := &eventFiringFactory{testFactory: newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	}), fireLoad: true}
	s := newOrderedServer(t, factory)
	s.request("h", protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersions: []int{protocol.Version},
		Client: protocol.Client{
			Name:         "test",
			Version:      "test",
			Capabilities: protocol.ClientCapabilities{PermissionAnswers: true},
		},
	})
	s.response("h")

	// A session to load: it is created and closed, so the load takes
	// it over.
	s.request("c", protocol.MethodSessionCreate, protocol.CreateParams{WorkingDir: factory.dir})
	result := s.response("c")
	var info protocol.SessionInfo
	if err := json.Unmarshal(result, &info); err != nil {
		t.Fatalf("decoding the session info: %v", err)
	}
	s.request("x", protocol.MethodSessionClose, protocol.CloseParams{SessionID: info.SessionID})
	s.response("x")

	s.request("l", protocol.MethodSessionLoad, protocol.LoadParams{SessionID: info.SessionID})
	loaded := s.response("l")
	var reopened protocol.SessionInfo
	if err := json.Unmarshal(loaded, &reopened); err != nil {
		t.Fatalf("decoding the load: %v", err)
	}
	if reopened.SessionID != info.SessionID {
		t.Errorf("the load named the session %q, want %q", reopened.SessionID, info.SessionID)
	}
	// The event was not lost, and it did not run ahead: the response
	// that names the session is on the wire before the event is.
	if early := s.recorder.notificationsBefore("l"); len(early) > 0 {
		t.Fatalf("the server sent %v before the response to the load", early)
	}
	s.notification(string(protocol.MethodEvent))
}
