package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/pootest"
)

// testHookName is the name every test hook carries.
const testHookName = "pre-push-gofmt"

// checkHook compiles a spec a test wrote.
func checkHook(t *testing.T, spec Spec) *Compiled {
	t.Helper()
	compiled, err := Validate(testHookName, spec)
	if err != nil {
		t.Fatalf("checking the hook: %v", err)
	}
	return compiled
}

// neutralActionOf is the action a point takes when a hook says nothing.
func neutralActionOf(point Point) Action {
	kinds := map[Point]ActionKind{
		PointBeforeRun:    ActionPass,
		PointBeforeTool:   ActionAllow,
		PointAfterTool:    ActionKeep,
		PointAfterTurn:    ActionPass,
		PointBeforeSettle: ActionAccept,
	}
	return Action{Kind: kinds[point]}
}

// oneStep is a spec whose single bool question takes the action on a
// sure "yes" and the point's neutral action on a "no".
func oneStep(point Point, action Action) Spec {
	neutral := neutralActionOf(point)
	return Spec{
		Point: point,
		Entry: "ask",
		Steps: map[string]Step{
			"ask": {
				Question: QuestionSpec{
					Kind:         QuestionBool,
					Instructions: "Does the evidence hold?",
					True:         "it holds",
					False:        "it does not",
				},
				Branches: map[string]Branch{
					"yes": {MinProbability: 0.5, Action: &action},
					"no":  {MinProbability: 0.5, Action: &neutral},
				},
			},
		},
	}
}

// runContext is the context a test hook is called with.
func runContext(runID string, decider decide.Decider) poo.HookContext {
	return poo.HookContext{
		Context:   context.Background(),
		SessionID: "s1",
		RunID:     runID,
		Decider:   decider,
	}
}

// preparedCall is a prepared call whose summary is its command.
type preparedCall struct{ summary string }

func (p preparedCall) Summary() string { return p.summary }

// bashCall is a bash call that runs the command line.
// askPrepared is a prepared call whose transcript summary and raw
// arguments are not what a decision about the call may rest on.
type askPrepared struct {
	// summary is the model-facing description.
	summary string
	// ask is the text the tool names for an ask.
	ask string
	// args are the arguments the tool names for a decision model.
	args json.RawMessage
}

func (p askPrepared) Summary() string { return p.summary }

func (p askPrepared) AskSummary() string { return p.ask }

func (p askPrepared) EvidenceArguments() json.RawMessage { return p.args }

func bashCall(command string) poo.ToolCallInfo {
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		panic(err)
	}
	return poo.ToolCallInfo{
		Spec: poo.ToolSpec{
			Name:       "bash",
			Class:      poo.ClassSideEffecting,
			PrimaryArg: "command",
		},
		Arguments: args,
		Prepared:  preparedCall{summary: command},
	}
}

// readCall is a call to a read-only tool.
func readCall(path string) poo.ToolCallInfo {
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		panic(err)
	}
	return poo.ToolCallInfo{
		Spec: poo.ToolSpec{
			Name:       "read",
			Class:      poo.ClassReadOnly,
			PrimaryArg: "path",
		},
		Arguments: args,
		Prepared:  preparedCall{summary: path},
	}
}

// runnerOf returns a runner with room under a session's hook timeout.
func runnerOf(compiled *Compiled, decider decide.Decider) *Runner {
	return NewRunner(compiled, ProbeEnv{}, 30*time.Second)
}

func TestAMatcherReadsTheCall(t *testing.T) {
	cases := []struct {
		name  string
		match Match
		call  poo.ToolCallInfo
		want  bool
	}{
		{name: "no matcher", match: Match{}, call: bashCall("ls"), want: true},
		{name: "named tool", match: Match{Tool: "bash"}, call: bashCall("ls"), want: true},
		{name: "another tool", match: Match{Tool: "bash"}, call: readCall("go.mod"), want: false},
		{name: "read-only class", match: Match{Class: ClassReadOnly}, call: readCall("go.mod"), want: true},
		{name: "side-effecting class", match: Match{Class: ClassReadOnly}, call: bashCall("ls"), want: false},
		{name: "command prefix", match: Match{Tool: "bash", CommandPrefix: "git push"}, call: bashCall("git push origin main"), want: true},
		{name: "command prefix after whitespace", match: Match{Tool: "bash", CommandPrefix: "git push"}, call: bashCall("   git push"), want: true},
		{name: "another command", match: Match{Tool: "bash", CommandPrefix: "git push"}, call: bashCall("git pull"), want: false},
		{name: "either substring", match: Match{Tool: "bash", CommandContains: []string{"git push", "git commit"}}, call: bashCall("git push origin main"), want: true},
		{name: "the second substring", match: Match{Tool: "bash", CommandContains: []string{"git push", "git commit"}}, call: bashCall("git commit -m \"the change\""), want: true},
		{name: "neither substring", match: Match{Tool: "bash", CommandContains: []string{"git push", "git commit"}}, call: bashCall("git pull"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
			spec.Match = tc.match
			decider := pootest.NewStubDecider(0.9)
			runner := runnerOf(checkHook(t, spec), decider)

			decision, err := runner.BeforeTool(runContext("run1", decider), tc.call)
			if err != nil {
				t.Fatalf("running the hook: %v", err)
			}
			fired := decider.Calls() == 1
			if fired != tc.want {
				t.Fatalf("the hook fired %v, want %v", fired, tc.want)
			}
			if tc.want && !decision.Denied {
				t.Errorf("the hook fired and allowed the call: %+v", decision)
			}
		})
	}
}

