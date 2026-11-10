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
	"io"
	"strings"
	"time"

	"github.com/sdougbrown/poo/exec"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/internal/caps"
)

// Defaults for the bash tool.
const (
	// DefaultTimeout is the per-call timeout when none is configured.
	DefaultTimeout = 120 * time.Second
	// MaxCallTimeout is the ceiling for a caller-supplied timeout.
	MaxCallTimeout = 10 * time.Minute
	// CaptureCap is the size of the buffer the output is captured into,
	// in bytes. Beyond it, output is read and discarded.
	CaptureCap = 1 << 20
	// KillGrace is how long the process group gets between SIGTERM and
	// SIGKILL.
	KillGrace = 2 * time.Second
	// ObservationCapMax is the largest observation the model ever sees.
	ObservationCapMax = caps.ObservationMax
	// CharsPerToken is the plan's estimate of characters per token.
	CharsPerToken = poo.CharsPerToken
	// ElisionHint tells the model how to see the elided middle.
	ElisionHint = "Output too long. Use grep/sed/head/tail to inspect narrower ranges."
)

// A nested call keeps all the capture holds only while the capture, in
// bytes, is no larger than the nested cap, in characters. The
// conversion does not compile when it is larger.
const _ = uint(poo.NestedOutputCapChars - CaptureCap)

// TruncationHint tells the model how to see output past the capture
// buffer.
var TruncationHint = fmt.Sprintf("Output passed the capture limit of %d bytes and the rest was dropped. Redirect it to a file and inspect narrower ranges.", CaptureCap)

// ObservationCap returns the cap on the observation shown to the model.
//
// The cap is context-relative: min(30000, floor(0.06 × context_limit ×
// 3.5)) characters.
func ObservationCap(contextLimit int) int {
	return caps.Observation(contextLimit, CharsPerToken)
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
	// EnvelopeStderr is the field that holds the error output that fits.
	EnvelopeStderr = "stderr"
	// EnvelopeStderrHead is the field that holds the head of an elided
	// error output.
	EnvelopeStderrHead = "stderr_head"
	// EnvelopeStderrTail is the field that holds the tail of an elided
	// error output.
	EnvelopeStderrTail = "stderr_tail"
	// EnvelopeElidedChars is the field that holds the number of
	// characters dropped from the middle.
	EnvelopeElidedChars = "elided_chars"
	// EnvelopeReturncode is the field that holds the command's exit
	// status.
	EnvelopeReturncode = "returncode"
	// EnvelopeTimedOut is the field that holds the timeout flag.
	EnvelopeTimedOut = "timed_out"
	// EnvelopeHint is the field that holds the hint about the elided
	// middle, or about the dropped end.
	EnvelopeHint = "hint"
	// EnvelopeTruncated is the field that says the capture buffer
	// dropped the end of the output. It is there only when it is true.
	EnvelopeTruncated = "truncated"
)

// Tool is the bash tool.
type Tool struct {
	// Timeout is the per-call timeout. Zero means DefaultTimeout.
	Timeout time.Duration
	// ObservationCap is the cap on the observation. Zero means the cap is
	// taken from the run's settings. A nonzero cap applies to a nested
	// call as well.
	ObservationCap int
	// Executor runs the command. Nil means the local executor.
	Executor exec.Executor
	// Scrub describes the environment the child gets.
	Scrub Scrub
}

// Option configures a bash tool.
type Option func(*Tool)

// WithExecutor gives the tool the executor its commands run through.
//
// The local executor runs the command in poo's own process space. A
// sandboxed executor runs it somewhere else, where the credentials the
// host tools use are not.
func WithExecutor(e exec.Executor) Option {
	return func(t *Tool) { t.Executor = e }
}

// WithScrub gives the tool the rules for the child environment.
func WithScrub(scrub Scrub) Option {
	return func(t *Tool) { t.Scrub = scrub }
}

// New returns a bash tool.
func New(opts ...Option) *Tool {
	t := &Tool{}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "bash",
		Description: "Run a shell command in the working directory and return its output.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The command to run with /bin/bash -c."},
    "timeout": {"type": "number", "description": "Optional timeout in milliseconds for a foreground call. At most 600000 (10 minutes), and never past the configured bash timeout when one is set. A background call runs under the job timeout."},
    "description": {"type": "string", "description": "Optional summary of what the command does. The event stream and the transcript show it; a permission ask still shows the command."}
  },
  "required": ["command"]
