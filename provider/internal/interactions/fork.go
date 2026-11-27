package interactions

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/transport"
)

// AppendSteps appends messages to a serialized request body, and replaces the
// output limit.
//
// The body is decoded, and the converted messages are appended after the last
// step of the input. The result is serialized again in the order the body
// already had its keys, and the generation config keeps its own order: every
// other byte is the body's own, so a fork's prefix is the one the endpoint has
// already cached.
//
// A system message the fork appends cannot become the request's instruction:
// that is part of the snapshot's prefix. It is dropped the way the Messages
// transport drops one, because a fork appends a question, not a new frame.
func AppendSteps(body []byte, appended []poo.Message, maxTokens int) ([]byte, error) {
	payload, err := decodeBody(body)
	if err != nil {
		return nil, err
	}

	if len(appended) > 0 {
		steps, _, err := Messages(appended)
		if err != nil {
			return nil, err
		}
		var existing []json.RawMessage
		if raw, ok := payload["input"]; ok {
			if err := json.Unmarshal(raw, &existing); err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the request snapshot's input does not decode",
				}
			}
		}
		for _, step := range steps {
			raw, err := json.Marshal(step)
			if err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the appended step does not encode",
				}
			}
			existing = append(existing, raw)
		}
		encoded, err := json.Marshal(existing)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the input steps do not encode",
			}
		}
		payload["input"] = encoded
	}

	if maxTokens > 0 {
		config, err := readConfig(payload)
		if err != nil {
			return nil, err
		}
		limit, err := json.Marshal(maxTokens)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the output limit does not encode",
			}
		}
		config.set("max_output_tokens", limit)
		payload["generation_config"] = config.encode()
	}

	return encodeBody(body, payload)
}

// WithoutTools rewrites the body's tool choice to none, so the fork's ask
// cannot be answered with work. A body that carries no tools is returned
// unchanged: there is nothing to forbid, and a rewrite that adds a field would
// make the fork differ from the snapshot for nothing.
func WithoutTools(body []byte) ([]byte, error) {
	payload, err := decodeBody(body)
	if err != nil {
		return nil, err
	}
	if _, ok := payload["tools"]; !ok {
		return body, nil
	}
	config, err := readConfig(payload)
	if err != nil {
		return nil, err
	}
	config.set("tool_choice", json.RawMessage(`"none"`))
	payload["generation_config"] = config.encode()
	return encodeBody(body, payload)
}

// decodeBody decodes a serialized request body.
func decodeBody(body []byte) (map[string]json.RawMessage, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}
	return payload, nil
}

// encodeBody writes a payload back in the key order the body had.
func encodeBody(body []byte, payload map[string]json.RawMessage) ([]byte, error) {
	order, err := transport.JSONKeyOrder(body)
	if err != nil {
		return nil, err
	}
	order = transport.WithKeys(order, payload, []string{"input", "generation_config"})
	return transport.EncodeObject(payload, order), nil
}

// config is a request's generation config, decoded with the order its own keys
// were written in.
//
// The fields a fork does not change keep their place, so the config it rewrites
// differs from the snapshot's only in the fields it meant to change.
type config struct {
	fields map[string]json.RawMessage
	order  []string
}

// readConfig decodes a payload's generation config.
//
// A body that carries none opens an empty one, because a fork that sets the
// output limit is writing a field the request needs and the snapshot did not
// carry.
func readConfig(payload map[string]json.RawMessage) (config, error) {
	raw, ok := payload["generation_config"]
	if !ok {
		return config{fields: map[string]json.RawMessage{}}, nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return config{}, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot's generation config does not decode",
		}
	}
	order, err := transport.JSONKeyOrder(raw)
	if err != nil {
		return config{}, err
	}
	return config{fields: fields, order: order}, nil
}

// set writes one of the config's fields.
func (c *config) set(name string, value json.RawMessage) {
	c.fields[name] = value
	c.order = transport.WithKeys(c.order, c.fields, []string{name})
}

// encode writes the config as a JSON object.
func (c config) encode() json.RawMessage {
	return transport.EncodeObject(c.fields, c.order)
}
