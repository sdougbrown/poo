package poo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// A job's after-tool chain runs once, at the job's completion, under
// the session's own context. There may be no active run then: the run
// that started the job may have settled long ago.

// TestTheJobChainRunsOnceAtCompletionWithNoActiveRun covers the rule: the chain runs at the completion, under the session's context,
// and the hook is told which job it runs for and which run started it.
func TestTheJobChainRunsOnceAtCompletionWithNoActiveRun(t *testing.T) {
	hook := &recordingAfterTool{decision: poo.Keep()}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	opts := jobOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)
	hook.session = s

	runID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the job to start", func() bool { return tool.Calls() == 1 })
	// The run is cancelled, and the job keeps running: the session is
	// idle when the job finishes.
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if s.Running() {
		t.Fatal("the session is still running after the run settled")
	}
	tool.ReleaseAll()

	waitFor(t, "the chain to run", func() bool { return hook.Calls() == 1 })
	// The chain ran once, at the completion.
	time.Sleep(20 * time.Millisecond)
	if got := hook.Calls(); got != 1 {
		t.Errorf("the chain ran %d time(s), want 1", got)
	}
	// The hook was told which job it runs for, and which run started
	// it.
	jobs := hook.Jobs()
	if len(jobs) != 1 || !strings.HasPrefix(jobs[0], "j") {
		t.Errorf("the hook's job is %v, want the job's identifier", jobs)
	}
	runIDs := hook.RunIDs()
	if len(runIDs) != 1 || runIDs[0] != runID {
		t.Errorf("the hook's run is %v, want the run that started the job %q", runIDs, runID)
	}
	// The chain ran with no active run.
	running := hook.Running()
	if len(running) != 1 || running[0] {
		t.Errorf("the session's run state at the chain was %v, want no active run", running)
	}
}

// TestTheJobsRedactionKeepsTheSecretOutOfTheStore covers the rule: only the chain's result is recorded and emitted, so a held
// result never leaks what a hook would have redacted.
func TestTheJobsRedactionKeepsTheSecretOutOfTheStore(t *testing.T) {
	const secret = "s3cr3t-token"
	hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the output with " + secret)
	opts := jobOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the job to start", func() bool { return tool.Calls() == 1 })
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	tool.ReleaseAll()

	waitFor(t, "the job to end", func() bool { return sink.count("job.ended") == 1 })
	// The record the job's end carries holds no secret.
	for _, rec := range sink.allRecords() {
		ended, ok := rec.(*poo.JobEndedRecord)
		if !ok {
			continue
		}
		if strings.Contains(ended.Text, secret) {
			t.Errorf("the job's end record holds the secret in %q, want it redacted", ended.Text)
		}
		if ended.Text != "[the output was redacted]" {
			t.Errorf("the job's end record holds %q, want the redacted text", ended.Text)
		}
	}
	// The event holds no secret.
	for _, ev := range s.Events() {
		if ev.Name != poo.EventJobEnded {
			continue
		}
		text, _ := ev.Payload["text"].(string)
		if strings.Contains(text, secret) {
			t.Errorf("the job's end event holds the secret in %q, want it redacted", text)
		}
	}
	// The store holds no secret.
	if strings.Contains(recordsText(sink), secret) {
		t.Error("the secret reached the store, want it redacted")
	}
}

// TestTheDeliveryDoesNotRerunTheJobsChain covers the rule: the
// chain runs at the completion, and the delivery never runs it again.
func TestTheDeliveryDoesNotRerunTheJobsChain(t *testing.T) {
	hook := &recordingAfterTool{decision: poo.Keep()}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	opts := jobOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the job to start", func() bool { return tool.Calls() == 1 })
	// The run waits for the job, and the job's result is delivered to
	// the waiting run.
	tool.ReleaseAll()
	waitFor(t, "the result to be delivered", func() bool { return sink.count("input.delivered") == 1 })
	waitFor(t, "the run to settle", func() bool { return sink.Settled() != nil })
	if got := hook.Calls(); got != 1 {
		t.Errorf("the chain ran %d time(s), want 1: the delivery does not run it again", got)
	}
}

