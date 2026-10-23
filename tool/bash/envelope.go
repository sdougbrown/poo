package bash

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Observation is one command's outcome, as a tool reports it.
//
// A tool that captures stdout and stderr together puts everything in
// Output. A tool that captures them apart fills Stderr as well.
type Observation struct {
	// Returncode is the command's exit status, nil when the process was
	// killed and has none.
	Returncode *int
	// TimedOut reports that the command hit its timeout.
	TimedOut bool
	// Output is what the command wrote to stdout.
	Output string
	// Stderr is what the command wrote to stderr. It is empty when the
	// streams were captured together.
	Stderr string
	// Cap is the cap on the observation, in characters. Zero means
	// ObservationCapMax.
	Cap int
}

// Envelope renders a command's outcome as the observation the model
// reads, and as the structured payload beside it.
//
// The output that fits under the cap is one field:
//
//	{"returncode": int|null, "timed_out": false, "output": "..."}
//
// The output that does not fit is elided, and the middle is dropped:
//
//	{"returncode": int|null, "timed_out": false, "output_head": "...",
//	 "output_tail": "...", "elided_chars": 1234, "hint": "..."}
//
// A command whose stderr is captured apart from its stdout carries it in
// a field of its own, and the two streams share the cap: each one is
// whole when both fit, and each one is elided to its head and its tail
// when they do not.
//
// The cap and elided_chars are counted in characters, not bytes, and a
// cut never splits a rune.
func Envelope(o Observation) (string, json.RawMessage) {
	cap := o.Cap
	if cap <= 0 {
		cap = ObservationCapMax
	}
	// A killed process has no exit code, so the field is null.
	var code any
	if o.Returncode != nil {
		code = *o.Returncode
	}
	fields := []field{
		{EnvelopeReturncode, code},
		{EnvelopeTimedOut, o.TimedOut},
	}

	// The two streams share the cap, so one command's observation keeps
	// its size whatever the command wrote.
	outCap, errCap := cap, cap
	if o.Output != "" && o.Stderr != "" {
		outCap, errCap = cap/2, cap-cap/2
	}

	elided := 0
	outChars := utf8.RuneCountInString(o.Output)
	if outChars <= outCap {
		fields = append(fields, field{EnvelopeOutput, o.Output})
	} else {
		head, tail := splitRunes(o.Output, outCap/2)
		fields = append(fields,
			field{EnvelopeOutputHead, head},
			field{EnvelopeOutputTail, tail},
		)
		elided += outChars - 2*(outCap/2)
	}
	if o.Stderr != "" {
		errChars := utf8.RuneCountInString(o.Stderr)
		if errChars <= errCap {
			fields = append(fields, field{EnvelopeStderr, o.Stderr})
		} else {
			head, tail := splitRunes(o.Stderr, errCap/2)
			fields = append(fields,
				field{EnvelopeStderrHead, head},
				field{EnvelopeStderrTail, tail},
			)
			elided += errChars - 2*(errCap/2)
		}
	}
	if elided > 0 {
		fields = append(fields,
			field{EnvelopeElidedChars, elided},
			field{EnvelopeHint, ElisionHint},
		)
	}

	structured := map[string]any{}
	for _, f := range fields {
		structured[f.key] = f.value
	}
	return render(fields), mustJSON(structured)
}

// field is one entry of the observation envelope.
type field struct {
	key   string
	value any
}

// render formats the observation the model reads.
//
// The fields keep the order the plan names them in, and the result is
// valid JSON, so the model can read it either way.
func render(fields []field) string {
	var b strings.Builder
	b.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		// The field vocabulary is a string and the values are string,
		// int, bool or nil, so neither marshal can fail.
		key, _ := json.Marshal(f.key)
		value, _ := json.Marshal(f.value)
		b.Write(key)
		b.WriteString(": ")
		b.Write(value)
	}
	b.WriteString("}")
	return b.String()
}

// splitRunes cuts a string into its head and its tail, each within a
// character budget, without splitting a rune.
//
// The text is walked rather than copied: the head is found by ranging
// over the string, and the tail by walking back over its last
// characters, so a large output costs no rune slice of its own.
func splitRunes(s string, half int) (string, string) {
	if half <= 0 {
		return "", ""
	}
	if utf8.RuneCountInString(s) <= half {
		return s, ""
	}
	return headRunes(s, half), tailRunes(s, half)
}

// headRunes returns the first n characters of a string.
func headRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// tailRunes returns the last n characters of a string.
func tailRunes(s string, n int) string {
	// The end of the string is found by walking back over its
	// characters, so a rune is never cut in half.
	// A byte that is not part of a valid rune counts as one character,
	// as it does for utf8.RuneCountInString and for the head.
	end := len(s)
	count := 0
	for end > 0 {
		_, size := utf8.DecodeLastRuneInString(s[:end])
		end -= size
		count++
		if count == n {
			break
		}
	}
	return s[end:]
}

// mustJSON encodes a value, falling back to an empty object.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
