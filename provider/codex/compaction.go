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
	"errors"
	"io"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/responses"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// Compact asks the endpoint to compact the view and returns the window it
// produced.
//
// The request is shaped from the view as it stands now, not from the last
// request's snapshot. The snapshot can be a turn behind, and the reply and
// results the run has since produced are what a compaction must keep. The
// trigger follows the view's last item, and the endpoint's answer becomes the
// window verbatim.
//
// The request goes through the same opening as a turn. The credential, the
// headers, the run's cancellation, the transport's timeout and the failure
// classification mean what they mean there. A failure that arrives before the
// endpoint answered is retried. One that arrives mid-stream settles this call,
// and the run asks for a new window at a later boundary.
//
// Whatever the answer holds, the request was billed the moment the endpoint
// answered it. A failure still reports what the endpoint said the request cost.
// Only a failure before the answer opened reports nothing: a credential, a
// configuration or a refused request never reached the work the endpoint
// charges for.
func (t *Transport) Compact(ctx context.Context, in poo.NativeCompaction) (poo.NativeCompactionResult, error) {
	body, err := t.compactionBody(in)
	if err != nil {
		return poo.NativeCompactionResult{}, err
	}
	// The session being compacted names the request's identity, the way a
	// turn's does: the headers and the cache key are the same id, so the
	// window is cached under its own session and not the transport's.
	session := t.sessionID(in.SessionID)
	// The opened reader is kept whole because the window is read from the
	// response's own items, which the stream interface does not carry.
	var stream *responses.Reader
	open := func(ctx context.Context) (support.Reader, error) {
		reader, err := t.open(ctx, body, session)
		if err != nil {
			return nil, err
		}
		reader.Capture()
		stream = reader
		return reader, nil
	}
	report := support.Reporter(t.cfg.OnAttempt, nil)
	if _, _, err := support.OpenWithRetry(ctx, t.policy, open, report); err != nil {
		return poo.NativeCompactionResult{}, err
	}
	defer stream.Close()
	// The endpoint answered, so the request is accounted for whatever its
	// answer holds. An interrupted stream and an answer this transport refuses
	// both cost what the endpoint reported. When the endpoint reported nothing,
	// the core charges the estimate.
	if err := drainCompaction(stream); err != nil {
		return poo.NativeCompactionResult{Billed: true, Usage: stream.ReportedUsage()}, err
	}
	window, err := responses.CompactionWindow(stream.Output())
	if err != nil {
		return poo.NativeCompactionResult{Billed: true, Usage: stream.ReportedUsage()}, err
	}
	return poo.NativeCompactionResult{Window: window, Usage: stream.ReportedUsage(), Billed: true}, nil
}

// compactionBody turns a native compaction into the wire payload.
//
// The view is encoded as a normal request's history: its system message
// becomes the instructions, and every message becomes the input items the
// endpoint takes. No tools are offered, because a compaction is the endpoint's
// own work and a call in its answer would not be the window. The request names
// the session being compacted, so its cache key is that session's.
func (t *Transport) compactionBody(in poo.NativeCompaction) ([]byte, error) {
	if in.View == nil {
		return nil, support.ConfigErrorf("the compaction was given no view to encode")
	}
	out, err := t.shapeRequest(poo.ChatRequest{
		Model:     in.Model.ModelID,
		ModelInfo: in.Model,
		Messages:  in.View.Messages(),
		SessionID: in.SessionID,
	})
	if err != nil {
		return nil, err
	}
	out.Input = responses.AppendCompactionTrigger(out.Input)
	return responses.Encode(out)
}

// drainCompaction reads a compaction response to its end.
//
// The stream's events are not the answer: the items the window installs
// arrive whole in the terminal event, or in an item event before it. Reading
// them through is what releases the response, and a stream that failed carries
// its failure out. A response the endpoint cut at its output limit is refused
// the same way. The state it carries is the part the endpoint reached, not the
// compacted view.
func drainCompaction(reader *responses.Reader) error {
	for {
		ev, err := reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if ev.Kind == "finish" && ev.FinishReason == poo.FinishLength {
			return &poo.ProviderError{
				Kind:    poo.ProviderKindServer,
				Message: "the compaction response stopped on its output limit, so the state it carries is short",
			}
		}
	}
}
