package wire

import (
	"sync"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/wire/protocol"
)

// The sources a close records.
//
// The source says why the run ended: a client that closed the session
// and a transport that broke are different things, and the settle
// record says which one it was.
const (
	// sourceCancelClose is the source of a run a close ended.
	sourceCancelClose = "cancel:close"
	// sourceTransportFailed is the source of a run a broken transport
	// ended.
	sourceTransportFailed = "transport_failed"
	// sourceCancelClient is the source of a run a client cancelled.
	sourceCancelClient = "cancel:client"
)

// session is one session the server serves.
//
// It holds the core session, what the factory said about it, and the
// event log the client pages through.
type session struct {
	// id identifies the session.
	id string
	// sess is the core session.
	sess *poo.Session
	// info is what the factory said about the session.
	info *Info

	// mu guards the fields below it.
	mu sync.Mutex
	// events are the session's events, oldest first.
	events []protocol.Event
	// settled holds the outcome of every run that settled, by run id.
	settled map[string]protocol.Settled
	// changed is closed and replaced when the session's state changes,
	// so a waiter may wait for the next change.
	changed chan struct{}
	// closing reports that the session is closing.
	closing bool
	// closed reports that the session is closed.
	closed bool
}

// newSession returns the server's entry for a session.
func newSession(sess *poo.Session, info *Info) *session {
	return &session{
		id:      sess.ID(),
		sess:    sess,
		info:    info,
		settled: map[string]protocol.Settled{},
		changed: make(chan struct{}),
	}
}

// record adds an event to the session's log and wakes the waiters.
func (s *session) record(evt protocol.Event) protocol.Event {
	evt.SessionID = s.id
	s.mu.Lock()
	s.events = append(s.events, evt)
	if evt.Type == string(poo.EventRunSettled) {
		if settled, ok := settledOf(evt); ok {
			s.settled[settled.RunID] = settled
		}
	}
	s.mu.Unlock()
	s.signal()
	return evt
}

// signal wakes the waiters.
func (s *session) signal() {
	s.mu.Lock()
	changed := s.changed
	s.changed = make(chan struct{})
	s.mu.Unlock()
	close(changed)
}

// eventsAfter returns the events after a sequence number, and the
// sequence number the next page asks for.
//
// The snapshot is taken under the session's lock, so a client never
// sees an event twice or misses one that came while it was reading.
func (s *session) eventsAfter(after int64) ([]protocol.Event, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]protocol.Event, 0, len(s.events))
	var next int64
	for _, evt := range s.events {
		if evt.Seq > after {
			out = append(out, evt)
		}
		if evt.Seq > next {
			next = evt.Seq
		}
	}
	return out, next
}

// markClosing marks the session closing.
//
// New requests fail with session_closed from here on, and the waiters
// are woken when the close ends.
func (s *session) markClosing() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.signal()
}

// markClosed marks the session closed and wakes the waiters.
//
// A waiter of a run that settled gets its result; the others get
// session_closed.
func (s *session) markClosed() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.signal()
}

// isClosed reports that the session is closing or closed.
func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing || s.closed
}

// waitForSettle waits for a run to settle.
//
// It reports the outcome when the run settles, and whether the wait ran
// out. A session that closes while the wait is on ends the wait: the
// caller then reads the run's outcome, if it has one.
func (s *session) waitForSettle(runID string, timeout time.Duration) (protocol.Settled, bool) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		if settled, ok := s.settled[runID]; ok {
			s.mu.Unlock()
			return settled, false
		}
		if s.closed {
			s.mu.Unlock()
			return protocol.Settled{}, false
		}
		changed := s.changed
		s.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return protocol.Settled{}, true
		}
		timer := time.NewTimer(remaining)
		select {
		case <-changed:
			timer.Stop()
		case <-timer.C:
			return protocol.Settled{}, true
		}
	}
}

// observer turns the core's events into notifications.
//
// The observer is handed to the factory, which puts it in the session
// it builds. The events that arrive before the session is registered
// wait for it.
type observer struct {
	// srv is the server the events go to.
	srv *server
	// mu guards the fields below it.
	mu sync.Mutex
	// entry is the session the events belong to.
	entry *session
	// pending holds the events that arrived before the session was
	// registered.
	pending []protocol.Event
}

// Observe records an event and sends it to the client.
func (o *observer) Observe(ev poo.Event) error {
	evt := protocol.Event{
		Seq:     ev.Seq,
		Type:    string(ev.Name),
		At:      ev.At,
		Payload: ev.Payload,
	}
	o.mu.Lock()
	if o.entry == nil {
		o.pending = append(o.pending, evt)
		o.mu.Unlock()
		return nil
	}
	entry := o.entry
	o.mu.Unlock()
	entry.record(evt)
	return o.srv.notifyEvent(entry, evt)
}

