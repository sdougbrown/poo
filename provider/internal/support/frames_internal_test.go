package support

import (
	"encoding/json"
	"testing"
)

// jsonPayloads are the shapes an endpoint's event payload takes, with the
// scalars a payload can carry: escapes, unicode, fractions, exponents and
// the literals.
var jsonPayloads = []string{
	`{}`,
	`[]`,
	`42`,
	`-1`,
	`3.14`,
	`1e10`,
	`-0.5E-3`,
	`1.0e+2`,
	`true`,
	`false`,
	`null`,
	`""`,
	`"plain"`,
	`"quote\" backslash\\ solidus\/ bell\b feed\f newline\n return\r tab\t"`,
	`"unicode \u0041 \u00e9 \ud83d\ude00 and é 😀"`,
	`{"type":"response.output_text.delta","output_index":0,"delta":"the answer"}`,
	`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"a\"b\u0041"}]}],"usage":{"input_tokens":1,"output_tokens":2}}}`,
	` [1,2.5,-3e7,{"a":[true,false,null,"x"]}] `,
}

// TestNoPrefixOfAJSONPayloadIsDropped covers the rule the no-blank-line
// framing depends on. A payload that may still be completed by another data line
// is never treated as one no line can repair. A frame spread over several lines
// is therefore never dropped. Every proper prefix of a payload, and the whole
// payload, is checked.
func TestNoPrefixOfAJSONPayloadIsDropped(t *testing.T) {
	for _, payload := range jsonPayloads {
		if got := payloadState([]string{payload}); got != payloadWhole {
			t.Errorf("the payload %s is state %d, want complete", payload, got)
		}
		for i := 1; i < len(payload); i++ {
			if got := payloadState([]string{payload[:i]}); got == payloadBroken {
				t.Errorf("the prefix %q of %s is dropped, want it kept for the next line", payload[:i], payload)
			}
		}
	}
}

// TestEveryMultilineSplitOfAJSONPayloadIsJoined covers the shape a server
// that separates its events with one newline sends when it splits a payload.
// The lines are joined wherever the split leaves valid JSON.
func TestEveryMultilineSplitOfAJSONPayloadIsJoined(t *testing.T) {
	joined := 0
	for _, payload := range jsonPayloads {
		for i := 1; i < len(payload); i++ {
			if !json.Valid([]byte(payload[:i] + "\n" + payload[i:])) {
				continue
			}
			joined++
			if got := payloadState([]string{payload[:i], payload[i:]}); got != payloadWhole {
				t.Errorf("the split payload %q + %q is state %d, want complete",
					payload[:i], payload[i:], got)
			}
		}
	}
	if joined == 0 {
		t.Fatal("no split payload was checked, want the splits that leave valid JSON")
	}
}
