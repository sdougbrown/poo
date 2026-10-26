package conformance

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/sdougbrown/poo/wire/protocol"
)

// The decode rule: the plan's forward-compatibility rule.
//
//   - A known shape is decoded strictly: a field the shape does not
//     have is a failure.
//   - The fields an announced capability covers are the exception: a
//     later binary may add them, and the client ignores the ones it
//     does not understand.
//   - An unknown key in the capabilities is tolerated.
//   - An event type the client does not know is ignored.

// decode reads a known shape and fails on a field the shape does not
// have.
//
// The fields an announced capability covers are the exception: a later
// binary may add them, and the client ignores the ones it does not
// understand.
func (c *client) decode(raw json.RawMessage, shape any) {
	c.t.Helper()
	if len(raw) == 0 {
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		c.t.Errorf("the result is not a JSON object: %s", string(raw))
		return
	}
	known := knownFields(shape)
	for name := range fields {
		if known[name] {
			continue
		}
		if c.covered(name) {
			continue
		}
		c.t.Errorf("the result holds the field %q, which the contract does not have", name)
	}
	if err := json.Unmarshal(raw, shape); err != nil {
		c.t.Errorf("the result is not what the contract says: %v", err)
	}
}

// decodeFailure reads a failure and fails on a field the shape does
// not have.
func (c *client) decodeFailure(raw json.RawMessage) {
	c.t.Helper()
	if len(raw) == 0 {
		return
	}
	c.decode(raw, &protocol.Error{})
}

// announce records the capabilities the handshake announced.
//
// The result is the handshake's result, and the capabilities are read
// as a map: a key the client does not know is tolerated, because a
// later binary may add one.
func (c *client) announce(result json.RawMessage) {
	c.t.Helper()
	if len(result) == 0 {
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(result, &fields); err != nil {
		c.t.Errorf("the handshake is not what the contract says: %v", err)
		return
	}
	raw, ok := fields["capabilities"]
	if !ok {
		c.t.Error("the handshake announces no capabilities")
		return
	}
	var capabilities map[string]json.RawMessage
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		c.t.Errorf("the capabilities are not what the contract says: %v", err)
		return
	}
	c.mu.Lock()
	c.capabilities = capabilities
	c.mu.Unlock()
}

// covered reports whether an announced capability covers a field.
//
// A capability announces the addition it names, and the fields it
// covers begin with the capability's name. A binary that adds a field
// and announces it may send both, and the client ignores the field it
// does not understand.
func (c *client) covered(field string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name := range c.capabilities {
		if field == name || strings.HasPrefix(field, name+"_") {
			return true
		}
	}
	return false
}

// knownFields returns the JSON field names a shape holds.
func knownFields(shape any) map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeOf(shape)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return out
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out[name] = true
	}
	return out
}

// decodeEvent reads an event.
//
// The event's envelope is the contract's: the session, the sequence
// number, the type and the timestamp. The rest is the event's own
// data, and the client reads the fields it knows. An event type the
// client does not know is read the same way: the type is a name, and
// the data is the event's own.
func decodeEvent(raw json.RawMessage) (event, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return event{}, err
	}
	out := event{Payload: map[string]any{}}
	for name, value := range fields {
		switch name {
		case "session_id":
			if err := json.Unmarshal(value, &out.SessionID); err != nil {
				return event{}, err
			}
		case "seq":
			if err := json.Unmarshal(value, &out.Seq); err != nil {
				return event{}, err
			}
		case "type":
			if err := json.Unmarshal(value, &out.Type); err != nil {
				return event{}, err
			}
		case "at":
			if err := json.Unmarshal(value, &out.At); err != nil {
				return event{}, err
			}
		default:
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				return event{}, err
			}
			out.Payload[name] = decoded
		}
	}
	return out, nil
}
