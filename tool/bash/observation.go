package bash

import (
	"encoding/json"
	"strings"

	"github.com/sdougbrown/poo/internal/caps"
)

// Observation is one command's outcome, as a tool reports it.
//
// A tool that captures stdout and stderr together puts everything in
// Output. A tool that captures them apart fills Stderr as well.
type Observation struct {
	// ExitCode is the command's exit status, nil when the process was
	// killed and has none.
	ExitCode *int
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
	// Truncated reports that the capture dropped the end of the output,
	// so Output is not all the command wrote.
	Truncated bool
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
// Output past the capture buffer is dropped before the cap applies, and
// the observation says so with "truncated": true and its own hint.
//
// A command whose stderr is captured apart from its stdout carries it in
// a field of its own, and the two streams share the cap: both stay whole
// when they fit together, and when they do not, the shorter one keeps
// what it needs and the longer one is elided to its head and tail.
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
	if o.ExitCode != nil {
		code = *o.ExitCode
	}
	fields := []field{
		{EnvelopeReturncode, code},
		{EnvelopeTimedOut, o.TimedOut},
	}

	// The two streams share the cap, so one command's observation keeps
	// its size whatever the command wrote.
	outCap, errCap := streamCaps(o.Output, o.Stderr, cap)

	elided := 0
	outChars := caps.RuneCount(o.Output)
	if outChars <= outCap {
		fields = append(fields, field{EnvelopeOutput, o.Output})
	} else {
		head, tail := caps.SplitRunes(o.Output, outCap/2)
		fields = append(fields,
			field{EnvelopeOutputHead, head},
			field{EnvelopeOutputTail, tail},
		)
		elided += outChars - 2*(outCap/2)
	}
	if o.Stderr != "" {
		errChars := caps.RuneCount(o.Stderr)
		if errChars <= errCap {
			fields = append(fields, field{EnvelopeStderr, o.Stderr})
		} else {
			head, tail := caps.SplitRunes(o.Stderr, errCap/2)
			fields = append(fields,
				field{EnvelopeStderrHead, head},
				field{EnvelopeStderrTail, tail},
			)
			elided += errChars - 2*(errCap/2)
		}
	}
	if elided > 0 {
		fields = append(fields, field{EnvelopeElidedChars, elided})
	}
	switch {
	case o.Truncated:
		fields = append(fields,
			field{EnvelopeTruncated, true},
			field{EnvelopeHint, TruncationHint},
		)
	case elided > 0:
		fields = append(fields, field{EnvelopeHint, ElisionHint})
	}

	structured := map[string]any{}
	for _, f := range fields {
		structured[f.key] = f.value
	}
	return render(fields), mustJSON(structured)
}

// streamCaps divides the cap between the two streams.
//
// A single stream may use the whole cap. Two streams stay whole when
// they fit together; when they do not, the short one keeps what it
// needs, up to half the cap, and the long one takes the rest.
func streamCaps(output, stderr string, cap int) (int, int) {
	outChars := caps.RuneCount(output)
	errChars := caps.RuneCount(stderr)
	if output == "" || stderr == "" || outChars+errChars <= cap {
		return cap, cap
	}
	shortCap := outChars
	if errChars < outChars {
		shortCap = errChars
	}
	if half := cap / 2; shortCap > half {
		shortCap = half
	}
	if outChars <= errChars {
		return shortCap, cap - shortCap
	}
	return cap - shortCap, shortCap
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

// mustJSON encodes a value, falling back to an empty object.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
