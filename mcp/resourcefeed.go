package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/feed"

	// The go-sdk's mcp package is aliased everywhere: this package is
	// an mcp too, and its own name would read as its own.
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newProtocolRevision is the protocol revision that changed how a
// resource subscription is granted. Before it, the subscribe request
// answered itself: the server's reply was the grant, and an error was
// the refusal. From it on, Subscribe opens a listen stream and returns
// without waiting, and the grant is the acknowledged notification the
// stream carries — a nil return proves nothing, so the acknowledgement
// the waiters route is what an attempt waits for.
const newProtocolRevision = "2026-07-28"

// resourceAckWait bounds the wait for the subscription's
// acknowledgement: a server that grants every listen answers within
// one round trip, so an acknowledgement that has not arrived by the
// bound is one that is not coming, and the attempt ends with the
// subscription cleaned up behind it.
var resourceAckWait = 10 * time.Second

// unsubscribeWait bounds the unsubscribe an ended attempt sends. The
// context it runs under is fresh — the session's own cancellation must
// not take the unsubscribe with it — and the bound keeps a server that
// will not answer from holding the attempt's return.
const unsubscribeWait = 5 * time.Second

// ResourceFeed returns the runner of one resource feed: one attempt
// subscribes the named server to the URI, reads the resource once —
// whatever it queued before the subscription — and reads it again on
// every update the server sends, until the context ends.
//
// The read is the server's own, and the feed is the reader: two feeds
// on one resource would be two readers of one drain, which is why the
// manifest load refuses them. What the read returned the feed sees
// alone — one record per text content, the server's name as the sender
// and the text verbatim — and a read that fails, a text longer than the
// record every feed path holds, or an emit the session refuses ends the
// attempt, with the records already emitted standing: the rest of that
// read is gone, for the server has drained it. The feed's restart
// policy is what brings another attempt.
//
// An attempt belongs to the generation the server runs when it starts.
// A server that exits ends the attempt with its generation — the feed's
// restart policy brings the next one, and that attempt subscribes on
// the generation the group runs then, so a relaunch is a feed's own
// first-wake contract running again. A server that reads failed — its
// relaunch budget spent, a Connect'ed one whose transport the group
// cannot bring back, or a generation that advertises no resource
// subscriptions — fails the attempt with the fault that suspends
// the feed, so a server that will not or cannot serve the feed does
// not restart one forever; a server that is down ends the attempt
// naming it, and the restart policy tries again.
//
// The runner answers the first-wake contract the servers poo runs
// against document: the waiter and the listener are registered before
// the subscribe round trip, the grant is waited for before the first
// read, and the reads after it answer the updates. An attempt that
// ends — cancelled, failed, its grant refused — takes its
// registrations with it, and the next attempt registers its own.
func ResourceFeed(g *Group, server, uri string) poo.FeedRunner {
	return func(ctx context.Context, io poo.FeedIO) error {
		s := g.state(server)
		if s == nil {
			return fmt.Errorf("the mcp group holds no server %q", server)
		}
		snap := s.snapshot()
		if snap.gen == nil {
			return fmt.Errorf("the mcp group holds no server %q", server)
		}
		switch snap.phase {
		case phaseFailed:
			// The server is gone for good, and the fault suspends the
			// feed with the diagnosis that fits: a relaunchable server
			// spent its budget of relaunches, and one the group holds
			// no relaunch for was simply lost.
			if snap.failNoRelaunch {
				return &poo.FeedFault{
					Code:    poo.FeedFaultServer,
					Message: fmt.Sprintf("server %q is gone and cannot be relaunched", server),
				}
			}
			return &poo.FeedFault{
				Code:    poo.FeedFaultServer,
				Message: fmt.Sprintf("server %s stopped after %d relaunches", server, relaunchBudget),
			}
		case phaseClosed:
			// The group is closing and the feed with it: the error is
			// the context's when the session ended the attempt, and a
			// plain refusal otherwise.
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("the mcp server %q is closed", server)
		case phaseDown:
			// The process ended and the relaunch has not brought it
			// back: the attempt ends here, and the feed's restart
			// policy is what tries again, on the generation the group
			// runs then.
			return fmt.Errorf("%w: %s", ErrServerDown, server)
		}
		return runResourceFeed(ctx, io, server, snap, uri)
	}
}

