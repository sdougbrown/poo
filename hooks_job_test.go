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

// TestTheJobChainRunsOnceAtCompletionWithNoActiveRun covers the plan's
// rule: the chain runs at the completion, under the session's context,
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

// TestTheJobsRedactionKeepsTheSecretOutOfTheStore covers the plan's
// rule: only the chain's result is recorded and emitted, so a held
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

// TestTheDeliveryDoesNotRerunTheJobsChain covers the plan's rule: the
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
