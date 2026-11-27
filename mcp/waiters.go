package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The resource notifications a server sends reach the waiters their
// URIs name, through the routing the client installs before it
// connects. The resource feeds (Stage 6) are the waiters: one attempt
// registers a waiter, and the notification that answers it — an
// update, or the subscription's acknowledgement — closes it.
//
// The waiters are per generation, because the notifications are: a URI
// one server holds across a relaunch is two routings over time — the
// retired generation's waiters answer no one, and each attempt
// registers on the generation it subscribes.
//
// Updates reach one shape of registration: the listener, a running
// feed's own, called for every update until it is stopped — a feed
// that re-registered per update would leave a window in which an
// update lands on no one, and a drain the feed never learns of would
// sit unread. Acknowledgements reach a waiter, a channel of one
// signal, fired once and then gone: it suits the attempt that asks
// one question — whether the subscription was acknowledged — and an
// attempt that ended never hears a notification that arrives late.
// One notification, one routing: updates take the updated call and
// acknowledgements the acknowledged call, the only path each takes.

// acknowledgedMethod is the notification a server sends when it
// grants the subscriptions a client asked to listen for. The SDK has
// no handler seam for it — the default discards it — so the receiving
// middleware is where it reaches the waiters.
const acknowledgedMethod = "notifications/subscriptions/acknowledged"

// listenMethod is the request that opens a SEP-2575 listen stream.
const listenMethod = "subscriptions/listen"

// resourceWaiters routes a server's resource notifications to the
// per-URI waiters registered for them.
//
// A waiter is a channel of one signal, fired once and then gone: an
// attempt that ended never hears a notification that arrives late, and
// the waiter a later attempt registers is answered only by a
// notification its own URI carries.
type resourceWaiters struct {
	// mu guards all three tables.
	mu sync.Mutex
	// listeners holds the update listeners, by URI: the running feeds'
	// own registrations, called for every update until they are
	// stopped.
	listeners map[string][]*listener
	// acks holds the acknowledgement waiters, by URI.
	acks map[string][]chan bool
	// streams holds the listen stream records, by the subscription ID
	// the stream's acknowledgement carries: the record that attributes
	// an acknowledgement to the attempt that asked for it.
	streams map[string]*streamRecord
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

// dropAck unregisters one acknowledgement waiter, and retires with it
// the stream records tagged to it: the attempt that asked for those
// streams has ended, so an acknowledgement that names one of them has
// no attempt left to answer.
func (w *resourceWaiters) dropAck(uri string, ch chan bool) {
	w.mu.Lock()
	w.acks[uri] = removeWaiter(w.acks[uri], ch)
	if len(w.acks[uri]) == 0 {
		delete(w.acks, uri)
	}
	for id, record := range w.streams {
		if record.ack == ch {
			delete(w.streams, id)
		}
	}
	w.mu.Unlock()
}

// streamRecord is one listen stream's entry: the URI the stream asked
// for, and the ack waiter of the attempt that asked — the tag that
// binds the stream's acknowledgement to that attempt alone, and that
// retires the record when the attempt ends.
type streamRecord struct {
	uri string
	ack chan bool
}

// updated calls the URI's update listeners: one update, one routing,
// every listener registered for the URI hears it.
func (w *resourceWaiters) updated(uri string) {
	w.mu.Lock()
	// The call copies the URI's listeners, for the iteration runs with
	// no lock held: a listener's own call may stop listeners, and a stop
	// that wrote through the shared array would race the iteration.
	hearing := make([]*listener, len(w.listeners[uri]))
	copy(hearing, w.listeners[uri])
	w.mu.Unlock()
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
		w.listeners[uri] = removeWaiter(w.listeners[uri], l)
		if len(w.listeners[uri]) == 0 {
			delete(w.listeners, uri)
		}
		w.mu.Unlock()
	}
}

// streamOpened records the URI a listen stream asked for, by the
// subscription ID its acknowledgement will carry. The transport's
// write of the listen request is where the pair is seen together: the
// request names the URI, and the ID the server answers by is the ID
// the request was written with.
//
// The record is tagged with the attempt's own ack waiter — the latest
// one registered for the URI, which is this attempt's: an attempt
// registers before it subscribes, and one feed's attempts are
// sequential, so the waiter the record names is the attempt the
// stream belongs to.
func (w *resourceWaiters) streamOpened(id, uri string) {
	w.mu.Lock()
	if w.streams == nil {
		w.streams = map[string]*streamRecord{}
	}
	record := &streamRecord{uri: uri}
	if chans := w.acks[uri]; len(chans) > 0 {
		record.ack = chans[len(chans)-1]
	}
	w.streams[id] = record
	w.mu.Unlock()
}

