package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/exec"
)

// fakeExecutor is an executor a test scripts.
type fakeExecutor struct {
	// start is the command the test runs.
	start func(ctx context.Context, command exec.Command, out io.Writer) (exec.Process, error)
	// started is how many commands the probe handed it.
	started int
	// command is the last command the probe handed it.
	command exec.Command
	// ctx is the context the last command started under.
	ctx context.Context
}

// Name returns the executor's name.
func (e *fakeExecutor) Name() string { return "fake" }

// Start runs the command the test scripted.
func (e *fakeExecutor) Start(ctx context.Context, command exec.Command, out io.Writer) (exec.Process, error) {
	e.started++
	e.command = command
	e.ctx = ctx
	return e.start(ctx, command, out)
}

// fakeProcess is a command a test executor started.
type fakeProcess struct {
	// code is the exit status of a command that exits on its own.
	code int
	// ctx is the context the command runs under.
	ctx context.Context
	// block makes the command wait for its context to end.
	block bool
	// waited reports that the process was reaped.
	waited bool
	// stopped reports that the command was killed.
	stopped bool
}

// Wait waits for the command the way an executor does: a blocked command
// ends with its context.
func (p *fakeProcess) Wait() (int, error) {
	p.waited = true
	if p.block {
		<-p.ctx.Done()
		p.stopped = true
		return -1, nil
	}
	return p.code, nil
}

// Kill stops the command.
func (p *fakeProcess) Kill() { p.stopped = true }

// Stopped reports that the command was killed.
func (p *fakeProcess) Stopped() bool { return p.stopped }

// fakeCaller is a read-only tool caller a test scripts.
type fakeCaller struct {
	// call is the answer the test returns.
	call func(ctx context.Context, tool string, args json.RawMessage) (poo.HookToolObservation, error)
	// tool is the last tool the probe named.
	tool string
	// args is the last arguments the probe passed.
	args json.RawMessage
	// ctx is the context the last call ran under.
	ctx context.Context
}

// CallReadOnly answers the way the test scripted it.
func (c *fakeCaller) CallReadOnly(ctx context.Context, tool string, args json.RawMessage) (poo.HookToolObservation, error) {
	c.tool = tool
	c.args = args
	c.ctx = ctx
	return c.call(ctx, tool, args)
}

// fakeView is the history a test hook reads.
type fakeView struct {
	// messages is the history the view holds.
	messages []poo.Message
}

// Messages returns the model's history.
func (v fakeView) Messages() []poo.Message { return v.messages }

// Turns returns no turn summaries.
func (v fakeView) Turns() []poo.TurnSummary { return nil }

// Result returns no tool result.
func (v fakeView) Result(string) (poo.ToolResult, bool) { return poo.ToolResult{}, false }

