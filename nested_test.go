package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode/starlark"
	toolcodemode "github.com/sdougbrown/poo/tool/codemode"
)

// The nested tool-call seam: a tool that runs in the turn may make
// calls of its own through ToolEnv.Nested, and the results come back to
// it without entering the model's history.

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

// denyNested is a consumer before-tool hook that refuses one tool.
type denyNested struct {
	// deny is the tool name every call of which is refused.
	deny string
}

func (h *denyNested) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	if info.Spec.Name == h.deny {
		return poo.Deny("the hook refused the " + info.Spec.Name + " call"), nil
	}
	return poo.Allow(), nil
}

// rewriteNested is a consumer after-tool hook that replaces one tool's
// result text.
type rewriteNested struct {
	// tool is the tool whose results are replaced.
	tool string
	// text is the result text the hook substitutes.
	text string
}

func (h *rewriteNested) AfterTool(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
	if info.Spec.Name != h.tool {
		return poo.Keep(), nil
	}
	return poo.Replace(poo.ToolResult{Text: h.text}), nil
}

// nestedTool is a tool that makes the nested calls a test scripts.
//
// script holds "tool arguments-json" lines; the tool makes one batch of
// them through env.Nested and reports the outcomes as its own result
// text. The seam's own results are kept for a test to assert on.
type nestedTool struct {
	mu sync.Mutex
	// nester is the env's nested caller, kept so a test can assert on
	// the seam itself.
	nester poo.NestedCaller
	// script is the batch the tool makes.
	script []string
	// wait, when set, blocks before the batch is made, so a test can
	// act at a known point of the parent call.
	wait func(ctx context.Context)
	// seamCtx, when set, is the context the batch is asked under, in
	// place of the call's own.
	seamCtx context.Context
	// results are the results the seam returned, in batch order.
	results []poo.NestedResult
	// seamErr is the error the seam returned, when it returned one.
	seamErr error
	// settings are the settings the tool's own call ran under.
	settings poo.ToolSettings
}

func (t *nestedTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "nest",
		Description: "make nested calls",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Class:       poo.ClassSideEffecting,
	}
}

func (t *nestedTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return nestPrepared{}, nil
}

func (t *nestedTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if env.Nested == nil {
		return poo.ToolResult{Text: "no nested caller", IsError: true}, nil
	}
	if t.wait != nil {
		t.wait(ctx)
	}
	calls := make([]poo.NestedCall, 0, len(t.script))
	for _, step := range t.script {
		parts := strings.SplitN(step, " ", 2)
		call := poo.NestedCall{Tool: parts[0], Arguments: json.RawMessage("{}")}
		if len(parts) == 2 {
			call.Arguments = json.RawMessage(parts[1])
		}
		calls = append(calls, call)
	}
	callCtx := ctx
	if t.seamCtx != nil {
		callCtx = t.seamCtx
	}
	results, err := env.Nested.Call(callCtx, calls)
	t.mu.Lock()
	t.nester = env.Nested
	t.settings = env.Settings
	t.results, t.seamErr = results, err
	t.mu.Unlock()
	if err != nil {
		return poo.ToolResult{Text: "the batch did not start: " + err.Error(), IsError: true}, nil
	}
	lines := make([]string, 0, len(results))
	for _, res := range results {
		lines = append(lines, res.Tool+": "+string(res.Outcome))
	}
	return poo.ToolResult{Text: strings.Join(lines, "\n")}, nil
}

// returned returns the results the seam gave back, and the error it
// gave instead.
func (t *nestedTool) returned() ([]poo.NestedResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]poo.NestedResult(nil), t.results...), t.seamErr
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

	// The events say a call is nested by naming its parent, and not by a
	// key of its own.
	seenNested := 0
	for _, ev := range s.Events() {
		if _, ok := ev.Payload["nested"]; ok {
			t.Errorf("the %s event carries a separate nested key", ev.Name)
		}
		switch ev.Name {
		case poo.EventToolCall:
			if ev.Payload["parent"] == "r1" {
				seenNested++
			}
		case poo.EventToolResult:
			if ev.Payload["handle"] != "r1" {
				continue
			}
			if _, ok := ev.Payload["parent"]; ok {
				t.Error("the nest call's own result names a parent")
			}
		}
	}
	if seenNested != 3 {
		t.Errorf("the event stream names %d nested tool calls, want 3", seenNested)
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
	bash := &namedTool{echoTool: echoTool{class: poo.ClassSideEffecting}, name: "bash"}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"reader": reader, "bash": bash}, sink,
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

