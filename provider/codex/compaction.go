// The native compaction.
//
// The endpoint compacts its own conversation when a request's last input item
// is the compaction trigger. Its answer holds the compacted state as an item
// the transport cannot read, and a later request gives the item back verbatim.
// That is the whole of a provider window: the endpoint produces it, the
// transport carries it, and the core installs it.

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// compactionTrigger is the input item that asks the endpoint to compact.
//
// It is the request's last item: the endpoint compacts the input that came
// before it.
var compactionTrigger = json.RawMessage(`{"type":"compaction_trigger"}`)

// maxCompactionItems bounds the output items one compaction may install.
//
// A compaction answer holds the compacted state, and at most the visible text
// the endpoint keeps beside it. An answer with more items than that is not a
// window this transport can install, and refusing it keeps a faulty endpoint
// from growing the window without bound.
const maxCompactionItems = 32

// Compact asks the endpoint to compact the view and returns the window it
// produced.
//
// The request is shaped from the view as it stands now, not from the last
// request's snapshot: the snapshot can be a turn behind, and the reply and the
// results the run has since produced are exactly what a compaction must keep.
// The trigger follows the view's last item, and the endpoint's answer becomes
// the window verbatim.
//
// The request goes through the same opening as a turn, so the credential, the
// headers, the run's cancellation, the transport's timeout and the failure
// classification mean what they mean there. A failure that arrives before the
// endpoint answered is retried. One that arrives mid-stream settles this call,
// and the run asks for a new window at a later boundary.
func (t *Transport) Compact(ctx context.Context, in poo.NativeCompaction) (poo.NativeCompactionResult, error) {
	body, err := t.compactionBody(in)
	if err != nil {
		return poo.NativeCompactionResult{}, err
	}
	// The opened reader is kept whole because the window is read from the
	// response's own items, which the stream interface does not carry.
	var stream *streamReader
	open := func(ctx context.Context) (support.Reader, error) {
		reader, err := t.open(ctx, body, t.sessionID(""))
		if err != nil {
			return nil, err
		}
		reader.captureOutput = true
		stream = reader
		return reader, nil
	}
	report := support.Reporter(t.cfg.OnAttempt, nil)
	if _, _, err := support.OpenWithRetry(ctx, t.policy, open, report); err != nil {
		return poo.NativeCompactionResult{}, err
	}
	defer stream.Close()
	if err := drainCompaction(stream); err != nil {
		return poo.NativeCompactionResult{}, err
	}
	window, err := compactionWindow(stream.output)
	if err != nil {
		return poo.NativeCompactionResult{}, err
	}
	// The request is billed like any other, so the core gets the
	// endpoint's own counts with the window. An answer that reported
	// none is left without a report, and the core charges the estimate a
	// side request gets.
	return poo.NativeCompactionResult{Window: window, Usage: stream.reportedUsage()}, nil
}

// compactionBody turns a native compaction into the wire payload.
//
// The view is encoded as a normal request's history: its system message
// becomes the instructions, and every message becomes the input items the
// endpoint takes. No tools are offered, because a compaction is the endpoint's
// own work and a call in its answer would not be the window.
func (t *Transport) compactionBody(in poo.NativeCompaction) ([]byte, error) {
	if in.View == nil {
		return nil, support.ConfigErrorf("the compaction was given no view to encode")
	}
	out, err := t.shapeRequest(poo.ChatRequest{
		Model:     in.Model.ModelID,
		ModelInfo: in.Model,
		Messages:  in.View.Messages(),
	})
	if err != nil {
		return nil, err
	}
	out.Input = append(out.Input, compactionTrigger)
	return encodeBody(out)
}

// drainCompaction reads a compaction response to its end.
//
// The stream's events are not the answer: the items the window installs
// arrive whole in the terminal event, or in an item event before it. Reading
// them through is what releases the response, and a stream that failed carries
// its failure out. A response the endpoint cut at its output limit is refused
// the same way: the state it carries is the part the endpoint reached, not the
// compacted view.
func drainCompaction(reader *streamReader) error {
	for {
		ev, err := reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if ev.Kind == "finish" && ev.FinishReason == poo.FinishLength {
			return compactionFailure(
				"the compaction response stopped on its output limit, so the state it carries is short")
		}
	}
}

// compactionWindow turns the items a compaction response carried into the
// window the core installs.
//
// The endpoint's items keep their order. A compaction item is the compacted
// state itself, so it is required and travels back verbatim: only the endpoint
// can read it, and nothing else can take its place. A message item is text the
// endpoint kept visible beside it. An item of any other type is data this
// transport cannot install, and dropping it would lose context the endpoint
// meant to keep, so the whole window is refused instead.
func compactionWindow(items []json.RawMessage) (poo.ProviderWindow, error) {
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

// compactionFailure is a failure a compaction response reports.
//
// The endpoint answered, and what it answered cannot be installed. The
// failure is the endpoint's, so the run may ask again at a later boundary.
func compactionFailure(format string, args ...any) error {
	return &poo.ProviderError{Kind: poo.ProviderKindServer, Message: fmt.Sprintf(format, args...)}
}
