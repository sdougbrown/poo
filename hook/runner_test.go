package hook

import (
	"context"
	"encoding/json"
	"errors"
	"math"
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
		{name: "every substring", match: Match{Tool: "bash", CommandContains: []string{"git push", "origin"}}, call: bashCall("git push origin main"), want: true},
		{name: "one substring missing", match: Match{Tool: "bash", CommandContains: []string{"git push", "origin"}}, call: bashCall("git push"), want: false},
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
	spec.Match = Match{Tool: "bash", CommandContains: []string{"git push"}}
	spec.MaxFiresPerRun = 1
	decider := pootest.NewStubDecider(0.9)
	runner := runnerOf(checkHook(t, spec), decider)
	hctx := runContext("run1", decider)

	if decision, err := runner.BeforeTool(hctx, bashCall("ls -la")); err != nil || decision.Denied {
		t.Fatalf("an unmatched call produced %+v with error %v", decision, err)
	}
	if decision, err := runner.BeforeTool(hctx, bashCall("git push")); err != nil || !decision.Denied {
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

	other, err := runner.BeforeTool(runContext("run2", decider), bashCall("git push"))
	if err != nil || !other.Denied {
		t.Fatalf("a later run produced %+v with error %v, want its own count", other, err)
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

func TestAHookThatFailsTakesItsOnErrorPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy FailurePolicy
		want   func(t *testing.T, decision poo.ToolDecision, err error)
	}{
		{
			name:   "fail open",
			policy: FailOpen,
			want: func(t *testing.T, decision poo.ToolDecision, err error) {
				if err != nil {
					t.Fatalf("the hook failed with %v, want its neutral decision", err)
				}
				if !decision.Allowed || decision.Denied {
					t.Errorf("the hook decided %+v, want the neutral decision", decision)
				}
			},
		},
		{
			name:   "fail closed",
			policy: FailClosed,
			want: func(t *testing.T, decision poo.ToolDecision, err error) {
				if err != nil {
					t.Fatalf("the hook failed with %v, want its blocking decision", err)
				}
				if !decision.Denied || decision.Detail != "the hook could not answer" {
					t.Errorf("the hook decided %+v, want the blocking decision", decision)
				}
			},
		},
		{
			name:   "fail the run",
			policy: FailRun,
			want: func(t *testing.T, _ poo.ToolDecision, err error) {
				var failure *RunError
				if !errors.As(err, &failure) {
					t.Fatalf("the hook failed with %v, want a *RunError", err)
				}
				if failure.Hook != testHookName || failure.Point != PointBeforeTool {
					t.Errorf("the failure names %q at %q", failure.Hook, failure.Point)
				}
				if !errors.Is(err, decide.ErrDeferred) {
					t.Errorf("the failure does not carry the deferral: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := oneStep(PointBeforeTool, Action{Kind: ActionDeny, Message: "refused"})
			spec.OnError = tc.policy
			if tc.policy == FailClosed {
				spec.FailClosedMessage = "the hook could not answer"
			}
			decider := pootest.NewStubDecider(0.9)
			decider.SetError(decide.ErrDeferred)
			runner := runnerOf(checkHook(t, spec), decider)

			decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
			tc.want(t, decision, err)
		})
	}
}

func TestAnUncertainAnswerTakesItsOnUncertainPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy FailurePolicy
		want   func(t *testing.T, decision poo.ToolDecision, err error)
	}{
		{
			name: "fail open",
			want: func(t *testing.T, decision poo.ToolDecision, err error) {
				if err != nil || !decision.Allowed {
					t.Fatalf("the tie produced %+v with error %v, want the neutral decision", decision, err)
				}
			},
		},
		{
			name:   "fail closed",
			policy: FailClosed,
			want: func(t *testing.T, decision poo.ToolDecision, err error) {
				if err != nil {
					t.Fatalf("the tie failed with %v", err)
				}
				if !decision.Denied || decision.Detail != "the hook could not tell" {
					t.Errorf("the tie produced %+v, want the blocking decision", decision)
				}
			},
		},
		{
			name:   "fail the run",
			policy: FailRun,
			want: func(t *testing.T, _ poo.ToolDecision, err error) {
				var failure *RunError
				if !errors.As(err, &failure) {
					t.Fatalf("the tie failed with %v, want a *RunError", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// An even split is a tie for the best answer, whatever the
			// branches need to be taken.
			spec := Spec{
				Point:       PointBeforeTool,
				OnUncertain: tc.policy,
				Entry:       "ask",
				Steps: map[string]Step{
					"ask": {
						Question: QuestionSpec{Kind: QuestionBool, Instructions: "Does the evidence hold?", True: "it holds", False: "it does not"},
						Branches: map[string]Branch{
							"yes": {MinProbability: 0.4, Action: &Action{Kind: ActionDeny, Message: "refused"}},
							"no":  {MinProbability: 0.4, Action: &Action{Kind: ActionAllow}},
						},
					},
				},
			}
			if tc.policy == FailClosed {
				spec.FailClosedMessage = "the hook could not tell"
			}
			decider := pootest.NewStubDecider(0.5)
			runner := runnerOf(checkHook(t, spec), decider)

			decision, err := runner.BeforeTool(runContext("run1", decider), bashCall("git push"))
			tc.want(t, decision, err)
		})
	}
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

func TestABeforeSettleHookStopsAFailingRunClosed(t *testing.T) {
	spec := oneStep(PointBeforeSettle, Action{Kind: ActionAccept})
	spec.OnError = FailClosed
	spec.FailClosedMessage = "the hook could not judge the settle"
	decider := pootest.NewStubDecider(0.9)
	decider.SetError(decide.ErrDeferred)
	runner := runnerOf(checkHook(t, spec), decider)

	decision, err := runner.BeforeSettle(runContext("run1", decider), poo.ReasonEndTurn)
	if err != nil {
		t.Fatalf("the hook failed with %v, want its blocking decision", err)
	}
	if !decision.Stop || decision.Detail != "the hook could not judge the settle" {
		t.Errorf("the hook decided %+v, want the stop", decision)
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
