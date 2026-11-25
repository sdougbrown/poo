package pootest_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The in-memory sink keeps every record, serializes its writes and can be
// told to fail.

// TestTheSinkKeepsEveryRecord covers the sink: the records come back in
// order, with their names.
func TestTheSinkKeepsEveryRecord(t *testing.T) {
	sink := pootest.NewSink()
	ctx := context.Background()

	if err := sink.Append(ctx, &poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run.started")}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := sink.Append(ctx, &poo.RunSettledRecord{RecordMeta: poo.NewRecordMeta("run.settled")}); err != nil {
		t.Fatalf("appending: %v", err)
	}

	names := sink.Names()
	if len(names) != 2 || names[0] != "run.started" || names[1] != "run.settled" {
		t.Errorf("the records are %v, want run.started and run.settled", names)
	}
	if sink.Settled() == nil {
		t.Error("the sink has no settle record")
	}
}

// TestTheSinkFailsOnTheNthAppend covers the failure injection: the nth
// append is refused and the ones before it are kept.
func TestTheSinkFailsOnTheNthAppend(t *testing.T) {
	sink := pootest.NewSink()
	sink.FailAt(2)
	ctx := context.Background()

	if err := sink.Append(ctx, &poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run.started")}); err != nil {
		t.Fatalf("the first append failed: %v", err)
	}
	err := sink.Append(ctx, &poo.RunSettledRecord{RecordMeta: poo.NewRecordMeta("run.settled")})
	if !errors.Is(err, pootest.ErrSinkFailed) {
		t.Errorf("the second append returned %v, want ErrSinkFailed", err)
	}
	if len(sink.Records()) != 1 {
		t.Errorf("the sink kept %d records, want 1", len(sink.Records()))
	}
	if sink.Appends() != 2 {
		t.Errorf("the sink saw %d appends, want 2", sink.Appends())
	}
}

// TestTheSinkTakesTheFailureError covers the failure error the caller
// names.
func TestTheSinkTakesTheFailureError(t *testing.T) {
	want := errors.New("the store is full")
	sink := pootest.NewSink()
	sink.SetError(want)
	sink.FailAt(1)

	err := sink.Append(context.Background(), &poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run.started")})
	if !errors.Is(err, want) {
		t.Errorf("the append returned %v, want %v", err, want)
	}
}

// TestTheSinkWaitsOnTheRecordCount covers the record wait: a count the
// sink already holds returns at once, the append that reaches the
// count ends the wait, and a cancel ends it with the context's error.
func TestTheSinkWaitsOnTheRecordCount(t *testing.T) {
	sink := pootest.NewSink()
	ctx := context.Background()

	if err := sink.Append(ctx, &poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run.started")}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := sink.WaitRecords(ctx, "run.started", 1); err != nil {
		t.Fatalf("the wait for a count the sink already holds returned %v", err)
	}

	gate := make(chan struct{})
	go func() {
		<-gate
		_ = sink.Append(ctx, &poo.RunSettledRecord{RecordMeta: poo.NewRecordMeta("run.settled")})
	}()
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- sink.WaitRecords(waitCtx, "run.settled", 1)
	}()
	close(gate)
	if err := <-waitDone; err != nil {
		t.Fatalf("the wait for the appended record returned %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go cancel()
	err := sink.WaitRecords(cancelCtx, "run.settled", 2)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the cancelled wait returned %v, want the context's error", err)
	}
}

// TestTheSinkWaitsOnTheCondition covers the condition wait: a cond the
// sink already holds returns at once, the append that makes the cond
// true ends the wait, and a cancel ends it with the context's error.
func TestTheSinkWaitsOnTheCondition(t *testing.T) {
	sink := pootest.NewSink()
	ctx := context.Background()

	if err := sink.Append(ctx, &poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run.started")}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := sink.WaitFunc(ctx, func() bool { return len(sink.Records()) >= 1 }); err != nil {
		t.Fatalf("the wait for a cond the sink already holds returned %v", err)
	}

	gate := make(chan struct{})
	go func() {
		<-gate
		_ = sink.Append(ctx, &poo.RunSettledRecord{RecordMeta: poo.NewRecordMeta("run.settled")})
	}()
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- sink.WaitFunc(waitCtx, func() bool { return len(sink.Records()) >= 2 })
	}()
	close(gate)
	if err := <-waitDone; err != nil {
		t.Fatalf("the wait for the appended record returned %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go cancel()
	err := sink.WaitFunc(cancelCtx, func() bool { return len(sink.Records()) >= 3 })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the cancelled wait returned %v, want the context's error", err)
	}
}

// TestTheSinkSerializesConcurrentAppends covers the sink's concurrency
// contract: many writers, and no record lost or interleaved.
func TestTheSinkSerializesConcurrentAppends(t *testing.T) {
	sink := pootest.NewSink()
	ctx := context.Background()

	const writers = 16
	const each = 8

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				_ = sink.Append(ctx, &poo.UsageRecord{RecordMeta: poo.NewRecordMeta("usage")})
			}
		}(w)
	}
	wg.Wait()

	if got := len(sink.Records()); got != writers*each {
		t.Errorf("the sink kept %d records, want %d", got, writers*each)
	}
	if sink.Appends() != writers*each {
		t.Errorf("the sink saw %d appends, want %d", sink.Appends(), writers*each)
	}
}
