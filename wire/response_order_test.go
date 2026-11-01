package wire_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// The order of a session's first messages: the response that names the
// session reaches the client before any event the session sent.

// wakingFactory returns a factory whose sessions emit an event while
// they are opened. A resumed session that a job wakes does this: the
// wake emits run.started before the client knows the session's id.
func wakingFactory(t *testing.T, fireCreate, fireLoad bool) *testFactory {
	t.Helper()
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	evt := poo.Event{
		Seq:     1,
		Name:    poo.EventRunStarted,
		At:      time.Now().UTC().Format(time.RFC3339Nano),
		Payload: map[string]any{"run_id": "run-woken"},
	}
	factory.onCreate = func(obs poo.Observer) {
		if fireCreate {
			_ = obs.Observe(evt)
		}
	}
	factory.onLoad = func(obs poo.Observer) {
		if fireLoad {
			_ = obs.Observe(evt)
		}
	}
	return factory
}

// answer records the response to a request the harness is waiting for,
// with the id the response carries.
func answer(t *testing.T, answered <-chan *protocol.Response) *protocol.Response {
	t.Helper()
	select {
	case resp := <-answered:
		return resp
	case <-time.After(20 * time.Second):
		t.Fatal("the server did not answer the request")
	}
	return nil
}

// requireEventAfter asserts the arrival order around a session's
// first response: no notification ran ahead of it, and the event the
// session sent follows it directly.
func requireEventAfter(t *testing.T, h *harness, id string) {
	t.Helper()
	order := h.arrivalOrder()
	for i, a := range order {
		if a.id != id {
			continue
		}
		if early := h.notificationsBefore(id); len(early) > 0 {
			t.Fatalf("the server sent %v before the response to %s", early, id)
		}
		if next := order[i+1]; next.method != string(protocol.MethodEvent) {
			t.Fatalf("the server sent %q after the response to %s, want the event", next.method, id)
		}
		return
	}
	t.Fatalf("the response to %s is not in the arrival order: %v", id, order)
}

// TestCreateAnswersBeforeTheSessionEvents covers the create whose
// session emits an event while it is built: the response that names the
// session reaches the client before the event does.
func TestCreateAnswersBeforeTheSessionEvents(t *testing.T) {
	factory := wakingFactory(t, true, false)
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	resp := answer(t, h.callAsync(protocol.MethodSessionCreate, protocol.CreateParams{WorkingDir: factory.dir}))
	var info protocol.SessionInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		t.Fatalf("decoding the session info: %v", err)
	}
	if info.SessionID == "" {
		t.Fatal("the session has no id")
	}
	// The event was not lost, and it did not run ahead: once the event
	// is read, the arrival order shows the response that names the
	// session ahead of it.
	h.event("run.started")
	var id string
	if err := json.Unmarshal(resp.ID, &id); err != nil {
		t.Fatalf("decoding the response's id: %v", err)
	}
	requireEventAfter(t, h, id)
}

// TestLoadAnswersBeforeTheSessionEvents covers the load whose session
// emits an event while it is opened — the resumed session a job wakes:
// the response that names the session reaches the client before the
// event does.
func TestLoadAnswersBeforeTheSessionEvents(t *testing.T) {
	factory := wakingFactory(t, false, true)
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	// A session to load: it is created and closed, so the load takes
	// it over.
	created, rerr := h.call(protocol.MethodSessionCreate, protocol.CreateParams{WorkingDir: factory.dir})
	if rerr != nil {
		t.Fatalf("the session could not be created: %v", rerr)
	}
	var info protocol.SessionInfo
	if err := json.Unmarshal(created, &info); err != nil {
		t.Fatalf("decoding the session info: %v", err)
	}
	if _, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: info.SessionID}); rerr != nil {
		t.Fatalf("the session could not be closed: %v", rerr)
	}

	resp := answer(t, h.callAsync(protocol.MethodSessionLoad, protocol.LoadParams{SessionID: info.SessionID}))
	var reopened protocol.SessionInfo
	if err := json.Unmarshal(resp.Result, &reopened); err != nil {
		t.Fatalf("decoding the load: %v", err)
	}
	if reopened.SessionID != info.SessionID {
		t.Errorf("the load named the session %q, want %q", reopened.SessionID, info.SessionID)
	}
	// The event was not lost, and it did not run ahead: once the event
	// is read, the arrival order shows the response that names the
	// session ahead of it.
	h.event("run.started")
	var id string
	if err := json.Unmarshal(resp.ID, &id); err != nil {
		t.Fatalf("decoding the response's id: %v", err)
	}
	requireEventAfter(t, h, id)
}
