package bash

import (
	"encoding/json"

	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/internal/envelope"
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
		{Key: EnvelopeReturncode, Value: code},
		{Key: EnvelopeTimedOut, Value: o.TimedOut},
	}

	// The two streams share the cap, so one command's observation keeps
	// its size whatever the command wrote.
	outCap, errCap := streamCaps(o.Output, o.Stderr, cap)

	elided := 0
	outChars := caps.RuneCount(o.Output)
	if outChars <= outCap {
		fields = append(fields, field{Key: EnvelopeOutput, Value: o.Output})
	} else {
		head, tail := caps.SplitRunes(o.Output, outCap/2)
		fields = append(fields,
			field{Key: EnvelopeOutputHead, Value: head},
			field{Key: EnvelopeOutputTail, Value: tail},
		)
		elided += outChars - 2*(outCap/2)
	}
	if o.Stderr != "" {
		errChars := caps.RuneCount(o.Stderr)
		if errChars <= errCap {
			fields = append(fields, field{Key: EnvelopeStderr, Value: o.Stderr})
		} else {
			head, tail := caps.SplitRunes(o.Stderr, errCap/2)
			fields = append(fields,
				field{Key: EnvelopeStderrHead, Value: head},
				field{Key: EnvelopeStderrTail, Value: tail},
			)
			elided += errChars - 2*(errCap/2)
		}
	}
	if elided > 0 {
		fields = append(fields, field{Key: EnvelopeElidedChars, Value: elided})
	}
	switch {
	case o.Truncated:
		fields = append(fields,
			field{Key: EnvelopeTruncated, Value: true},
			field{Key: EnvelopeHint, Value: TruncationHint},
		)
	case elided > 0:
		fields = append(fields, field{Key: EnvelopeHint, Value: ElisionHint})
	}

	return render(fields), mustJSON(fields)
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
type field = envelope.Field

// render formats the observation the model reads.
//
// The fields keep the order the plan names them in, and the result is
// valid JSON, so the model can read it either way.
var render = envelope.Render

// mustJSON encodes a value, falling back to an empty object.
var mustJSON = envelope.Structured
