package chat

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// AppendMessages appends messages to a serialized request body.
//
// The fork re-sends the snapshot as it was built, so the rewrite is the one
// the support package owns: the messages are marshaled here, because the
// wire shape is this package's, and the body they join is the caller's own.
//
// The appended messages arrive in the wire shape, already shaped by the
// profiles the snapshot was shaped with: they are new, so the caller decides
// what they need, and this package decides only where they go.
func AppendMessages(body []byte, messages []Message, maxTokens int) ([]byte, error) {
	encoded := make([][]byte, 0, len(messages))
	for _, cm := range messages {
		raw, err := json.Marshal(cm)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the appended message does not encode",
			}
		}
		encoded = append(encoded, raw)
	}
	return support.AppendMessagesToBody(body, encoded, maxTokens)
}
