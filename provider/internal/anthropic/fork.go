package anthropic

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// appendBody appends messages to a serialized request body, and replaces the
// output limit.
//
// The body is decoded, the messages go after the last one and the output
// limit is replaced, and the result is serialized again. The key order is
// the one the body already had, so a fork's prefix is the one the endpoint
// has already cached.
func appendBody(body []byte, messages []Message, maxTokens int) ([]byte, error) {
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
		for _, m := range messages {
			encoded, err := json.Marshal(m)
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
	// prefix is byte-identical to the original. Every key the payload holds
	// came from the body, except the two the fork may add; those go last, in
	// the order they are named.
	order, err := support.JSONKeyOrder(body)
	if err != nil {
		return nil, err
	}
	order = support.WithKeys(order, payload, []string{"messages", "max_tokens"})
	return support.EncodeObject(payload, order), nil
}