func TestAnUnmatchedCallChargesNoFiring(t *testing.T) {
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	// The fixture's matcher: a push or a commit, either one.
	spec.Match = Match{Tool: "bash", CommandContains: []string{"git push", "git commit"}}
	spec.MaxFiresPerRun = 1
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)
	hctx := runContext("run1", decider)

	if decision, err := runner.BeforeTool(hctx, bashCall("ls -la")); err != nil || decision.Denied {
		t.Fatalf("an unmatched call produced %+v with error %v", decision, err)
	}
	if decision, err := runner.BeforeTool(hctx, bashCall("git commit -m the change")); err != nil || !decision.Denied {
		t.Fatalf("the matching call produced %+v with error %v", decision, err)
	}
	if calls := decider.Calls(); calls != 1 {
		t.Errorf("the decider was asked %d times, want once", calls)
	}
}

func TestABeforeToolHookTurnsItsActionIntoADecision(t *testing.T) {
	t.Run("deny", func(t *testing.T) {
		decider := pootest.NewStubDecider(0.9)
		runner := runnerOf(checkHook(t, oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "run gofmt first"})), decider)

		decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
		if err != nil {
			t.Fatalf("running the hook: %v", err)
		}
		if !decision.Denied || decision.Detail != "run gofmt first" {
			t.Errorf("the hook decided %+v, want the denial", decision)
		}
	})

	t.Run("short circuit", func(t *testing.T) {
		decider := pootest.NewStubDecider(0.9)
		runner := runnerOf(checkHook(t, oneStep(PointBeforeTool, Action{Kind: ActionShortCircuit, Message: "read the file first"})), decider)

		decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
		if err != nil {
			t.Fatalf("running the hook: %v", err)
		}
		if decision.Denied || !decision.Allowed || decision.Result == nil {
			t.Fatalf("the hook decided %+v, want a short circuit", decision)
		}
		if decision.Result.Text != "read the file first" {
			t.Errorf("the short circuit carries %q", decision.Result.Text)
		}
	})

	t.Run("allow", func(t *testing.T) {
		decider := pootest.NewStubDecider(0.1)
		runner := runnerOf(checkHook(t, oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "run gofmt first"})), decider)

		decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
		if err != nil {
			t.Fatalf("running the hook: %v", err)
		}
		if !decision.Allowed || decision.Denied || decision.Result != nil || decision.Detail != "" {
			t.Errorf("the hook decided %+v, want the neutral decision", decision)
		}
	})
}

func TestASureDecisionChargesOneFiring(t *testing.T) {
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	spec.MaxFiresPerRun = 1
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)
	hctx := runContext("run1", decider)

	first, err := runner.BeforeTool(hctx, bashCall("git push"))
	if err != nil || !first.Denied {
		t.Fatalf("the first call produced %+v with error %v", first, err)
	}
	second, err := runner.BeforeTool(hctx, bashCall("git push"))
	if err != nil || !second.Allowed {
		t.Fatalf("the second call produced %+v with error %v, want the neutral decision", second, err)
	}
	if calls := decider.Calls(); calls != 1 {
		t.Errorf("the decider was asked %d times after the cap, want once", calls)
	}
}

// countingDecider counts the calls a decider served.
type countingDecider struct {
	decide.Decider
	calls atomic.Int32
}

// Decide serves the call and counts it.
func (d *countingDecider) Decide(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	d.calls.Add(1)
	return d.Decider.Decide(ctx, state, instructions, qs)
}

// Calls returns how many calls the decider served.
func (d *countingDecider) Calls() int { return int(d.calls.Load()) }

func TestTheFiringCapHoldsUnderConcurrentCalls(t *testing.T) {
	// A turn's read-only calls run at once, so the calls a cap of one
	// covers arrive together. Every call is judged, and the decisions race
	// for the cap: exactly one of them takes effect.
	const calls = 8
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	spec.MaxFiresPerRun = 1

	var (
		entered    = make(chan struct{}, calls)
		allEntered = make(chan struct{})
		stopped    = make(chan struct{})
		release    = make(chan struct{})
	)
	defer close(stopped)
	go func() {
		for range calls {
			select {
			case <-entered:
			case <-stopped:
				return
			}
		}
		close(allEntered)
	}()
	decider := &countingDecider{Decider: decide.DeciderFunc(func(context.Context, any, string, []decide.Question) ([]decide.Answer, error) {
		entered <- struct{}{}
		<-release
		return []decide.Answer{{ID: "hook:" + testHookName + ":ask", Probabilities: map[string]float64{"yes": 0.99, "no": 0.01}}}, nil
	})}
	runner := runnerOf(checkHook(t, spec), decider)

	var (
		denials  atomic.Int32
		allows   atomic.Int32
		failures atomic.Int32
		returned = make(chan struct{}, calls)
		start    = make(chan struct{})
		wg       sync.WaitGroup
	)
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
			if err != nil {
				t.Errorf("the call failed: %v", err)
				failures.Add(1)
			}
			if decision.Denied {
				denials.Add(1)
			} else {
				allows.Add(1)
			}
			returned <- struct{}{}
		}()
	}
	close(start)

	// The decider holds every call until the whole batch has been asked,
	// so the decisions race for the cap rather than taking turns. A call
	// that ends before the batch is in is a firing in flight holding the
	// cap against the others, which it must not.
	select {
	case <-allEntered:
	case <-returned:
	}
	close(release)
	wg.Wait()

	if got := failures.Load(); got != 0 {
		t.Fatalf("%d calls failed", got)
	}
	if got := decider.Calls(); got != calls {
		t.Errorf("the decider was asked %d times, want every call judged", got)
	}
	if got := denials.Load(); got != 1 {
		t.Errorf("%d calls were denied under a cap of one", got)
	}
	if got := allows.Load(); got != calls-1 {
		t.Errorf("%d calls were allowed, want %d", got, calls-1)
	}
}

