package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo/pootest"
)

// TestTheRequestCarriesTheSessionsID covers the id a transport keys an
// endpoint's prompt cache by. It travels with the request rather than with the
// transport, so one transport that serves several sessions names each one's
// cache without having to be built per session.
func TestTheRequestCarriesTheSessionsID(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "hi"})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.SessionID = "session-under-test"
	s := newTestSession(t, opts)

	runSession(t, s, "hello")

	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the transport got %d requests, want one", len(requests))
	}
	if requests[0].SessionID != "session-under-test" {
		t.Errorf("the request names the session %q, want the id the session holds", requests[0].SessionID)
	}
}

// TestTheRequestNamesNoSessionWithoutOne covers the other side: a session built
// without an id sends none, so a transport falls back to its own configuration
// rather than naming an empty cache key.
func TestTheRequestNamesNoSessionWithoutOne(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "hi"})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.SessionID = ""
	s := newTestSession(t, opts)

	runSession(t, s, "hello")

	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the transport got %d requests, want one", len(requests))
	}
	if requests[0].SessionID != "" {
		t.Errorf("the request names the session %q, want none", requests[0].SessionID)
	}
}
