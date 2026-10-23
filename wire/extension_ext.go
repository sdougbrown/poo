//go:build conformanceext

package wire

import (
	"encoding/json"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// The extension build proves the protocol's forward-compatibility rule.
//
// The build announces a capability the contract does not have, adds an
// optional field to a result, and sends an event type the contract
// does not have. A client that follows the rule ignores all three: the
// capability key is unknown, the field is covered by the announced
// capability, and the event type is not one it knows.

// The names the extension announces.
const (
	// ExtensionCapability is the capability key the extension
	// announces.
	ExtensionCapability = "extension"
	// ExtensionField is the optional field the extension adds to a
	// result. The announced capability covers it.
	ExtensionField = "extension_note"
	// ExtensionEvent is the event type the extension sends. A client
	// that does not know it ignores it.
	ExtensionEvent = "session.extension"
	// ExtensionNote is what the extension says.
	ExtensionNote = "the extension says hello"
)

// extendedResult is the handshake's result with the extension's
// capability and its field.
//
// The result is written by hand, so the capabilities hold the
// extension's key and the result holds the extension's field.
type extendedResult struct {
	// Result is the result the contract describes.
	Result protocol.InitializeResult
	// Note is the extension's field.
	Note string
}

// MarshalJSON writes the result with the extension's key and field.
func (r extendedResult) MarshalJSON() ([]byte, error) {
	encoded, err := json.Marshal(r.Result)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	capabilities := map[string]json.RawMessage{}
	if raw, ok := fields["capabilities"]; ok {
		if err := json.Unmarshal(raw, &capabilities); err != nil {
			return nil, err
		}
	}
	capabilities[ExtensionCapability] = json.RawMessage("true")
	fields["capabilities"], err = json.Marshal(capabilities)
	if err != nil {
		return nil, err
	}
	fields[ExtensionField], err = json.Marshal(r.Note)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

// extendedGet is the session's state with the extension's field.
type extendedGet struct {
	// Result is the result the contract describes.
	Result protocol.GetResult
	// Note is the extension's field.
	Note string
}

// MarshalJSON writes the state with the extension's field.
func (g extendedGet) MarshalJSON() ([]byte, error) {
	encoded, err := json.Marshal(g.Result)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	fields[ExtensionField], err = json.Marshal(g.Note)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

// lastSeq returns the sequence number of the session's last event.
//
// An event that is not from the session's bus takes the next number:
// the numbers the client pages with are the session's own.
func (s *session) lastSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last int64
	for _, evt := range s.events {
		if evt.Seq > last {
			last = evt.Seq
		}
	}
	return last
}

// initializeResult returns the handshake's result with the extension's
// capability.
func (s *server) initializeResult(version int) any {
	return extendedResult{
		Result: protocol.InitializeResult{
			ProtocolVersion: version,
			Build:           s.opts.Build,
			Profiles:        s.factory.Profiles(),
			Models:          s.factory.Roles(),
			Capabilities:    s.factory.Capabilities(),
		},
		Note: ExtensionNote,
	}
}

// getResult returns the session's state with the extension's field.
func (s *server) getResult(result protocol.GetResult) any {
	return extendedGet{Result: result, Note: ExtensionNote}
}

// sessionCreated is called when a session is created.
func (s *server) sessionCreated(entry *session) {}

// sessionClosed is called when a session is closed.
//
// The extension announces itself with an event of its own. The event
// takes the next sequence number of the session, so the client reads
// it in the order the events came.
func (s *server) sessionClosed(entry *session) {
	_ = s.notifyEvent(entry, protocol.Event{
		Seq:     entry.lastSeq() + 1,
		Type:    ExtensionEvent,
		At:      time.Now().UTC().Format(time.RFC3339Nano),
		Payload: map[string]any{"note": ExtensionNote},
	})
}