func TestEveryCallIsJudgedWhenTheAnswersAreNeutral(t *testing.T) {
	// The cap bounds the decisions that take effect, not the calls that
	// are judged: a batch whose answers are all neutral is asked in full
	// and takes no decision slot between them.
	const calls = 8
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	spec.MaxFiresPerRun = 1
	decider := pootest.NewStubDecider(0.1)
	runner := runnerOf(checkHook(t, spec), decider)

	var (
		denials atomic.Int32
		start   = make(chan struct{})
		wg      sync.WaitGroup
	)
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
			if err != nil {
				t.Errorf("the call failed: %v", err)
			}
			if decision.Denied {
				denials.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := decider.Calls(); got != calls {
		t.Errorf("the decider was asked %d times, want every call judged", got)
	}
	if got := denials.Load(); got != 0 {
		t.Errorf("%d calls were denied on an answer that allowed them", got)
	}
}

func TestANeutralDecisionChargesNoFiring(t *testing.T) {
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	spec.MaxFiresPerRun = 1
	decider := pootest.NewStubDecider(0.1)
	runner := runnerOf(checkHook(t, spec), decider)
	hctx := runContext("run1", decider)

	for range 2 {
		if decision, err := runner.BeforeTool(hctx, bashCall("git push")); err != nil || decision.Denied {
			t.Fatalf("the call produced %+v with error %v", decision, err)
		}
	}
	if calls := decider.Calls(); calls != 2 {
		t.Errorf("the decider was asked %d times, want twice: a neutral decision is not a firing", calls)
	}
}

func TestALateJobCompletionCountsItsOwnFirings(t *testing.T) {
	spec := oneStep(PointAfterTool, Action{Kind: ActionAnnotate, Message: "the file is unformatted"})
	spec.MaxFiresPerRun = 1
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)
	result := poo.ToolResult{Text: "done"}

	job := func(jobID string) poo.HookContext {
		hctx := runContext("run1", decider)
		hctx.JobID = jobID
		return hctx
	}

	if decision, err := runner.AfterTool(job("j1"), bashCall("git push"), result); err != nil || decision.Annotated == "" {
		t.Fatalf("the job's first result produced %+v with error %v", decision, err)
	}
	if decision, err := runner.AfterTool(job("j1"), bashCall("git push"), result); err != nil || decision.Annotated != "" {
		t.Fatalf("the job's second result produced %+v with error %v, want the neutral decision", decision, err)
	}
	if decision, err := runner.AfterTool(job("j2"), bashCall("git push"), result); err != nil || decision.Annotated == "" {
		t.Fatalf("another job's result produced %+v with error %v, want its own count", decision, err)
	}
	if calls := decider.Calls(); calls != 2 {
		t.Errorf("the decider was asked %d times, want twice", calls)
	}
}

// pointVerdict is what one point's decision changed.
type pointVerdict struct {
	// neutral reports that the decision leaves the point as it was.
	neutral bool
	// blocked reports that the decision is the point's blocking one.
	blocked bool
	// detail is the blocking decision's message.
	detail string
}

// pointCases is every hook point, the action that changes something
// there, how a test asks it, and whether Validate allows a fail_closed
// policy at it.
var pointCases = []struct {
	point  Point
	action Action
	ask    func(runner *Runner, hctx poo.HookContext) (pointVerdict, error)
	closed bool
}{
	{
		point:  PointBeforeRun,
		action: Action{Kind: ActionAppend, Message: "the tree is unformatted"},
		ask: func(runner *Runner, hctx poo.HookContext) (pointVerdict, error) {
			decision, err := runner.BeforeRun(hctx, poo.RunTrigger{Kind: poo.TriggerPrompt})
			return pointVerdict{neutral: len(decision.Append) == 0 && decision.SystemPrompt == nil}, err
		},
	},
	{
		point:  PointBeforeTool,
		action: Action{Kind: ActionDeny, Message: "refused"},
		closed: true,
		ask: func(runner *Runner, hctx poo.HookContext) (pointVerdict, error) {
			decision, err := runner.BeforeTool(hctx, bashCall("git push"))
			return pointVerdict{
				neutral: decision.Allowed && !decision.Denied && decision.Result == nil,
				blocked: decision.Denied,
				detail:  decision.Detail,
			}, err
		},
	},
	{
		point:  PointAfterTool,
		action: Action{Kind: ActionReplace, Message: "the tool failed"},
		ask: func(runner *Runner, hctx poo.HookContext) (pointVerdict, error) {
			decision, err := runner.AfterTool(hctx, bashCall("go build ./..."), poo.ToolResult{Text: "the build failed", IsError: true})
			return pointVerdict{neutral: decision.Kept && decision.Annotated == "" && decision.Replaced == nil && !decision.Redacted}, err
		},
	},
	{
		point:  PointAfterTurn,
		action: Action{Kind: ActionStop, Message: "the work is done"},
		closed: true,
		ask: func(runner *Runner, hctx poo.HookContext) (pointVerdict, error) {
			decision, err := runner.AfterTurn(hctx)
			return pointVerdict{neutral: len(decision.Append) == 0 && !decision.Stop, blocked: decision.Stop, detail: decision.Detail}, err
		},
	},
	{
		point:  PointBeforeSettle,
		action: Action{Kind: ActionStop, Message: "the run is over"},
		closed: true,
		ask: func(runner *Runner, hctx poo.HookContext) (pointVerdict, error) {
			decision, err := runner.BeforeSettle(hctx, poo.ReasonEndTurn)
			return pointVerdict{neutral: decision.Accepted && decision.Continue == nil && !decision.Stop, blocked: decision.Stop, detail: decision.Detail}, err
		},
	},
}

