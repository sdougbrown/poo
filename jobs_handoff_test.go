package poo_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestAMovedCallTimesOutUnderTheJobTimeout covers the rule: a moved
// call runs under the job timeout like any other job, so a call held
// open past the bound ends with the timeout outcome.
func TestAMovedCallTimesOutUnderTheJobTimeout(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "bash", Arguments: []byte(`{"command":"echo $$ > started; until [ -f release ]; do /bin/sleep 0.02; done"}`)}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: dir,
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithJobs(poo.JobOptions{Timeout: 300 * time.Millisecond, OnIdle: poo.OnIdleHold}))
	done := make(chan error, 1)
	go func() {
		_, err := s.Prompt(context.Background(), "run it")
		done <- err
	}()
	var runID string
	for end := time.Now().Add(10 * time.Second); runID == ""; {
		if time.Now().After(end) {
			t.Fatal("the call never started")
		}
		if data, err := os.ReadFile(filepath.Join(dir, "started")); err == nil && strings.HasSuffix(string(data), "\n") {
			for _, rec := range sink.allRecords() {
				if r, ok := rec.(*poo.ToolCallRecord); ok && r.Handle == "r1" {
					runID = r.RunID
				}
			}
		}
		if runID == "" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if _, err := s.BackgroundCall(runID, "r1"); err != nil {
		t.Fatalf("moving the call: %v", err)
	}
	var ended *poo.JobEndedRecord
	waitForSink(t, sink, "the job timeout", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.JobEndedRecord); ok && r.JobID == "j1" {
				ended = r
				return true
			}
		}
		return false
	})
	if ended.Outcome != poo.OutcomeError || !ended.TimedOut {
		t.Errorf("job.ended outcome=%s timed_out=%v; want error/timed_out", ended.Outcome, ended.TimedOut)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