// TestNestedCallsCountTowardTheDoomLoop covers the counting: with the
// threshold at three, the third and fourth of four identical nested
// calls are refused inside one script, and none of it reaches the model.
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
	if denied != 2 {
		t.Errorf("the log holds %d denied results, want 2 (the third and fourth calls)", denied)
	}
}

// renamedFinishTool is the finish tool a consumer renamed: it is not
// called "finish", and its prepared call carries the typed result that
// settles the run.
type renamedFinishTool struct{}

func (renamedFinishTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "settle",
		Description: "settle the run with a result",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}
}

func (renamedFinishTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return renamedFinishPrepared{}, nil
}

func (renamedFinishTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "settled"}, nil
}

// renamedFinishPrepared is the renamed finish tool's prepared call.
type renamedFinishPrepared struct{}

func (renamedFinishPrepared) Summary() string { return "settle the run" }

// FinishResult makes the prepared call the finish tool's own, whatever
// the tool is called.
func (renamedFinishPrepared) FinishResult() json.RawMessage {
	return json.RawMessage(`{"ok":true}`)
}

// TestANestedBatchIsValidatedBeforeItStarts covers the batch's
// validation: an entry that names a run tool, a renamed finish tool, an
// unknown tool, or a call in the background fails the whole batch before
// any of it is claimed, so a typo in a fan-out has no effect.
func TestANestedBatchIsValidatedBeforeItStarts(t *testing.T) {
	big := `reader {"text":"` + strings.Repeat("a", poo.NestedArgumentMaxBytes+1) + `"}`
	for _, tc := range []struct {
		name  string
		entry string
		want  string
		// tools are the extra tools the batch's entry needs.
		tools map[string]poo.Tool
	}{
		{name: "run tool", entry: `finish {}`, want: `"finish" cannot run as a nested call`},
		{name: "oversized arguments", entry: big, want: "budget for a nested call"},
		{name: "renamed finish tool", entry: `settle {}`, want: `"settle" cannot run as a nested call`,
			tools: map[string]poo.Tool{"settle": renamedFinishTool{}}},
		{name: "unknown tool", entry: `typo {"text":"x"}`, want: `unknown tool "typo"`},
		{name: "background request", entry: `worker {"text":"x","background":true}`,
			want:  `"worker" cannot run in the background as a nested call`,
			tools: map[string]poo.Tool{"worker": &namedTool{echoTool: echoTool{background: poo.BackgroundAllowed}, name: "worker"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nest := &nestedTool{script: []string{`reader {"text":"one"}`, tc.entry}}
			reader := &namedTool{echoTool: echoTool{result: "ok"}, name: "reader"}
			tools := map[string]poo.Tool{"reader": reader}
			for name, tool := range tc.tools {
				tools[name] = tool
			}
			sink := &recordingSink{}
			s := nestedSession(t, nest, tools, sink)
			runSession(t, s, "hello")

			_, err := nest.returned()
			if err == nil {
				t.Fatal("the seam returned no error for a batch it may not run")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the seam's error is %q, want it to name %q", err, tc.want)
			}
			if got := len(reader.Calls()); got != 0 {
				t.Errorf("the reader ran %d times, want 0 (the batch never started)", got)
			}
			for _, rec := range sink.allRecords() {
				switch r := rec.(type) {
				case *poo.ToolCallRecord:
					if r.Tool != "nest" {
						t.Errorf("the log holds a tool.call record for %q, want only the nest call", r.Tool)
					}
				case *poo.ToolResultRecord:
					if r.Tool != "nest" {
						t.Errorf("the log holds a tool.result record for %q, want only the nest call", r.Tool)
					}
				}
			}
		})
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

	inner.mu.Lock()
	nerster := inner.nester
	inner.mu.Unlock()
	if nerster != nil {
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
	results, err := nest.returned()
	if err != nil {
		t.Fatalf("the seam returned an error for a batch it started: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("the seam returned %d results, want 3", len(results))
	}
	if results[1].Outcome != poo.OutcomeError || !results[1].IsError {
		t.Errorf("the failed call's result is %q/%v, want an error", results[1].Outcome, results[1].IsError)
	}
	if !strings.Contains(results[1].Text, "no such file") {
		t.Errorf("the failed call's text is %q, want the validation failure", results[1].Text)
	}
}

// TestTheConsumerHooksSeeNestedCalls covers the hooks: a nested call goes
// through the before-tool and after-tool chains like a model-made call,
// so a consumer's refusal and a consumer's rewrite both reach the
// script.
func TestTheConsumerHooksSeeNestedCalls(t *testing.T) {
	nest := &nestedTool{script: []string{
		`reader {"text":"one"}`,
		`bash {"command":"rm -rf /"}`,
	}}
	reader := &namedTool{echoTool: echoTool{result: "the secret"}, name: "reader"}
	bash := &namedTool{echoTool: echoTool{class: poo.ClassSideEffecting}, name: "bash"}
	sink := &recordingSink{}
	s := nestedSession(t, nest,
		map[string]poo.Tool{"reader": reader, "bash": bash}, sink,
		poo.WithBeforeTool(&denyNested{deny: "bash"}),
		poo.WithAfterTool(&rewriteNested{tool: "reader", text: "the redacted text"}),
	)
	runSession(t, s, "hello")

	results, err := nest.returned()
	if err != nil {
		t.Fatalf("the seam returned an error for a batch it started: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("the seam returned %d results, want 2", len(results))
	}
	if !strings.Contains(results[0].Text, "the redacted text") {
		t.Errorf("the nested result the caller read is %q, want the after-tool hook's text", results[0].Text)
	}
	if strings.Contains(results[0].Text, "the secret") {
		t.Error("the caller read the result text the after-tool hook replaced")
	}
	if results[1].Outcome != poo.OutcomeDenied || !results[1].IsError {
		t.Errorf("the refused call's result is %q/%v, want a denial", results[1].Outcome, results[1].IsError)
	}
	if !strings.Contains(results[1].Text, "the hook refused") {
		t.Errorf("the refused call's text is %q, want the hook's reason", results[1].Text)
	}
	if got := len(bash.Calls()); got != 0 {
		t.Errorf("the refused tool ran %d times, want 0", got)
	}
}

// rendezvous is where the two calls of a parallel batch meet.
//
// A call announces that it is inside, then waits for the other call to
// announce itself. A batch that ran its calls one after another never
// gets the second announcement, and the wait is bounded so the test
// fails instead of hanging.
type rendezvous struct {
	mu sync.Mutex
	// arrived counts the calls that have entered the tool.
	arrived int
	// once closes met when the second call arrives.
	once sync.Once
	// met is closed once both calls are inside.
	met chan struct{}
}

// newRendezvous returns a rendezvous no call has reached yet.
func newRendezvous() *rendezvous {
	return &rendezvous{met: make(chan struct{})}
}

// meet announces the call and waits for the other call to arrive.
func (r *rendezvous) meet(context.Context) (poo.ToolResult, error) {
	r.mu.Lock()
	r.arrived++
	together := r.arrived >= 2
	r.mu.Unlock()
	if together {
		r.once.Do(func() { close(r.met) })
	}
	select {
	case <-r.met:
		return poo.ToolResult{Text: "met"}, nil
	case <-time.After(5 * time.Second):
		return poo.ToolResult{Text: "alone"}, nil
	}
}

// order says whether a batch ran its calls one after another.
//
// The first call marks the end of its run, and the second reports
// whether it started after that mark. A batch that ran its calls at the
// same time starts the second call while the first one runs.
type order struct {
	mu sync.Mutex
	// finished is set when the first call's tool returns.
	finished bool
	// overlapped is set when the second call starts before that.
	overlapped bool
}

// first returns the first call's tool, which marks its run as finished.
func (o *order) first() func(context.Context) (poo.ToolResult, error) {
	return func(context.Context) (poo.ToolResult, error) {
		o.mu.Lock()
		o.finished = true
		o.mu.Unlock()
		return poo.ToolResult{Text: "first"}, nil
	}
}

// second returns the second call's tool, which reports whether the first
// call was still running when it started.
func (o *order) second() func(context.Context) (poo.ToolResult, error) {
	return func(context.Context) (poo.ToolResult, error) {
		o.mu.Lock()
		o.overlapped = !o.finished
		o.mu.Unlock()
		return poo.ToolResult{Text: "second"}, nil
	}
}

// overlappedTheFirst reports whether the second call started before the
// first one finished.
func (o *order) overlappedTheFirst() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.overlapped
}

// TestANestedBatchIsScheduledLikeTheModels covers the scheduling rule:
// a batch runs in parallel only when every call in it is read-only, so a
// side-effecting call never overlaps another.
func TestANestedBatchIsScheduledLikeTheModels(t *testing.T) {
	t.Run("read-only batch", func(t *testing.T) {
		// Both calls of a read-only batch run at once, so each one
		// meets the other inside its tool.
		r := newRendezvous()
		reader := &namedTool{echoTool: echoTool{call: r.meet}, name: "reader"}
		walker := &namedTool{echoTool: echoTool{call: r.meet}, name: "walker"}
		nest := &nestedTool{script: []string{`reader {"text":"one"}`, `walker {"text":"two"}`}}
		sink := &recordingSink{}
		s := nestedSession(t, nest,
			map[string]poo.Tool{"reader": reader, "walker": walker}, sink)
		runSession(t, s, "hello")

		results, err := nest.returned()
		if err != nil {
			t.Fatalf("the seam returned an error for a batch it started: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("the seam returned %d results, want 2", len(results))
		}
		for _, res := range results {
			if !strings.Contains(res.Text, "met") {
				t.Errorf("the %s call's result is %q, want it to have met the other call", res.Tool, res.Text)
			}
		}
		if got := len(reader.Calls()) + len(walker.Calls()); got != 2 {
			t.Errorf("the batch ran %d calls, want 2", got)
		}
	})

	t.Run("side-effecting batch", func(t *testing.T) {
		// One side-effecting call makes the batch sequential: the
		// second call starts after the first one has finished.
		o := &order{}
		reader := &namedTool{echoTool: echoTool{call: o.first()}, name: "reader"}
		walker := &namedTool{echoTool: echoTool{call: o.second(), class: poo.ClassSideEffecting}, name: "walker"}
		nest := &nestedTool{script: []string{`reader {"text":"one"}`, `walker {"text":"two"}`}}
		sink := &recordingSink{}
		s := nestedSession(t, nest,
			map[string]poo.Tool{"reader": reader, "walker": walker}, sink)
		runSession(t, s, "hello")

		results, err := nest.returned()
		if err != nil {
			t.Fatalf("the seam returned an error for a batch it started: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("the seam returned %d results, want 2", len(results))
		}
		if o.overlappedTheFirst() {
			t.Error("the second call started before the first finished, so the batch ran its calls at the same time")
		}
		if got := len(reader.Calls()) + len(walker.Calls()); got != 2 {
			t.Errorf("the batch ran %d calls, want 2", got)
		}
	})
}

// resultCapture reports whether the view answered with a handle's
// result at each turn end.
type resultCapture struct {
	// handle is the handle the hook asks about.
	handle string
	mu     sync.Mutex
	// seen holds the answer of each turn end.
	seen []bool
}

func (c *resultCapture) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	_, ok := ctx.HistoryView.Result(c.handle)
	c.mu.Lock()
	c.seen = append(c.seen, ok)
	c.mu.Unlock()
	return poo.TurnDecision{}, nil
}

// everSeen reports whether the view ever held the handle's result.
func (c *resultCapture) everSeen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, seen := range c.seen {
		if seen {
			return true
		}
	}
	return false
}

// TestTheRestoreLeavesNestedCallsOutOfTheTurns covers the restore's
// rule for nested calls: a restored turn holds the calls the model made
// and not the calls a tool made, and the nested results are not among
// the results the restored view answers with.
func TestTheRestoreLeavesNestedCallsOutOfTheTurns(t *testing.T) {
	nest := &nestedTool{script: []string{`reader {"text":"one"}`}}
	reader := &namedTool{echoTool: echoTool{result: "ok"}, name: "reader"}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"reader": reader}, sink)
	runSession(t, s, "hello")

	// The nested call's handle is the one the log names with a parent.
	var nestedHandle, parentHandle string
	for _, rec := range sink.allRecords() {
		call, ok := rec.(*poo.ToolCallRecord)
		if !ok {
			continue
		}
		if call.Parent != "" {
			nestedHandle = call.Handle
			continue
		}
		parentHandle = call.Handle
	}
	if nestedHandle == "" || parentHandle == "" {
		t.Fatalf("the log holds no nested call: nested %q, parent %q", nestedHandle, parentHandle)
	}

	created := header("s1")
	created.Tools = []poo.ToolInfo{
		{Name: "nest", Class: poo.ClassSideEffecting, Background: poo.BackgroundNever},
		{Name: "reader", Class: poo.ClassReadOnly, Background: poo.BackgroundNever},
	}
	records := append([]poo.Record{created}, sink.allRecords()...)

	// The restore invokes no hook, so a later turn is where a hook reads
	// the rebuilt turn summary.
	turns := &turnCapture{}
	nestedResult := &resultCapture{handle: nestedHandle}
	restoreOpts := restoreOptions(t, scripted([]fakeResponse{{text: "again"}}), &recordingSink{})
	restoreOpts.Tools = map[string]poo.Tool{
		"nest":   &nestedTool{},
		"reader": &namedTool{echoTool: echoTool{}, name: "reader"},
	}
	restoreOpts.ToolOrder = []string{"nest", "reader"}
	restoreOpts.Profiles = []poo.Profile{{Name: "code", Tools: created.Tools}}
	restoreOpts.AfterTurn = []poo.AfterTurnHook{turns, nestedResult}
	session, _, err := poo.Restore(restoreOpts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	runSession(t, session, "more")

	sawParent := false
	for _, snapshot := range turns.captured() {
		for _, turn := range snapshot {
			for _, call := range turn.Calls {
				if call.Handle == nestedHandle {
					t.Errorf("the restored turn holds the nested call %s", nestedHandle)
				}
				if call.Handle == parentHandle {
					sawParent = true
				}
			}
		}
	}
	if !sawParent {
		t.Errorf("the restored turn holds no call for %s", parentHandle)
	}
	if nestedResult.everSeen() {
		t.Errorf("the restored view answers with the nested call %s's result", nestedHandle)
	}
}

// TestANestedCallSettledWithTheRunLeavesNoHistory covers the settle
// path: a nested call that is outstanding when the run settles gets its
// record, but nothing enters the model's history and no message record
// is written for it.
func TestANestedCallSettledWithTheRunLeavesNoHistory(t *testing.T) {
	blocker := &namedTool{echoTool: echoTool{
		call: func(ctx context.Context) (poo.ToolResult, error) {
			<-ctx.Done()
			return poo.ToolResult{Text: "the partial output"}, ctx.Err()
		},
	}, name: "blocker"}
	nest := &nestedTool{script: []string{`blocker {}`}}
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "nest", Arguments: json.RawMessage(`{}`)}}},
		{text: "done"},
	})
	opts := poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"nest": nest, "blocker": blocker},
		ToolOrder:    []string{"nest", "blocker"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The nested call is in flight when the run is cancelled.
	var nestedHandle string
	waitFor(t, "the nested call to start", func() bool {
		for _, ev := range s.Events() {
			if ev.Name != poo.EventToolCall || ev.Payload["parent"] != "r1" {
				continue
			}
			nestedHandle, _ = ev.Payload["handle"].(string)
			return nestedHandle != ""
		}
		return false
	})
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The nested call's result is recorded exactly once, for the script
	// that asked for it.
	results := map[string]int{}
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			results[r.Handle]++
		}
	}
	if got := results[nestedHandle]; got != 1 {
		t.Errorf("the nested call's result landed %d times, want 1", got)
	}
	// The log holds no message for the nested call: the model never saw
	// it, and a message would carry no call id.
	for _, rec := range sink.allRecords() {
		msg, ok := rec.(*poo.MessageRecord)
		if !ok {
			continue
		}
		for _, block := range msg.Content {
			if block.Kind == poo.ContentToolResult && block.ToolCallID == "" {
				t.Errorf("the log holds a tool result with no call id: %q", block.Text)
			}
		}
	}

	// The next run's request carries the model's view, and the view
	// holds no such result either.
	if _, err := s.Prompt(context.Background(), "again"); err != nil {
		t.Fatalf("starting the next run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the next run: %v", err)
	}
	requests := tr.Requests()
	last := requests[len(requests)-1]
	for _, msg := range last.Messages {
		for _, block := range msg.Content {
			if block.Kind != poo.ContentToolResult {
				continue
			}
			if block.ToolCallID == "" {
				t.Errorf("the next request carries a tool result with no call id: %q", block.Text)
			}
			if strings.Contains(block.Text, nestedHandle+" ·") {
				t.Errorf("the next request carries the nested call's result: %q", block.Text)
			}
		}
	}
}

