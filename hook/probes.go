package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/exec"
)

// defaultProbeTimeoutMs is how long one probe may run when its spec
// names no timeout.
const defaultProbeTimeoutMs = 3000

// maxCallArgumentsBytes is the most of a call's arguments the evidence
// keeps.
//
// The arguments are one field of the evidence, and a call whose
// arguments run longer than a probe's output is not a call a question
// can be asked about: the evidence is a summary, not a transcript.
const maxCallArgumentsBytes = MaxProbeOutputBytes

// ProbeEnv is what a hook's probes run with.
//
// The runner is given it by the binary that linked the executor: a hook
// policy is a file, so the command a probe runs is the operator's own
// and obeys the executor's sandbox.
type ProbeEnv struct {
	// Executor runs a command probe. It is nil when the binary links no
	// executor a probe may use, and a spec that names a command probe is
	// then refused before any run.
	Executor exec.Executor
	// Env is a command probe's environment, already scrubbed.
	Env []string
}

// PointData is what a hook point knows about the activity its hook
// judges.
//
// A runner fills the fields its point has and leaves the rest zero, so
// the evidence names the call a tool hook judges and the trigger a run
// hook judges.
type PointData struct {
	// Trigger is the kind of run the hook runs for: one of the Trigger
	// values. before_run only.
	Trigger string
	// Call is the call the hook judges. The tool points fill it.
	Call *CallData
	// Result is the tool result the hook judges. after_tool fills it.
	Result *ResultData
}

// CallData is the tool call a hook judges.
type CallData struct {
	// Tool is the tool's name.
	Tool string
	// Summary is the call in words, as the transcript shows it.
	Summary string
	// Class is the tool's class.
	Class string
	// Arguments are the call's raw arguments.
	Arguments json.RawMessage
}

// ResultData is the tool result a hook judges.
type ResultData struct {
	// Text is the result the model saw.
	Text string `json:"text,omitempty"`
	// IsError reports that the tool failed.
	IsError bool `json:"is_error,omitempty"`
}

// ProbeResult is what one probe saw.
//
// A command probe reports its exit status and the head of its output. A
// nonzero status is evidence, not a failure: a check like gofmt -l says
// what it found with its status, and a hook that read the status as an
// error could never ask about the finding. A tool probe reports the
// tool's answer instead.
type ProbeResult struct {
	// ExitCode is a command probe's exit status. It is absent for a tool
	// probe.
	ExitCode *int `json:"exit_code,omitempty"`
	// Output is the head of what the probe wrote or returned.
	Output string `json:"output"`
	// Truncated reports that Output is the head of a longer answer.
	Truncated bool `json:"truncated,omitempty"`
}

// ProbeError is a probe that produced no evidence.
//
// The error names the probe and how it failed, never what it wrote: a
// probe's output is the evidence a question is asked about, and it is
// not the error the run reports. The reason is one of a few fixed
// phrases for the same end.
type ProbeError struct {
	// Name is the probe's name.
	Name string
	// Reason says how the probe failed.
	Reason string
}

// Error renders the failure.
func (e *ProbeError) Error() string {
	return fmt.Sprintf("hook probe %q %s", e.Name, e.Reason)
}

// statePayload is the state a hook's questions are asked about.
//
// The evidence frame is embedded, so the payload is decide's evidence
// plus the hook's own fields, and a decision model reads it the way it
// reads a permission ask's evidence. The probes are the facts the hook
// asked for, kept apart from the conversation so a bounded window never
// hides them.
type statePayload struct {
	decide.EvidencePayload
	// Point is where the hook runs.
	Point Point `json:"point"`
	// Trigger is the kind of run the hook runs for.
	Trigger string `json:"trigger,omitempty"`
	// Turn is the turn the hook runs in.
	Turn int `json:"turn,omitempty"`
	// Budget is the room the run has left.
	Budget budgetPayload `json:"budget"`
	// Result is the tool result the hook judges.
	Result *ResultData `json:"result,omitempty"`
	// Probes is what each probe saw, by name.
	Probes map[string]ProbeResult `json:"probes,omitempty"`
}

// budgetPayload is the room a run has left, as the evidence shows it.
type budgetPayload struct {
	// ContextLimit is the model's context window, in tokens.
	ContextLimit int `json:"context_limit,omitempty"`
	// InputLimit is the largest prompt the endpoint accepts, in tokens.
	InputLimit int `json:"input_limit,omitempty"`
	// EstimatedInput is the prompt the next request carries, in tokens.
	EstimatedInput int `json:"estimated_input,omitempty"`
	// OutputRoom is the max_tokens the next request has left.
	OutputRoom int `json:"output_room,omitempty"`
}

