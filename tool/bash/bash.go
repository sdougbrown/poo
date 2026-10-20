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
	"fmt"
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
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return nil, poo.InvalidArgsf("bash: %v", err)
	}
	if strings.TrimSpace(a.Command) == "" {
		return nil, poo.InvalidArgsf("bash: command must not be empty")
	}
	return &prepared{command: a.Command}, nil
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
	structured := map[string]any{
		"returncode": res.returncode,
		"timed_out":  res.timedOut,
	}
	if len(out) <= cap {
		structured["output"] = out
	} else {
		half := cap / 2
		structured["output_head"] = truncate(out[:half], half)
		structured["output_tail"] = truncate(out[len(out)-half:], half)
		structured["elided_chars"] = len(out) - 2*half
		structured["hint"] = ElisionHint
	}

	text := render(structured)
	return poo.ToolResult{
		Text:       text,
		Structured: mustJSON(structured),
	}, nil
}

// render formats the observation the model reads.
func render(obs map[string]any) string {
	var b strings.Builder
	b.WriteString("{")
	keys := []string{"returncode", "timed_out"}
	for _, k := range keys {
		fmt.Fprintf(&b, "%q: %v, ", k, obs[k])
	}
	if _, ok := obs["output"]; ok {
		fmt.Fprintf(&b, "%q: %q", "output", obs["output"])
	} else {
		fmt.Fprintf(&b, "%q: %q, %q: %q, %q: %d, %q: %q",
			"output_head", obs["output_head"],
			"output_tail", obs["output_tail"],
			"elided_chars", obs["elided_chars"],
			"hint", obs["hint"])
	}
	b.WriteString("}")
	return b.String()
}

// truncate keeps a string within a byte budget, without splitting a rune.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	b := s[:limit]
	for len(b) > 0 && !utf8.ValidString(b) {
		b = b[:len(b)-1]
	}
	return b
}

// mustJSON encodes a value, falling back to an empty object.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
