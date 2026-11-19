// Package transport holds the request-shape half the wire APIs share.
//
// Nothing here moves a request or reads an answer: that is the support
// package's. What is here is what a request carries and how its body is
// written — the stable key order a fork preserves, the named parameters
// merged into a top level, the messages a fork appends, and the policy for
// the provider data an API may carry back. A transport composes both
// halves with the wire package that knows its API's own shapes.

package transport

import (
	"bytes"
	"encoding/json"
	"sort"

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

// MergeParams writes a request body with named parameters merged into its
// top level.
//
// The parameters are written last and in name order, so the body of two
// requests that ask for the same thing is the same bytes. A body that is
// not a bare JSON object is a body the merge cannot spell, and the error is
// the caller's to report.
func MergeParams(body []byte, params map[string]json.RawMessage) ([]byte, error) {
	if len(params) == 0 {
		return body, nil
	}
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	var b bytes.Buffer
	b.Write(body[:len(body)-1])
	for _, name := range names {
		b.WriteByte(',')
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(params[name])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