func TestEveryPointTakesItsPolicyDecision(t *testing.T) {
	const closedMessage = "the hook could not judge this"
	sources := []struct {
		name    string
		decider func() *pootest.StubDecider
	}{
		{
			name: "on_error",
			decider: func() *pootest.StubDecider {
				decider := pootest.NewStubDecider(0.9)
				decider.SetError(decide.ErrDeferred)
				return decider
			},
		},
		{
			name: "on_uncertain",
			// An even split is a tie for the best answer.
			decider: func() *pootest.StubDecider { return pootest.NewStubDecider(0.5) },
		},
	}
	for _, tc := range pointCases {
		for _, source := range sources {
			policies := []FailurePolicy{FailOpen, FailRun}
			if tc.closed {
				policies = append(policies, FailClosed)
			}
			for _, policy := range policies {
				t.Run(fmt.Sprintf("%s/%s/%s", tc.point, source.name, policy), func(t *testing.T) {
					spec := oneStep(tc.point, tc.action)
					if source.name == "on_error" {
						spec.OnError = policy
					} else {
						spec.OnUncertain = policy
					}
					if policy == FailClosed {
						spec.FailClosedMessage = closedMessage
					}
					decider := source.decider()
					runner := runnerOf(checkHook(t, spec), decider)

					verdict, err := tc.ask(runner, runContext("run1", decider))
					if policy == FailRun {
						var failure *RunError
						if !errors.As(err, &failure) {
							t.Fatalf("the hook failed with %v, want a *RunError", err)
						}
						if failure.Hook != testHookName || failure.Point != tc.point {
							t.Errorf("the failure names %q at %q", failure.Hook, failure.Point)
						}
						if text := failure.Error(); !strings.Contains(text, string(tc.point)) {
							t.Errorf("the settle detail %q does not name the point", text)
						}
						return
					}
					if err != nil {
						t.Fatalf("the hook failed with %v, want its %s decision", err, policy)
					}
					if policy == FailClosed {
						if verdict.neutral || !verdict.blocked {
							t.Fatalf("the hook decided %+v, want the blocking decision", verdict)
						}
						if verdict.detail != closedMessage {
							t.Errorf("the blocking decision reads %q, want the fail_closed_message", verdict.detail)
						}
						return
					}
					if !verdict.neutral || verdict.blocked || verdict.detail != "" {
						t.Errorf("the hook decided %+v, want the neutral decision", verdict)
					}
				})
			}
		}
	}
}

func TestOneRunsFiringsDoNotStopAnothers(t *testing.T) {
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	spec.MaxFiresPerRun = 1
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)

	if decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push")); err != nil || !decision.Denied {
		t.Fatalf("run one's first call produced %+v with error %v", decision, err)
	}
	if decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push")); err != nil || decision.Denied {
		t.Fatalf("run one's second call produced %+v with error %v, want the neutral decision", decision, err)
	}
	if decision, err := runner.BeforeTool(runContext("run2", decider), bashCall("git push")); err != nil || !decision.Denied {
		t.Fatalf("run two's call produced %+v with error %v, want its own count", decision, err)
	}
	if calls := decider.Calls(); calls != 2 {
		t.Errorf("the decider was asked %d times, want once for each run", calls)
	}
}

func TestAHookJudgesTheCallTheToolNamesForAnAsk(t *testing.T) {
	// The transcript's summary may be the model's own description of a
	// call it has not run, and a model may describe the arguments it
	// wrote; the evidence a hook judges carries what the tool names for
	// an ask instead.
	capture := func(seen *any) decide.Decider {
		return decide.DeciderFunc(func(_ context.Context, state any, _ string, qs []decide.Question) ([]decide.Answer, error) {
			*seen = state
			return []decide.Answer{{ID: qs[0].ID, Probabilities: map[string]float64{"yes": 0.1, "no": 0.9}}}, nil
		})
	}
	evidence := func(t *testing.T, info poo.ToolCallInfo) *decide.Call {
		t.Helper()
		spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
		spec.Context.IncludeToolCall = true
		var state any
		decider := capture(&state)
		runner := runnerOf(checkHook(t, spec), decider)
		if _, err := runner.BeforeTool(runContext("run1", decider), info); err != nil {
			t.Fatalf("running the hook: %v", err)
		}
		payload, ok := state.(statePayload)
		if !ok {
			t.Fatalf("the evidence is a %T", state)
		}
		if payload.Call == nil {
			t.Fatal("the evidence carries no call")
		}
		return payload.Call
	}

	t.Run("the tool names its ask", func(t *testing.T) {
		info := bashCall("ls")
		info.Prepared = askPrepared{
			summary: "list files",
			ask:     "rm -rf ./build",
			args:    json.RawMessage(`{"command":"rm -rf ./build"}`),
		}
		call := evidence(t, info)
		if call.Summary != "rm -rf ./build" {
			t.Errorf("the evidence carries %q, want the tool's own ask text", call.Summary)
		}
		if string(call.Arguments) != `{"command":"rm -rf ./build"}` {
			t.Errorf("the evidence carries the arguments %s, want the tool's own", call.Arguments)
		}
	})

	t.Run("a tool that names nothing", func(t *testing.T) {
		call := evidence(t, bashCall("ls"))
		if call.Summary != "ls" {
			t.Errorf("the evidence carries %q, want the call's summary", call.Summary)
		}
		if string(call.Arguments) != `{"command":"ls"}` {
			t.Errorf("the evidence carries the arguments %s, want the ones the model wrote", call.Arguments)
		}
	})
}

