package poo_test

import (
	"context"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The fix-pass-2 reproducers: Close owns a job completion chain that
// outlives its teardown deadline.

// fix2JobCompletionHook blocks the job's completion chain until it is
// released or its context ends.
type fix2JobCompletionHook struct{ entered, release chan struct{} }

func (h *fix2JobCompletionHook) AfterTool(ctx poo.HookContext, info poo.ToolCallInfo, r poo.ToolResult) (poo.ResultDecision, error) {
	close(h.entered)
	select {
	case <-h.release:
	case <-ctx.Done():
		return poo.ResultDecision{}, ctx.Err()
	}
	return poo.Keep(), nil
}

// TestCloseFinishesJobHookBeforeReturning covers the rule that Close
// owns a completion chain that outlives its teardown deadline.
func TestCloseFinishesJobHookBeforeReturning(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("output")
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{ID: "jcall", Name: "job", Arguments: []byte(`{}`)}}}})
	sink := &recordingSink{}
	h := &fix2JobCompletionHook{entered: make(chan struct{}), release: make(chan struct{})}
	opts := testOptions(t, tr, nil, sink)
	opts.Tools = map[string]poo.Tool{"job": tool}
	opts.ToolOrder = []string{"job"}
	opts.MaxTurns = 1
	opts.AfterTool = []poo.AfterToolHook{h}
	s := newTestSession(t, opts)
	poo.SetRecordWriteBudget(s, 30*time.Millisecond)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-tool.Started()
	tool.ReleaseAll()
	<-h.entered
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := sink.count("job.ended")
	close(h.release)
	waitFor(t, "late job.ended", func() bool { return sink.count("job.ended") == 1 })
	if before != 1 {
		t.Fatal("Close returned and flushed with job.ended=0; releasing completion hook writes job.ended after Close")
	}
}