// attach registers the session the observer belongs to, and sends the
// events that arrived before it was registered.
func (o *observer) attach(entry *session) {
	o.mu.Lock()
	o.entry = entry
	pending := o.pending
	o.pending = nil
	o.mu.Unlock()
	for _, evt := range pending {
		entry.record(evt)
		_ = o.srv.notifyEvent(entry, evt)
	}
}

// notifyEvent sends an event to the client.
//
// The event is a notification: it carries the session, the sequence
// number, the type and whatever the event holds.
func (s *server) notifyEvent(entry *session, evt protocol.Event) error {
	return s.sendNotification(protocol.MethodEvent, evt)
}

// settledOf reads the outcome of a settled run from an event.
//
// The event's payload carries the run's id, its reason and its detail.
func settledOf(evt protocol.Event) (protocol.Settled, bool) {
	runID, _ := evt.Payload["run_id"].(string)
	reason, _ := evt.Payload["reason"].(string)
	if runID == "" {
		return protocol.Settled{}, false
	}
	settled := protocol.Settled{RunID: runID, Reason: reason}
	if detail, ok := evt.Payload["detail"].(poo.SettleDetail); ok {
		settled.Detail = detailOf(detail)
	}
	return settled, true
}

// detailOf converts a settle detail into the wire's shape.
func detailOf(detail poo.SettleDetail) protocol.SettleDetail {
	out := protocol.SettleDetail{
		Source:  detail.Source,
		Text:    detail.Text,
		Losing:  detail.Losing,
		Handoff: detail.Handoff,
		Result:  detail.Result,
	}
	if detail.Provider != nil {
		out.Provider = &protocol.ProviderDetail{
			Kind:        detail.Provider.Kind,
			Status:      detail.Provider.Status,
			Code:        detail.Provider.Code,
			RetryAfterS: detail.Provider.RetryAfterSec,
			ResetsAt:    detail.Provider.ResetsAt,
		}
	}
	for _, input := range detail.UndeliveredInput {
		out.UndeliveredInput = append(out.UndeliveredInput, protocol.UndeliveredInput{
			InputID: input.InputID,
			Mode:    input.Mode,
		})
	}
	out.JobsRunning = detail.JobsRunning
	return out
}

// register adds a session to the server.
func (s *server) register(entry *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[entry.id] = entry
}

// removeSession takes a session out of the server.
func (s *server) removeSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// lookup finds a session the client named.
//
// A session that is closing answers with session_closed, and a session
// the server does not know answers with session_not_found.
func (s *server) lookup(id string) (*session, *protocol.Error) {
	s.mu.Lock()
	entry, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return nil, protocol.Errorf(protocol.CodeServerError, protocol.DataSessionNotFound,
			"there is no session %q", id)
	}
	if entry.isClosed() {
		return nil, protocol.Errorf(protocol.CodeServerError, protocol.DataSessionClosed,
			"the session %q is closing", id)
	}
	return entry, nil
}

// closeAll closes every session the server serves.
func (s *server) closeAll(source string) {
	s.mu.Lock()
	entries := make([]*session, 0, len(s.sessions))
	for _, entry := range s.sessions {
		entries = append(entries, entry)
	}
	s.mu.Unlock()
	for _, entry := range entries {
		s.closeSession(entry, source)
	}
}

// closeSession closes one session, in the plan's order.
//
// The steps are the plan's: the session is marked closing under its
// own lock, the run is finalized and waited for, the waiters are
// released, the observers and the jobs are stopped, and the lock is
// released at the end.
func (s *server) closeSession(entry *session, source string) {
	// Step 1: the session is closing, so new requests fail with
	// session_closed and no job starts or wakes.
	entry.markClosing()
	// Step 2: the run is finalized and waited for. A transport that
	// broke says so in the settle record, so a reader of the log can
	// tell a close from a failure.
	if source == sourceTransportFailed {
		entry.sess.Cancel(source)
	}
	entry.sess.Close()
	// Steps 3 and 4: the waiters are released. The waiter of the run
	// that settled reads its outcome; the others are told the session
	// is closed.
	entry.markClosed()
	// Step 7: the lock is released, and the session is gone.
	if entry.info != nil && entry.info.Release != nil {
		_ = entry.info.Release()
	}
	s.removeSession(entry.id)
}
