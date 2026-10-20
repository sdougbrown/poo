package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sdougbrown/poo"
)

// parse runs the parser over a text and returns the tool calls and the
// prose it found.
func parse(t *testing.T, mode, text string) (calls []poo.ToolCallDelta, prose string) {
	t.Helper()
	p := newParser(mode)
	if mode == "native" {
		if p.enabled {
			t.Fatal("the native mode left the parser on")
		}
		return nil, text
	}
	var events []poo.StreamEvent
	events = append(events, p.feed(text)...)
	events = append(events, p.flush()...)
	for _, ev := range events {
		switch ev.Kind {
		case "tool_call":
			calls = append(calls, *ev.ToolCall)
		case "delta":
			prose += ev.Delta.Text
		}
	}
	return calls, prose
}

// wantCall is the expected shape of a parsed call.
type wantCall struct {
	name string
	args string
}

func checkCalls(t *testing.T, got []poo.ToolCallDelta, want []wantCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the parser found %d calls, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Name != w.name {
			t.Errorf("call %d is %q, want %q", i, got[i].Name, w.name)
		}
		// The arguments keep the model's own formatting, so they are
		// compared as JSON rather than as text.
		var gotArgs, wantArgs any
		if err := json.Unmarshal(got[i].Arguments, &gotArgs); err != nil {
			t.Errorf("call %d has unparseable arguments %s: %v", i, got[i].Arguments, err)
			continue
		}
		if err := json.Unmarshal([]byte(w.args), &wantArgs); err != nil {
			t.Fatalf("the expected arguments are not valid JSON: %v", err)
		}
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			t.Errorf("call %d has arguments %s, want %s", i, got[i].Arguments, w.args)
		}
		if !got[i].Complete {
			t.Errorf("call %d is not marked complete", i)
		}
	}
}

func TestTheHermesFormatIsParsed(t *testing.T) {
	text := `I will list the files.
<tool_call>{"name": "bash", "arguments": {"command": "ls -la"}}</tool_call>
That shows everything.`

	calls, prose := parse(t, "text", text)
	checkCalls(t, calls, []wantCall{{name: "bash", args: `{"command":"ls -la"}`}})
	// The newlines around the call belong to the prose.
	if prose != "I will list the files.\n\nThat shows everything." {
		t.Errorf("the prose is %q, want the text around the call", prose)
	}
}

func TestTheQwenXMLFormatIsParsed(t *testing.T) {
	text := `<tool_call>
<name>read</name>
<arguments>{"path": "main.go", "offset": 10}</arguments>
</tool_call>`

	calls, prose := parse(t, "text", text)
	checkCalls(t, calls, []wantCall{{name: "read", args: `{"path":"main.go","offset":10}`}})
	if prose != "" {
		t.Errorf("the prose is %q, want none", prose)
	}
}

func TestTheBareJSONFormatIsParsed(t *testing.T) {
	text := `{"name": "bash", "arguments": {"command": "pwd"}}`

	calls, prose := parse(t, "text", text)
	checkCalls(t, calls, []wantCall{{name: "bash", args: `{"command":"pwd"}`}})
	if prose != "" {
		t.Errorf("the prose is %q, want none", prose)
	}
}

func TestTheFencedJSONFormatIsParsed(t *testing.T) {
	text := "Here is the call:\n```json\n{\"name\": \"read\", \"arguments\": {\"path\": \"a.txt\"}}\n```\nDone."

	calls, prose := parse(t, "text", text)
	checkCalls(t, calls, []wantCall{{name: "read", args: `{"path":"a.txt"}`}})
	if prose != "Here is the call:\n\nDone." {
		t.Errorf("the prose is %q, want the text around the fence", prose)
	}
}

func TestAMalformedCallIsLeftAsProse(t *testing.T) {
	// The body is not valid JSON, so the block is not a call.
	text := `<tool_call>{"name": "bash", "arguments": }</tool_call>`

	calls, prose := parse(t, "text", text)
	if len(calls) != 0 {
		t.Errorf("the parser found %d calls, want none", len(calls))
	}
	if prose == "" {
		t.Error("the malformed call was dropped instead of shown as prose")
	}
}

func TestACallWithoutANameIsNotACall(t *testing.T) {
	text := `<tool_call>{"arguments": {"command": "ls"}}</tool_call>`

	calls, _ := parse(t, "text", text)
	if len(calls) != 0 {
		t.Errorf("the parser found %d calls, want none", len(calls))
	}
}

func TestTheNativeModeLeavesTheParserOff(t *testing.T) {
	text := `<tool_call>{"name": "bash", "arguments": {"command": "ls"}}</tool_call>`

	calls, prose := parse(t, "native", text)
	if len(calls) != 0 {
		t.Errorf("the native mode found %d calls, want none", len(calls))
	}
	if prose != text {
		t.Errorf("the native mode changed the text")
	}
}

func TestACallSplitAcrossChunksIsStillFound(t *testing.T) {
	p := newParser("text")

	var events []poo.StreamEvent
	events = append(events, p.feed("<tool_call>{\"name\": \"bash\", ")...)
	events = append(events, p.feed("\"arguments\": {\"command\": \"ls\"}}</tool_call>")...)
	events = append(events, p.flush()...)

	var calls []poo.ToolCallDelta
	for _, ev := range events {
		if ev.Kind == "tool_call" {
			calls = append(calls, *ev.ToolCall)
		}
	}
	checkCalls(t, calls, []wantCall{{name: "bash", args: `{"command":"ls"}`}})
}

func TestACallWithoutAClosingTagIsStillFound(t *testing.T) {
	p := newParser("text")

	var events []poo.StreamEvent
	events = append(events, p.feed(`<tool_call>{"name": "bash", "arguments": {"command": "ls"}}`)...)
	events = append(events, p.flush()...)

	var calls []poo.ToolCallDelta
	for _, ev := range events {
		if ev.Kind == "tool_call" {
			calls = append(calls, *ev.ToolCall)
		}
	}
	checkCalls(t, calls, []wantCall{{name: "bash", args: `{"command":"ls"}`}})
}

func TestSeveralCallsAreFoundInOrder(t *testing.T) {
	text := `<tool_call>{"name": "bash", "arguments": {"command": "one"}}</tool_call>
<tool_call>{"name": "read", "arguments": {"path": "a.txt"}}</tool_call>`

	calls, _ := parse(t, "text", text)
	checkCalls(t, calls, []wantCall{
		{name: "bash", args: `{"command":"one"}`},
		{name: "read", args: `{"path":"a.txt"}`},
	})
}

func TestProseWithABraceIsNotACall(t *testing.T) {
	text := "The value is {not json} and it stays prose."

	calls, prose := parse(t, "text", text)
	if len(calls) != 0 {
		t.Errorf("the parser found %d calls, want none", len(calls))
	}
	if prose != text {
		t.Errorf("the prose is %q, want the whole text", prose)
	}
}