// TestASkippedNestedCallIsData covers the script's view of a nested call
// that never ran: the first call of a side-effecting batch blocks until
// the run is cancelled, and the second call does not start.
func TestASkippedNestedCallIsData(t *testing.T) {
	entered := make(chan struct{})
	blocker := &namedTool{echoTool: echoTool{
		class: poo.ClassSideEffecting,
		call: func(ctx context.Context) (poo.ToolResult, error) {
			close(entered)
			<-ctx.Done()
			return poo.ToolResult{Text: "the partial output"}, ctx.Err()
		},
	}, name: "blocker"}
	walker := &namedTool{echoTool: echoTool{result: "walked", class: poo.ClassSideEffecting}, name: "walker"}
	nest := &nestedTool{script: []string{`blocker {}`, `walker {}`}}
	sink := &recordingSink{}
	s := nestedSession(t, nest,
		map[string]poo.Tool{"blocker": blocker, "walker": walker}, sink)
	if _, err := s.Prompt(context.Background(), "hello"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the nested call never started")
	}
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	results, err := nest.returned()
	if err != nil {
		t.Fatalf("the seam returned an error for a batch it started: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("the seam returned %d results, want 2", len(results))
	}
	second := results[1]
	if second.Outcome != poo.OutcomeSkipped {
		t.Errorf("the second call's result is %q, want skipped", second.Outcome)
	}
	if !second.IsError {
		t.Errorf("the skipped call's result is %q, want an error", second.Outcome)
	}
	// A call that never started never reached its tool: a skipped call
	// that ran would be the seam starting work the run had stopped.
	if got := len(walker.Calls()); got != 0 {
		t.Errorf("the walker ran %d times for a skipped call, want 0", got)
	}

	// The record the log wrote for the skipped call, when it wrote one,
	// says which call made it.
	for _, rec := range sink.allRecords() {
		call, ok := rec.(*poo.ToolCallRecord)
		if !ok || call.Handle != second.Handle {
			continue
		}
		if call.Parent != "r1" {
			t.Errorf("the skipped call's record names the parent %q, want r1", call.Parent)
		}
	}
}

// TestANestedBatchDoesNotStartInAWindingDownRun covers the seam's
// refusal for a run that is already settling: the parent call outlives
// the run's cancellation and asks for its batch afterwards, and the
// batch never starts.
func TestANestedBatchDoesNotStartInAWindingDownRun(t *testing.T) {
	reader := &namedTool{echoTool: echoTool{result: "ok"}, name: "reader"}
	entered := make(chan struct{})
	nest := &nestedTool{script: []string{`reader {"text":"one"}`}}
	// The run's cancellation ends the parent call's context, and the
	// finalization begins before it cancels: a call that wakes here
	// asks the seam for a batch in a run that is winding down. The
	// batch is asked for under a context of its own, so the run's state
	// is the only thing that can refuse it.
	nest.wait = func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
	}
	nest.seamCtx = context.Background()
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"reader": reader}, sink)
	if _, err := s.Prompt(context.Background(), "hello"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the nest call never started")
	}
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	results, err := nest.returned()
	if err == nil {
		t.Fatalf("the seam started a batch in a winding-down run: %v", results)
	}
	if !strings.Contains(err.Error(), "winding down") {
		t.Errorf("the seam's error is %q, want the winding-down refusal", err)
	}
	if got := len(reader.Calls()); got != 0 {
		t.Errorf("the reader ran %d times, want 0 (the batch never started)", got)
	}
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolCallRecord); ok && r.Tool == "reader" {
			t.Errorf("the log holds a tool.call record for %q, want none", r.Tool)
		}
	}
}

