package openai

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The text tool-call formats.
//
// The plan: Hermes, Qwen XML, Mistral and bare JSON all parse into the
// same tool call, and a call that cannot be parsed is a visible
// recoverable error.

// parseEvents runs the parser over a text and returns the events it
// produced.
func parseEvents(t *testing.T, format, text string) []poo.StreamItem {
	t.Helper()
	p := newParser("text", format)
	var events []poo.StreamItem
	events = append(events, p.feed(text)...)
	events = append(events, p.flush()...)
	return events
}

// callsOf returns the tool call events.
func callsOf(events []poo.StreamItem) []poo.ToolCallDelta {
	var calls []poo.ToolCallDelta
	for _, ev := range events {
		if ev.Kind == "tool_call" && ev.ToolCall != nil {
			calls = append(calls, *ev.ToolCall)
		}
	}
	return calls
}

// TestTheMistralFormatIsParsed covers the Mistral shape: a [TOOL_CALLS]
// marker and a JSON array of calls.
func TestTheMistralFormatIsParsed(t *testing.T) {
	text := `[TOOL_CALLS] [{"name": "bash", "arguments": {"command": "ls"}}]`
	calls := callsOf(parseEvents(t, "", text))
	if len(calls) != 1 {
		t.Fatalf("the parser found %d calls, want 1", len(calls))
	}
	if calls[0].Name != "bash" {
		t.Errorf("the call is %q, want bash", calls[0].Name)
	}
	if string(calls[0].Arguments) != `{"command": "ls"}` {
		t.Errorf("the call's arguments are %s", calls[0].Arguments)
	}
	if !calls[0].Complete {
		t.Error("the call is not marked complete")
	}
}

// TestTheMistralFormatHoldsSeveralCalls covers a list with more than one
// call: every call is read, in the order the model wrote them.
func TestTheMistralFormatHoldsSeveralCalls(t *testing.T) {
	text := `[TOOL_CALLS] [{"name": "bash", "arguments": {"command": "ls"}}, {"name": "read", "arguments": {"path": "a.txt"}}]`
	calls := callsOf(parseEvents(t, "", text))
	if len(calls) != 2 {
		t.Fatalf("the parser found %d calls, want 2", len(calls))
	}
	if calls[0].Name != "bash" || calls[1].Name != "read" {
		t.Errorf("the calls are %q and %q, want bash and read", calls[0].Name, calls[1].Name)
	}
	if calls[0].Index == calls[1].Index {
		t.Errorf("the calls share the index %d, want them apart", calls[0].Index)
	}
}

// TestTheMistralArgumentsMayBeAString covers the shape some endpoints
// send: the arguments are a string holding the object.
func TestTheMistralArgumentsMayBeAString(t *testing.T) {
	text := `[TOOL_CALLS] [{"name": "bash", "arguments": "{\"command\": \"ls\"}"}]`
	calls := callsOf(parseEvents(t, "", text))
	if len(calls) != 1 {
		t.Fatalf("the parser found %d calls, want 1", len(calls))
	}
	if string(calls[0].Arguments) != `{"command": "ls"}` {
		t.Errorf("the call's arguments are %s", calls[0].Arguments)
	}
}

// TestEveryFormatIsRead covers the plan's list: Hermes, Qwen XML,
// Mistral and bare JSON all parse into the same call.
func TestEveryFormatIsRead(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"hermes", `<tool_call>{"name": "bash", "arguments": {"command": "ls"}}</tool_call>`},
		{"qwen", `<tool_call><name>bash</name><arguments>{"command":"ls"}</arguments></tool_call>`},
		{"mistral", `[TOOL_CALLS] [{"name": "bash", "arguments": {"command": "ls"}}]`},
		{"bare", "{\"name\": \"bash\", \"arguments\": {\"command\": \"ls\"}}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := callsOf(parseEvents(t, "", c.text))
			if len(calls) != 1 {
				t.Fatalf("the parser found %d calls, want 1", len(calls))
			}
			checkCalls(t, calls, []wantCall{{name: "bash", args: `{"command": "ls"}`}})
		})
	}
}

// TestTheFormatNamesTheShapeTheModelWrites covers the descriptor's text
// format: a model that writes one shape is read for that shape only.
func TestTheFormatNamesTheShapeTheModelWrites(t *testing.T) {
	hermes := `<tool_call>{"name": "bash", "arguments": {"command": "ls"}}</tool_call>`
	if calls := callsOf(parseEvents(t, "hermes", hermes)); len(calls) != 1 {
		t.Errorf("the parser found %d calls in the Hermes shape, want 1", len(calls))
	}
	if calls := callsOf(parseEvents(t, "mistral", hermes)); len(calls) != 0 {
		t.Errorf("the parser found %d calls in a shape the model does not write, want none", len(calls))
	}
}

// TestAMalformedCallIsAVisibleError covers the plan's rule: text that
// looks like a tool call but does not parse is not dropped. It stays in
// the prose, and the failure is reported.
func TestAMalformedCallIsAVisibleError(t *testing.T) {
	text := `<tool_call>{"name": "bash", "arguments": "not an object"}</tool_call>`
	events := parseEvents(t, "", text)

	var prose strings.Builder
	var failure *poo.StreamError
	for _, ev := range events {
		switch ev.Kind {
		case "delta":
			if ev.Delta != nil {
				prose.WriteString(ev.Delta.Text)
			}
		case "error":
			failure = ev.Error
		}
	}
	if !strings.Contains(prose.String(), "not an object") {
		t.Errorf("the prose is %q, want the model's own text", prose.String())
	}
	if failure == nil {
		t.Fatal("the parser reported no failure")
	}
	if !strings.HasPrefix(failure.Message, "your tool call could not be parsed: ") {
		t.Errorf("the failure reads %q, want the plan's wording", failure.Message)
	}
	if !failure.Recoverable {
		t.Error("the failure is not marked recoverable")
	}
	if failure.Text == "" {
		t.Error("the failure holds no text: the model's own words are lost")
	}
}

// TestACallWithoutANameIsAVisibleError covers the same rule for a call
// that names no tool.
func TestACallWithoutANameIsAVisibleError(t *testing.T) {
	text := `<tool_call>{"arguments": {"command": "ls"}}</tool_call>`
	events := parseEvents(t, "", text)

	var failure *poo.StreamError
	for _, ev := range events {
		if ev.Kind == "error" {
			failure = ev.Error
		}
	}
	if failure == nil {
		t.Fatal("the parser reported no failure")
	}
	if !strings.Contains(failure.Message, "no name") {
		t.Errorf("the failure reads %q, want it to name the reason", failure.Message)
	}
}
