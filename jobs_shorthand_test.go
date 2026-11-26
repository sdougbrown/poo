package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

func TestTrailingAmpersandStartsAManagedJob(t *testing.T) {
	const command = "printf 'job output'&"
	sink := &recordingSink{}
	args, _ := json.Marshal(map[string]string{"command": command})
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "bash", Arguments: args}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: t.TempDir(),
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	})
	if _, err := s.Prompt(context.Background(), "run it"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	var started bool
	var recorded bool
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Outcome == poo.OutcomeStarted {
			started = strings.Contains(r.Text, "job j1") && strings.Contains(r.Text, "trailing &")
		}
		if r, ok := rec.(*poo.ToolCallRecord); ok && r.CallID == "one" {
			recorded = r.Mode == poo.CallModeJob && string(r.Arguments) == string(args)
		}
	}
	if !started {
		t.Fatal("the trailing & did not return a managed job receipt")
	}
	if !recorded {
		t.Fatal("the tool call record did not keep the original arguments and job mode")
	}
	waitForSink(t, sink, "job output", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.JobEndedRecord); ok && r.Outcome == poo.OutcomeOK && strings.Contains(r.Text, "job output") {
				return true
			}
		}
		return false
	})
}

func TestPolicyCanRefuseATrailingAmpersandJob(t *testing.T) {
	policy := &recordingBeforeTool{decision: poo.Deny("no background work")}
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "bash", Arguments: []byte(`{"command":"printf denied&"}`)}}},
			{text: "done"},
		}),
		Policy: policy, Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: t.TempDir(),
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	})
	runSession(t, s, "run it")
	var effective struct {
		Command    string `json:"command"`
		Background bool   `json:"background"`
	}
	if err := json.Unmarshal(policy.lastArguments(), &effective); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(effective.Command) != "printf denied" || !effective.Background {
		t.Fatalf("policy saw %s; want the effective command and background flag", policy.lastArguments())
	}
	var denied bool
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			if r.Outcome == poo.OutcomeStarted {
				t.Fatal("the policy refused the call, but a job started")
			}
			denied = denied || r.Outcome == poo.OutcomeDenied
		}
	}
	if !denied {
		t.Fatal("the call did not report the policy refusal")
	}
}

func TestTrailingAmpersandConflictsWithBackgroundFalse(t *testing.T) {
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "bash", Arguments: []byte(`{"command":"listen &","background":false}`)}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: t.TempDir(),
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	})
	runSession(t, s, "run it")
	var conflict bool
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Outcome == poo.OutcomeStarted {
			t.Fatal("the call set \"background\": false, but a job started")
		}
		conflict = conflict || (r.Outcome == poo.OutcomeError && strings.Contains(r.Text, `"background": false`))
	}
	if !conflict {
		t.Error("the model did not see the background conflict error")
	}
}

func TestTrailingAmpersandBesideBackgroundTrue(t *testing.T) {
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "bash", Arguments: []byte(`{"command":"printf 'job output'&","background":true}`)}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: t.TempDir(),
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	})
	runSession(t, s, "run it")
	var started bool
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Outcome == poo.OutcomeError {
			t.Fatalf("an explicit true beside the trailing & was refused: %s", r.Text)
		}
		started = started || (r.Outcome == poo.OutcomeStarted && strings.Contains(r.Text, "job j1"))
	}
	if !started {
		t.Fatal("the trailing & beside an explicit true did not start a job")
	}
	waitForSink(t, sink, "job output", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.JobEndedRecord); ok && r.Outcome == poo.OutcomeOK && strings.Contains(r.Text, "job output") {
				return true
			}
		}
		return false
	})
}
