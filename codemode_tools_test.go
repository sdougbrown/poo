package poo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode/quickjs"
	toolcodemode "github.com/sdougbrown/poo/tool/codemode"
)

// TestAQuickJSScriptCallsTheSessionsTools covers the wiring a model
// script depends on: the session lists its tools, the codemode tool
// hands that list to the engine, and the engine's wrapper turns each
// name into a callable. A break in any link leaves `tools.reader`
// undefined, so the script fails here rather than for the model.
func TestAQuickJSScriptCallsTheSessionsTools(t *testing.T) {
	script := `
const r = await tools.reader({text: "the argument"});
return [r.tool, r.is_error, r.text.includes("the tool's text"),
  typeof tools.worker, typeof tools.jobs, typeof tools.finish];
`
	arguments, err := json.Marshal(map[string]string{"script": script})
	if err != nil {
		t.Fatalf("marshalling the script: %v", err)
	}
	reader := &namedTool{echoTool: echoTool{result: "the tool's text"}, name: "reader"}
	// A background-only tool, plus the run's own job tools, are offered to
	// the model but are not a script's to call.
	worker := &namedTool{echoTool: echoTool{result: "a job", background: poo.BackgroundAlways}, name: "worker"}
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "codemode", Arguments: arguments}}},
		{text: "done"},
	})
	s := newTestSession(t, poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"codemode": toolcodemode.New(quickjs.New()), "reader": reader, "worker": worker},
		ToolOrder:    []string{"codemode", "reader", "worker"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	})
	runSession(t, s, "hello")

	if got := len(reader.Calls()); got != 1 {
		t.Fatalf("the session's reader ran %d times, want 1", got)
	}
	if got := reader.Calls()[0]; !strings.Contains(got, "the argument") {
		t.Errorf("the reader got the arguments %q, want the script's", got)
	}
	var res *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Handle == "r1" {
			res = r
		}
	}
	if res == nil || res.IsError {
		t.Fatalf("the codemode result is %+v, want a clean run", res)
	}
	if !strings.Contains(res.Text, `["reader",false,true,"undefined","undefined","undefined"]`) {
		t.Errorf("the run returned %s, want the named call's result and no background-only or core tool", res.Text)
	}
}
