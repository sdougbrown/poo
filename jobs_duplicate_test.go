package poo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The duplicate rule: a call whose tool and arguments match a job that
// is still running is refused, and the refusal names the running job.

// rawJobCall is a background job call with the arguments' exact bytes,
// so a test controls the spacing and the key order.
func rawJobCall(callID, arguments string) poo.ToolCall {
	withBackground := strings.TrimSuffix(arguments, "}") + `,"background":true}`
	return poo.ToolCall{CallID: callID, Name: "job", Arguments: []byte(withBackground)}
}

// TestAJobThatMatchesARunningJobIsRefused covers the rule: while a job
// runs, a second call with the same tool and the same data is refused
// with the running job's name, and a call with different data still
// starts. The match reads the arguments' canonical JSON, so a different
// key order or spacing is still the same data.
func TestAJobThatMatchesARunningJobIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{rawJobCall("call_1", `{"text":"one","background":true}`)}},
		{calls: []poo.ToolCall{
			rawJobCall("call_2", `{ "background":true, "text":"one" }`),
			rawJobCall("call_3", `{"text":"two","background":true}`),
		}},
	})
	sink := &recordingSink{}
	// The tool never releases, so the first job runs for the whole
	// test, across both runs.
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	opts := jobOptions(t, tr, tool, sink)
	opts.MaxTurns = 1
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The twin call of the second run is refused; the different call is
	// not.
	if _, err := s.Prompt(context.Background(), "do it again"); err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	refused := false
	started := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		switch r.CallID {
		case "call_2":
			if !r.IsError {
				t.Errorf("the reordered call was admitted, want refused: %q", r.Text)
			}
			if !strings.Contains(r.Text, "already running as job j1") {
				t.Errorf("the refusal is %q, want it to name the running job j1", r.Text)
			}
			refused = true
		case "call_3":
			if r.IsError || r.Outcome != poo.OutcomeStarted {
				t.Errorf("the different call was not started: %q", r.Text)
			}
			started = true
		}
	}
	if !refused {
		t.Error("the log holds no refusal for the reordered call")
	}
	if !started {
		t.Error("the log holds no started result for the different call")
	}
	// The refusal started no job: the log holds the first job and the
	// different one, and no job for the reordered call.
	if got := sink.count("job.started"); got != 2 {
		t.Errorf("the log holds %d job.started records, want 2", got)
	}
}

// TestATwinThatDiffersOnlyInTheBackgroundArgumentIsRefused covers the
// envelope: the reserved background argument is not part of a call's
// data, so a tool that runs every call in the background refuses the
// twin that spells the flag differently.
func TestATwinThatDiffersOnlyInTheBackgroundArgumentIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			rawJobCall("call_1", `{"text":"one"}`),
			rawJobCall("call_2", `{"background":true,"text":"one"}`),
		}},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	opts := jobOptions(t, tr, tool, sink)
	opts.MaxTurns = 1
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	refused := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.CallID != "call_2" {
			continue
		}
		if r.IsError && strings.Contains(r.Text, "already running as job j1") {
			refused = true
		} else {
			t.Errorf("the flagged twin was not refused: %q", r.Text)
		}
	}
	if !refused {
		t.Error("the log holds no refusal for the flagged twin")
	}
	if got := sink.count("job.started"); got != 1 {
		t.Errorf("the log holds %d job.started records, want 1", got)
	}
}

// TestARepeatCallAfterTheJobEndedRuns covers the rule's edge: the
// duplicate rule refuses a call while its twin runs, and a call that
// arrives after the twin ended is a job again.
func TestARepeatCallAfterTheJobEndedRuns(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{rawJobCall("call_1", `{"text":"one"}`)}},
		{calls: []poo.ToolCall{rawJobCall("call_2", `{"text":"one"}`)}},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")
	opts := jobOptions(t, tr, tool, sink)
	// The finished result is held, so it never wakes a run of its own:
	// the test drives both runs.
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	opts.MaxTurns = 1
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The run has settled before the job ends, so the result lands on
	// an idle session: it is held for the next prompt, and OnIdleHold
	// wakes no run of its own.
	tool.Release()
	waitForSink(t, sink, "the job's result to be held", func() bool {
		return sink.count("input.held") >= 1
	})
	held := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.InputHeldRecord)
		if !ok {
			continue
		}
		if len(r.Jobs) != 1 || r.Jobs[0] != "j1" || r.Reason != poo.HeldOnIdle {
			t.Errorf("the held record is %v for %q, want j1 held on idle", r.Jobs, r.Reason)
		}
		held = true
	}
	if !held {
		t.Error("the log holds no input.held record")
	}
	if got := sink.count("run.started"); got != 1 {
		t.Errorf("the log holds %d run.started records, want 1: the held result woke a run", got)
	}
	if got := sink.count("job.started"); got != 1 {
		t.Fatalf("the log holds %d job.started records, want 1", got)
	}

	// The second run's call carries the same data, and no job runs
	// any more, so it is a job again.
	if _, err := s.Prompt(context.Background(), "do it again"); err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if got := sink.count("job.started"); got != 2 {
		t.Errorf("the log holds %d job.started records, want 2", got)
	}
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.CallID != "call_2" {
			continue
		}
		if r.IsError {
			t.Errorf("the repeat call was refused: %q", r.Text)
		}
		if !strings.Contains(r.Text, "started job j2") {
			t.Errorf("the repeat call's result is %q, want it to start job j2", r.Text)
		}
	}
}
