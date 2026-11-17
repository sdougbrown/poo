package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
)

// The compaction items.
//
// A compaction request is a normal request whose last input item is the
// trigger. Its answer holds the compacted state as an item this package cannot
// read: only the endpoint can, and a later request gives the item back
// verbatim. Turning that answer into the window the core installs is here,
// because the items are the API's.

// compactionTrigger is the input item that asks the endpoint to compact.
//
// It is the request's last item: the endpoint compacts the input that came
// before it.
const compactionTrigger = `{"type":"compaction_trigger"}`

// maxCompactionItems bounds the output items one compaction may install.
//
// A compaction answer holds the compacted state, and at most the visible text
// the endpoint keeps beside it. An answer with more items than that is not a
// window a transport can install. Refusing it keeps a faulty endpoint from
// growing the window without bound.
const maxCompactionItems = 32

// AppendCompactionTrigger puts the compaction trigger after the request's last
// input item.
//
// The trigger goes last because that is the position the endpoint reads it in:
// it compacts the input that came before it.
func AppendCompactionTrigger(items []json.RawMessage) []json.RawMessage {
	return append(items, json.RawMessage(compactionTrigger))
}

// CompactionWindow turns the items a compaction answer carried into the window
// the core installs.
//
// The endpoint's items keep their order. A compaction item is the compacted
// state itself, so it is required and travels back verbatim. Only the endpoint
// can read it, and nothing else can take its place. A message item is text the
// endpoint kept visible beside it. An item of any other type is data this
// package cannot install. Dropping it would lose context the endpoint meant to
// keep, so the whole window is refused.
func CompactionWindow(items []json.RawMessage) (poo.ProviderWindow, error) {
	if len(items) > maxCompactionItems {
		return poo.ProviderWindow{}, compactionFailure(
			"the compaction response carries %d items, more than the %d a window holds",
			len(items), maxCompactionItems)
	}
	var window poo.ProviderWindow
	compactions := 0
	for _, raw := range items {
		item, ok := decodeItem(raw)
		if !ok {
			return poo.ProviderWindow{}, compactionFailure("the compaction response carries an item that does not decode")
		}
		switch item.Type {
		case "compaction":
			if item.EncryptedContent == "" {
				return poo.ProviderWindow{}, compactionFailure(
					"the compaction item carries no encrypted content to give back")
			}
			compactions++
			window.Items = append(window.Items, poo.ContentBlock{
				Kind: poo.ContentProvider,
				Provider: &poo.ProviderBlock{
					API:      poo.APIResponses,
					Type:     "compaction",
					Data:     append(json.RawMessage(nil), raw...),
					Required: true,
				},
			})
		case "message":
			if text := partsText(item.Content); strings.TrimSpace(text) != "" {
				window.Items = append(window.Items, poo.ContentBlock{Kind: poo.ContentText, Text: text})
			}
		default:
			return poo.ProviderWindow{}, compactionFailure(
				"the compaction response carries a %q item, which a window cannot hold", item.Type)
		}
	}
	switch {
	case compactions == 0:
		return poo.ProviderWindow{}, compactionFailure("the compaction response carries no compaction item")
	case compactions > 1:
		return poo.ProviderWindow{}, compactionFailure(
			"the compaction response carries %d compaction items, want one", compactions)
	}
	return window, nil
}

// compactionFailure is a failure a compaction answer reports.
//
// The endpoint answered, and what it answered cannot be installed. The
// failure is the endpoint's, so the run may ask again at a later boundary.
func compactionFailure(format string, args ...any) error {
	return &poo.ProviderError{Kind: poo.ProviderKindServer, Message: fmt.Sprintf(format, args...)}
}