// runResourceFeed runs one attempt of one resource feed: the
// subscription, the wait for its grant, the first read and the reads
// the updates ask for. The attempt is bound to the snapshot's
// generation — its session, its waiters, its capabilities — so a
// relaunch under it cannot mix one generation's subscription with
// another's reads.
func runResourceFeed(ctx context.Context, io poo.FeedIO, server string, snap serverSnapshot, uri string) error {
	gen := snap.gen
	// The attempt ends with its generation: the context is merged with
	// the generation's own, so a server that exits takes the attempt
	// with it — the feed's restart policy brings the next attempt, on
	// the generation the group runs then — and the session's
	// cancellation ends it the way it ends every feed.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if stop := context.AfterFunc(gen.ctx, cancel); stop != nil {
		defer stop()
	}
	// The attempt's own registrations go in before the subscribe round
	// trip, so a notification that races it still reaches the attempt
	// it belongs to. Both come off when the attempt ends: a late
	// notification never reaches a later attempt, which registers
	// waiter and listener of its own — on the generation the attempt
	// subscribes, for a retired generation's waiters answer no one.
	ack, stopAck := gen.waiters.waitAck(uri)
	defer stopAck()
	pending := make(chan struct{}, 1)
	stopUpdates := gen.waiters.listenUpdates(uri, func() {
		// One read answers every update that arrived while it ran, so
		// the listener only has to say there is a read to do: a second
		// signal while one is pending coalesces into it.
		select {
		case pending <- struct{}{}:
		default:
		}
	})
	defer stopUpdates()
	// The subscription ends whatever end the attempt comes to, with a
	// context of its own so the cancellation cannot take the
	// unsubscribe with it — and only when the subscribe returned, for
	// an attempt whose subscribe failed holds no subscription to end.
	// Its failure is logged, not returned: the attempt is ending
	// either way, and the feed's own error is the one the session
	// reports.
	subscribed := false
	defer func() {
		if !subscribed {
			return
		}
		if gen.ctx.Err() != nil {
			// The generation the subscription lived on is gone, and its
			// session with it: there is no subscription left to end,
			// and an unsubscribe on a closed connection is only noise.
			return
		}
		uctx, ucancel := context.WithTimeout(context.Background(), unsubscribeWait)
		defer ucancel()
		if err := gen.session.Unsubscribe(uctx, &sdkmcp.UnsubscribeParams{URI: uri}); err != nil {
			io.Log.Warn("the resource feed's subscription did not end cleanly",
				"server", server, "uri", uri, "error", err.Error())
		}
	}()

	// The subscription needs the generation's own grant: a relaunch may
	// install a generation whose initialize result advertises no
	// resource subscriptions, and a Subscribe against it fails the
	// plain refusal every attempt — the restart policy would bring
	// another one forever. The generation's own capability ends the
	// attempt with the fault that suspends the feed, the way a server
	// that cannot come back does.
	if !subscribes(gen.result) {
		return &poo.FeedFault{
			Code:    poo.FeedFaultServer,
			Message: fmt.Sprintf("server %q advertises no resource subscriptions", server),
		}
	}
	if err := gen.session.Subscribe(ctx, &sdkmcp.SubscribeParams{URI: uri}); err != nil {
		return fmt.Errorf("the mcp server %q: subscribing to %q: %w", server, uri, err)
	}
	subscribed = true
	if newRevision(gen.result) {
		select {
		case granted := <-ack:
			if !granted {
				return fmt.Errorf("the mcp server %q acknowledged the subscription to %q without granting it", server, uri)
			}
		case <-time.After(resourceAckWait):
			return fmt.Errorf("the mcp server %q never acknowledged the subscription to %q", server, uri)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := drainResource(ctx, io, server, gen, uri); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pending:
			if err := drainResource(ctx, io, server, gen, uri); err != nil {
				return err
			}
		}
	}
}

// newRevision reports whether the generation negotiated the revision
// that grants a subscription by acknowledged notification.
func newRevision(result *sdkmcp.InitializeResult) bool {
	return result != nil && result.ProtocolVersion >= newProtocolRevision
}

// drainResource reads the URI once and emits what came back: one record
// per text content, the server's name as the sender and the text
// verbatim.
//
// A content with no text is skipped, and a blob is skipped with a line
// in the feed's log — the feed reads text, and the blob's bytes are
// not the log's to hold. A text longer than the record every feed path
// holds ends the attempt naming it: the resource is the server's to
// bound, and a record past the limit is the server answering with
// something that is not a message. So does a read that fails or an
// emit the session refuses: the records already emitted stand, and the
// rest of the read is lost, the server having drained it.
func drainResource(ctx context.Context, io poo.FeedIO, server string, gen *generation, uri string) error {
	res, err := gen.session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		return fmt.Errorf("the mcp server %q: reading %q: %w", server, uri, err)
	}
	for _, contents := range res.Contents {
		if contents.Blob != nil {
			io.Log.Warn("the resource returned a blob the feed does not read", "server", server, "uri", uri)
			continue
		}
		if contents.Text == "" {
			continue
		}
		if len(contents.Text) > feed.MaxLineBytes {
			return fmt.Errorf("the mcp server %q: the record read from %q is %d bytes, and a record is at most %d",
				server, uri, len(contents.Text), feed.MaxLineBytes)
		}
		if err := io.Emit(server, contents.Text, nil, nil); err != nil {
			return err
		}
	}
	return nil
}
