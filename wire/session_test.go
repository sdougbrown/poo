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

// The notification and the close: what a client reads when a session's
// events arrive and when it goes away.

// A notification names the session the event belongs to.
//
// A client that serves several sessions reads one stream, so every
// notification says which session it is about.
func TestTheNotificationNamesTheSession(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}

	evt := h.event("run.started")
	if evt.SessionID != sessionID {
		t.Errorf("the event names the session %q, want %q", evt.SessionID, sessionID)
	}
}

// A close that races a settling run releases the waiter with the
// run's outcome.
//
// The settle record reaches the session's log through the observer,
// which delivers the events on its own goroutine. The close waits for
// it, so the waiter reads the outcome and not a refusal.
func TestTheCloseReleasesTheWaiterWithTheOutcome(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt: %v", err)
	}
	transport.waitStarted(t)

	// The client waits for the run, and closes the session while it
	// waits.
	waiting := h.callAsync(protocol.MethodRunInspect, protocol.InspectParams{
		SessionID: sessionID,
		Wait: &protocol.InspectWait{
			Until:     "settled",
			RunID:     prompted.RunID,
			TimeoutMS: 20000,
		},
	})
	// The wait is given a moment to start, so the close finds a waiter
	// and not a request that has not been read yet.
	time.Sleep(200 * time.Millisecond)
	if _, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID}); rerr != nil {
		t.Fatalf("the close was refused: %v", rerr)
	}

	select {
	case resp := <-waiting:
		if resp.Error != nil {
			t.Fatalf("the waiter was refused: %v", resp.Error)
		}
		var inspected protocol.InspectResult
		if err := json.Unmarshal(resp.Result, &inspected); err != nil {
			t.Fatalf("decoding the inspect: %v", err)
		}
		if inspected.Settled == nil {
			t.Fatal("the waiter got no outcome")
		}
		if inspected.Settled.RunID != prompted.RunID {
			t.Errorf("the settled run is %q, want %q", inspected.Settled.RunID, prompted.RunID)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the waiter was not released")
	}
}