// TestANestedBatchDoesNotStartOnACancelledContext covers the seam's
// refusal for a context that is already gone: the parent call is still
// running in an active run and asks for its batch under a context that
// was cancelled before it asked.
func TestANestedBatchDoesNotStartOnACancelledContext(t *testing.T) {
	reader := &namedTool{echoTool: echoTool{result: "ok"}, name: "reader"}
	nest := &nestedTool{script: []string{`reader {"text":"one"}`}}
	// The run is active, and the batch's own context is already done, so
	// the seam's check on the caller's context is the only refusal left.
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	nest.seamCtx = dead
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"reader": reader}, sink)
	res := runSession(t, s, "hello")

	results, err := nest.returned()
	if err == nil {
		t.Fatalf("the seam started a batch on a cancelled context: %v", results)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the seam's error is %q, want the context's own", err)
	}
	if got := len(reader.Calls()); got != 0 {
		t.Errorf("the reader ran %d times, want 0 (the batch never started)", got)
	}
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolCallRecord); ok && r.Tool == "reader" {
			t.Errorf("the log holds a tool.call record for %q, want none", r.Tool)
		}
	}
	// The refusal is the parent call's own result: the run carries on to
	// its normal end.
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want the natural end of the turn", res.Reason)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
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
	if _, err := nest.nester.Call(context.Background(), []poo.NestedCall{{Tool: "nope"}}); err == nil {
		t.Fatal("the nested call of an unknown tool returned no error")
	}
}