// gatherState assembles the state a hook's questions are asked about.
//
// The state is evidence to assess, and it is bounded: the conversation
// window the spec asks for, the call or result the point judges, the
// run's own numbers, and what each probe saw. The whole payload is held
// to MaxEvidenceBytes. A probe that produces no evidence is an error,
// and the runner answers it with the hook's on_error policy.
func gatherState(ctx context.Context, compiled *Compiled, hctx poo.HookContext, point PointData, probeEnv ProbeEnv) (any, error) {
	framed := decide.Evidence(evidenceTurns(compiled, hctx), evidenceOptions(compiled, hctx, point))
	evidence, ok := framed.(decide.EvidencePayload)
	if !ok {
		return nil, fmt.Errorf("hook: the evidence is a %T, not a decide.EvidencePayload", framed)
	}
	probes, err := runProbes(ctx, compiled, hctx, probeEnv)
	if err != nil {
		return nil, err
	}
	payload := statePayload{
		EvidencePayload: evidence,
		Point:           compiled.Point,
		Trigger:         point.Trigger,
		Turn:            hctx.Turn,
		Budget: budgetPayload{
			ContextLimit:   hctx.Budget.ContextLimit,
			InputLimit:     hctx.Budget.InputLimit,
			EstimatedInput: hctx.Budget.EstimatedInput,
			OutputRoom:     hctx.Budget.OutputRoom(),
		},
		Result: resultEvidence(point.Result),
		Probes: probes,
	}
	if err := fitEvidence(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// evidenceTurns is the conversation window the spec asks for.
func evidenceTurns(compiled *Compiled, hctx poo.HookContext) []decide.Utterance {
	if compiled.Context.Turns <= 0 {
		return nil
	}
	return poo.EvidenceTurns(hctx.HistoryView, compiled.Context.Turns)
}

// evidenceOptions fills the parts of decide's evidence the spec asks
// for. A part the spec leaves out is not gathered at all, so a hook that
// judges a command does not carry the directory it runs in.
func evidenceOptions(compiled *Compiled, hctx poo.HookContext, point PointData) decide.EvidenceOptions {
	var opts decide.EvidenceOptions
	if compiled.Context.IncludeWorkingDir {
		opts.WorkingDir = hctx.WorkingDir
	}
	if compiled.Context.IncludeToolCall {
		opts.Call = callEvidence(point.Call)
	}
	return opts
}

// callEvidence builds the evidence of the call a hook judges.
func callEvidence(call *CallData) *decide.Call {
	if call == nil {
		return nil
	}
	return &decide.Call{
		Tool:      call.Tool,
		Summary:   clip(call.Summary, poo.EvidenceTextCap),
		Class:     call.Class,
		Arguments: callArguments(call.Arguments),
	}
}

// callArguments keeps a call's arguments when they fit the evidence and
// are a JSON object. The frame promises an object, so anything else is
// dropped rather than sent as a value the decision model cannot read.
func callArguments(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || len(trimmed) > maxCallArgumentsBytes || !json.Valid(trimmed) {
		return nil
	}
	if trimmed[0] != '{' {
		return nil
	}
	return slices.Clone(trimmed)
}

// resultEvidence builds the evidence of the tool result a hook judges.
func resultEvidence(result *ResultData) *ResultData {
	if result == nil {
		return nil
	}
	return &ResultData{
		Text:    clip(result.Text, poo.EvidenceOutputCap),
		IsError: result.IsError,
	}
}

// runProbes runs the spec's probes in the order it wrote them and
// returns what each saw.
//
// The probes are sequential: a hook runs inside a run's path, so four
// probes at once would put four processes in the path of one tool call
// for no answer the hook waits on any longer.
func runProbes(ctx context.Context, compiled *Compiled, hctx poo.HookContext, probeEnv ProbeEnv) (map[string]ProbeResult, error) {
	probes := compiled.Context.Probes
	if len(probes) == 0 {
		return nil, nil
	}
	results := make(map[string]ProbeResult, len(probes))
	for _, probe := range probes {
		var (
			result ProbeResult
			err    error
		)
		if probe.Tool != nil {
			result, err = runToolProbe(ctx, hctx.ReadOnlyTools, probe)
		} else {
			result, err = runCommandProbe(ctx, probeEnv.Executor, probeEnv.Env, hctx.WorkingDir, probe)
		}
		if err != nil {
			return nil, err
		}
		results[probe.Name] = result
	}
	return results, nil
}

// runCommandProbe runs one command probe and returns what it saw.
//
// The command writes both of its streams to one sink, so the output
// arrives in the order the command wrote it and one drain reads it: two
// streams into two writers would interleave a probe's evidence by
// scheduling. The probe's output is drained to the cap whatever the
// command writes, so a noisy command never blocks on a sink that stopped
// taking bytes, and the process is reaped whatever the outcome: a probe
// that timed out leaves no child behind. A nonzero exit status is
// evidence.
func runCommandProbe(ctx context.Context, executor exec.Executor, env []string, workingDir string, probe Probe) (ProbeResult, error) {
	if executor == nil {
		return ProbeResult{}, &ProbeError{Name: probe.Name, Reason: "has no executor to run it"}
	}
	probeCtx, cancel := probeContext(ctx, probe.TimeoutMs)
	defer cancel()
	out := &limitedBuffer{}
	proc, err := executor.Start(probeCtx, exec.Command{
		Argv: probe.Command,
		Dir:  workingDir,
		Env:  env,
	}, out)
	if err != nil {
		return ProbeResult{}, &ProbeError{Name: probe.Name, Reason: "did not start"}
	}
	code, _ := proc.Wait()
	if err := probeCtx.Err(); err != nil {
		return ProbeResult{}, &ProbeError{Name: probe.Name, Reason: stopReason(err)}
	}
	return ProbeResult{
		ExitCode:  &code,
		Output:    out.String(),
		Truncated: out.overflowed(),
	}, nil
}

// runToolProbe runs one read-only tool probe and returns what it saw.
//
// The call is the session's own read: it starts no tracked call and
// leaves nothing behind, which is why a probe may run inside a hook
// without the run owing it a receipt.
func runToolProbe(ctx context.Context, caller poo.ReadOnlyToolCaller, probe Probe) (ProbeResult, error) {
	if caller == nil {
		return ProbeResult{}, &ProbeError{Name: probe.Name, Reason: "has no read-only tool to call"}
	}
	probeCtx, cancel := probeContext(ctx, probe.TimeoutMs)
	defer cancel()
	observed, err := caller.CallReadOnly(probeCtx, probe.Tool.Name, probe.Tool.Args)
	if err != nil {
		if probeCtx.Err() != nil {
			return ProbeResult{}, &ProbeError{Name: probe.Name, Reason: stopReason(probeCtx.Err())}
		}
		return ProbeResult{}, &ProbeError{Name: probe.Name, Reason: fmt.Sprintf("could not call the tool %q", probe.Tool.Name)}
	}
	if observed.IsError {
		return ProbeResult{}, &ProbeError{Name: probe.Name, Reason: fmt.Sprintf("ran the tool %q, which reported an error", probe.Tool.Name)}
	}
	return ProbeResult{
		Output:    clip(observed.Text, MaxProbeOutputBytes),
		Truncated: observed.Truncated || len(observed.Text) > MaxProbeOutputBytes,
	}, nil
}

// probeContext bounds one probe by its own timeout, under the hook's
// firing context: a probe never outlives the hook that asked for it, and
// a hook that has run out of time stops the probe with it.
func probeContext(ctx context.Context, timeoutMs int) (context.Context, context.CancelFunc) {
	if timeoutMs <= 0 {
		timeoutMs = defaultProbeTimeoutMs
	}
	return context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
}

// stopReason says how a probe ended with its context done.
func stopReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "was stopped"
}

// fitEvidence shrinks the evidence until it fits MaxEvidenceBytes.
//
// The turns go first, oldest first: the conversation is the window the
// hook asked to read, and the call in front of it is what the questions
// are about. Then the probe outputs go, longest first, because a probe
// is the fact the hook asked for and each keeps as much of its answer as
// the cap leaves. Evidence that still does not fit is an error rather
// than a request the decision model cannot take.
func fitEvidence(payload *statePayload) error {
	for {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("hook: the evidence does not encode: %w", err)
		}
		if len(encoded) <= MaxEvidenceBytes {
			return nil
		}
		if len(payload.Turns) > 0 {
			payload.Turns = payload.Turns[1:]
			continue
		}
		name, ok := longestProbe(payload.Probes)
		if !ok {
			return fmt.Errorf("hook: the evidence does not fit the %d bytes a hook may carry", MaxEvidenceBytes)
		}
		result := payload.Probes[name]
		result.Output = result.Output[:len(result.Output)/2]
		result.Truncated = true
		payload.Probes[name] = result
	}
}

