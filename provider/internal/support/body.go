package support

import (
	"bytes"
	"encoding/json"

	"github.com/sdougbrown/poo"
)

// JSONKeyOrder returns the top-level keys of a JSON object, in the order
// the object holds them.
//
// The order matters for a fork: the forked body has to be byte-identical
// to the body it came from, apart from the fields that changed, and a
// JSON object decoded into a map loses its order.
func JSONKeyOrder(body []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON object",
		}
	}
	var order []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the request snapshot's keys do not decode",
			}
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the request snapshot's keys are not strings",
			}
		}
		order = append(order, key)
		// The value is skipped: the payload map already holds it.
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the request snapshot's values do not decode",
			}
		}
	}
	return order, nil
}

// EncodeObject writes a payload back as a JSON object in the given key
// order.
//
// The bytes are the body's own apart from the fields the caller changed,
// which is what makes a fork's prefix the one the endpoint already has.
func EncodeObject(payload map[string]json.RawMessage, order []string) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range order {
		if i > 0 {
			b.WriteByte(',')
		}
		// A string always encodes.
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteByte(':')
		b.Write(payload[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

// WithKeys appends the keys a fork may have added to the object's order.
//
// Every key the payload holds came from the body, except the ones the
// fork may add; those go last, in the order the caller names them.
func WithKeys(order []string, payload map[string]json.RawMessage, keys []string) []string {
	seen := make(map[string]bool, len(order))
	for _, k := range order {
		seen[k] = true
	}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		if _, ok := payload[k]; !ok {
			continue
		}
		order = append(order, k)
		seen[k] = true
	}
	return order
}
