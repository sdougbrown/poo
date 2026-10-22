package poo

import (
	"sync"
	"time"
)

// eventBus is the session's event stream.
//
// The bus hands every event a session-monotonic sequence number, so an
// observer sees the events in the order they happened. The core emits
// the events; hooks and tools act through their return values and never
// emit one themselves.
type eventBus struct {
	mu sync.Mutex
	// seq numbers the events.
	seq int64
	// observers are the registered observers.
	observers []*observerSlot
	// closed reports that the bus has stopped.
	closed bool
	// log holds the events, so a client can page through them.
	log []Event
}

// observerSlot is one observer's slot on the bus.
//
// Each observer gets its own bounded buffer and its own goroutine, so a
// slow observer never holds the loop up.
type observerSlot struct {
	// mu guards dropped.
	mu sync.Mutex
	// observer is the observer itself.
	observer Observer
	// ch is the observer's buffer.
	ch chan Event
	// dropped counts the events the observer missed.
	dropped int
	// done is closed when the observer's goroutine ends.
	done chan struct{}
	// stop asks the observer's goroutine to end.
	stop chan struct{}
}

// DefaultObserverBuffer is how many events an observer may fall behind
// before the bus drops them for that observer.
const DefaultObserverBuffer = 1024

// newEventBus returns an empty bus.
func newEventBus() *eventBus {
	return &eventBus{}
}

// addObserver registers an observer and starts its goroutine.
func (b *eventBus) addObserver(o Observer) {
	if o == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	slot := &observerSlot{
		observer: o,
		ch:       make(chan Event, DefaultObserverBuffer),
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
	}
	b.observers = append(b.observers, slot)
	go slot.run()
}

// run delivers the events to one observer.
//
// The observer's error is logged and never settles a run, and the events
// arrive in sequence order.
func (s *observerSlot) run() {
	defer close(s.done)
	for {
		select {
		case ev := <-s.ch:
			// An observer's error is the observer's problem: it is
			// logged and the stream carries on.
			_ = s.observer.Observe(ev)
		case <-s.stop:
			// One last drain, so the observer sees what it already
			// had.
			for {
				select {
				case ev := <-s.ch:
					_ = s.observer.Observe(ev)
				default:
					return
				}
			}
		}
	}
}

// emit publishes one event.
//
// The event gets the next sequence number, and every observer that has
// room receives it.
func (b *eventBus) emit(name EventName, payload map[string]any) Event {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return Event{}
	}
	b.seq++
	ev := Event{
		Seq:     b.seq,
		Name:    name,
		At:      time.Now().UTC().Format(time.RFC3339Nano),
		Payload: payload,
	}
	b.log = append(b.log, ev)
	observers := make([]*observerSlot, len(b.observers))
	copy(observers, b.observers)
	b.mu.Unlock()

	for _, o := range observers {
		select {
		case o.ch <- ev:
		default:
			// The observer is too far behind. The event is dropped
			// for that observer alone, and the count is held for the
			// coalesced observer.dropped record the observer stage
			// (Stage 4) writes. The record is never delivered to
			// observers, so overflow diagnostics cannot cause more
			// overflow.
			o.mu.Lock()
			o.dropped++
			o.mu.Unlock()
		}
	}
	return ev
}

// Events returns the events the bus published, in order.
func (b *eventBus) events() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Event(nil), b.log...)
}

// stop stops every observer and waits for them, bounded at two seconds.
//
// A stalled observer never holds the session's teardown up.
func (b *eventBus) stop() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	observers := make([]*observerSlot, len(b.observers))
	copy(observers, b.observers)
	b.observers = nil
	b.mu.Unlock()

	for _, o := range observers {
		close(o.stop)
	}
	deadline := time.After(2 * time.Second)
	for _, o := range observers {
		select {
		case <-o.done:
		case <-deadline:
		}
	}
}