// longestProbe names the probe with the most output, in name order when
// two are the same length.
func longestProbe(probes map[string]ProbeResult) (string, bool) {
	name := ""
	longest := 0
	for _, key := range slices.Sorted(maps.Keys(probes)) {
		if length := len(probes[key].Output); length > longest {
			name, longest = key, length
		}
	}
	return name, longest > 0
}

// limitedBuffer collects a probe's output up to MaxProbeOutputBytes and
// says whether it went past it.
//
// It never fails a write: an executor keeps writing whatever the sink
// takes, and a sink that returned an error would leave the probe blocked
// with its output half-read. The bytes past the cap are dropped.
type limitedBuffer struct {
	// buf holds the bytes that fit.
	buf bytes.Buffer
	// over reports that the output went past the cap.
	over bool
}

// Write keeps the bytes that fit and records that the rest were dropped.
func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := MaxProbeOutputBytes - b.buf.Len()
	if left >= n {
		b.buf.Write(p)
		return n, nil
	}
	if left > 0 {
		b.buf.Write(p[:left])
	}
	b.over = true
	return n, nil
}

// overflowed reports that the output went past the cap.
func (b *limitedBuffer) overflowed() bool { return b.over }

// String returns what was kept.
func (b *limitedBuffer) String() string { return b.buf.String() }

// clipMark is what clip appends to a text it cut.
const clipMark = "..."

// clip returns the head of a text, marked when it was cut. The mark is
// inside the limit, so a clip never hands back more than the caller
// allowed.
func clip(text string, limit int) string {
	if limit <= len(clipMark) || len(text) <= limit {
		return text
	}
	return text[:limit-len(clipMark)] + clipMark
}
