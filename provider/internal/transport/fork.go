package transport

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
)

// AppendMessagesToBody appends serialized messages to a serialized request
// body, and replaces the output limit. The tool choice is the fork's own
// to set, with SetToolChoice.
//
// The fork re-sends the snapshot as it was built: the body is decoded, the
// messages go after the last one and the output limit is replaced, and the
// result is serialized again. The key order is the one the body already had,
// so a fork's prefix is the one the endpoint has already cached.
//
// The messages arrive already marshaled, so the rewrite knows nothing about
// the wire shapes of the API that sent them: a fork of a Chat Completions
// request and a fork of a Messages one are the same rewrite over different
// payloads.
func AppendMessagesToBody(body []byte, messages [][]byte, maxTokens int) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}

	if len(messages) > 0 {
		var items []json.RawMessage
		if existing, ok := payload["messages"]; ok {
			if err := json.Unmarshal(existing, &items); err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the request snapshot's messages do not decode",
				}
			}
		}
		for _, raw := range messages {
			items = append(items, json.RawMessage(raw))
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the messages do not encode",
			}
		}
		payload["messages"] = encoded
	}

	if maxTokens > 0 {
		raw, err := json.Marshal(maxTokens)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the output limit does not encode",
			}
		}
		payload["max_tokens"] = raw
	}

	// The keys are written in the order the body had them, so the fork's
	// prefix is byte-identical to the original. Every key the payload holds
	// came from the body, except the two the fork may add; those go last,
	// in the order they are named.
	order, err := JSONKeyOrder(body)
	if err != nil {
		return nil, err
	}
	order = WithKeys(order, payload, []string{"messages", "max_tokens"})
	return EncodeObject(payload, order), nil
}

// SetToolChoice rewrites the body's tool choice, keeping the key order
// the body had. A body that carries no tools is returned unchanged:
// there is nothing to forbid, and a rewrite that adds a key would make
// the fork differ from the snapshot for nothing.
func SetToolChoice(body []byte, value json.RawMessage) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}
	if _, ok := payload["tools"]; !ok {
		return body, nil
	}
	payload["tool_choice"] = value
	order, err := JSONKeyOrder(body)
	if err != nil {
		return nil, err
	}
	order = WithKeys(order, payload, []string{"tool_choice"})
	return EncodeObject(payload, order), nil
}
