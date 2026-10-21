package poo_test

import (
	"context"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// stuckSink is a record sink whose writes return only when their context
// ends.
type stuckSink struct{}

// Append waits for the context.
func (stuckSink) Append(ctx context.Context, rec poo.Record) error {
	<-ctx.Done()
	return ctx.Err()
}

// Flush waits for the context.
func (stuckSink) Flush(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestAStuckSinkCannotHangTheRun covers a store that never answers: each
// write is bounded, so the run returns instead of hanging.
func TestAStuckSinkCannotHangTheRun(t *testing.T) {
	defer poo.SetRecordWriteBudget(50 * time.Millisecond)()

	tr := &fakeTransport{responses: []fakeResponse{{text: "all done"}}}
	tool := &echoTool{result: "echoed"}

	done := make(chan error, 1)
	go func() {
		_, err := poo.NewRun(testOptions(t, tr, tool, stuckSink{})).Execute(context.Background(), "do the thing")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the run succeeded against a sink that writes nothing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run hung on a sink that never answers")
	}
}
