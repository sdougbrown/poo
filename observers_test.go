package poo_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The observer machinery: an observer never settles a run, and the
// events an observer missed are reported in a coalesced record that is
// never delivered to an observer.

// brokenObserver is an observer that always fails.
type brokenObserver struct {
	mu sync.Mutex
	// calls counts the events the observer saw.
	calls int
}

// Observe fails.
func (o *brokenObserver) Observe(ev poo.Event) error {
	o.mu.Lock()
	o.calls++
	o.mu.Unlock()
	return errors.New("the observer is broken")
}

// Calls returns how many events the observer saw.
func (o *brokenObserver) Calls() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

// stallingObserver is an observer that never answers.
//
// It holds the first event it is given, so the bus fills its buffer and
// drops the events that follow.
type stallingObserver struct {
	mu sync.Mutex
	// name is the observer's name, for the overflow record.
	name string
	// events holds the events the observer was given.
	events []poo.Event
	// entered is signalled when the observer takes its first event.
	entered chan struct{}
	// release lets the observer finish.
	release chan struct{}
	// once guards the entered signal.
	once sync.Once
}

// HookName names the observer, so the overflow record says which
// observer fell behind.
func (o *stallingObserver) HookName() string { return o.name }

// Observe holds the event until the test releases the observer.
func (o *stallingObserver) Observe(ev poo.Event) error {
	o.mu.Lock()
	o.events = append(o.events, ev)
	o.mu.Unlock()
	o.once.Do(func() {
		if o.entered != nil {
			close(o.entered)
		}
	})
	if o.release != nil {
		<-o.release
	}
	return nil
}

// Seen returns the events the observer was given.
func (o *stallingObserver) Seen() []poo.Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]poo.Event(nil), o.events...)
}

// TestAnObserverErrorDoesNotSettleTheRun covers the plan's rule: an
// observer's error is the observer's problem, and the run settles as it
// would have.
func TestAnObserverErrorDoesNotSettleTheRun(t *testing.T) {
	observer := &brokenObserver{}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Observers = []poo.Observer{observer}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	// The delivery is asynchronous, so the observer may not have seen
	// the run's events yet.
	waitFor(t, "the observer to see an event", func() bool {
		return observer.Calls() > 0
	})
}

// TestTwoStalledObserversGetCoalescedDroppedRecords covers the plan's
// rule: the events an observer missed are reported in a coalesced
// record, the record is never delivered to an observer, and the number
// of records stays bounded.
func TestTwoStalledObserversGetCoalescedDroppedRecords(t *testing.T) {
	restoreBuffer := poo.SetObserverBuffer(1)
	defer restoreBuffer()
	restoreInterval := poo.SetObserverDropInterval(50 * time.Millisecond)
	defer restoreInterval()

	first := &stallingObserver{name: "first", entered: make(chan struct{}), release: make(chan struct{})}
	second := &stallingObserver{name: "second", entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Observers = []poo.Observer{first, second}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	<-first.entered
	<-second.entered

	// The events the observers missed are reported in a coalesced
	// record, one per observer.
	waitFor(t, "the overflow records", func() bool {
		return sink.count("observer.dropped") >= 2
	})
	// The records are bounded: a stalled observer does not produce a
	// record per dropped event.
	if got := sink.count("observer.dropped"); got > 6 {
		t.Errorf("the store holds %d overflow records, want a bounded number", got)
	}
	// The records name the observers and count what they missed.
	named := map[string]bool{}
	for _, rec := range sink.allRecords() {
		dropped, ok := rec.(*poo.ObserverDropped)
		if !ok {
			continue
		}
		if dropped.Count <= 0 {
			t.Errorf("the overflow record counts %d events, want more than none", dropped.Count)
		}
		named[dropped.Observer] = true
	}
	if len(named) != 2 {
		t.Errorf("the overflow records name %d observer(s), want 2", len(named))
	}
	// The records are never delivered to an observer: the diagnostics
	// must not cause more overflow.
	for _, observer := range []*stallingObserver{first, second} {
		for _, ev := range observer.Seen() {
			if ev.Name == poo.EventObserverDropped {
				t.Errorf("an observer was given the overflow report %+v, want none", ev)
			}
		}
	}
	close(first.release)
	close(second.release)
}
