package poo_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// mutatingArguments is a before- or after-tool hook that edits its copy
// of the call's arguments.
type mutatingArguments struct{}

// BeforeTool edits the arguments it was given.
func (mutatingArguments) BeforeTool(_ poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	if len(info.Arguments) > 0 {
		copy(info.Arguments, []byte(`{"text":"edited"}`))
	}
	return poo.Allow(), nil
}

// AfterTool edits the arguments it was given.
func (mutatingArguments) AfterTool(_ poo.HookContext, info poo.ToolCallInfo, _ poo.ToolResult) (poo.ResultDecision, error) {
	if len(info.Arguments) > 0 {
		copy(info.Arguments, []byte(`{"text":"edited"}`))
	}
	return poo.Keep(), nil
}

// readingAfterTool records the arguments it was given.
type readingAfterTool struct {
	// arguments is the last call's arguments.
	arguments json.RawMessage
}

// AfterTool records the arguments.
func (h *readingAfterTool) AfterTool(_ poo.HookContext, info poo.ToolCallInfo, _ poo.ToolResult) (poo.ResultDecision, error) {
	h.arguments = append(json.RawMessage(nil), info.Arguments...)
	return poo.Keep(), nil
}

// TestAnAfterToolHookCannotEditAnotherHooksArguments covers the
// isolation rule for the after-tool chain: a hook gets its own copy of
// the call's arguments, so a later hook never sees an earlier hook's
// edits.
func TestAnAfterToolHookCannotEditAnotherHooksArguments(t *testing.T) {
	reader := &readingAfterTool{}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTool = []poo.AfterToolHook{mutatingArguments{}, reader}
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	if string(reader.arguments) != `{"text":"one"}` {
		t.Errorf("the second hook saw arguments %s, want the model's own", reader.arguments)
	}
}

// readingBeforeTool records the arguments it was given.
type readingBeforeTool struct {
	// arguments is the last call's arguments.
	arguments json.RawMessage
}

// BeforeTool records the arguments.
func (h *readingBeforeTool) BeforeTool(_ poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	h.arguments = append(json.RawMessage(nil), info.Arguments...)
	return poo.Allow(), nil
}

// TestABeforeToolHookCannotEditAnotherHooksArguments covers the same
// rule for the before-tool chain.
func TestABeforeToolHookCannotEditAnotherHooksArguments(t *testing.T) {
	reader := &readingBeforeTool{}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeTool = []poo.BeforeToolHook{mutatingArguments{}, reader}
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	if string(reader.arguments) != `{"text":"one"}` {
		t.Errorf("the second hook saw arguments %s, want the model's own", reader.arguments)
	}
}
