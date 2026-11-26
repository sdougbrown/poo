package mcp

import (
	"context"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The resource notifications a server sends reach the waiters their
// URIs name, through the routing the client installs before it
// connects. The resource feeds (Stage 6) are the waiters: one attempt
// registers a waiter, and the notification that answers it — an
// update, or the subscription's acknowledgement — closes it.
//
// The waiters are per server, because the notifications are: a URI two
// servers hold is two resources, and each server's waiters answer to
// that server alone.

// acknowledgedMethod is the notification a server sends when it
// grants the subscriptions a client asked to listen for. The SDK has
// no handler seam for it — the default discards it — so the receiving
// middleware is where it reaches the waiters.
const acknowledgedMethod = "notifications/subscriptions/acknowledged"

// resourceWaiters routes a server's resource notifications to the
// per-URI waiters registered for them.
//
// A waiter is a channel of one signal, fired once and then gone: an
// attempt that ended never hears a notification that arrives late, and
// a late notification never satisfies a later attempt, because the
// later attempt registered a waiter of its own.
type resourceWaiters struct {
	// mu guards both tables.
	mu sync.Mutex
	// updates holds the update waiters, by URI.
	updates map[string][]chan struct{}
	// acks holds the acknowledgement waiters, by URI.
	acks map[string][]chan bool
}

// waitUpdate registers a waiter for the URI's updates. The channel
// receives one signal when the server reports the resource updated;
// stop unregisters it without a signal, and an attempt that ends
// always stops its waiter.
func (w *resourceWaiters) waitUpdate(uri string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	if w.updates == nil {
		w.updates = map[string][]chan struct{}{}
	}
	w.updates[uri] = append(w.updates[uri], ch)
	w.mu.Unlock()
	return ch, func() { w.dropUpdate(uri, ch) }
}

// dropUpdate unregisters one update waiter.
func (w *resourceWaiters) dropUpdate(uri string, ch chan struct{}) {
	w.mu.Lock()
	w.updates[uri] = removeWaiter(w.updates[uri], ch)
	if len(w.updates[uri]) == 0 {
		delete(w.updates, uri)
	}
	w.mu.Unlock()
}

// waitAck registers a waiter for the URI's acknowledgement. The
// channel receives one answer: true when the acknowledgement grants
// the URI, false when it does not. The waiter answers the first
// acknowledgement and then is gone, so a grant that names another
// resource ends the wait with its own answer.
func (w *resourceWaiters) waitAck(uri string) (<-chan bool, func()) {
	ch := make(chan bool, 1)
	w.mu.Lock()
	if w.acks == nil {
		w.acks = map[string][]chan bool{}
	}
	w.acks[uri] = append(w.acks[uri], ch)
	w.mu.Unlock()
	return ch, func() { w.dropAck(uri, ch) }
}

// dropAck unregisters one acknowledgement waiter.
func (w *resourceWaiters) dropAck(uri string, ch chan bool) {
	w.mu.Lock()
	w.acks[uri] = removeWaiter(w.acks[uri], ch)
	if len(w.acks[uri]) == 0 {
		delete(w.acks, uri)
	}
	w.mu.Unlock()
}

// updated signals the URI's update waiters. Every waiter registered
// for the URI is answered and unregistered: one update is one answer
// per waiter, and the waiter that wants another registers again.
func (w *resourceWaiters) updated(uri string) {
	w.mu.Lock()
	waiting := w.updates[uri]
	delete(w.updates, uri)
	w.mu.Unlock()
	for _, ch := range waiting {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// acknowledged answers every registered acknowledgement waiter with
// whether the server granted its URI. The acknowledgement is one
// answer for the whole listen: every waiter is answered and
// unregistered, whatever its URI came to.
func (w *resourceWaiters) acknowledged(granted []string) {
	w.mu.Lock()
	waiting := w.acks
	w.acks = map[string][]chan bool{}
	w.mu.Unlock()
	if len(waiting) == 0 {
		return
	}
	set := make(map[string]bool, len(granted))
	for _, uri := range granted {
		set[uri] = true
	}
	for uri, chans := range waiting {
		for _, ch := range chans {
			select {
			case ch <- set[uri]:
			default:
			}
		}
	}
}

// removeWaiter returns the list without the waiter.
func removeWaiter[T comparable](list []T, waiter T) []T {
	for i, held := range list {
		if held == waiter {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// routeAcknowledged returns the middleware that routes the
// subscription acknowledgements to the waiters.
//
// Every message is passed on: the middleware observes, and the SDK's
// own handling of each one still runs.
func routeAcknowledged(waiters *resourceWaiters) sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method == acknowledgedMethod {
				if params, ok := req.GetParams().(*sdkmcp.SubscriptionsAcknowledgedParams); ok && params != nil {
					waiters.acknowledged(params.Notifications.ResourceSubscriptions)
				}
			}
			return next(ctx, method, req)
		}
	}
}
