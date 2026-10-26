package pootest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

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
