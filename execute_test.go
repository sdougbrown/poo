package poo_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// TestACallWhoseRecordFailsStartsNoEffect covers the failure table: a
// tool.call record the store refuses is terminal, so the tool never runs.
func TestACallWhoseRecordFailsStartsNoEffect(t *testing.T) {
	sink := &failingSink{failRecord: "tool.call"}
	tool := &echoTool{result: "effect executed"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "start")
	if got := len(tool.Calls()); got != 0 {
		t.Fatalf("the tool ran %d time(s) after its tool.call record was refused, want 0", got)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
}

// blockingRecordSink holds one record until the test releases it.
type blockingRecordSink struct {
	recordingSink
	kind    string
	fail    bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Append holds the named record.
func (s *blockingRecordSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == s.kind {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
		if s.fail {
			return errors.New("injected refusal")
		}
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestACancelDuringTheCallRecordStillGetsAResult covers the rule that
// every call gets exactly one result: a call whose tool.call write is in
// flight when the run settles is registered first, so the finalization
// settles it.
func TestACancelDuringTheCallRecordStillGetsAResult(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(fmt.Sprint(background), func(t *testing.T) {
			sink := &blockingRecordSink{
				kind:    "tool.call",
				entered: make(chan struct{}),
				release: make(chan struct{}),
			}
			tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{jobCall("one", background)}},
				{text: "done"},
			})
			s := newTestSession(t, jobOptions(t, tr, tool, sink))
			id, err := s.Prompt(context.Background(), "start")
			if err != nil {
				t.Fatal(err)
			}
			<-sink.entered
			// The record write stays in flight while the finalization
			// starts. It is released shortly after, so the call record can
			// land before the settle only if the finalization waits for
			// the write.
			go func() {
				time.Sleep(50 * time.Millisecond)
				close(sink.release)
			}()
			s.Cancel("probe")
			if _, err := s.WaitRun(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the late turn end", func() bool {
				for _, e := range s.Events() {
					if e.Name == poo.EventTurnEnded {
						return true
					}
				}
				return false
			})
			call, result, settled := -1, -1, -1
			for i, name := range sink.names() {
				switch name {
				case "tool.call":
					call = i
				case "tool.result":
					result = i
				case "run.settled":
					settled = i
				}
			}
			if call < 0 || !(call < result && result < settled) {
				t.Fatalf("records are %v; want tool.call before tool.result before run.settled", sink.names())
			}
			if got := sink.count("tool.result"); got != 1 {
				t.Fatalf("the recorded call has %d result(s) after cancellation, want 1; records: %v", got, sink.names())
			}
			if got := tool.Calls(); got != 0 {
				t.Errorf("the tool ran %d time(s) after the run settled, want 0", got)
			}
		})
	}
}

// TestATruncatedInvalidBackgroundIsSkipped covers the truncation
// completeness rule: a call whose arguments parse but whose reserved
// background argument fails validation did not pass preparation, so in
// a truncated response it is skipped rather than counted as processed.
func TestATruncatedInvalidBackgroundIsSkipped(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tr := scripted([]fakeResponse{
		{
			finish: poo.FinishLength,
			calls:  []poo.ToolCall{{ID: "c", Name: "job", Arguments: []byte(`{"text":"one","background":null}`)}},
		},
		{text: "done"},
	})
	s := newTestSession(t, jobOptions(t, tr, tool, &recordingSink{}))
	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonOutputTruncated || res.Turns != 1 {
		t.Fatalf("the truncated call settled %q after %d turn(s), want output_truncated after 1", res.Reason, res.Turns)
	}
	if got := tool.Calls(); got != 0 {
		t.Errorf("the invalid call ran %d time(s), want 0", got)
	}
}