func TestAnAnswerNoBranchCoversIsUncertain(t *testing.T) {
	spec := Spec{
		Point:             PointBeforeTool,
		OnUncertain:       FailClosed,
		FailClosedMessage: "the answer led nowhere",
		Entry:             "ask",
		Steps: map[string]Step{
			"ask": {
				Question: QuestionSpec{Kind: QuestionBool, Instructions: "Does the evidence hold?", True: "it holds", False: "it does not"},
				Branches: map[string]Branch{
					"yes": {MinProbability: 0.5, Action: &Action{Kind: ActionDeny, Message: "the answer was sure"}},
				},
			},
		},
	}
	decider := pootest.NewStubDecider(0.1)
	runner := runnerOf(checkHook(t, spec), decider)

	decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if !decision.Denied || decision.Detail != "the answer led nowhere" {
		t.Errorf("the hook decided %+v, want the uncertain policy's decision", decision)
	}
}

func TestAnInterruptedCallDropsTheHooksSay(t *testing.T) {
	t.Run("before the hook runs", func(t *testing.T) {
		decider := pootest.NewStubDecider(0.9)
		runner := runnerOf(checkHook(t, oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})), decider)
		hctx := runContext("run1", decider)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		hctx.Context = ctx

		decision, err := runner.BeforeTool(hctx, bashCall("git push"))
		if err != nil {
			t.Fatalf("an interrupt failed the hook: %v", err)
		}
		if !decision.Allowed || decision.Denied {
			t.Errorf("the hook decided %+v, want the neutral decision", decision)
		}
		if calls := decider.Calls(); calls != 0 {
			t.Errorf("the decider was asked %d times under an interrupted run", calls)
		}
	})

	t.Run("while the hook decides", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		decider := decide.DeciderFunc(func(context.Context, any, string, []decide.Question) ([]decide.Answer, error) {
			cancel()
			return nil, context.Canceled
		})
		// A failing policy makes the difference visible: an interrupt is
		// not a hook failure, so the closed decision must not be taken.
		spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
		spec.OnError = FailClosed
		spec.FailClosedMessage = "the hook could not answer"
		runner := runnerOf(checkHook(t, spec), decider)
		hctx := runContext("run1", decider)
		hctx.Context = ctx

		decision, err := runner.BeforeTool(hctx, bashCall("git push"))
		if err != nil {
			t.Fatalf("an interrupt failed the hook: %v", err)
		}
		if !decision.Allowed || decision.Denied {
			t.Errorf("the hook decided %+v, want the neutral decision", decision)
		}
	})
}

func TestTheFiringBudgetSitsUnderTheHookTimeout(t *testing.T) {
	cases := []struct {
		name       string
		hook       time.Duration
		firingMs   int
		wantBudget time.Duration
	}{
		{name: "clamped under the hook timeout", hook: 5 * time.Second, firingMs: 10000, wantBudget: 3 * time.Second},
		{name: "the hook's own timeout is the tighter", hook: 30 * time.Second, firingMs: 4000, wantBudget: 4 * time.Second},
		{name: "no session timeout", hook: 0, firingMs: 4000, wantBudget: 4 * time.Second},
		{name: "a floor for a short session timeout", hook: 100 * time.Millisecond, firingMs: 10000, wantBudget: minFiringBudget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen time.Duration
			decider := decide.DeciderFunc(func(ctx context.Context, _ any, _ string, _ []decide.Question) ([]decide.Answer, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Error("the hook ran with no deadline, so a stuck question would hold the run")
				}
				seen = time.Until(deadline)
				return nil, decide.ErrDeferred
			})
			spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
			spec.FiringTimeoutMs = tc.firingMs
			runner := NewRunner(checkHook(t, spec), ProbeEnv{}, tc.hook)

			if _, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push")); err != nil {
				t.Fatalf("running the hook: %v", err)
			}
			if seen < tc.wantBudget-250*time.Millisecond || seen > tc.wantBudget {
				t.Errorf("the hook's budget was %v, want %v", seen, tc.wantBudget)
			}
		})
	}
}

func TestAHookOutOfTimeFailsOpenBeforeItsDeadline(t *testing.T) {
	decider := decide.DeciderFunc(func(ctx context.Context, _ any, _ string, _ []decide.Question) ([]decide.Answer, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	const hookTimeout = 2500 * time.Millisecond
	runner := NewRunner(checkHook(t, oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})), ProbeEnv{}, hookTimeout)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	hctx := runContext("run1", decider)
	hctx.Context = parent

	started := time.Now()
	decision, err := runner.BeforeTool(hctx, bashCall("git push"))
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("running out of time failed the hook: %v", err)
	}
	if !decision.Allowed || decision.Denied {
		t.Errorf("the hook decided %+v, want the neutral decision", decision)
	}
	if elapsed < minFiringBudget/2 {
		t.Errorf("the hook answered after %v, before its own budget was spent", elapsed)
	}
	if elapsed >= hookTimeout {
		t.Errorf("the hook answered after %v, past the %v callHook would have settled the run on", elapsed, hookTimeout)
	}
	if parent.Err() != nil {
		t.Errorf("the hook's own budget ended the run's context: %v", parent.Err())
	}
}

func TestAnAnswerThatArrivesPastTheStepTimeoutIsDropped(t *testing.T) {
	// The step's own timeout runs out first, and the answer the model
	// gave afterwards is one callHook would have discarded.
	decider := decide.DeciderFunc(func(ctx context.Context, _ any, _ string, qs []decide.Question) ([]decide.Answer, error) {
		<-ctx.Done()
		return []decide.Answer{{
			ID:            qs[0].ID,
			Probabilities: map[string]float64{"yes": 0.99, "no": 0.01},
		}}, nil
	})
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	spec.Steps["ask"] = Step{
		TimeoutMs: 5,
		Question:  spec.Steps["ask"].Question,
		Branches:  spec.Steps["ask"].Branches,
	}
	runner := runnerOf(checkHook(t, spec), decider)

	started := time.Now()
	decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("the late answer failed the hook: %v", err)
	}
	if decision.Denied {
		t.Errorf("the hook decided %+v on an answer that arrived past its step's timeout", decision)
	}
	if elapsed >= time.Second {
		t.Errorf("the step waited %v, past the %dms its own timeout allows", elapsed, spec.Steps["ask"].TimeoutMs)
	}
}