// probeSpec is a checked before_tool hook whose context a test names.
func probeSpec(t *testing.T, context ContextSpec) *Compiled {
	t.Helper()
	compiled, err := Validate("test-hook", Spec{
		Point:   PointBeforeTool,
		Context: context,
		Entry:   "ask",
		Steps: map[string]Step{
			"ask": {
				Question: QuestionSpec{
					Kind:         QuestionBool,
					Instructions: "Does the evidence hold?",
					True:         "it holds",
					False:        "it does not",
				},
				Branches: map[string]Branch{
					"yes": {MinProbability: 0.5, Action: &Action{Kind: ActionAllow}},
					"no":  {MinProbability: 0.5, Action: &Action{Kind: ActionAllow}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("validating the probe spec: %v", err)
	}
	return compiled
}

// probeErrorOf returns the probe failure a call reported.
func probeErrorOf(t *testing.T, err error) *ProbeError {
	t.Helper()
	var failure *ProbeError
	if !errors.As(err, &failure) {
		t.Fatalf("the probe failed with %v, not a *ProbeError", err)
	}
	return failure
}

// historyOf builds a view of the texts, one user message each.
func historyOf(texts ...string) poo.View {
	messages := make([]poo.Message, len(texts))
	for i, text := range texts {
		messages[i] = poo.NewTextMessage(poo.RoleUser, text)
	}
	return poo.NewHistoryView(messages, nil, nil)
}

func TestACommandProbeKeepsTheHeadOfItsOutput(t *testing.T) {
	output := bytes.Repeat([]byte("a"), MaxProbeOutputBytes+128)
	proc := &fakeProcess{}
	executor := &fakeExecutor{start: func(_ context.Context, _ exec.Command, out io.Writer) (exec.Process, error) {
		// Every write is handed over whole, the way an executor that
		// keeps reading does: a sink that refused bytes would block
		// the command on a full pipe.
		for _, chunk := range [][]byte{output[:len(output)/2], output[len(output)/2:]} {
			n, err := out.Write(chunk)
			if n != len(chunk) || err != nil {
				t.Errorf("the sink took %d of %d bytes with error %v, so a full pipe would block the command",
					n, len(chunk), err)
			}
		}
		return proc, nil
	}}
	env := []string{"PATH=/usr/bin"}
	probe := Probe{Name: "out", Command: []string{"gofmt", "-l", "."}, TimeoutMs: 1000}

	result, err := runCommandProbe(context.Background(), executor, env, "/work", probe)
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if got, want := result.Output, string(output[:MaxProbeOutputBytes]); got != want {
		t.Errorf("the probe kept %d bytes, want the head of %d", len(got), len(want))
	}
	if !result.Truncated {
		t.Error("the probe dropped the tail of its output without saying so")
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("the probe reported exit code %v, want 0", result.ExitCode)
	}
	if !proc.waited {
		t.Error("the probe never reaped its command")
	}
	if strings.Join(executor.command.Argv, " ") != "gofmt -l ." {
		t.Errorf("the executor ran %q", executor.command.Argv)
	}
	if executor.command.Dir != "/work" {
		t.Errorf("the executor ran in %q, want the hook's working directory", executor.command.Dir)
	}
	if strings.Join(executor.command.Env, " ") != strings.Join(env, " ") {
		t.Errorf("the executor ran with %q, want the probe's environment", executor.command.Env)
	}
	if _, ok := executor.ctx.Deadline(); !ok {
		t.Error("the probe ran under no deadline, so a stuck command would hold the hook")
	}
}

func TestACommandProbeReapsAProbeThatTimesOut(t *testing.T) {
	proc := &fakeProcess{block: true}
	executor := &fakeExecutor{start: func(ctx context.Context, _ exec.Command, out io.Writer) (exec.Process, error) {
		proc.ctx = ctx
		out.Write([]byte("half-written-output"))
		return proc, nil
	}}
	probe := Probe{Name: "slow", Command: []string{"sleep", "10"}, TimeoutMs: 5}

	result, err := runCommandProbe(context.Background(), executor, nil, "", probe)
	if err == nil {
		t.Fatalf("the probe handed back %+v after timing out", result)
	}
	if failure := probeErrorOf(t, err); failure.Reason != "timed out" {
		t.Errorf("the probe failed with %q, want a timeout", failure.Reason)
	}
	if !proc.waited {
		t.Error("the probe left its command unreaped")
	}
	if strings.Contains(err.Error(), "half-written-output") {
		t.Errorf("the error carries the probe's output: %v", err)
	}
}

func TestACommandProbeReportsANonzeroExitAsEvidence(t *testing.T) {
	proc := &fakeProcess{code: 1}
	executor := &fakeExecutor{start: func(_ context.Context, _ exec.Command, out io.Writer) (exec.Process, error) {
		out.Write([]byte("main.go\n"))
		return proc, nil
	}}
	probe := Probe{Name: "unformatted_files", Command: []string{"gofmt", "-l", "."}}

	result, err := runCommandProbe(context.Background(), executor, nil, "", probe)
	if err != nil {
		t.Fatalf("a nonzero exit was treated as a failure: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Fatalf("the probe reported exit code %v, want 1", result.ExitCode)
	}
	if result.Output != "main.go\n" {
		t.Errorf("the probe kept %q, want the finding", result.Output)
	}
	if result.Truncated {
		t.Error("the probe said it truncated an output that fit")
	}
}

func TestACommandProbeThatCannotStartDoesNotRun(t *testing.T) {
	executor := &fakeExecutor{start: func(context.Context, exec.Command, io.Writer) (exec.Process, error) {
		return nil, errors.New("the executor refused /secret/path")
	}}

	_, err := runCommandProbe(context.Background(), executor, nil, "", Probe{Name: "out", Command: []string{"true"}})
	if failure := probeErrorOf(t, err); failure.Reason != "did not start" {
		t.Errorf("the probe failed with %q, want a start failure", failure.Reason)
	}
	if executor.started != 1 {
		t.Errorf("the probe started %d commands, want 1", executor.started)
	}
	if strings.Contains(err.Error(), "/secret/path") {
		t.Errorf("the error carries the executor's own text: %v", err)
	}
}

func TestACommandProbeWithoutAnExecutorIsAProbeError(t *testing.T) {
	_, err := runCommandProbe(context.Background(), nil, nil, "", Probe{Name: "out", Command: []string{"true"}})
	if failure := probeErrorOf(t, err); failure.Reason != "has no executor to run it" {
		t.Errorf("the probe failed with %q, want a missing executor", failure.Reason)
	}
}

func TestAToolProbeIsTheToolsAnswer(t *testing.T) {
	caller := &fakeCaller{call: func(context.Context, string, json.RawMessage) (poo.HookToolObservation, error) {
		return poo.HookToolObservation{Text: "module github.com/sdougbrown/poo\n"}, nil
	}}
	probe := Probe{Name: "go_mod", Tool: &ToolProbe{
		Name: "read",
		Args: json.RawMessage(`{"path":"go.mod","limit":30}`),
	}}

	result, err := runToolProbe(context.Background(), caller, probe)
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if result.Output != "module github.com/sdougbrown/poo\n" {
		t.Errorf("the probe kept %q, want the tool's answer", result.Output)
	}
	if result.ExitCode != nil {
		t.Errorf("a tool probe reported exit code %d", *result.ExitCode)
	}
	if caller.tool != "read" {
		t.Errorf("the probe called %q", caller.tool)
	}
	if string(caller.args) != `{"path":"go.mod","limit":30}` {
		t.Errorf("the probe passed %s", caller.args)
	}
	if _, ok := caller.ctx.Deadline(); !ok {
		t.Error("the probe ran under no deadline, so a stuck tool would hold the hook")
	}
}

func TestAToolProbeClipsAnAnswerPastTheCap(t *testing.T) {
	caller := &fakeCaller{call: func(context.Context, string, json.RawMessage) (poo.HookToolObservation, error) {
		return poo.HookToolObservation{Text: strings.Repeat("g", MaxProbeOutputBytes+64)}, nil
	}}

	result, err := runToolProbe(context.Background(), caller, Probe{Name: "go_mod", Tool: &ToolProbe{Name: "read"}})
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if len(result.Output) != MaxProbeOutputBytes {
		t.Errorf("the probe kept %d bytes, want %d", len(result.Output), MaxProbeOutputBytes)
	}
	if !result.Truncated {
		t.Error("the probe dropped the tail of the tool's answer without saying so")
	}
}

func TestAToolProbeThatFailsIsAProbeError(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		caller := &fakeCaller{call: func(context.Context, string, json.RawMessage) (poo.HookToolObservation, error) {
			return poo.HookToolObservation{}, errors.New("no such file: the model's own text")
		}}
		_, err := runToolProbe(context.Background(), caller, Probe{Name: "go_mod", Tool: &ToolProbe{Name: "read"}})
		if failure := probeErrorOf(t, err); failure.Reason != `could not call the tool "read"` {
			t.Errorf("the probe failed with %q, want a refused call", failure.Reason)
		}
		if strings.Contains(err.Error(), "the model's own text") {
			t.Errorf("the error carries the tool's own text: %v", err)
		}
	})

	t.Run("tool error", func(t *testing.T) {
		caller := &fakeCaller{call: func(context.Context, string, json.RawMessage) (poo.HookToolObservation, error) {
			return poo.HookToolObservation{Text: "the file is gone", IsError: true}, nil
		}}
		_, err := runToolProbe(context.Background(), caller, Probe{Name: "go_mod", Tool: &ToolProbe{Name: "read"}})
		if failure := probeErrorOf(t, err); failure.Reason != `ran the tool "read", which reported an error` {
			t.Errorf("the probe failed with %q, want the tool's error", failure.Reason)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		caller := &fakeCaller{call: func(ctx context.Context, _ string, _ json.RawMessage) (poo.HookToolObservation, error) {
			<-ctx.Done()
			return poo.HookToolObservation{}, ctx.Err()
		}}
		_, err := runToolProbe(context.Background(), caller, Probe{Name: "go_mod", Tool: &ToolProbe{Name: "read"}, TimeoutMs: 5})
		if failure := probeErrorOf(t, err); failure.Reason != "timed out" {
			t.Errorf("the probe failed with %q, want a timeout", failure.Reason)
		}
	})

	t.Run("no caller", func(t *testing.T) {
		_, err := runToolProbe(context.Background(), nil, Probe{Name: "go_mod", Tool: &ToolProbe{Name: "read"}})
		if failure := probeErrorOf(t, err); failure.Reason != "has no read-only tool to call" {
			t.Errorf("the probe failed with %q, want a missing caller", failure.Reason)
		}
	})
}

func TestTheEvidenceCarriesTheFrameAndThePoint(t *testing.T) {
	compiled := probeSpec(t, ContextSpec{Turns: 2, IncludeToolCall: true, IncludeWorkingDir: true})
	hctx := poo.HookContext{
		HistoryView: historyOf("first", "second", "third"),
		WorkingDir:  "/work",
		Turn:        4,
		Budget: poo.ContextBudget{
			ContextLimit:   200000,
			InputLimit:     190000,
			EstimatedInput: 1200,
		},
	}
	point := PointData{
		Call: &CallData{
			Tool:      "bash",
			Summary:   "git push",
			Class:     "side_effecting",
			Arguments: json.RawMessage(`{"command":"git push"}`),
		},
		Result: &ResultData{Text: strings.Repeat("r", poo.EvidenceOutputCap+64), IsError: true},
	}

	state, err := gatherState(context.Background(), compiled, hctx, point, ProbeEnv{})
	if err != nil {
		t.Fatalf("gathering the evidence: %v", err)
	}
	payload, ok := state.(statePayload)
	if !ok {
		t.Fatalf("the evidence is a %T", state)
	}
	framed := decide.Evidence(nil, decide.EvidenceOptions{}).(decide.EvidencePayload)
	if payload.Instructions != framed.Instructions {
		t.Errorf("the evidence is not framed as data to assess: %q", payload.Instructions)
	}
	if len(payload.Turns) != 2 || payload.Turns[1].Text != "third" {
		t.Errorf("the evidence holds %d turns, want the last two of the history", len(payload.Turns))
	}
	if payload.WorkingDir != "/work" {
		t.Errorf("the evidence names the directory %q", payload.WorkingDir)
	}
	if payload.Call == nil {
		t.Fatal("the evidence holds no tool call")
	}
	if payload.Call.Tool != "bash" || payload.Call.Class != "side_effecting" || payload.Call.Summary != "git push" {
		t.Errorf("the evidence holds the call %+v", *payload.Call)
	}
	if string(payload.Call.Arguments) != `{"command":"git push"}` {
		t.Errorf("the evidence holds the arguments %s", payload.Call.Arguments)
	}
	if payload.Point != PointBeforeTool {
		t.Errorf("the evidence names the point %q", payload.Point)
	}
	if payload.Turn != 4 {
		t.Errorf("the evidence names the turn %d, want 4", payload.Turn)
	}
	if payload.Budget.ContextLimit != 200000 || payload.Budget.EstimatedInput != 1200 {
		t.Errorf("the evidence holds the budget %+v", payload.Budget)
	}
	if payload.Result == nil || !payload.Result.IsError {
		t.Fatalf("the evidence holds the result %+v", payload.Result)
	}
	if !strings.HasSuffix(payload.Result.Text, "...") || len(payload.Result.Text) > poo.EvidenceOutputCap {
		t.Errorf("the evidence holds the whole result, not its head: %d bytes", len(payload.Result.Text))
	}
}

func TestTheEvidenceNamesTheRunTrigger(t *testing.T) {
	compiled, err := Validate("test-hook", Spec{
		Point: PointBeforeRun,
		Entry: "ask",
		Steps: map[string]Step{
			"ask": {
				Question: QuestionSpec{Kind: QuestionBool, Instructions: "Does the evidence hold?", True: "it holds", False: "it does not"},
				Branches: map[string]Branch{
					"yes": {MinProbability: 0.5, Action: &Action{Kind: ActionPass}},
					"no":  {MinProbability: 0.5, Action: &Action{Kind: ActionPass}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("validating the run spec: %v", err)
	}

	state, err := gatherState(context.Background(), compiled, poo.HookContext{}, PointData{Trigger: TriggerJob}, ProbeEnv{})
	if err != nil {
		t.Fatalf("gathering the evidence: %v", err)
	}
	payload := state.(statePayload)
	if payload.Trigger != TriggerJob || payload.Point != PointBeforeRun {
		t.Errorf("the evidence names the trigger %q at %q", payload.Trigger, payload.Point)
	}
}

func TestTheEvidenceCarriesOnlyWhatTheSpecAsksFor(t *testing.T) {
	compiled := probeSpec(t, ContextSpec{})
	hctx := poo.HookContext{HistoryView: historyOf("first", "second"), WorkingDir: "/work", Turn: 2}
	point := PointData{
		Call:   &CallData{Tool: "bash", Arguments: json.RawMessage(`{"command":"git push"}`)},
		Result: &ResultData{Text: "the result"},
	}

	state, err := gatherState(context.Background(), compiled, hctx, point, ProbeEnv{})
	if err != nil {
		t.Fatalf("gathering the evidence: %v", err)
	}
	payload := state.(statePayload)
	if payload.Call != nil {
		t.Errorf("the evidence holds a call the spec did not ask for: %+v", *payload.Call)
	}
	if payload.WorkingDir != "" {
		t.Errorf("the evidence names a directory the spec did not ask for: %q", payload.WorkingDir)
	}
	if len(payload.Turns) != 0 {
		t.Errorf("the evidence holds %d turns the spec did not ask for", len(payload.Turns))
	}
	if payload.Result == nil {
		t.Error("the evidence holds no result, so a result hook judges nothing")
	}
}

func TestTheEvidenceDropsACallItsArgumentsWouldNotFit(t *testing.T) {
	compiled := probeSpec(t, ContextSpec{IncludeToolCall: true})
	arguments := json.RawMessage(`{"command":"` + strings.Repeat("x", maxCallArgumentsBytes) + `"}`)
	point := PointData{Call: &CallData{
		Tool:      "bash",
		Summary:   strings.Repeat("s", poo.EvidenceTextCap+64),
		Arguments: arguments,
	}}

	state, err := gatherState(context.Background(), compiled, poo.HookContext{}, point, ProbeEnv{})
	if err != nil {
		t.Fatalf("gathering the evidence: %v", err)
	}
	payload := state.(statePayload)
	if payload.Call == nil {
		t.Fatal("the evidence holds no call")
	}
	if payload.Call.Arguments != nil {
		t.Errorf("the evidence holds %d bytes of arguments", len(payload.Call.Arguments))
	}
	if !strings.HasSuffix(payload.Call.Summary, "...") || len(payload.Call.Summary) > poo.EvidenceTextCap {
		t.Errorf("the evidence holds the whole summary: %d bytes", len(payload.Call.Summary))
	}
}

func TestTheEvidenceFitsTheCap(t *testing.T) {
	messages := make([]string, 0, 20)
	for range 20 {
		messages = append(messages, strings.Repeat("m", 600))
	}
	probes := make([]Probe, 0, MaxProbes)
	for i := range MaxProbes {
		probes = append(probes, Probe{Name: "p" + string(rune('a'+i)), Command: []string{"du"}})
	}
	executor := &fakeExecutor{start: func(_ context.Context, _ exec.Command, out io.Writer) (exec.Process, error) {
		out.Write(bytes.Repeat([]byte("b"), MaxProbeOutputBytes))
		return &fakeProcess{}, nil
	}}
	compiled := probeSpec(t, ContextSpec{Turns: 20, Probes: probes})
	hctx := poo.HookContext{HistoryView: historyOf(messages...)}

	state, err := gatherState(context.Background(), compiled, hctx, PointData{}, ProbeEnv{Executor: executor})
	if err != nil {
		t.Fatalf("gathering the evidence: %v", err)
	}
	payload := state.(statePayload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encoding the evidence: %v", err)
	}
	if len(encoded) > MaxEvidenceBytes {
		t.Errorf("the evidence carries %d bytes, the most is %d", len(encoded), MaxEvidenceBytes)
	}
	if len(payload.Turns) == len(messages) {
		t.Error("the evidence kept every turn, so nothing was shrunk to fit")
	}
	if len(payload.Probes) != MaxProbes {
		t.Errorf("the evidence holds %d probes, want %d", len(payload.Probes), MaxProbes)
	}
	shrunk := false
	for name, result := range payload.Probes {
		if result.Truncated && len(result.Output) < MaxProbeOutputBytes {
			shrunk = true
		}
		if len(result.Output) > MaxProbeOutputBytes {
			t.Errorf("the probe %q kept %d bytes, the most is %d", name, len(result.Output), MaxProbeOutputBytes)
		}
	}
	if !shrunk {
		t.Error("the evidence fit by shrinking no probe, so the cap did not bound the probes")
	}
}
