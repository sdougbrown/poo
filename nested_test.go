package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// The nested tool-call seam: a tool that runs in the turn may make
// calls of its own through ToolEnv.Nested, and the results are
// recorded without entering the model's history.

// nestPolicy is the deny-by-tool-name policy the nested tests use.
type nestPolicy struct {
	// deny is the tool name every call of which is refused.
	deny string
	// doomAt is the exact repeat count at which a call is refused, 0
	// for never.
	doomAt int
}

func (p *nestPolicy) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	if p.deny != "" && info.Spec.Name == p.deny {
		return poo.Deny("the " + info.Spec.Name + " call was refused by the test policy"), nil
	}
	if p.doomAt > 0 && info.RepeatCount >= p.doomAt {
		return poo.Deny("the same call repeated too often"), nil
	}
	return poo.Allow(), nil
}

// nestedTool is a tool that makes the nested calls a test scripts.
//
// script holds "tool arguments-json" lines; the tool runs each through
// env.Nested in order and reports the outcomes as its own result text.
type nestedTool struct {
	mu sync.Mutex
	// nester is the env's nested caller, kept so a test can assert on
	// the seam itself.
	nester poo.NestedCaller
	// sawNested reports whether the tool's env carried a nested caller.
	script []string
}

func (t *nestedTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "nest",
		Description: "make nested calls",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Class:       poo.ClassSideEffecting,
	}
}

func (t *nestedTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return nestPrepared{}, nil
}

func (t *nestedTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.mu.Lock()
	t.nester = env.Nested
	t.mu.Unlock()
	if env.Nested == nil {
		return poo.ToolResult{Text: "no nested caller", IsError: true}, nil
	}
	var lines []string
	for _, step := range t.script {
		parts := strings.SplitN(step, " ", 2)
		name := parts[0]
		args := json.RawMessage("{}")
		if len(parts) == 2 {
			args = json.RawMessage(parts[1])
		}
		res, err := env.Nested.Call(ctx, env.Handle, name, args)
		if err != nil {
			lines = append(lines, name+": error: "+err.Error())
			continue
		}
		lines = append(lines, name+": "+string(res.Outcome))
	}
	return poo.ToolResult{Text: strings.Join(lines, "\n")}, nil
}

type nestPrepared struct{}

func (nestPrepared) Summary() string { return "make nested calls" }

// nestedSession runs one session whose model-made call is the nested
// tool's, with the extra tools the script names alongside it.
func nestedSession(t *testing.T, nest *nestedTool, extra map[string]poo.Tool, sink *recordingSink, fns ...poo.Option) *poo.Session {
	t.Helper()
	tools := map[string]poo.Tool{"nest": nest}
	order := []string{"nest"}
	for name, tool := range extra {
		tools[name] = tool
		order = append(order, name)
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "nest", Arguments: json.RawMessage(`{}`)}}},
		{text: "done"},
	})
	opts := poo.Options{
		Transport:    tr,
		Tools:        tools,
		ToolOrder:    order,
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}
	return newTestSession(t, opts, fns...)
}

// TestNestedResultsNeverReachTheHistory covers the seam's headline
// invariant: three nested calls produce three recorded call/result
// pairs, and the log's messages are only the ones the model saw.
func TestNestedResultsNeverReachTheHistory(t *testing.T) {
	nest := &nestedTool{script: []string{
		`reader {"text":"one"}`,
		`reader {"text":"two"}`,
		`reader {"text":"three"}`,
	}}
	reader := &echoTool{result: "the file's text"}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"reader": reader}, sink)
	runSession(t, s, "hello")

	nested, parentResults := 0, 0
	for _, rec := range sink.allRecords() {
		switch r := rec.(type) {
		case *poo.ToolCallRecord:
			if r.Parent == "" {
				continue
			}
			nested++
			if r.Parent != "r1" {
				t.Errorf("the nested call's parent is %q, want r1", r.Parent)
			}
			if r.Mode != poo.CallModeNested {
				t.Errorf("the nested call's mode is %q, want nested", r.Mode)
			}
			if r.CallID != "" {
				t.Errorf("the nested call carries the provider id %q, want none", r.CallID)
			}
		case *poo.ToolResultRecord:
			if r.Handle == "r1" {
				parentResults++
			}
		case *poo.MessageRecord:
			for _, block := range r.Content {
				if strings.Contains(block.Text, "the file's text") {
					t.Error("a nested result's text reached the model's history")
				}
			}
		}
	}
	if nested != 3 {
		t.Errorf("the log holds %d nested tool.call records, want 3", nested)
	}
	if parentResults != 1 {
		t.Errorf("the nest call's own result landed %d times, want 1", parentResults)
	}
	// The model's tool-call message and the nest call's result are the
	// only tool-role messages: the three nested results never became
	// messages, whatever else the log holds.
	toolMessages := 0
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.MessageRecord); ok && r.Role == poo.RoleTool {
			toolMessages++
		}
	}
	if toolMessages != 1 {
		t.Errorf("the log holds %d tool-role messages, want 1 (the nest call's own)", toolMessages)
	}
}

// TestNestedCallsShareThePermissionEngine covers refusals: a denial of
// a nested call is data to the script, and the run carries on.
func TestNestedCallsShareThePermissionEngine(t *testing.T) {
	nest := &nestedTool{script: []string{
		`reader {"text":"one"}`,
		`bash {"command":"rm -rf /"}`,
		`reader {"text":"two"}`,
	}}
	reader := &namedTool{echoTool: echoTool{result: "ok"}, name: "reader"}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"reader": reader, "bash": &namedTool{echoTool: echoTool{}, name: "bash"}}, sink,
		poo.WithPolicy(&nestPolicy{deny: "bash"}))
	runSession(t, s, "hello")

	denied := 0
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Outcome == poo.OutcomeDenied {
			denied++
		}
	}
	if denied != 1 {
		t.Errorf("the log holds %d denied results, want 1 (the nested bash call)", denied)
	}
	if got := len(reader.Calls()); got != 2 {
		t.Errorf("the reader ran %d times, want 2 (the script continued past the refusal)", got)
	}
}

