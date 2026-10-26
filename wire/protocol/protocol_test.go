package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo/wire/protocol"
)

// The params are decoded strictly: an unknown field is its own
// failure.
func TestDecodeParamsIsStrict(t *testing.T) {
	var params protocol.GetParams
	err := protocol.DecodeParams(json.RawMessage(`{"session_id":"x","extra":1}`), &params)
	if err == nil {
		t.Fatal("the unknown field was accepted")
	}
	perr, ok := err.(*protocol.ParamsError)
	if !ok {
		t.Fatalf("the failure is a %T, want a *protocol.ParamsError", err)
	}
	if perr.Code != protocol.DataUnknownField {
		t.Errorf("the code = %q, want %q", perr.Code, protocol.DataUnknownField)
	}
	if perr.Field != "extra" {
		t.Errorf("the field = %q, want extra", perr.Field)
	}
}

// A params object that is null is invalid.
func TestDecodeParamsRefusesNull(t *testing.T) {
	var params protocol.GetParams
	err := protocol.DecodeParams(json.RawMessage(`null`), &params)
	if err == nil {
		t.Fatal("the null params were accepted")
	}
	perr, ok := err.(*protocol.ParamsError)
	if !ok {
		t.Fatalf("the failure is a %T, want a *protocol.ParamsError", err)
	}
	if perr.Code != protocol.DataInvalidParams {
		t.Errorf("the code = %q, want %q", perr.Code, protocol.DataInvalidParams)
	}
}

// A value of the wrong type is invalid.
func TestDecodeParamsRefusesTheWrongType(t *testing.T) {
	var params protocol.GetParams
	err := protocol.DecodeParams(json.RawMessage(`{"session_id":7}`), &params)
	if err == nil {
		t.Fatal("the wrong type was accepted")
	}
	perr, ok := err.(*protocol.ParamsError)
	if !ok {
		t.Fatalf("the failure is a %T, want a *protocol.ParamsError", err)
	}
	if perr.Code != protocol.DataInvalidParams {
		t.Errorf("the code = %q, want %q", perr.Code, protocol.DataInvalidParams)
	}
}

// An omitted params object takes the shape's defaults.
func TestDecodeParamsAcceptsOmittedParams(t *testing.T) {
	var params protocol.GetParams
	if err := protocol.DecodeParams(nil, &params); err != nil {
		t.Fatalf("the omitted params were refused: %v", err)
	}
	if params.SessionID != "" {
		t.Errorf("the session id = %q, want the default", params.SessionID)
	}
}

// The event carries its own data in the same object.
func TestEventMarshalsItsPayload(t *testing.T) {
	evt := protocol.Event{
		SessionID: "s1",
		Seq:       3,
		Type:      "run.started",
		At:        "2026-01-01T00:00:00Z",
		Payload:   map[string]any{"run_id": "r1"},
	}
	line, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("encoding the event: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(line, &fields); err != nil {
		t.Fatalf("decoding the event: %v", err)
	}
	if fields["session_id"] != "s1" {
		t.Errorf("the session id = %v, want s1", fields["session_id"])
	}
	if fields["seq"] != float64(3) {
		t.Errorf("the seq = %v, want 3", fields["seq"])
	}
	if fields["type"] != "run.started" {
		t.Errorf("the type = %v, want run.started", fields["type"])
	}
	if fields["run_id"] != "r1" {
		t.Errorf("the run id = %v, want r1", fields["run_id"])
	}
}