// capsTool is a tool that keeps the output caps its environment gave it.
type capsTool struct {
	mu          sync.Mutex
	observation int
	read        int
}

func (t *capsTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "caps",
		Description: "keep the caps",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Class:       poo.ClassReadOnly,
	}
}

func (t *capsTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return capsPrepared{}, nil
}

func (t *capsTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.mu.Lock()
	t.observation = env.Settings.ObservationCapChars
	t.read = env.Settings.ReadOutputCapChars
	t.mu.Unlock()
	return poo.ToolResult{Text: "ok"}, nil
}

type capsPrepared struct{}

func (capsPrepared) Summary() string { return "keep the caps" }

// TestANestedCallGetsTheNestedOutputCaps covers the caps a nested call
// runs under: its output goes to the tool that made the call, not to
// the model, so it is capped at poo.NestedOutputCapChars and not at the
// session's context-relative caps.
func TestANestedCallGetsTheNestedOutputCaps(t *testing.T) {
	inner := &capsTool{}
	nest := &nestedTool{script: []string{`caps {}`}}
	s := nestedSession(t, nest, map[string]poo.Tool{"caps": inner}, &recordingSink{}, func(o *poo.Options) {
		o.Settings.ObservationCapChars = 1000
		o.Settings.ReadOutputCapChars = 2000
	})
	runSession(t, s, "hello")

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if inner.observation != poo.NestedOutputCapChars {
		t.Errorf("the nested call's observation cap = %d, want %d", inner.observation, poo.NestedOutputCapChars)
	}
	if inner.read != poo.NestedOutputCapChars {
		t.Errorf("the nested call's read cap = %d, want %d", inner.read, poo.NestedOutputCapChars)
	}

	// The model-made call keeps the session's caps: its result is what
	// the model reads.
	nest.mu.Lock()
	defer nest.mu.Unlock()
	if got := nest.settings.ObservationCapChars; got != 1000 {
		t.Errorf("the model-made call's observation cap = %d, want 1000", got)
	}
	if got := nest.settings.ReadOutputCapChars; got != 2000 {
		t.Errorf("the model-made call's read cap = %d, want 2000", got)
	}
}

