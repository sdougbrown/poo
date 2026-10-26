package poo

import (
	"context"
	"log/slog"
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
	// diag receives the observer errors and the coalesced overflow
	// lines. It is nil when the bus is built by hand in a test.
	diag *slog.Logger
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
	// dropped counts the events the observer missed. The event bus
	// writes them as a coalesced observer.dropped record, which is
	// never delivered to observers.
	dropped int
	// done is closed when the observer's goroutine ends.
	done chan struct{}
	// stop asks the observer's goroutine to end.
	stop chan struct{}
	// logger receives the observer's errors.
	logger *slog.Logger
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
//
// The logger is the bus's diagnostics, with the session's id already
// attached. A nil logger drops them.
func newEventBus(logger *slog.Logger) *eventBus {
	b := &eventBus{
		reportDone:   make(chan struct{}),
		reporterDone: make(chan struct{}),
		interval:     observerDropInterval,
		diag:         logger,
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
		logger:   b.logger().With(slog.String("observer", hookName(o))),
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
			s.observe(ev)
		case <-s.stop:
			// One last drain, so the observer sees what it already
			// had.
			for {
				select {
				case ev := <-s.ch:
					s.observe(ev)
				default:
					return
				}
			}
		}
	}
}

// observe delivers one event and logs the observer's error.
func (s *observerSlot) observe(ev Event) {
	if err := s.observer.Observe(ev); err != nil {
		s.logger.Error("observer error",
			slog.String("event", string(ev.Name)),
			slog.Int64("seq", ev.Seq),
			slog.Any("error", err))
	}
}

// logger returns the bus's logger, or the no-op logger.
func (b *eventBus) logger() *slog.Logger {
	if b.diag != nil {
		return b.diag
	}
	return discardLogger
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
	// The delivery happens under the bus's lock: the sends are
	// non-blocking, so nothing can stall the emit, and two concurrent
	// emitters cannot invert the sequence order an observer sees.
	for _, o := range observers {
		select {
		case o.ch <- ev:
		default:
			// The observer is too far behind. The event is dropped
			// for that observer alone, and the count is held for the
			// coalesced observer.dropped record the event bus writes.
			// The record is never delivered to observers, so overflow
			// diagnostics cannot cause more overflow.
			o.mu.Lock()
			o.dropped++
			o.mu.Unlock()
		}
	}
	b.mu.Unlock()

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
		// The failure table calls overflow "logged / coalesced
		// observer.dropped": the record is the coalesced record, and
		// the logger gets the matching line.
		b.logger().Warn("observer overflow",
			slog.String("observer", o.name),
			slog.Int("count", count))
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
	ctx, cancel := context.WithTimeout(context.Background(), s.recordBudget())
	defer cancel()
	_ = s.opts.Sink.Append(ctx, &ObserverDroppedRecord{
		RecordMeta: NewRecordMeta("observer.dropped"),
		SessionID:  s.opts.SessionID,
		Observer:   observer,
		Count:      count,
		DroppedAt:  nowRFC3339(),
	})
}
