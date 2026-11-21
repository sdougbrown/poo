package poo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode/quickjs"
	toolcodemode "github.com/sdougbrown/poo/tool/codemode"
)

// The QuickJS codemode tool on the real nested-call seam: the model
// calls codemode, the script names the session's other tools, and every
// call goes through the permission engine. These tests assert the
// scheduling the script observes.

// quickjsSession runs one session whose model-made call is the codemode
// tool's on the real QuickJS engine, with the extra tools the script
// names alongside it.
func quickjsSession(t *testing.T, script string, extra map[string]poo.Tool, sink *recordingSink, fns ...poo.Option) *poo.Session {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"script": script})
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

// codemodeResult returns the model's codemode call's result record.
func codemodeResult(t *testing.T, sink *recordingSink) *poo.ToolResultRecord {
	t.Helper()
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Handle == "r1" {
			return r
		}
	}
	t.Fatal("the log holds no result for the model's codemode call")
	return nil
}

// TestSubmittedReadsMeetAtARendezvous covers the synchronous submissions
// of Promise.all: two read-only named calls submitted together share one
// read-only batch, so each one meets the other inside its tool.
func TestSubmittedReadsMeetAtARendezvous(t *testing.T) {
	// strip drops a nested result's handle label, so the assertions below
	// match the text the tool returned and not the seam's presentation of
	// it.
	script := `
const strip = (r) => r.text.split("\n").slice(1).join("\n");
const [a, b] = await Promise.all([tools.reader({text: "one"}), tools.walker({text: "two"})]);
return [strip(a), strip(b)];
`
	r := newRendezvous()
	reader := &namedTool{echoTool: echoTool{call: r.meet}, name: "reader"}
	walker := &namedTool{echoTool: echoTool{call: r.meet}, name: "walker"}
	sink := &recordingSink{}
	s := quickjsSession(t, script,
		map[string]poo.Tool{"reader": reader, "walker": walker}, sink)
	runSession(t, s, "hello")

	if got := len(reader.Calls()) + len(walker.Calls()); got != 2 {
		t.Fatalf("the batch ran %d calls, want 2", got)
	}
	rec := codemodeResult(t, sink)
	if rec.IsError || rec.Outcome != poo.OutcomeOK {
		t.Fatalf("the script failed: %s", rec.Text)
	}
	// A call only answers "met" when the other call was inside its tool
	// at the same time: the batch ran its calls at once.
	if !strings.Contains(rec.Text, `["met","met"]`) {
		t.Errorf("the script's reads returned %s, want both to have met at the rendezvous", rec.Text)
	}
}

// TestAnExplicitParallelBatchOverlapsWritesAndStillAsks covers the
// explicit batch: side-effecting tools the script planned together run
// at the same time, a refusal by the permission engine is data in the
// batch's own order, and the refused tool never ran.
func TestAnExplicitParallelBatchOverlapsWritesAndStillAsks(t *testing.T) {
	script := `
const strip = (r) => r.text.split("\n").slice(1).join("\n");
const rs = await parallel([
  ["walker", {text: "one"}],
  ["stomper", {text: "two"}],
  ["denied", {text: "three"}],
]);
return rs.map(r => [r.tool, r.outcome, strip(r)]);
`
	r := newRendezvous()
	walker := &namedTool{echoTool: echoTool{call: r.meet, class: poo.ClassSideEffecting}, name: "walker"}
	stomper := &namedTool{echoTool: echoTool{call: r.meet, class: poo.ClassSideEffecting}, name: "stomper"}
	denied := &namedTool{echoTool: echoTool{class: poo.ClassSideEffecting}, name: "denied"}
	sink := &recordingSink{}
	s := quickjsSession(t, script,
		map[string]poo.Tool{"walker": walker, "stomper": stomper, "denied": denied},
		sink, poo.WithPolicy(&nestPolicy{deny: "denied"}))
	runSession(t, s, "hello")

	if got := len(denied.Calls()); got != 0 {
		t.Errorf("the refused tool ran %d times, want 0", got)
	}
	if got := len(walker.Calls()) + len(stomper.Calls()); got != 2 {
		t.Fatalf("the batch ran %d calls, want 2", got)
	}
	rec := codemodeResult(t, sink)
	if rec.IsError || rec.Outcome != poo.OutcomeOK {
		t.Fatalf("the script failed: %s", rec.Text)
	}
	// The two writers met inside their tools: the explicit batch ran
	// them at the same time. The refusal is data to the script, in the
	// batch's own order: walker, stomper, then the denied call with the
	// policy's reason.
	want := `[["walker","ok","met"],["stomper","ok","met"],["denied","denied","the denied call was refused by the test policy"]]`
	if !strings.Contains(rec.Text, want) {
		t.Errorf("the script's batch returned %s, want %s", rec.Text, want)
	}
}

// TestAMixedBatchOfNamedCallsRunsInOrder covers the default scheduling
// in the real engine: named calls submitted together form one batch, and
// a batch with a side-effecting call runs in order, so the second call
// starts only after the first has finished.
func TestAMixedBatchOfNamedCallsRunsInOrder(t *testing.T) {
	script := `
const strip = (r) => r.text.split("\n").slice(1).join("\n");
const [a, b] = await Promise.all([tools.reader({text: "one"}), tools.writer({text: "two"})]);
return [strip(a), strip(b)];
`
	o := &order{}
	reader := &namedTool{echoTool: echoTool{call: o.first()}, name: "reader"}
	writer := &namedTool{echoTool: echoTool{call: o.second(), class: poo.ClassSideEffecting}, name: "writer"}
	sink := &recordingSink{}
	s := quickjsSession(t, script,
		map[string]poo.Tool{"reader": reader, "writer": writer}, sink)
	runSession(t, s, "hello")

	if o.overlappedTheFirst() {
		t.Error("the side-effecting call started before the read-only call finished, so the mixed batch ran its calls at the same time")
	}
	if got := len(reader.Calls()) + len(writer.Calls()); got != 2 {
		t.Fatalf("the batch ran %d calls, want 2", got)
	}
	rec := codemodeResult(t, sink)
	if rec.IsError || rec.Outcome != poo.OutcomeOK {
		t.Fatalf("the script failed: %s", rec.Text)
	}
	if !strings.Contains(rec.Text, `["first","second"]`) {
		t.Errorf("the script's batch returned %s, want the calls' results in batch order", rec.Text)
	}
}