func TestAnAfterToolHookTurnsItsActionIntoADecision(t *testing.T) {
	result := poo.ToolResult{Text: "the build failed", IsError: true}
	cases := []struct {
		name   string
		action Action
		want   func(t *testing.T, decision poo.ResultDecision)
	}{
		{
			name:   "keep",
			action: Action{Kind: ActionKeep},
			want: func(t *testing.T, decision poo.ResultDecision) {
				if !decision.Kept || decision.Annotated != "" || decision.Replaced != nil || decision.Redacted {
					t.Errorf("the hook decided %+v, want the result kept", decision)
				}
			},
		},
		{
			name:   "annotate",
			action: Action{Kind: ActionAnnotate, Message: "the build log is above"},
			want: func(t *testing.T, decision poo.ResultDecision) {
				if !decision.Kept || decision.Annotated != "the build log is above" || decision.Replaced != nil {
					t.Errorf("the hook decided %+v, want the annotation", decision)
				}
			},
		},
		{
			name:   "replace",
			action: Action{Kind: ActionReplace, Message: "the tool failed"},
			want: func(t *testing.T, decision poo.ResultDecision) {
				if decision.Replaced == nil || decision.Replaced.Text != "the tool failed" {
					t.Fatalf("the hook decided %+v, want the replacement", decision)
				}
				if !decision.Replaced.IsError {
					t.Error("the replacement lost the failure the model was reading")
				}
				if decision.Redacted {
					t.Error("a replacement was recorded as a redaction")
				}
			},
		},
		{
			name:   "redact",
			action: Action{Kind: ActionRedact},
			want: func(t *testing.T, decision poo.ResultDecision) {
				if !decision.Redacted {
					t.Fatalf("the hook decided %+v, want a redaction", decision)
				}
				if decision.Replaced == nil || decision.Replaced.Text != poo.RedactedResultText {
					t.Errorf("the redaction shows %+v, want the core's redacted text", decision.Replaced)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decider := pootest.NewStubDecider(0.9)
			runner := runnerOf(checkHook(t, oneStep(PointAfterTool, tc.action)), decider)

			decision, err := runner.AfterTool(runContext("run1", decider), bashCall("go build ./..."), result)
			if err != nil {
				t.Fatalf("running the hook: %v", err)
			}
			tc.want(t, decision)
		})
	}
}

func TestAnAfterToolHookMatchesAResultThatFailed(t *testing.T) {
	spec := oneStep(PointAfterTool, Action{Kind: ActionAnnotate, Message: "the call failed"})
	failed := true
	spec.Match = Match{IsError: &failed}
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)
	hctx := runContext("run1", decider)

	if decision, err := runner.AfterTool(hctx, bashCall("ls"), poo.ToolResult{Text: "a.go b.go"}); err != nil || decision.Annotated != "" {
		t.Fatalf("a successful result produced %+v with error %v", decision, err)
	}
	if calls := decider.Calls(); calls != 0 {
		t.Errorf("the decider was asked %d times about a result the matcher skipped", calls)
	}
	if decision, err := runner.AfterTool(hctx, bashCall("ls"), poo.ToolResult{Text: "no such file", IsError: true}); err != nil || decision.Annotated == "" {
		t.Fatalf("a failed result produced %+v with error %v", decision, err)
	}
}

func TestABeforeRunHookAppendsForTheTriggerItMatches(t *testing.T) {
	spec := oneStep(PointBeforeRun, Action{Kind: ActionAppend, Message: "the tree is unformatted"})
	spec.Match = Match{Trigger: TriggerJob}
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)

	prompted, err := runner.BeforeRun(runContext("run1", decider), poo.RunTrigger{Kind: poo.TriggerPrompt})
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if len(prompted.Append) != 0 {
		t.Fatalf("the hook appended to a run its matcher skipped: %+v", prompted)
	}
	if calls := decider.Calls(); calls != 0 {
		t.Errorf("the decider was asked %d times about a trigger the matcher skipped", calls)
	}

	woken, err := runner.BeforeRun(runContext("run1", decider), poo.RunTrigger{Kind: poo.TriggerJob})
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if len(woken.Append) != 1 {
		t.Fatalf("the hook appended %d messages, want one", len(woken.Append))
	}
	if text := woken.Append[0].Text(); text != "the tree is unformatted" {
		t.Errorf("the hook appended %q", text)
	}
	if role := woken.Append[0].Role; role != poo.RoleUser {
		t.Errorf("the hook appended a %q message", role)
	}
}

func TestAnAfterTurnHookStopsTheRunFromItsTurn(t *testing.T) {
	spec := oneStep(PointAfterTurn, Action{Kind: ActionStop, Message: "the work is done"})
	minimum := 2
	spec.Match = Match{MinTurn: &minimum}
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)

	early := runContext("run1", decider)
	early.Turn = 1
	if decision, err := runner.AfterTurn(early); err != nil || decision.Stop {
		t.Fatalf("turn one produced %+v with error %v", decision, err)
	}
	if calls := decider.Calls(); calls != 0 {
		t.Errorf("the decider was asked %d times before the first turn the matcher covers", calls)
	}

	later := runContext("run1", decider)
	later.Turn = 2
	decision, err := runner.AfterTurn(later)
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if !decision.Stop || decision.Detail != "the work is done" {
		t.Errorf("the hook decided %+v, want the stop", decision)
	}
}