// acknowledged answers the waiters the acknowledgement grants.
//
// A SEP-2575 acknowledgement belongs to one listen stream, and its
// subscription ID names the stream it answers: the one whose listen
// request the transport recorded. An acknowledgement that carries the
// ID answers its own stream's waiters — granted when the grant names
// the URI the stream asked for, refused when it grants nothing — and
// no other attempt hears it, so one feed's refusal is never another
// feed's answer. An attempt that has ended took its waiter with it,
// and its stream's record went with the waiter: an acknowledgement
// that arrives late names a record that is gone, and answers no one.
//
// An acknowledgement that names no stream cannot be attributed to one
// attempt. A grant still names the URIs it answers, and an empty grant
// answers a lone waiter — one attempt waiting to hear it is that
// attempt's answer — while several waiting are each answered by the
// acknowledgement of their own stream, and the attempt's own bound
// ends any whose answer never comes. An acknowledgement whose URIs no
// one is waiting for has no one to answer.
func (w *resourceWaiters) acknowledged(id string, granted []string) {
	w.mu.Lock()
	var answers []ackAnswer
	if record, ok := w.streams[id]; ok {
		// The acknowledgement named its stream: whatever it carries is
		// that stream's answer, and its record is spent either way. The
		// answer reaches only the attempt the record tags — the waiter
		// still registered is the attempt the stream belongs to, and one
		// whose attempt has ended has no waiter to hear it.
		delete(w.streams, id)
		if chans := w.acks[record.uri]; len(chans) > 0 && containsWaiter(chans, record.ack) {
			answers = append(answers, ackAnswer{waiters: chans, granted: contains(granted, record.uri)})
			delete(w.acks, record.uri)
		}
	} else if id == "" {
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
	}
	// An acknowledgement that names a stream this client holds no live
	// record for answers no one: the attempt that asked for the stream
	// has ended — its record retired with its waiter — or the stream is
	// not one this client opened, and neither has a waiter to hear it.
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

// containsWaiter reports whether the waiters hold the one named.
func containsWaiter(chans []chan bool, waiter chan bool) bool {
	for _, held := range chans {
		if held == waiter {
			return true
		}
	}
	return false
}

// contains reports whether the URIs name the one asked for.
func contains(uris []string, uri string) bool {
	for _, held := range uris {
		if held == uri {
			return true
		}
	}
	return false
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
					waiters.acknowledged(acknowledgementID(params.Meta), params.Notifications.ResourceSubscriptions)
				}
			}
			return next(ctx, method, req)
		}
	}
}

// acknowledgementID reads the subscription ID an acknowledgement's
// meta carries, or the empty string when it names no stream.
func acknowledgementID(meta sdkmcp.Meta) string {
	if meta == nil {
		return ""
	}
	if raw, ok := meta[sdkmcp.MetaKeySubscriptionID]; ok {
		return subscriptionID(raw)
	}
	return ""
}

// listenStreamsTransport records the listen streams the client opens,
// so an acknowledgement names the stream it answers.
type listenStreamsTransport struct {
	transport sdkmcp.Transport
	waiters   *resourceWaiters
}

func (t *listenStreamsTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	conn, err := t.transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &listenStreamsConnection{Connection: conn, waiters: t.waiters}, nil
}

// listenStreamsConnection observes the listen requests the client
// writes: each names the URI its stream asks for, and the ID the
// stream's acknowledgement will answer by.
type listenStreamsConnection struct {
	sdkmcp.Connection
	waiters *resourceWaiters
}

func (c *listenStreamsConnection) Write(ctx context.Context, msg sdkjsonrpc.Message) error {
	if req, ok := msg.(*sdkjsonrpc.Request); ok && req.Method == listenMethod && req.ID.IsValid() {
		var params struct {
			Notifications struct {
				ResourceSubscriptions []string `json:"resourceSubscriptions"`
			} `json:"notifications"`
		}
		if json.Unmarshal(req.Params, &params) == nil && len(params.Notifications.ResourceSubscriptions) == 1 {
			c.waiters.streamOpened(subscriptionID(req.ID.Raw()), params.Notifications.ResourceSubscriptions[0])
		}
	}
	return c.Connection.Write(ctx, msg)
}

// subscriptionID names an ID the way an acknowledgement's meta carries
// it: the ID is the value's JSON form on the wire, so the number the
// client wrote as an int arrives back as a float64, and the two must
// read as one name.
func subscriptionID(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
