package chat

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// AppendMessages appends messages to a serialized request body.
//
// The fork re-sends the snapshot as it was built: the body is decoded, the
// messages go after the last one and the output limit is replaced, and the
// result is serialized again. The key order is the one the body already had,
// because the keys are written in the order the body had them and any key it
// does not know keeps its position. Every other byte is the body's own, so a
// fork's prefix is the one the endpoint has already cached.
//
// The appended messages arrive in the wire shape, already shaped by the
// profiles the snapshot was shaped with: they are new, so the caller decides
// what they need, and this package decides only where they go.
func AppendMessages(body []byte, messages []Message, maxTokens int) ([]byte, error) {
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
		for _, cm := range messages {
			encoded, err := json.Marshal(cm)
			if err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the appended message does not encode",
				}
			}
			items = append(items, encoded)
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
	// prefix is byte-identical to the original. Every key the payload
	// holds came from the body, except the two the fork may add; those
	// go last, in the order they are named.
	order, err := support.JSONKeyOrder(body)
	if err != nil {
		return nil, err
	}
	order = support.WithKeys(order, payload, []string{"messages", "max_tokens"})
	return support.EncodeObject(payload, order), nil
}
