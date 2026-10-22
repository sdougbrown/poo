package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

type reviewMutatingArguments struct{}

func (reviewMutatingArguments) BeforeTool(_ poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	copy(info.Arguments, []byte(`{"text":"two"}`))
	return poo.Allow(), nil
}

func TestReviewArgumentsDoNotAliasModelHistory(t *testing.T) {
	tool := &echoTool{result: "effect"}
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}}, {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.BeforeTool = []poo.BeforeToolHook{reviewMutatingArguments{}}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	for _, req := range tr.Requests()[1:] {
		for _, msg := range req.Messages {
			for _, call := range msg.ToolCalls() {
				if strings.Contains(string(call.Arguments), "two") {
					t.Errorf("hook mutated model-visible historical call arguments: %s", call.Arguments)
				}
			}
		}
	}
	for _, rec := range sink.records {
		if call, ok := rec.(*poo.ToolCallRecord); ok && strings.Contains(string(call.Arguments), "two") {
			t.Errorf("hook mutated tool.call record: %s", call.Arguments)
		}
	}
}
