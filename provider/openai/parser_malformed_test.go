package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestAnUnclosedMalformedCallKeepsItsText covers a block that never
// closed and whose body is not a call: the text the model wrote stays in
// the model's prose, and no closing marker is invented.
func TestAnUnclosedMalformedCallKeepsItsText(t *testing.T) {
	for _, text := range []string{
		`<tool_call>not a call at all`,
		`<tool_call>{"name": "bash", "arguments": omg`,
		"```json\nnot a call\n",
	} {
		calls, prose := parse(t, "text", text)
		if len(calls) != 0 {
			t.Errorf("the parser found %d calls in %q, want none", len(calls), text)
		}
		// The model's own words are all still there.
		body := strings.TrimPrefix(text, "<tool_call>")
		body = strings.TrimSuffix(body, "```")
		for _, word := range strings.Fields(strings.Trim(body, "\n")) {
			if !strings.Contains(prose, word) {
				t.Errorf("the prose %q lost %q from %q", prose, word, text)
			}
		}
		// The parser did not invent a closing marker.
		if strings.Count(prose, "</tool_call>") > 1 {
			t.Errorf("the prose %q invented a closing marker", prose)
		}
	}
}

// TestAnUnclosedMalformedCallKeepsTheWholeTail covers the tail the parser
// held back while it looked for a closing marker.
func TestAnUnclosedMalformedCallKeepsTheWholeTail(t *testing.T) {
	text := `<tool_call>{"name": "bash", "argu`
	calls, prose := parse(t, "text", text)
	if len(calls) != 0 {
		t.Fatalf("the parser found %d calls, want none", len(calls))
	}
	if !strings.Contains(prose, `"argu`) {
		t.Errorf("the prose %q lost the tail of the block", prose)
	}
}

// TestArgumentsMustBeAnObject covers a call whose arguments are not a
// JSON object: the block is not a call, because the tools take an object.
func TestArgumentsMustBeAnObject(t *testing.T) {
	for _, text := range []string{
		`<tool_call>{"name":"bash","arguments":[]}</tool_call>`,
		`<tool_call>{"name":"bash","arguments":"x"}</tool_call>`,
		`<tool_call>{"name":"bash","arguments":null}</tool_call>`,
	} {
		calls, prose := parse(t, "text", text)
		if len(calls) != 0 {
			t.Errorf("the parser found %d calls in %q, want none", len(calls), text)
		}
		if prose == "" {
			t.Errorf("the block %q was dropped instead of shown as prose", text)
		}
	}
}

// TestAMalformedArgumentsBlockStaysProse covers an arguments field that
// is not valid JSON at all.
func TestAMalformedArgumentsBlockStaysProse(t *testing.T) {
	text := `<tool_call>{"name": "bash", "arguments": {"command": }}</tool_call>`

	calls, prose := parse(t, "text", text)
	if len(calls) != 0 {
		t.Fatalf("the parser found %d calls, want none", len(calls))
	}
	if !strings.Contains(prose, "bash") {
		t.Errorf("the prose %q lost the block", prose)
	}
}

// TestAnObjectArgumentIsAccepted covers the shape the tools take.
func TestAnObjectArgumentIsAccepted(t *testing.T) {
	text := `<tool_call>{"name": "bash", "arguments": {"command": "ls"}}</tool_call>`

	calls, _ := parse(t, "text", text)
	if len(calls) != 1 {
		t.Fatalf("the parser found %d calls, want 1", len(calls))
	}
	var args map[string]any
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("the arguments are not a JSON object: %s", calls[0].Arguments)
	}
	if args["command"] != "ls" {
		t.Errorf("the arguments are %v, want the command", args)
	}
}

// TestTheXMLCallArgumentsMustBeAnObject covers the same rule for the
// XML shape.
func TestTheXMLCallArgumentsMustBeAnObject(t *testing.T) {
	text := `<tool_call>
<name>read</name>
<arguments>[1, 2, 3]</arguments>
</tool_call>`

	calls, prose := parse(t, "text", text)
	if len(calls) != 0 {
		t.Fatalf("the parser found %d calls, want none", len(calls))
	}
	if prose == "" {
		t.Error("the block was dropped instead of shown as prose")
	}
}

// TestTheXMLCallWithoutArgumentsIsStillACall covers a call that names
// its tool and gives it nothing: the arguments are an empty object.
func TestTheXMLCallWithoutArgumentsIsStillACall(t *testing.T) {
	text := `<tool_call>
<name>read</name>
</tool_call>`

	calls, _ := parse(t, "text", text)
	if len(calls) != 1 {
		t.Fatalf("the parser found %d calls, want 1", len(calls))
	}
	if string(calls[0].Arguments) != "{}" {
		t.Errorf("the arguments are %s, want an empty object", calls[0].Arguments)
	}
}

var _ = poo.ToolCallDelta{}