// bigTool is a tool whose result is longer than the session's caps.
type bigTool struct{ text string }

func (t *bigTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "big",
		Description: "return a long result",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Class:       poo.ClassReadOnly,
	}
}

func (t *bigTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return capsPrepared{}, nil
}

func (t *bigTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: t.text, Structured: json.RawMessage(`{"long":true}`)}, nil
}

// TestANestedResultKeepsTheSessionCapOutsideItsCaller covers where a
// nested result goes: its caller reads it whole, but its tool.result
// event and the text of its record hold no more than the session's
// observation cap, so a script's large results cannot flood an event
// consumer or the log. The record keeps the structured payload whole.
func TestANestedResultKeepsTheSessionCapOutsideItsCaller(t *testing.T) {
	long := strings.Repeat("é", 5000)
	nest := &nestedTool{script: []string{`big {}`}}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"big": &bigTool{text: long}}, sink, func(o *poo.Options) {
		o.Settings.ObservationCapChars = 1000
	})
	runSession(t, s, "hello")

	results, err := nest.returned()
	if err != nil || len(results) != 1 {
		t.Fatalf("the seam returned %d results and %v", len(results), err)
	}
	if !strings.Contains(results[0].Text, long) {
		t.Errorf("the caller got %d characters, want the whole result", utf8.RuneCountInString(results[0].Text))
	}

	found := false
	for _, ev := range s.Events() {
		if ev.Name != poo.EventToolResult || ev.Payload["parent"] == nil {
			continue
		}
		found = true
		text, _ := ev.Payload["text"].(string)
		elided, _ := ev.Payload["elided_chars"].(int)
		checkCutText(t, "the nested result event", text, elided, results[0].Text, 1000)
	}
	if !found {
		t.Fatal("no nested tool.result event")
	}

	var rec *poo.ToolResultRecord
	for _, r := range sink.allRecords() {
		if r, ok := r.(*poo.ToolResultRecord); ok && r.Handle == results[0].Handle {
			rec = r
		}
	}
	if rec == nil {
		t.Fatal("no record of the nested result")
	}
	checkCutText(t, "the nested result record", rec.Text, rec.ElidedChars, results[0].Text, 1000)
	if string(rec.Structured) != `{"long":true}` {
		t.Errorf("the record's structured payload is %s, want it whole", rec.Structured)
	}
}

