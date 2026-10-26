package poo

import (
	"context"
	"sync"
	"testing"
	"time"
)

// sealingInputSink holds the acceptance record of one input until the
// test releases it.
type sealingInputSink struct {
	mu      sync.Mutex
	records []Record
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Append holds the input.queued record.
func (s *sealingInputSink) Append(ctx context.Context, r Record) error {
	if r.Meta().Record == "input.queued" {
		hold := false
		s.once.Do(func() {
			close(s.entered)
			select {
			case <-s.release:
			case <-ctx.Done():
				hold = true
			}
		})
		if hold {
			return ctx.Err()
		}
	}
	s.mu.Lock()
	s.records = append(s.records, r)
	s.mu.Unlock()
	return nil
}

// Flush does nothing.
func (*sealingInputSink) Flush(context.Context) error { return nil }

// TestAnInputAcceptedWhileTheFinalizationWaitsHasAFate covers the seal:
// a Send that finishes its acceptance after the finalization stopped
// waiting does not append to a drained list. The finalization owns the
// input and reports it dropped with the run's reason.
func TestAnInputAcceptedWhileTheFinalizationWaitsHasAFate(t *testing.T) {
	sink := &sealingInputSink{entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancelCause(context.Background())
	r := &runState{id: "run", ctx: ctx, cancel: cancel, pendingCalls: map[string]*issuedCall{}, workDone: make(chan struct{}, 1)}
	s := &Session{opts: Options{Sink: sink, Model: ModelInfo{ContextLimit: 32768}}, state: StateExecuting, run: r, events: newEventBus(nil), messageIDs: &MessageIDs{}}
	SetRecordWriteBudget(s, 100*time.Millisecond)
	commit := make(chan struct{})
	releaseCommit := make(chan struct{})
	restoreHooks := s.SetJobRaceHooks(&JobRaceHooks{BeforeCommit: func(WaitForResult) {
		close(commit)
		<-releaseCommit
	}})
	defer restoreHooks()
	sendDone := make(chan string, 1)
	go func() {
		id, err := s.Send("accepted message", Steer)
		if err != nil {
			sendDone <- "ERROR: " + err.Error()
			return
		}
		sendDone <- id
	}()
	// The input's acceptance record is written, and the Send is then
	// descheduled until the finalization has stopped waiting for it.
	<-sink.entered
	cancelDone := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelDone)
	}()
	// The poll is bounded, so a regression that never emits the event
	// fails with a diagnostic instead of hanging to the package
	// timeout.
	deadline := time.After(5 * time.Second)
	for {
		found := false
		for _, ev := range s.Events() {
			if ev.Name == EventRunSettling {
				found = true
			}
		}
		if found {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the run never emitted run.settling")
		case <-time.After(time.Millisecond):
		}
	}
	s.events.mu.Lock()
	close(sink.release)
	<-commit
	s.events.mu.Unlock()
	id := <-sendDone
	close(releaseCommit)
	<-cancelDone

	sink.mu.Lock()
	defer sink.mu.Unlock()
	queued, delivered, dropped := 0, 0, 0
	var names []string
	for _, rec := range sink.records {
		names = append(names, rec.Meta().Record)
		switch rec := rec.(type) {
		case *InputQueuedRecord:
			if rec.InputID == id {
				queued++
			}
		case *InputDeliveredRecord:
			if rec.InputID == id {
				delivered++
			}
		case *InputDroppedRecord:
			if rec.InputID == id {
				dropped++
			}
		}
	}
	if queued != 1 || delivered+dropped != 1 {
		t.Fatalf("the accepted input has queued=%d delivered=%d dropped=%d undelivered=%v, records=%v", queued, delivered, dropped, s.lastResult.Detail.UndeliveredInput, names)
	}
}