,"additionalProperties":false}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "command",
		// A command may be long, and the model may know it is long
		// before it starts it.
		Background: poo.BackgroundAllowed,
	}
}

// args are the tool's arguments.
type args struct {
	Command string `json:"command"`
	// Timeout is the requested timeout in milliseconds. It is a float so
	// that any JSON number in float64 range decodes, in any form.
	Timeout *float64 `json:"timeout"`
	// Description is a summary the model may volunteer, as Claude Code's
	// bash tool does. The summary the event stream and the transcript
	// show is the description when it says something, and the command
	// otherwise. It never reaches the canonical key, the evidence or a
	// permission ask: the command alone is what runs and what a rule
	// matches.
	Description string `json:"description"`
}

// prepared is a validated bash call.
type prepared struct {
	command string
	// description is the model's summary of the call, when it supplied
	// one.
	description string
	// timeout is the bound the call asked for, already clamped to
	// MaxCallTimeout. Zero means the call asked for none.
	timeout time.Duration
}

// Summary describes the call for a human reader.
//
// The model's description is the better line when it supplied one; an
// absent, empty or blank description falls back to the command.
func (p *prepared) Summary() string {
	if strings.TrimSpace(p.description) != "" {
		return p.description
	}
	return p.command
}

// AskSummary describes the call for a permission ask.
//
// The description is model-authored, so a permission decision never
// reads it: the ask shows the command the model asked to run.
func (p *prepared) AskSummary() string { return p.command }

// CanonicalKey returns the call's prepared input.
func (p *prepared) CanonicalKey() json.RawMessage {
	key, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{p.command})
	return key
}

// EvidenceArguments returns the arguments a decision model may see.
func (p *prepared) EvidenceArguments() json.RawMessage {
	return p.CanonicalKey()
}

// Prepare validates the arguments.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("bash: %v", err)
	}
	if strings.TrimSpace(a.Command) == "" {
		return nil, poo.InvalidArgsf("bash: command must not be empty")
	}
	return &prepared{command: a.Command, description: a.Description, timeout: requestedTimeout(a.Timeout)}, nil
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

// requestedTimeout turns a call's `timeout`, in milliseconds, into the
// bound it asks for. A value past MaxCallTimeout is clamped rather than
// refused, so a call never fails over its size. Zero, below zero, or no
// value means the call asked for none.
func requestedTimeout(ms *float64) time.Duration {
	if ms == nil || *ms <= 0 {
		return 0
	}
	if *ms >= float64(MaxCallTimeout.Milliseconds()) {
		return MaxCallTimeout
	}
	return time.Duration(*ms * float64(time.Millisecond))
}

// resolveTimeout chooses the bound a call runs under.
//
// The configured timeout, from the settings or the tool, is the operator's
// bound: it is the default, and a call's own timeout may shorten it but
// never pass it. With nothing configured, a call may ask for up to
// MaxCallTimeout, and DefaultTimeout applies when it asks for none.
func resolveTimeout(requested time.Duration, settings int, tool time.Duration) time.Duration {
	configured := time.Duration(0)
	switch {
	case settings > 0:
		configured = time.Duration(settings) * time.Second
	case tool > 0:
		configured = tool
	}
	if configured > 0 {
		if requested > 0 && requested < configured {
			return requested
		}
		return configured
	}
	if requested > 0 {
		return requested
	}
	return DefaultTimeout
}

// Call runs the command.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, errors.New("bash: prepared value is not a bash call")
	}

	timeout := resolveTimeout(p.timeout, env.Settings.BashTimeoutS, t.Timeout)

	cap := t.ObservationCap
	if cap <= 0 {
		cap = env.Settings.ObservationCapChars
	}
	if cap <= 0 {
		cap = ObservationCapMax
	}

	res, err := t.run(ctx, p.command, env, timeout)
	if err != nil {
		return poo.ToolResult{}, err
	}

	text, structured := Envelope(Observation{
		ExitCode:  res.exitCode,
		TimedOut:  res.timedOut,
		Output:    res.output,
		Truncated: res.truncated,
		Cap:       cap,
	})

	return poo.ToolResult{
		Text:       text,
		Structured: structured,
		// A timed-out call is a recoverable error: the model has to see
		// that it failed, or it will read the partial output as a
		// finished command. A process that died by a signal has no return
		// code and failed the same way.
		IsError: res.timedOut || res.cancelled || res.exitCode == nil,
	}, nil
}