// TestANestedResultWithNoSessionCapKeepsTheDefault covers a session
// built without an observation cap: a nested result's record holds its
// text to the largest observation the model ever sees, 30000
// characters.
func TestANestedResultWithNoSessionCapKeepsTheDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		body int
		cut  bool
	}{
		{"under the default", 29000, false},
		{"over the default", 31000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nest := &nestedTool{script: []string{`big {}`}}
			sink := &recordingSink{}
			body := strings.Repeat("x", tc.body)
			s := nestedSession(t, nest, map[string]poo.Tool{"big": &bigTool{text: body}}, sink)
			runSession(t, s, "hello")

			results, err := nest.returned()
			if err != nil || len(results) != 1 {
				t.Fatalf("the seam returned %d results and %v", len(results), err)
			}
			if !strings.Contains(results[0].Text, body) {
				t.Fatal("the caller did not get the whole result")
			}
			var rec *poo.ToolResultRecord
			for _, r := range sink.allRecords() {
				if r, ok := r.(*poo.ToolResultRecord); ok && r.Handle == results[0].Handle {
					rec = r
				}
			}
			if rec == nil {
				t.Fatal("no record of the nested result")
			}
			if !tc.cut {
				if rec.Text != results[0].Text || rec.ElidedChars != 0 {
					t.Errorf("the record cut a result under the default: %d elided", rec.ElidedChars)
				}
				return
			}
			checkCutText(t, "the nested result record", rec.Text, rec.ElidedChars, results[0].Text, 30000)
			if got := utf8.RuneCountInString(strings.TrimSuffix(rec.Text, fmt.Sprintf(poo.NestedElisionNote, rec.ElidedChars))); got != 30000 {
				t.Errorf("the record keeps %d characters, want 30000", got)
			}
		})
	}
}

