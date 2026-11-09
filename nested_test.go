package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
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
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
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
	for _, tc := range []struct {
		name  string
		entry string
		want  string
		// tools are the extra tools the batch's entry needs.
		tools map[string]poo.Tool
	}{
		{name: "run tool", entry: `finish {}`, want: `"finish" cannot run as a nested call`},
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

// gate is a rendezvous two calls meet at.
//
// A call arrives, reports the most calls that were ever inside at once,
// and leaves. A batch whose calls run one after another never reaches
// two.
type gate struct {
	mu   sync.Mutex
	in   int
	peak int
}

func (g *gate) arrive() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.in++
	if g.in > g.peak {
		g.peak = g.in
	}
}

func (g *gate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.in--
}

// peakIn returns the most calls that were inside at once.
func (g *gate) peakIn() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

// TestANestedBatchIsScheduledLikeTheModels covers the scheduling rule:
// a batch runs in parallel only when every call in it is read-only, so a
// side-effecting call never overlaps another.
func TestANestedBatchIsScheduledLikeTheModels(t *testing.T) {
	const wait = 200 * time.Millisecond
	// meet waits for a second call, and reports whether one came: a
	// sequential batch never has two calls inside at once, so the wait
	// is bounded and the answer is no.
	meet := func(g *gate) func(context.Context) (poo.ToolResult, error) {
		return func(ctx context.Context) (poo.ToolResult, error) {
			g.arrive()
			defer g.leave()
			deadline := time.Now().Add(wait)
			for time.Now().Before(deadline) {
				if g.peakIn() >= 2 {
					return poo.ToolResult{Text: "met"}, nil
				}
				time.Sleep(time.Millisecond)
			}
			return poo.ToolResult{Text: "alone"}, nil
		}
	}
	for _, tc := range []struct {
		name    string
		other   poo.ToolKind
		wantMet bool
	}{
		{name: "read-only batch", other: poo.ClassReadOnly, wantMet: true},
		{name: "side-effecting batch", other: poo.ClassSideEffecting, wantMet: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &gate{}
			reader := &namedTool{echoTool: echoTool{call: meet(g)}, name: "reader"}
			walker := &namedTool{echoTool: echoTool{call: meet(g), class: tc.other}, name: "walker"}
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
				met := strings.Contains(res.Text, "met")
				if met != tc.wantMet {
					t.Errorf("the %s call's result is %q, want met=%v", res.Tool, res.Text, tc.wantMet)
				}
			}
			// The tool ran both calls, whatever the answer: the batch
			// runs to completion.
			if got := len(reader.Calls()) + len(walker.Calls()); got != 2 {
				t.Errorf("the batch ran %d calls, want 2", got)
			}
		})
	}
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
	if second.Outcome != poo.OutcomeSkipped && second.Outcome != poo.OutcomeCancelled {
		t.Errorf("the skipped call's result is %q, want skipped or cancelled", second.Outcome)
	}
	if !second.IsError {
		t.Errorf("the skipped call's result is %q, want an error", second.Outcome)
	}
	t.Logf("the second nested call's outcome is %q", second.Outcome)
	// A call that never started never reached its tool: a skipped call
	// that ran would be the seam starting work the run had stopped.
	if second.Outcome == poo.OutcomeSkipped && len(walker.Calls()) != 0 {
		t.Errorf("the walker ran %d times for a skipped call, want 0", len(walker.Calls()))
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
