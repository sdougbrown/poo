package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode/quickjs"
	toolcodemode "github.com/sdougbrown/poo/tool/codemode"
)

// codemodeJobSession builds a session whose codemode tool runs its
// scripts on QuickJS, beside the tools the script names.
func codemodeJobSession(t *testing.T, script string, extra map[string]poo.Tool, sink *recordingSink, fns ...poo.Option) *poo.Session {
	t.Helper()
	arguments, err := json.Marshal(map[string]any{"script": script, "background": true})
	if err != nil {
		t.Fatalf("marshalling the script: %v", err)
	}
	tools := map[string]poo.Tool{"codemode": toolcodemode.New(quickjs.New())}
	order := []string{"codemode"}
	for name, tool := range extra {
		tools[name] = tool
		order = append(order, name)
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "codemode", Arguments: arguments}}},
		{text: "done"},
	})
	return newTestSession(t, poo.Options{
		Transport:    tr,
		Tools:        tools,
		ToolOrder:    order,
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}, fns...)
}

// waitUntil waits for a condition with a budget of its own.
//
// The shared waitFor's five seconds is enough for a call the session makes
// itself. A job's script first pays for the QuickJS runtime, and a cold
// CI runner under -race takes longer than that, so a step that waits on a
// script gets its own budget and reports what the job was doing.
func waitUntil(t *testing.T, budget time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", budget, what)
}

// jobEnded returns the job.ended record the log holds.
func jobEnded(t *testing.T, sink *recordingSink) *poo.JobEndedRecord {
	t.Helper()
	for _, rec := range sink.allRecords() {
		if ended, ok := rec.(*poo.JobEndedRecord); ok {
			return ended
		}
	}
	t.Fatal("the log holds no job.ended record")
	return nil
}

// TestABackgroundScriptCallsToolsAfterItsRunEnds covers the mode: a
// codemode call that asks for the background runs as a job, and the
// tools its script calls still run after the run that started the job
// has been cancelled. The script's calls belong to the job, so a settle
// does not stop them.
func TestABackgroundScriptCallsToolsAfterItsRunEnds(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	reader := &namedTool{echoTool: echoTool{
		result: "the reader's text",
		call: func(ctx context.Context) (poo.ToolResult, error) {
			close(started)
			select {
			case <-release:
				return poo.ToolResult{Text: "the reader's text"}, nil
			case <-ctx.Done():
				return poo.ToolResult{Text: "the call was cancelled", IsError: true}, ctx.Err()
			}
		},
	}, name: "reader"}
	sink := &recordingSink{}
	s := codemodeJobSession(t, `
const r = await tools.reader({text: "one"});
return [r.tool, r.is_error, r.text.includes("the reader's text")];
`, map[string]poo.Tool{"reader": reader}, sink)

	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	waitUntil(t, 30*time.Second, "the script to make its nested call", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})
	// The run is cancelled while the script's call is in flight: the job
	// outlives it, and so does the call the script made.
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	close(release)

	waitUntil(t, 10*time.Second, "the job to end", func() bool {
		for _, rec := range sink.allRecords() {
			if _, ok := rec.(*poo.JobEndedRecord); ok {
				return true
			}
		}
		return false
	})
	if got := len(reader.Calls()); got != 1 {
		t.Fatalf("the session's reader ran %d times, want 1", got)
	}
	ended := jobEnded(t, sink)
	if ended.Outcome != poo.OutcomeOK {
		t.Fatalf("the job ended %q: %s", ended.Outcome, ended.Text)
	}
	if !strings.Contains(ended.Text, `["reader",false,true]`) {
		t.Errorf("the job's result is %s, want the script's return", ended.Text)
	}
	// The script's call is recorded as the job's, so a reader of the log
	// can tell it apart from a call the job's tool does not own.
	var nested *poo.ToolCallRecord
	for _, rec := range sink.allRecords() {
		call, ok := rec.(*poo.ToolCallRecord)
		if !ok || call.Tool != "reader" {
			continue
		}
		nested = call
	}
	if nested == nil {
		t.Fatal("the log holds no call for the script's reader")
	}
	if nested.Parent == "" || nested.Mode != poo.CallModeNested {
		t.Errorf("the script's call is parent %q mode %q, want a nested call under the job", nested.Parent, nested.Mode)
	}
}

// TestADeniedNestedCallInABackgroundScriptIsData covers a refusal inside
// a job's script: the policy's denial is a result the script reads, and
// the refused tool never runs.
func TestADeniedNestedCallInABackgroundScriptIsData(t *testing.T) {
	reader := &namedTool{echoTool: echoTool{result: "the reader's text"}, name: "reader"}
	sink := &recordingSink{}
	s := codemodeJobSession(t, `
const strip = (r) => r.text.split("\n").slice(1).join("\n");
const r = await tools.reader({text: "one"});
return [r.outcome, r.is_error, strip(r)];
`, map[string]poo.Tool{"reader": reader}, sink, poo.WithPolicy(&nestPolicy{deny: "reader"}))
	runSession(t, s, "start")

	waitUntil(t, 10*time.Second, "the job to end", func() bool {
		for _, rec := range sink.allRecords() {
			if _, ok := rec.(*poo.JobEndedRecord); ok {
				return true
			}
		}
		return false
	})
	if got := len(reader.Calls()); got != 0 {
		t.Errorf("the refused tool ran %d times, want 0", got)
	}
	ended := jobEnded(t, sink)
	if !strings.Contains(ended.Text, `["denied",true,"the reader call was refused by the test policy"]`) {
		t.Errorf("the job's result is %s, want the refusal as data", ended.Text)
	}
}
