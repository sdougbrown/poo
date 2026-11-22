package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode/quickjs"
	"github.com/sdougbrown/poo/perm"
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
	if nested.Parent != "r1" || nested.Mode != poo.CallModeNested {
		t.Errorf("the script's call is parent %q mode %q, want a nested call under the model's call", nested.Parent, nested.Mode)
	}
	if nested.Turn != 1 {
		t.Errorf("the script's call is recorded under turn %d, want the turn that started the job", nested.Turn)
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

// denyingAsker answers every ask with a refusal, so a policy whose
// unmatched action is ask refuses the call once the ask resolves.
type denyingAsker struct{}

func (denyingAsker) Available() bool       { return true }
func (denyingAsker) Budget() time.Duration { return time.Minute }
func (denyingAsker) Ask(context.Context, perm.Ask) (perm.Decision, error) {
	return perm.DecisionDeny, nil
}

// TestABackgroundScriptCannotRaiseAnAsk covers the rule that keeps a
// job's ask out of the log: the turn that started the job may already be
// over, so the job's owner refuses to ask. The script reads a refusal,
// and no permission ask is written.
func TestABackgroundScriptCannotRaiseAnAsk(t *testing.T) {
	// The model's codemode call is allowed; the script's own call is what
	// the policy asks about.
	policy, err := perm.New(perm.Options{
		Rules:     []perm.Rule{{Tool: "codemode", Action: perm.ActionAllow}},
		Unmatched: perm.ActionAsk,
		Askers:    []perm.Asker{denyingAsker{}},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	// A side-effecting call is the one the unmatched action judges; a
	// read-only call is allowed without an ask.
	reader := &namedTool{echoTool: echoTool{result: "the reader's text", class: poo.ClassSideEffecting}, name: "reader"}
	sink := &recordingSink{}
	s := codemodeJobSession(t, `
const r = await tools.reader({text: "one"});
return [r.outcome, r.is_error];
`, map[string]poo.Tool{"reader": reader}, sink, poo.WithPolicy(policy))
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
		t.Errorf("the tool ran %d times under an ask it could not get", got)
	}
	if got := sink.count("permission.asked"); got != 0 {
		t.Errorf("the log holds %d permission.asked records for a background script, want 0", got)
	}
	ended := jobEnded(t, sink)
	if !strings.Contains(ended.Text, `["denied",true]`) {
		t.Errorf("the job's result is %s, want the refusal as data", ended.Text)
	}
}

// TestABackgroundScriptReportsAFatalNestedCall covers what happens when a
// call inside a background script is fatal: the job's result carries the
// fatal flag, so the run that receives it settles tool_fatal instead of
// reading a script that merely mentioned an error.
func TestABackgroundScriptReportsAFatalNestedCall(t *testing.T) {
	sink := &recordingSink{}
	s := codemodeJobSession(t, `
const r = await tools.echo({text: "one"});
return r.is_error;
`, map[string]poo.Tool{"echo": &fatalTool{}}, sink)
	runSession(t, s, "start")

	waitUntil(t, 10*time.Second, "the job to end", func() bool {
		for _, rec := range sink.allRecords() {
			if _, ok := rec.(*poo.JobEndedRecord); ok {
				return true
			}
		}
		return false
	})
	if ended := jobEnded(t, sink); !ended.Fatal {
		t.Errorf("the job ended without the fatal flag: outcome %q, text %s", ended.Outcome, ended.Text)
	}
}

// TestAHookFailureDoesNotStopABackgroundScript covers the other half: a
// settle candidate from a call the script made belongs to the job, which
// is never finalized, so it must not disable the script's remaining
// calls. The failure is still recorded on the call that hit it.
func TestAHookFailureDoesNotStopABackgroundScript(t *testing.T) {
	first := &namedTool{echoTool: echoTool{result: "one"}, name: "first"}
	second := &namedTool{echoTool: echoTool{result: "two"}, name: "second"}
	sink := &recordingSink{}
	s := codemodeJobSession(t, `
const a = await tools.first({text: "one"});
const b = await tools.second({text: "two"});
return [a.is_error, b.is_error];
`, map[string]poo.Tool{"first": first, "second": second}, sink, poo.WithBeforeTool(&failingScriptHook{}))
	runSession(t, s, "start")

	waitUntil(t, 10*time.Second, "the job to end", func() bool {
		for _, rec := range sink.allRecords() {
			if _, ok := rec.(*poo.JobEndedRecord); ok {
				return true
			}
		}
		return false
	})
	var calls int
	for _, rec := range sink.allRecords() {
		if call, ok := rec.(*poo.ToolCallRecord); ok && (call.Tool == "first" || call.Tool == "second") {
			calls++
		}
	}
	if calls != 2 {
		t.Errorf("the log holds %d of the script's 2 calls after a hook failure, want both", calls)
	}
	if got := len(first.Calls()) + len(second.Calls()); got != 0 {
		t.Errorf("the hook failed, so %d calls ran, want 0", got)
	}
}

// failingScriptHook fails the calls a script makes, and lets the model's
// own calls through so the script starts at all.
type failingScriptHook struct{}

func (*failingScriptHook) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	switch info.Spec.Name {
	case "first", "second":
		return poo.ToolDecision{}, errors.New("the hook could not decide")
	}
	return poo.Allow(), nil
}