func TestAnAfterTurnHookMatchesAnInterruptedTurn(t *testing.T) {
	spec := oneStep(PointAfterTurn, Action{Kind: ActionAppend, Message: "the turn was cut short"})
	interrupted := true
	spec.Match = Match{Interrupted: &interrupted}
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)

	whole := runContext("run1", decider)
	if decision, err := runner.AfterTurn(whole); err != nil || len(decision.Append) != 0 {
		t.Fatalf("a whole turn produced %+v with error %v", decision, err)
	}
	cut := runContext("run1", decider)
	cut.Interrupted = true
	decision, err := runner.AfterTurn(cut)
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if len(decision.Append) != 1 || decision.Append[0].Text() != "the turn was cut short" {
		t.Errorf("the hook decided %+v, want its message", decision)
	}
}

func TestABeforeSettleHookMatchesTheReasonItJudges(t *testing.T) {
	spec := oneStep(PointBeforeSettle, Action{Kind: ActionContinue, Message: "the tests are not run"})
	spec.Match = Match{Reason: ReasonEndTurn}
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)

	truncated, err := runner.BeforeSettle(runContext("run1", decider), poo.ReasonOutputTruncated)
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if !truncated.Accepted || truncated.Continue != nil {
		t.Fatalf("the hook decided %+v, want the settle accepted", truncated)
	}
	if calls := decider.Calls(); calls != 0 {
		t.Errorf("the decider was asked %d times about a reason the matcher skipped", calls)
	}

	ended, err := runner.BeforeSettle(runContext("run1", decider), poo.ReasonEndTurn)
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if ended.Continue == nil || ended.Continue.Text() != "the tests are not run" {
		t.Errorf("the hook decided %+v, want its continuation", ended)
	}
}

func TestAHookWalksItsGraphFromOneStepToTheNext(t *testing.T) {
	// The graph is the fixture's: a bool question whose sure answer leads
	// to the choice that words the denial.
	spec := Spec{
		Point:       PointBeforeTool,
		OnUncertain: FailOpen,
		Entry:       "is_pushing_go_code",
		Steps: map[string]Step{
			"is_pushing_go_code": {
				Question: QuestionSpec{
					Kind:         QuestionBool,
					Instructions: "Is the command committing or pushing Go changes?",
					True:         "committing or pushing",
					False:        "safe to proceed",
				},
				Branches: map[string]Branch{
					"yes": {MinProbability: 0.75, Next: "classify_fix"},
					"no":  {MinProbability: 0.5, Action: &Action{Kind: ActionAllow}},
				},
			},
			"classify_fix": {
				Question: QuestionSpec{
					Kind:         QuestionChoice,
					Instructions: "Which instruction should be given to the agent?",
					Options: []decide.Option{
						{Name: "run_gofmt", Desc: "Tell the agent to run gofmt first"},
						{Name: "allow_anyway", Desc: "False positive; allow the command"},
					},
				},
				Branches: map[string]Branch{
					"run_gofmt":    {MinProbability: 0.70, Action: &Action{Kind: ActionDeny, Message: "run gofmt first"}},
					"allow_anyway": {MinProbability: 0.5, Action: &Action{Kind: ActionAllow}},
				},
			},
		},
	}
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)

	decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
	if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	if !decision.Denied || decision.Detail != "run gofmt first" {
		t.Errorf("the hook decided %+v, want the second step's denial", decision)
	}
	if calls := decider.Calls(); calls != 2 {
		t.Fatalf("the decider was asked %d times, want once per step", calls)
	}
	questions := decider.Questions()
	if len(questions) != 2 {
		t.Fatalf("the decider saw %d question sets", len(questions))
	}
	if id := questions[0][0].ID; id != "hook:pre-push-gofmt:is_pushing_go_code" {
		t.Errorf("the first question's id is %q", id)
	}
	if id := questions[1][0].ID; id != "hook:pre-push-gofmt:classify_fix" {
		t.Errorf("the second question's id is %q", id)
	}
}

func TestAScoreQuestionRoutesOnTheLevelItPicks(t *testing.T) {
	scoreSpec := func() Spec {
		return Spec{
			Point: PointBeforeTool,
			Entry: "score",
			Steps: map[string]Step{
				"score": {
					Question: QuestionSpec{
						Kind:         QuestionScore,
						Instructions: "How much of the tree is unformatted?",
						Levels:       []string{"none", "some", "all"},
					},
					Branches: map[string]Branch{
						"0": {MinProbability: 0.70, Action: &Action{Kind: ActionDeny, Message: "run gofmt first"}},
						"1": {MinProbability: 0.5, Action: &Action{Kind: ActionDeny, Message: "run gofmt first"}},
						"2": {MinProbability: 0.5, Action: &Action{Kind: ActionDeny, Message: "run gofmt first"}},
					},
				},
			},
		}
	}
	answer := func(mass *float64) decide.Decider {
		return decide.DeciderFunc(func(_ context.Context, _ any, _ string, qs []decide.Question) ([]decide.Answer, error) {
			return []decide.Answer{{
				ID:            qs[0].ID,
				Probabilities: map[string]float64{"0": 0.9, "1": 0.06, "2": 0.04},
				LabelMass:     mass,
			}}, nil
		})
	}

	t.Run("the level holds", func(t *testing.T) {
		decider := answer(nil)
		runner := runnerOf(checkHook(t, scoreSpec()), decider)
		if decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push")); err != nil || !decision.Denied {
			t.Fatalf("the answer produced %+v with error %v, want the denial", decision, err)
		}
	})

	t.Run("the label mass scales it under the threshold", func(t *testing.T) {
		mass := 0.5
		decider := answer(&mass)
		runner := runnerOf(checkHook(t, scoreSpec()), decider)
		if decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push")); err != nil || decision.Denied {
			t.Fatalf("the scaled answer produced %+v with error %v, want the neutral decision", decision, err)
		}
	})
}

