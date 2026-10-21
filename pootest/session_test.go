package pootest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The helpers are what a consumer's own tests drive a session with, so
// they have to work with the session the way a consumer uses it.

// TestTheScriptedTransportDrivesASession covers the transport: a session
// built with the scripted transport runs the script and settles.
func TestTheScriptedTransportDrivesASession(t *testing.T) {
	transport := pootest.NewTransport(
		pootest.Turn{
			Calls: []pootest.Call{{
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		pootest.Turn{Text: "the answer"},
	)
	sink := pootest.NewSink()

	session, err := poo.New(poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
	// The transport recorded the requests.
	if len(transport.Requests()) != 2 {
		t.Errorf("the transport got %d requests, want 2", len(transport.Requests()))
	}
	// The sink recorded the records, including the settle record.
	settled := sink.Settled()
	if settled == nil {
		t.Fatal("the sink holds no settle record")
	}
	if settled.Reason != poo.ReasonEndTurn {
		t.Errorf("the settle record says %q, want end_turn", settled.Reason)
	}
}

// TestTheFailingSinkStopsASession covers the failure injection: a sink
// that is told to fail breaks the session.
func TestTheFailingSinkStopsASession(t *testing.T) {
	transport := pootest.NewTransport(pootest.Turn{Text: "the answer"})
	sink := pootest.NewSink()
	sink.FailAt(1)

	session, err := poo.New(poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if !session.Broken() {
		t.Error("the session is not broken after a store failure")
	}
}

// TestTheScriptedCompactorDrivesASession covers the compactor: a
// session with the scripted compactor asks it before every request.
func TestTheScriptedCompactorDrivesASession(t *testing.T) {
	transport := pootest.NewTransport(pootest.Turn{Text: "the answer"})
	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "test"})
	sink := pootest.NewSink()

	session, err := poo.New(poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
		Compactor:    compactor,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if compactor.Calls() == 0 {
		t.Error("the compactor was never asked")
	}
	// The compaction was recorded.
	found := false
	for _, rec := range sink.Records() {
		if rec.Meta().Record == "compaction" {
			found = true
		}
	}
	if !found {
		t.Error("the sink holds no compaction record")
	}
}

// echoTool is a tool that answers with a fixed text.
type echoTool struct{}

// Spec describes the tool.
func (t *echoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object"}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (t *echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{}, nil
}

// Call answers with a fixed text.
func (t *echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "echoed"}, nil
}

// echoPrepared is a prepared echo call.
type echoPrepared struct{}

// Summary describes the call.
func (p echoPrepared) Summary() string { return "echo" }
