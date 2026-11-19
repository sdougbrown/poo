package responses

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/transport"
)

// AppendInput appends messages to a serialized request body.
//
// The body is decoded, and the converted messages are appended after the last
// input item. The result is serialized again in the order the body already had
// its keys. Every other byte is the body's own, so a fork's prefix is the one
// the endpoint has already cached.
func AppendInput(body []byte, appended []poo.Message, reads Modalities) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}

	if len(appended) > 0 {
		var items []json.RawMessage
		if raw, ok := payload["input"]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the request snapshot's input does not decode",
				}
			}
		}
		converted, instructions, err := InputItems(appended, reads, len(items))
		if err != nil {
			return nil, err
		}
		// A system message the fork appends cannot become the request's
		// instructions: those are part of the snapshot's prefix. It rides
		// as the instruction item a later system message rides as.
		if instructions != "" {
			raw, err := json.Marshal(inputMessage{
				Role:    "developer",
				Content: []inputPart{{Type: "input_text", Text: instructions}},
			})
			if err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the appended instruction does not encode",
				}
			}
			items = append(items, raw)
		}
		items = append(items, converted...)
		raw, err := json.Marshal(items)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the input items do not encode",
			}
		}
		payload["input"] = raw
	}

	order, err := transport.JSONKeyOrder(body)
	if err != nil {
		return nil, err
	}
	// A snapshot can lack input. Include the added field in the order so
	// encoding retains the appended items.
	order = transport.WithKeys(order, payload, []string{"input"})
	return transport.EncodeObject(payload, order), nil
}