func TestAMalformedLabelMassFailsTheHook(t *testing.T) {
	cases := []struct {
		name string
		mass float64
	}{
		{name: "above one", mass: 1.5},
		{name: "negative", mass: -0.5},
		{name: "not a number", mass: math.NaN()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
			spec.OnError = FailClosed
			spec.FailClosedMessage = "the answer could not be read"
			mass := tc.mass
			decider := decide.DeciderFunc(func(_ context.Context, _ any, _ string, qs []decide.Question) ([]decide.Answer, error) {
				return []decide.Answer{{
					ID:            qs[0].ID,
					Probabilities: map[string]float64{"yes": 0.9, "no": 0.1},
					LabelMass:     &mass,
				}}, nil
			})
			runner := runnerOf(checkHook(t, spec), decider)

			decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
			if err != nil {
				t.Fatalf("the label mass failed the hook: %v", err)
			}
			if !decision.Denied || decision.Detail != "the answer could not be read" {
				t.Errorf("the answer produced %+v, want the on_error decision", decision)
			}
		})
	}
}

func TestAHookWithNoDecisionModelTakesItsOnErrorPolicy(t *testing.T) {
	spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
	spec.OnError = FailClosed
	spec.FailClosedMessage = "the hook has nothing to ask"
	runner := runnerOf(checkHook(t, spec), nil)

	decision, err := runner.BeforeTool(runContext("run1", nil), bashCall("git push"))
	if err != nil {
		t.Fatalf("a hook with no decision model failed with %v", err)
	}
	if !decision.Denied || decision.Detail != "the hook has nothing to ask" {
		t.Errorf("the hook decided %+v, want the on_error decision", decision)
	}
}

func TestAMalformedAnswerFailsTheHook(t *testing.T) {
	cases := []struct {
		name          string
		probabilities map[string]float64
	}{
		{name: "an answer missing", probabilities: map[string]float64{"yes": 1}},
		{name: "a distribution that does not sum to one", probabilities: map[string]float64{"yes": 0.9, "no": 0.9}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
			spec.OnError = FailClosed
			spec.FailClosedMessage = "the answer could not be read"
			decider := decide.DeciderFunc(func(_ context.Context, _ any, _ string, qs []decide.Question) ([]decide.Answer, error) {
				return []decide.Answer{{ID: qs[0].ID, Probabilities: tc.probabilities}}, nil
			})
			runner := runnerOf(checkHook(t, spec), decider)

			decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
			if err != nil {
				t.Fatalf("a malformed answer failed the hook: %v", err)
			}
			if !decision.Denied || decision.Detail != "the answer could not be read" {
				t.Errorf("the hook decided %+v, want the on_error decision", decision)
			}
		})
	}
}

func TestAProbeThatSeesNothingTakesTheOnErrorPolicy(t *testing.T) {
	probeSpec := func(policy FailurePolicy) Spec {
		spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
		spec.OnError = policy
		if policy == FailClosed {
			spec.FailClosedMessage = "the evidence could not be gathered"
		}
		spec.Context.Probes = []Probe{{Name: "unformatted_files", Command: []string{"gofmt", "-l", "."}}}
		return spec
	}

	t.Run("fail open", func(t *testing.T) {
		decider := pootest.NewStubDecider(0.9)
		runner := runnerOf(checkHook(t, probeSpec(FailOpen)), decider)

		decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
		if err != nil {
			t.Fatalf("the probe failed the hook: %v", err)
		}
		if decision.Denied {
			t.Errorf("the probe produced %+v, want the neutral decision", decision)
		}
		if calls := decider.Calls(); calls != 0 {
			t.Errorf("the decider was asked %d times about evidence that never arrived", calls)
		}
	})

	t.Run("fail closed", func(t *testing.T) {
		decider := pootest.NewStubDecider(0.9)
		runner := runnerOf(checkHook(t, probeSpec(FailClosed)), decider)

		decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
		if err != nil {
			t.Fatalf("the probe failed the hook: %v", err)
		}
		if !decision.Denied || decision.Detail != "the evidence could not be gathered" {
			t.Errorf("the probe produced %+v, want the on_error decision", decision)
		}
	})
}

func TestTheRunnerNamesItsHookAndItsProbesDecideItsIdempotency(t *testing.T) {
	readOnly := oneStep(PointBeforeTool, Action{Kind: ActionAllow})
	readOnly.Context.Probes = []Probe{{Name: "go_mod", Tool: &ToolProbe{Name: "read"}}}
	withCommand := oneStep(PointBeforeTool, Action{Kind: ActionAllow})
	withCommand.Context.Probes = []Probe{{Name: "unformatted_files", Command: []string{"gofmt", "-l", "."}}}

	cases := []struct {
		name       string
		spec       Spec
		idempotent bool
	}{
		{name: "no probes", spec: oneStep(PointBeforeTool, Action{Kind: ActionAllow}), idempotent: true},
		{name: "a tool probe", spec: readOnly, idempotent: true},
		{name: "a command probe", spec: withCommand, idempotent: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := runnerOf(checkHook(t, tc.spec), nil)
			if name := runner.HookName(); name != testHookName {
				t.Errorf("the runner is named %q, want the bare hook name", name)
			}
			if got := runner.Idempotent(); got != tc.idempotent {
				t.Errorf("the runner reports idempotent %v, want %v", got, tc.idempotent)
			}
		})
	}
}