// TestAJobTimeoutKeepsItsPartialOutputAndItsFlag covers the rule:
// timeout is an error with timed_out true and the partial output as its
// result. The chain runs at completion on a context of its own, so a
// hook cannot turn the timeout into a missed hook deadline.
func TestAJobTimeoutKeepsItsPartialOutputAndItsFlag(t *testing.T) {
	const partial = "the job's partial output"
	for _, withHook := range []bool{false, true} {
		name := "no hook"
		if withHook {
			name = "with an after-tool hook"
		}
		t.Run(name, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{backgroundCall("slow", "one")}},
				{text: "done"},
			})
			sink := &recordingSink{}
			tool := newHoldTool("slow", partial)
			tool.block = true
			tool.background = poo.BackgroundAlways
			opts := oneToolOptions(t, tr, tool, sink)
			opts.Jobs = poo.JobOptions{Timeout: 40 * time.Millisecond}
			hook := &recordingAfterTool{decision: poo.Keep()}
			if withHook {
				opts.AfterTool = []poo.AfterToolHook{hook}
			}
			s := newTestSession(t, opts)

			if _, err := s.Prompt(context.Background(), "go"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			waitFor(t, "the job to end", func() bool { return sink.count("job.ended") == 1 })
			ended := jobEndedRecord(t, sink)
			if !ended.TimedOut {
				t.Errorf("job.ended.TimedOut is false, want true (outcome %q, text %q)", ended.Outcome, ended.Text)
			}
			if !strings.Contains(ended.Text, partial) {
				t.Errorf("job.ended.Text is %q, want the partial output %q", ended.Text, partial)
			}
			if withHook && hook.Calls() != 1 {
				t.Errorf("the chain ran %d time(s), want 1", hook.Calls())
			}
		})
	}
}

// TestACancelledJobRunsItsAfterToolChain covers the rule: a
// cancelled job's partial output is delivered as its result, and the
// chain's redaction is what gets recorded.
func TestACancelledJobRunsItsAfterToolChain(t *testing.T) {
	const secret = "s3cr3t-in-a-job"
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{backgroundCall("slow", "one")}}})
	sink := &recordingSink{}
	tool := newHoldTool("slow", "the output with "+secret)
	tool.block = true
	tool.background = poo.BackgroundAlways
	opts := oneToolOptions(t, tr, tool, sink)
	hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.started
	if err := s.CancelJob("j1"); err != nil {
		t.Fatalf("cancelling the job: %v", err)
	}
	waitFor(t, "the job to end", func() bool { return sink.count("job.ended") == 1 })

	ended := jobEndedRecord(t, sink)
	if strings.Contains(ended.Text, secret) {
		t.Errorf("job.ended.Text holds the secret: %q", ended.Text)
	}
	if got := hook.Calls(); got != 1 {
		t.Errorf("the job's chain ran %d time(s), want 1 at its completion", got)
	}
	if store := sinkJSON(sink, secret); strings.Contains(store, secret) {
		t.Errorf("the secret reached the store:\n%s", store)
	}
}

// TestASessionCloseRunsTheJobsAfterToolChain covers the rule: a
// close is a job completion. The chain runs once, and the held result
// never leaks what a hook would redact.
func TestASessionCloseRunsTheJobsAfterToolChain(t *testing.T) {
	const secret = "s3cr3t-in-a-job"
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{backgroundCall("slow", "one")}},
		{text: "done"},
	})
	sink := &recordingSink{}
	tool := newHoldTool("slow", "the output with "+secret)
	tool.block = true
	tool.background = poo.BackgroundAlways
	opts := oneToolOptions(t, tr, tool, sink)
	hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
	opts.AfterTool = []poo.AfterToolHook{hook}
	s, err := poo.New(opts)
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.started
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}

	ended := jobEndedRecord(t, sink)
	if strings.Contains(ended.Text, secret) {
		t.Errorf("job.ended.Text holds the secret: %q", ended.Text)
	}
	if got := hook.Calls(); got != 1 {
		t.Errorf("the job's chain ran %d time(s), want 1: the close is the job's completion", got)
	}
	if store := sinkJSON(sink, secret); strings.Contains(store, secret) {
		t.Errorf("the secret reached the store:\n%s", store)
	}
	if ended.Cause != poo.CauseSessionClosed {
		t.Errorf("the job's cause is %q, want session_closed", ended.Cause)
	}
}

// TestAReplacedJobKeepsTheOriginalInItsRecord covers the rule for
// a job completion: the same provenance a synchronous tool.result keeps
// is recorded on job.ended, including the original a hook replaced.
func TestAReplacedJobKeepsTheOriginalInItsRecord(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("original job result")
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{jobCall("one", true)}}, {text: "done"}})
	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs.OnIdle = poo.OnIdleHold
	opts.AfterTool = []poo.AfterToolHook{afterToolFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
		return poo.Replace(poo.ToolResult{Text: "replacement"}), nil
	})}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	s.Cancel("cancel:review")
	tool.ReleaseAll()
	waitFor(t, "job.ended", func() bool { return sink.count("job.ended") == 1 })

	ended := jobEndedRecord(t, sink)
	if ended.Text != "replacement" {
		t.Errorf("job.ended.Text is %q, want the replacement", ended.Text)
	}
	if ended.Original == nil || ended.Original.Text != "original job result" {
		t.Errorf("job.ended.Original is %+v, want the original job result", ended.Original)
	}
	if len(ended.Hooks) != 1 || ended.Hooks[0].Action != poo.ResultActionReplace {
		t.Errorf("job.ended.Hooks is %+v, want the replacement decision", ended.Hooks)
	}
}
