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
//
// Updates reach two shapes of registration. A waiter is a channel of
// one signal, fired once and then gone: it suits the attempt that
// asks one question — whether the subscription was acknowledged — and
// an attempt that ended never hears a notification that arrives late.
// A listener is the running feed's own registration: it is called for
// every update until it is stopped, because a feed that re-registered
// per update would leave a window in which an update lands on no one,
// and a drain the feed never learns of would sit unread. One
// notification, one routing: both shapes are answered by the same
// updated call, which is the only path an update takes.

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
// the waiter a later attempt registers is answered only by a
// notification its own URI carries.
type resourceWaiters struct {
	// mu guards both tables.
	mu sync.Mutex
	// updates holds the update waiters, by URI.
	updates map[string][]chan struct{}
	// listeners holds the update listeners, by URI: the running feeds'
	// own registrations, called for every update until they are
	// stopped.
	listeners map[string][]*listener
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
// channel receives one answer: true when an acknowledgement grants
// the URI, false when its own stream's subscription was refused. The
// waiter answers one acknowledgement and then is gone, and an
// acknowledgement that names another resource is not this waiter's
// answer: another feed's grant leaves it waiting for its own.
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
	hearing := w.listeners[uri]
	w.mu.Unlock()
	for _, ch := range waiting {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	// The listeners are called with no lock held, in the order they
	// registered, so a listener may register and stop listeners itself.
	for _, l := range hearing {
		l.called()
	}
}

// listener is one running feed's update registration. The pointer is
// the registration's identity: a stop removes the listener its own
// call registered, where a bare func value cannot say which one it
// was.
type listener struct {
	// called runs when the URI is updated.
	called func()
}

// listenUpdates registers a listener the URI's updates call until stop
// unregisters it.
//
// The listener is the running feed's own registration, and it belongs
// to one attempt: the attempt stops it when it ends, so a later
// attempt hears only the updates its own listener carries, the way a
// later attempt waits for an acknowledgement of its own.
func (w *resourceWaiters) listenUpdates(uri string, listen func()) (stop func()) {
	l := &listener{called: listen}
	w.mu.Lock()
	if w.listeners == nil {
		w.listeners = map[string][]*listener{}
	}
	w.listeners[uri] = append(w.listeners[uri], l)
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		w.listeners[uri] = removeListener(w.listeners[uri], l)
		if len(w.listeners[uri]) == 0 {
			delete(w.listeners, uri)
		}
		w.mu.Unlock()
	}
}

// removeListener returns the list without the listener.
func removeListener(list []*listener, l *listener) []*listener {
	for i, held := range list {
		if held == l {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// acknowledged answers the waiters the acknowledgement grants.
//
// A SEP-2575 acknowledgement belongs to one listen stream and names
// only that stream's URIs. Two feeds on one server are two streams, so
// the grant each acknowledgement carries answers its own URI's waiter
// alone, and every other attempt keeps waiting for its own: one feed's
// acknowledgement is never another feed's refusal.
//
// An acknowledgement that names no resource URI has refused its own
// stream's subscription, and nothing it carries says which stream that
// was. One attempt waiting to hear it is that attempt's answer;
// several waiting are each answered by the acknowledgement of their
// own stream, and the attempt's own bound ends any whose answer never
// comes. An acknowledgement whose URIs no one is waiting for has no
// one to answer.
func (w *resourceWaiters) acknowledged(granted []string) {
	w.mu.Lock()
	answers := make([]ackAnswer, 0, len(granted)+1)
	for _, uri := range granted {
		if chans := w.acks[uri]; len(chans) > 0 {
			answers = append(answers, ackAnswer{waiters: chans, granted: true})
			delete(w.acks, uri)
		}
	}
	if len(granted) == 0 && len(w.acks) == 1 {
		for uri, chans := range w.acks {
			answers = append(answers, ackAnswer{waiters: chans})
			delete(w.acks, uri)
		}
	}
	w.mu.Unlock()
	for _, answer := range answers {
		for _, ch := range answer.waiters {
			select {
			case ch <- answer.granted:
			default:
			}
		}
	}
}

// ackAnswer is one acknowledgement's answers: the waiters a grant
// names, or the ones a refusal answers, and the word they all hear.
type ackAnswer struct {
	waiters []chan bool
	granted bool
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
