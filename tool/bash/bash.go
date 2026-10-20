// Package bash implements the bash tool: one command, one fresh shell.
//
// The tool is a thin, careful wrapper around /bin/bash. It runs the command
// in its own process group, captures the output into a capped buffer, kills
// whatever is left in the group when the call ends, and shows the model an
// observation that fits the context window.
package bash

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
)

// Defaults for the bash tool.
const (
	// DefaultTimeout is the per-call timeout when none is configured.
	DefaultTimeout = 120 * time.Second
	// CaptureCap is the size of the buffer the output is captured into.
	// Beyond it, output is read and discarded.
	CaptureCap = 1 << 20
	// KillGrace is how long the process group gets between SIGTERM and
	// SIGKILL.
	KillGrace = 2 * time.Second
	// ObservationCapMax is the largest observation the model ever sees.
	ObservationCapMax = 30000
	// CharsPerToken is the plan's estimate of characters per token.
	CharsPerToken = 3.5
	// ElisionHint tells the model how to see the elided middle.
	ElisionHint = "Output too long. Use grep/sed/head/tail to inspect narrower ranges."
)

// ObservationCap returns the cap on the observation shown to the model.
//
// The cap is context-relative: min(30000, floor(0.06 × context_limit ×
// 3.5)) characters.
func ObservationCap(contextLimit int) int {
	cap := int(0.06 * float64(contextLimit) * CharsPerToken)
	if cap > ObservationCapMax {
		return ObservationCapMax
	}
	if cap < 0 {
		return 0
	}
	return cap
}

// The observation the model reads is an envelope.
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
// The cap and elided_chars are counted in characters, not bytes, and a
// cut never splits a rune. returncode is null when the process was
// killed, which is the case for a timeout. A timed-out call is a
// recoverable error, so the model is told the command failed.
const (
	// EnvelopeOutput is the field that holds the output that fits.
	EnvelopeOutput = "output"
	// EnvelopeOutputHead is the field that holds the head of an elided
	// output.
	EnvelopeOutputHead = "output_head"
	// EnvelopeOutputTail is the field that holds the tail of an elided
	// output.
	EnvelopeOutputTail = "output_tail"
	// EnvelopeElidedChars is the field that holds the number of
	// characters dropped from the middle.
	EnvelopeElidedChars = "elided_chars"
	// EnvelopeReturncode is the field that holds the command's exit
	// status.
	EnvelopeReturncode = "returncode"
	// EnvelopeTimedOut is the field that holds the timeout flag.
	EnvelopeTimedOut = "timed_out"
	// EnvelopeHint is the field that holds the hint about the elided
	// middle.
	EnvelopeHint = "hint"
)

// Tool is the bash tool.
type Tool struct {
	// Timeout is the per-call timeout. Zero means DefaultTimeout.
	Timeout time.Duration
	// ObservationCap is the cap on the observation. Zero means the cap is
	// taken from the run's settings.
	ObservationCap int
}

// New returns a bash tool.
func New() *Tool { return &Tool{} }

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "bash",
		Description: "Run a shell command in the working directory and return its output.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The command to run with /bin/bash -c."}
  },
  "required": ["command"]
}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "command",
	}
}

// args are the tool's arguments.
type args struct {
	Command string `json:"command"`
}

// prepared is a validated bash call.
type prepared struct {
	command string
}

// Summary describes the call for a human reader.
func (p *prepared) Summary() string { return p.command }

// Prepare validates the arguments.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("bash: %v", err)
	}
	if strings.TrimSpace(a.Command) == "" {
		return nil, poo.InvalidArgsf("bash: command must not be empty")
	}
	return &prepared{command: a.Command}, nil
}

// decodeStrict decodes one JSON value and nothing else.
//
// A second value, or anything after the first, is a malformed call: the
// model sent more than it meant to, and taking the first object would
// silently run the wrong command.
func decodeStrict(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("the arguments hold more than one JSON value")
		}
		return err
	}
	return nil
}

// Call runs the command.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, errors.New("bash: prepared value is not a bash call")
	}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if s := env.Settings.BashTimeoutSeconds; s > 0 {
		timeout = time.Duration(s) * time.Second
	}

	cap := t.ObservationCap
	if cap <= 0 {
		cap = env.Settings.ObservationCapChars
	}
	if cap <= 0 {
		cap = ObservationCapMax
	}

	res, err := run(ctx, p.command, env.WorkingDir, env.Env, timeout)
	if err != nil {
		return poo.ToolResult{}, err
	}

	out := res.output
	// The envelope's fields, in the order the plan names them.
	var fields []field
	// A killed process has no exit code, so the field is null.
	var code any
	if res.returncode != nil {
		code = *res.returncode
	}
	fields = append(fields, field{EnvelopeReturncode, code}, field{EnvelopeTimedOut, res.timedOut})
	// The cap is in characters, and a cut never splits a rune.
	outChars := utf8.RuneCountInString(out)
	if outChars <= cap {
		fields = append(fields, field{EnvelopeOutput, out})
	} else {
		half := cap / 2
		head, tail := splitRunes(out, half)
		fields = append(fields,
			field{EnvelopeOutputHead, head},
			field{EnvelopeOutputTail, tail},
			field{EnvelopeElidedChars, outChars - 2*half},
			field{EnvelopeHint, ElisionHint},
		)
	}

	text := render(fields)
	structured := map[string]any{}
	for _, f := range fields {
		structured[f.key] = f.value
	}

	return poo.ToolResult{
		Text:       text,
		Structured: mustJSON(structured),
		// A timed-out call is a recoverable error: the model has to see
		// that it failed, or it will read the partial output as a
		// finished command.
		IsError: res.timedOut || res.cancelled,
	}, nil
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
		key, err := json.Marshal(f.key)
		if err != nil {
			continue
		}
		value, err := json.Marshal(f.value)
		if err != nil {
			continue
		}
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
	end := len(s)
	count := 0
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:end])
		if r == utf8.RuneError && size == 1 {
			// A byte that is not a rune of its own. It is dropped
			// rather than kept, because it is not a character.
			end--
			continue
		}
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