// checkCutText asserts that a nested result's cut text is the result's
// head, no longer than the cap, followed by the elision note, and that
// the head and the elided count add up to the whole result.
func checkCutText(t *testing.T, what, text string, elided int, whole string, limit int) {
	t.Helper()
	note := fmt.Sprintf(poo.NestedElisionNote, elided)
	head, ok := strings.CutSuffix(text, note)
	if !ok {
		t.Errorf("%s does not end with the elision note: %q", what, text[max(0, len(text)-80):])
		return
	}
	if n := utf8.RuneCountInString(head); n > limit {
		t.Errorf("%s keeps %d characters, want at most %d", what, n, limit)
	}
	if !utf8.ValidString(head) {
		t.Errorf("%s split a character", what)
	}
	if !strings.HasPrefix(whole, head) {
		t.Errorf("%s is not the head of the result", what)
	}
	if utf8.RuneCountInString(head)+elided != utf8.RuneCountInString(whole) {
		t.Errorf("%s and its elided count (%d) do not add up to the result", what, elided)
	}
}

// TestANestedResultsReplacedOriginalIsCut covers a hook that replaces a
// nested result: the record keeps the original the tool produced, cut
// like the result's own text.
func TestANestedResultsReplacedOriginalIsCut(t *testing.T) {
	long := strings.Repeat("y", 5000)
	nest := &nestedTool{script: []string{`big {}`}}
	sink := &recordingSink{}
	s := nestedSession(t, nest, map[string]poo.Tool{"big": &bigTool{text: long}}, sink,
		func(o *poo.Options) { o.Settings.ObservationCapChars = 1000 },
		poo.WithAfterTool(&rewriteNested{tool: "big", text: "replaced"}),
	)
	runSession(t, s, "hello")

	var rec *poo.ToolResultRecord
	for _, r := range sink.allRecords() {
		if r, ok := r.(*poo.ToolResultRecord); ok && r.Tool == "big" {
			rec = r
		}
	}
	if rec == nil || rec.Original == nil {
		t.Fatal("no record of the replaced nested result's original")
	}
	checkCutText(t, "the replaced original", rec.Original.Text, 5000-1000, long, 1000)
}

// TestAScriptCannotCallCodemode drives the nesting depth through the
// real seam: the model calls codemode, the script calls codemode
// again, and the inner call is a recoverable error the script reads.
// The run carries on, so the model gets a second turn.
func TestAScriptCannotCallCodemode(t *testing.T) {
	script := `r = call("codemode", {"script": "print('inner')"})
print(r["is_error"])
print(r["text"])
`
	arguments, err := json.Marshal(map[string]string{"script": script})
	if err != nil {
		t.Fatalf("marshalling the script: %v", err)
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "codemode", Arguments: arguments}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"codemode": toolcodemode.New(starlark.New())},
		ToolOrder:    []string{"codemode"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	})
	runSession(t, s, "hello")

	// The model was asked again: the inner codemode call did not settle
	// the run.
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the model was asked %d times, want 2 (the run continued past the inner call)", got)
	}

	// The inner call's own record is the error, and the outer script read
	// it as data.
	inner, outer := (*poo.ToolResultRecord)(nil), (*poo.ToolResultRecord)(nil)
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.Tool != "codemode" {
			continue
		}
		if r.Handle == "r1" {
			outer = r
			continue
		}
		inner = r
	}
	if inner == nil {
		t.Fatal("the log holds no record for the inner codemode call")
	}
	if !inner.IsError || !strings.Contains(inner.Text, "a nested call or a background job has no tools to call") {
		t.Errorf("the inner call's result is %q/%v, want the depth-limit error", inner.Text, inner.IsError)
	}
	if outer == nil {
		t.Fatal("the log holds no record for the model's codemode call")
	}
	if outer.IsError {
		t.Errorf("the model's call failed: %s", outer.Text)
	}
	if !strings.Contains(outer.Text, "True") {
		t.Errorf("the script's print of is_error is %q, want the error visible as data", outer.Text)
	}
	if !strings.Contains(outer.Text, "a nested call or a background job has no tools to call") {
		t.Errorf("the model's result is %q, want it to carry the inner call's reason", outer.Text)
	}
}