// TestNestedCallsCountTowardTheDoomLoop covers the counting: the same
// nested call repeated past the threshold is refused inside one script,
// and none of it reaches the model.
func TestNestedCallsCountTowardTheDoomLoop(t *testing.T) {
	nest := &nestedTool{script: []string{
		`reader {"text":"one"}`,
		`reader {"text":"one"}`,
		`reader {"text":"one"}`,
		`reader {"text":"one"}`,
	}}
	reader := &echoTool{result: "same file", primaryArg: "text"}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"reader": reader}, sink,
		poo.WithPolicy(&nestPolicy{doomAt: 3}))
	runSession(t, s, "hello")

	denied := 0
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Outcome == poo.OutcomeDenied {
			denied++
		}
	}
	if denied < 1 {
		t.Error("the doom loop never fired on the nested calls")
	}
}

// TestNestedCallsExcludeTheRunTools covers the exclusions: finish and
// the job tools are refused before they are judged, and a nested finish
// never settles the run.
func TestNestedCallsExcludeTheRunTools(t *testing.T) {
	nest := &nestedTool{script: []string{
		`finish {}`,
		`jobs {}`,
		`cancel_job {}`,
	}}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{
		"finish":     &echoTool{},
		"jobs":       &echoTool{},
		"cancel_job": &echoTool{},
	}, sink)
	runSession(t, s, "hello")

	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Tool == "finish" && r.Outcome == poo.OutcomeOK {
			t.Error("a nested finish call ran")
		}
	}
}

// TestNestedToolSeesNoNestedCaller covers the depth limit: a nested
// call's own tool has no seam of its own.
func TestNestedToolSeesNoNestedCaller(t *testing.T) {
	inner := &nestedTool{script: []string{`reader {"text":"x"}`}}
	nest := &nestedTool{script: []string{`inner {}`}}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"inner": inner}, sink)
	runSession(t, s, "hello")

	if inner.nester != nil {
		t.Error("the nested call's tool saw a nested caller of its own")
	}
}

// TestNestedPrepareFailureIsData covers a nested call whose arguments
// fail the tool's validation: the script sees the error as data, and
// the calls after it still run.
func TestNestedPrepareFailureIsData(t *testing.T) {
	nest := &nestedTool{script: []string{
		`reader {"text":"one"}`,
		`broken {"text":"bad"}`,
		`reader {"text":"two"}`,
	}}
	reader := &namedTool{echoTool: echoTool{result: "ok"}, name: "reader"}
	broken := &namedTool{echoTool: echoTool{prepareErr: errors.New("no such file")}, name: "broken"}
	sink := &recordingSink{}
	s := nestedSession(t, nest,
		map[string]poo.Tool{"reader": reader, "broken": broken}, sink)
	runSession(t, s, "hello")

	if got := len(reader.Calls()); got != 2 {
		t.Errorf("the reader ran %d times, want 2 (the script continued past the failed call)", got)
	}
}

// TestAnInterruptMidScriptSettlesEachNestedCallOnce covers the
// finalization's ownership: a run interrupted while a nested call is in
// flight settles it exactly once, and nothing enters the history.
func TestAnInterruptMidScriptSettlesEachNestedCallOnce(t *testing.T) {
	blocker := &namedTool{echoTool: echoTool{
		call: func(ctx context.Context) (poo.ToolResult, error) {
			<-ctx.Done()
			return poo.ToolResult{Text: "the partial output"}, ctx.Err()
		},
	}, name: "blocker"}
	nest := &nestedTool{script: []string{`blocker {}`}}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"blocker": blocker}, sink)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The nested call is in flight when the interrupt lands.
	waitFor(t, "the nested call to start", func() bool {
		for _, ev := range s.Events() {
			if ev.Name == poo.EventToolCall && ev.Payload["nested"] == true {
				return true
			}
		}
		return false
	})
	if _, err := s.Send("stop", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	results := map[string]int{}
	outcomes := map[string]poo.Outcome{}
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			results[r.Handle]++
			outcomes[r.Handle] = r.Outcome
		}
	}
	if got := results["r2"]; got != 1 {
		t.Errorf("the nested call's result landed %d times, want 1", got)
	}
	switch outcomes["r2"] {
	case poo.OutcomeCancelled, poo.OutcomeInterrupted:
	default:
		t.Errorf("the nested call's outcome is %q, want cancelled or interrupted", outcomes["r2"])
	}
	if got := results["r1"]; got != 1 {
		t.Errorf("the nest call's own result landed %d times, want 1", got)
	}
}

// TestTheNestedSeamIsTheTurnsAlone covers the seam's availability: the
// turn's tool has it, and a call of an unknown tool through it is an
// error, not a recorded call.
func TestTheNestedSeamIsTheTurnsAlone(t *testing.T) {
	nest := &nestedTool{}
	sink := &recordingSink{}
	s := nestedSession(t, nest, nil, sink)
	runSession(t, s, "hello")

	if nest.nester == nil {
		t.Fatal("the turn's tool saw no nested caller")
	}
	if _, err := nest.nester.Call(context.Background(), "r1", "nope", json.RawMessage(`{}`)); err == nil {
		t.Fatal("the nested call of an unknown tool returned no error")
	}
}
