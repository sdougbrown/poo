package poo

import (
	"context"
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
	// reportDrop writes the coalesced overflow record of one observer.
	reportDrop func(observer string, count int)
	// reportDone ends the overflow reporter.
	reportDone chan struct{}
	// reporterDone is closed when the overflow reporter has ended.
	reporterDone chan struct{}
	// interval is how often the overflow records are written.
	interval time.Duration
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
	// name is the observer's name, for the overflow record.
	name string
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

// observerBuffer is the buffer a new observer gets.
//
// It is the default unless a test shortens it.
var observerBuffer = DefaultObserverBuffer

// observerDropInterval is how often the overflow records are written.
//
// The plan's rule is at most one record per second per observer, and
// one more at the close. It is a variable so a test can shorten it.
var observerDropInterval = time.Second

// newEventBus returns an empty bus.
func newEventBus() *eventBus {
	b := &eventBus{
		reportDone:   make(chan struct{}),
		reporterDone: make(chan struct{}),
		interval:     observerDropInterval,
	}
	go b.reportDrops()
	return b
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
		name:     hookName(o),
		ch:       make(chan Event, observerBuffer),
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
			// for that observer alone, and the count is reported
			// in a coalesced record.
			o.mu.Lock()
			o.dropped++
			o.mu.Unlock()
		}
	}
	return ev
}

// setDropReporter sets the writer of the overflow records.
//
// The writer is called from the reporter's goroutine, so a store that
// is slow to answer never holds the loop up.
func (b *eventBus) setDropReporter(fn func(observer string, count int)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reportDrop = fn
}

// reportDrops writes the coalesced overflow records.
//
// The records are written at most once a second per observer, so an
// observer that fell behind does not flood the store. The reporter runs
// on its own goroutine: a stalled observer must not stop the reports.
func (b *eventBus) reportDrops() {
	defer close(b.reporterDone)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.flushDrops(b.observerSlots())
		case <-b.reportDone:
			return
		}
	}
}

// observerSlots returns the registered observers.
func (b *eventBus) observerSlots() []*observerSlot {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*observerSlot, len(b.observers))
	copy(out, b.observers)
	return out
}

// flushDrops writes the overflow record of every observer that missed
// events.
//
// The count of each observer is taken and reset, so a report never
// counts the same event twice.
func (b *eventBus) flushDrops(observers []*observerSlot) {
	b.mu.Lock()
	report := b.reportDrop
	b.mu.Unlock()
	if report == nil {
		return
	}
	for _, o := range observers {
		o.mu.Lock()
		count := o.dropped
		o.dropped = 0
		o.mu.Unlock()
		if count == 0 {
			continue
		}
		// The diagnostic is the record's own: it is not an event, so
		// neither an observer nor a client reading the event log sees
		// it.
		report(o.name, count)
	}
}

// Events returns the events the bus published, in order.
func (b *eventBus) events() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Event(nil), b.log...)
}

// stop stops every observer and waits for them, bounded at two seconds.
//
// A stalled observer never holds the session's teardown up. The last
// overflow report is written before the stop returns, so the record is
// in the store before the store is flushed.
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
	// One deadline for the whole wait: a context's Done channel is
	// closed once and stays closed for every observer.
	deadline, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, o := range observers {
		select {
		case <-o.done:
		case <-deadline.Done():
		}
	}
	// The reporter ends with the bus, and the last report is written
	// here.
	close(b.reportDone)
	<-b.reporterDone
	b.flushDrops(observers)
}

// writeObserverDropped writes the coalesced overflow record of one
// observer.
//
// The record is the store's, not a run's: an observer that fell behind
// says nothing about the run, and a store that refuses the record does
// not break the session.
func (s *Session) writeObserverDropped(observer string, count int) {
	if s.opts.Sink == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), recordWriteBudget)
	defer cancel()
	_ = s.opts.Sink.Append(ctx, &ObserverDropped{
		RecordMeta: NewRecordMeta("observer.dropped"),
		SessionID:  s.opts.SessionID,
		Observer:   observer,
		Count:      count,
		DroppedAt:  nowRFC3339(),
	})
}
