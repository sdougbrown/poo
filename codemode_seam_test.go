package poo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode/starlark"
	toolcodemode "github.com/sdougbrown/poo/tool/codemode"
)

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
