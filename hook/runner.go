package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
)

// The time a hook has to answer.
const (
	// minFiringBudget is the smallest time a firing may take. A session
	// whose hook timeout is shorter than the clamp can carry gets the
	// floor: a budget of nothing would fail every hook open.
	minFiringBudget = 500 * time.Millisecond
	// hookDeadlineRoom is what the clamp leaves between the hook's own
	// answer and the deadline callHook holds it to, so a hook that runs
	// out of time answers through its on_error policy instead of being
	// discarded as a missed deadline.
	hookDeadlineRoom = 2 * time.Second
)

// Runner executes one compiled hook at its point.
//
// A runner is the hook's adapter to the five runtime hook points: it
// matches the call, gathers the evidence, walks the compiled graph with
// the session's decision model, and hands back the point's typed
// decision. It owns no lifecycle step of its own — a decision a hook
// takes is carried out by the core's own owners.
//
// One runner serves one session, and its firing counts are that
// session's: the map is in memory, so a recovered session starts each
// run's count again.
type Runner struct {
	// compiled is the checked policy the runner executes.
	compiled *Compiled
	// probes is what a probe runs with.
	probes ProbeEnv
	// hookTimeout is the session's deadline for one hook call.
	hookTimeout time.Duration
	// mu guards fires.
	mu sync.Mutex
	// fires counts the firings of each run or job.
	fires map[string]int
}

var (
	_ poo.BeforeRunHook    = (*Runner)(nil)
	_ poo.BeforeToolHook   = (*Runner)(nil)
	_ poo.AfterToolHook    = (*Runner)(nil)
	_ poo.AfterTurnHook    = (*Runner)(nil)
	_ poo.BeforeSettleHook = (*Runner)(nil)
	_ poo.IdempotentHook   = (*Runner)(nil)
)

// NewRunner returns a runner for a compiled hook.
//
// The hook timeout is the session's deadline for one hook call; the
// runner keeps its own budget under it. The compiled policy is
// read-only: the runner never rewrites what the check accepted.
func NewRunner(compiled *Compiled, probeEnv ProbeEnv, hookTimeout time.Duration) *Runner {
	return &Runner{
		compiled:    compiled,
		probes:      probeEnv,
		hookTimeout: hookTimeout,
		fires:       make(map[string]int),
	}
}

// HookName returns the hook's name.
//
// It is the bare name, so the core's origin renders it as hook:<name>
// without prefixing it twice.
func (r *Runner) HookName() string { return r.compiled.Name }

// Idempotent reports whether the hook may run again after a crash.
//
// A hook that gathers no command probe reads the same evidence whenever
// it is asked, so running it twice decides nothing twice.
func (r *Runner) Idempotent() bool { return r.compiled.Idempotent }

// BeforeRun runs the hook at a run's start.
func (r *Runner) BeforeRun(hctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	point := PointData{Trigger: string(trigger.Kind)}
	action, err := r.evaluate(hctx, matchInput{point: point})
	if action == nil || err != nil {
		return poo.StartDecision{}, err
	}
	if action.Kind == ActionAppend {
		return poo.StartDecision{Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, action.Message)}}, nil
	}
	return poo.StartDecision{}, nil
}

// BeforeTool runs the hook before a call runs.
func (r *Runner) BeforeTool(hctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	point := PointData{Call: callData(info)}
	action, err := r.evaluate(hctx, matchInput{point: point, command: commandOf(info), turn: hctx.Turn})
	if action == nil || err != nil {
		return poo.Allow(), err
	}
	switch action.Kind {
	case ActionDeny:
		return poo.Deny(action.Message), nil
	case ActionShortCircuit:
		return poo.ShortCircuit(poo.ToolResult{Text: action.Message}), nil
	}
	return poo.Allow(), nil
}

// AfterTool runs the hook after a call ran, before the model sees the
// result.
func (r *Runner) AfterTool(hctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
	point := PointData{
		Call:   callData(info),
		Result: &ResultData{Text: result.Text, IsError: result.IsError},
	}
	action, err := r.evaluate(hctx, matchInput{point: point, command: commandOf(info), turn: hctx.Turn})
	if action == nil || err != nil {
		return poo.Keep(), err
	}
	switch action.Kind {
	case ActionAnnotate:
		return poo.Annotate(action.Message), nil
	case ActionReplace:
		return poo.Replace(poo.ToolResult{Text: action.Message, IsError: result.IsError}), nil
	case ActionRedact:
		return poo.Redact(poo.ToolResult{Text: poo.RedactedResultText, IsError: result.IsError}), nil
	}
	return poo.Keep(), nil
}

// AfterTurn runs the hook at a turn's end.
func (r *Runner) AfterTurn(hctx poo.HookContext) (poo.TurnDecision, error) {
	in := matchInput{turn: hctx.Turn, interrupted: hctx.Interrupted}
	action, err := r.evaluate(hctx, in)
	if action == nil || err != nil {
		return poo.TurnDecision{}, err
	}
	switch action.Kind {
	case ActionAppend:
		return poo.AppendMessages(poo.NewTextMessage(poo.RoleUser, action.Message)), nil
	case ActionStop:
		return poo.StopTurn(action.Message), nil
	}
	return poo.TurnDecision{}, nil
}

// BeforeSettle runs the hook at settle, with the reason the run would
// settle under.
func (r *Runner) BeforeSettle(hctx poo.HookContext, reason poo.Reason) (poo.SettleDecision, error) {
	in := matchInput{turn: hctx.Turn, reason: reason}
	action, err := r.evaluate(hctx, in)
	if action == nil || err != nil {
		return poo.Accept(), err
	}
	switch action.Kind {
	case ActionContinue:
		return poo.Continue(poo.NewTextMessage(poo.RoleUser, action.Message)), nil
	case ActionStop:
		return poo.StopSettle(action.Message), nil
	}
	return poo.Accept(), nil
}

// RunError is a hook whose failure fails the run it judged.
//
// The runner hands it to the core, and the core's own finalizer settles
// the run: a hook never finalizes anything itself.
type RunError struct {
	// Hook is the hook's name.
	Hook string
	// Point is where the hook ran.
	Point Point
	// Err is what went wrong: a probe that saw nothing, a question the
	// decision model could not answer, or an answer the hook could not
	// use.
	Err error
}

// Error renders the failure.
func (e *RunError) Error() string {
	return fmt.Sprintf("hook %q at %s: %v", e.Hook, e.Point, e.Err)
}

// Unwrap returns what went wrong.
func (e *RunError) Unwrap() error { return e.Err }

// failureKind says why a firing produced no decision of its own.
type failureKind int

const (
	// failureNone reports that the walk reached a decision.
	failureNone failureKind = iota
	// failureError reports that the hook failed: no decision model, a
	// probe, a malformed answer, or an answer that fits no branch it
	// could route.
	failureError
	// failureUncertain reports that the model answered, and no branch
	// was sure enough to take.
	failureUncertain
)

// matchInput is what a hook's matcher reads about the activity it
// judges.
type matchInput struct {
	// point is the activity, in the shape the evidence names it.
	point PointData
	// command is the call's primary argument, trimmed. It is empty when
	// the call has none.
	command string
	// turn is the turn the hook runs in.
	turn int
	// interrupted reports that the activity was interrupted.
	interrupted bool
	// reason is the settle reason the run would settle under.
	// before_settle only.
	reason poo.Reason
}

// evaluate runs the hook and returns its terminal decision, or nil for
// the point's neutral decision.
//
// The matcher is read before anything else: an unmatched call costs no
// probe and no decision-model call, and it charges no firing. A call a
// hook has already fired its fill on is unmatched too.
func (r *Runner) evaluate(hctx poo.HookContext, in matchInput) (*Action, error) {
	if !evalMatch(r.compiled.Match, in) {
		return nil, nil
	}
	key := fireKey(hctx)
	if r.exhausted(key) {
		return nil, nil
	}
	parent := hctx.Context
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(parent, r.firingBudget())
	defer cancel()

	var (
		action *Action
		kind   = failureNone
		cause  error
	)
	state, err := gatherState(ctx, r.compiled, hctx, in.point, r.probes)
	if err != nil {
		kind, cause = failureError, err
	} else {
		action, kind, cause = r.walkDAG(ctx, hctx, state)
	}
	if kind != failureNone {
		if parent.Err() != nil {
			// The activity was interrupted: the hook's own say is
			// dropped, and an interrupt is not a hook error.
			return nil, nil
		}
		policy := r.compiled.OnError
		if kind == failureUncertain {
			policy = r.compiled.OnUncertain
		}
		if action, err = r.policyAction(policy, cause); err != nil {
			return nil, err
		}
	}
	if action != nil && !neutralAction(r.compiled.Point, action.Kind) {
		// A firing is charged for a decision that changes something. A
		// neutral decision leaves the run as it was, so it is not the
		// hook's say either.
		r.charge(key)
	}
	return action, nil
}

// policyAction takes the policy the hook named for a failure or an
// uncertain answer.
func (r *Runner) policyAction(policy FailurePolicy, cause error) (*Action, error) {
	switch policy {
	case FailRun:
		return nil, &RunError{Hook: r.compiled.Name, Point: r.compiled.Point, Err: cause}
	case FailClosed:
		return closedAction(r.compiled), nil
	}
	return nil, nil
}

// walkDAG asks the graph's questions from its entry and returns the
// terminal decision the answers reach.
func (r *Runner) walkDAG(ctx context.Context, hctx poo.HookContext, state any) (*Action, failureKind, error) {
	if hctx.Decider == nil {
		return nil, failureError, errors.New("the hook has no decision model to ask")
	}
	step, ok := r.compiled.Steps[r.compiled.Entry]
	if !ok {
		return nil, failureError, fmt.Errorf("the hook has no step %q to start at", r.compiled.Entry)
	}
	for depth := 0; ; depth++ {
		if depth > MaxSteps {
			return nil, failureError, fmt.Errorf("the hook took more than %d steps", MaxSteps)
		}
		question := step.Question
		question.ID = "hook:" + r.compiled.Name + ":" + step.Name
		stepCtx, cancel := stepContext(ctx, step.TimeoutMs)
		answers, err := hctx.Decider.Decide(stepCtx, state, question.Instructions, []decide.Question{question})
		expired := stepCtx.Err() != nil
		cancel()
		if expired {
			return nil, failureError, fmt.Errorf("the hook ran out of time at step %q", step.Name)
		}
		if err != nil {
			return nil, failureError, err
		}
		if err := decide.ValidateAnswers([]decide.Question{question}, answers); err != nil {
			return nil, failureError, err
		}
		answer, ok := answerFor(question, answers)
		if !ok {
			return nil, failureError, fmt.Errorf("the decision model answered no question %q", question.ID)
		}
		branch, kind, cause := branchOf(step, question, answer)
		if kind != failureNone {
			return nil, kind, cause
		}
		switch {
		case branch.Action != nil:
			action := *branch.Action
			return &action, failureNone, nil
		case branch.Next != "":
			next, ok := r.compiled.Steps[branch.Next]
			if !ok {
				return nil, failureError, fmt.Errorf("the hook has no step %q", branch.Next)
			}
			step = next
		default:
			return nil, failureError, fmt.Errorf("the step %q takes neither a next step nor an action", step.Name)
		}
	}
}

// answerFor returns the answer to one question.
func answerFor(question decide.Question, answers []decide.Answer) (decide.Answer, bool) {
	for _, answer := range answers {
		if answer.ID == question.ID {
			return answer, true
		}
	}
	return decide.Answer{}, false
}

// branchOf routes one answer to its branch.
//
// The answer's best key is the argmax of its probabilities, scaled by the
// label mass when the endpoint reports one. A tie, a key no branch
// covers, or a score under the branch's threshold is no branch the hook
// may act on, so the answer is uncertain rather than a decision. A label
// mass outside [0, 1] is not a distribution the hook can read, so that is
// an error.
func branchOf(step CompiledStep, question decide.Question, answer decide.Answer) (CompiledBranch, failureKind, error) {
	mass := 1.0
	if answer.LabelMass != nil {
		mass = *answer.LabelMass
		if math.IsNaN(mass) || mass < 0 || mass > 1 {
			return CompiledBranch{}, failureError, fmt.Errorf("the answer to %q carries the label mass %v, which is outside [0, 1]", question.ID, mass)
		}
	}
	best, score, tied := bestKey(question.Keys(), answer.Probabilities, mass)
	switch {
	case best == "":
		return CompiledBranch{}, failureError, fmt.Errorf("the question %q has no answer key", question.ID)
	case tied:
		return CompiledBranch{}, failureUncertain, fmt.Errorf("no answer to %q is the best one", question.ID)
	}
	branch, ok := step.Branches[best]
	if !ok {
		return CompiledBranch{}, failureUncertain, fmt.Errorf("the answer %q to %q leads to no branch", best, question.ID)
	}
	if score < branch.MinProbability {
		return CompiledBranch{}, failureUncertain, fmt.Errorf("the answer %q to %q holds at %v, under the %v its branch needs", best, question.ID, score, branch.MinProbability)
	}
	return branch, failureNone, nil
}

// bestKey returns the key with the highest score, and the score.
//
// The key is empty when there is none, and tied reports that two keys
// hold the same best score: an answer that cannot say which of two
// answers it gave is no decision.
func bestKey(keys []string, probabilities map[string]float64, mass float64) (key string, score float64, tied bool) {
	score = math.Inf(-1)
	for _, candidate := range keys {
		scaled := probabilities[candidate] * mass
		switch {
		case scaled > score:
			key, score, tied = candidate, scaled, false
		case scaled == score:
			tied = true
		}
	}
	if key == "" {
		return "", 0, false
	}
	return key, score, tied
}

// stepContext bounds one step's ask by its own timeout, under the hook's
// firing context.
func stepContext(ctx context.Context, timeoutMs int) (context.Context, context.CancelFunc) {
	if timeoutMs <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
}

// firingBudget is how long the hook has to gather its evidence and
// answer.
//
// The budget sits under the hook call's own deadline, so a hook that runs
// out of time answers with its on_error policy before callHook records a
// missed deadline. A session whose hook timeout is shorter than the clamp
// can carry gets the floor.
func (r *Runner) firingBudget() time.Duration {
	budget := time.Duration(r.compiled.FiringTimeoutMs) * time.Millisecond
	if r.hookTimeout > 0 {
		if limit := r.hookTimeout - hookDeadlineRoom; limit < budget {
			budget = limit
		}
	}
	if budget < minFiringBudget {
		budget = minFiringBudget
	}
	return budget
}

// fireKey names the bucket a firing is counted in.
//
// A run counts its own firings. A background job's late completion names
// the job instead: the run that started the job may already have settled,
// and its count went with it.
func fireKey(hctx poo.HookContext) string {
	if hctx.JobID != "" {
		return "job:" + hctx.JobID
	}
	return "run:" + hctx.RunID
}

// exhausted reports that the call's key has taken every firing the hook
// allows.
func (r *Runner) exhausted(key string) bool {
	if r.compiled.MaxFiresPerRun <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fires[key] >= r.compiled.MaxFiresPerRun
}

// charge counts one firing of the call's key.
func (r *Runner) charge(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fires[key]++
}

// neutralAction reports whether a decision leaves its point as it was.
func neutralAction(point Point, kind ActionKind) bool {
	switch point {
	case PointBeforeRun, PointAfterTurn:
		return kind == ActionPass
	case PointBeforeTool:
		return kind == ActionAllow
	case PointAfterTool:
		return kind == ActionKeep
	case PointBeforeSettle:
		return kind == ActionAccept
	}
	return false
}

// closedAction is the blocking decision a fail_closed policy takes.
func closedAction(compiled *Compiled) *Action {
	kind := blockedKind(compiled.Point)
	if kind == "" {
		// The point has no blocking decision, and Validate refuses a
		// policy that names one. A policy built past the check takes the
		// neutral decision rather than inventing one.
		return nil
	}
	return &Action{Kind: kind, Message: compiled.FailClosedMessage}
}

// blockedKind is the decision a point takes when a hook fails closed.
func blockedKind(point Point) ActionKind {
	switch point {
	case PointBeforeTool:
		return ActionDeny
	case PointAfterTurn, PointBeforeSettle:
		return ActionStop
	}
	return ""
}

// evalMatch reports whether the hook fires on the activity.
//
// Every field a matcher sets must hold, and a matcher that sets nothing
// fires on everything at its point. The command comparison is lexical:
// the call's primary argument has its leading whitespace trimmed and the
// rest is compared as text, so a compound command matches its first
// part.
func evalMatch(match Match, in matchInput) bool {
	if match.Trigger != "" && match.Trigger != in.point.Trigger {
		return false
	}
	if match.Tool != "" && (in.point.Call == nil || match.Tool != in.point.Call.Tool) {
		return false
	}
	if match.Class != "" && (in.point.Call == nil || match.Class != in.point.Call.Class) {
		return false
	}
	if match.CommandPrefix != "" && !strings.HasPrefix(in.command, match.CommandPrefix) {
		return false
	}
	for _, substring := range match.CommandContains {
		if !strings.Contains(in.command, substring) {
			return false
		}
	}
	if match.IsError != nil {
		if in.point.Result == nil || *match.IsError != in.point.Result.IsError {
			return false
		}
	}
	if match.MinTurn != nil && in.turn < *match.MinTurn {
		return false
	}
	if match.Interrupted != nil && *match.Interrupted != in.interrupted {
		return false
	}
	if match.Reason != "" && match.Reason != string(in.reason) {
		return false
	}
	return true
}

// callData is the evidence of the call a tool hook judges.
func callData(info poo.ToolCallInfo) *CallData {
	data := &CallData{
		Tool:      info.Spec.Name,
		Class:     string(info.Spec.Class),
		Arguments: info.Arguments,
	}
	if info.Prepared != nil {
		data.Summary = info.Prepared.Summary()
	}
	return data
}

// commandOf returns the call's primary argument as the command matchers
// read it.
//
// The argument is the one the tool's spec names, read as a top-level
// string with its leading whitespace trimmed. A tool with no primary
// argument, or a call whose arguments do not carry it, has nothing a
// matcher could read.
func commandOf(info poo.ToolCallInfo) string {
	name := info.Spec.PrimaryArg
	if name == "" || len(info.Arguments) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(info.Arguments, &fields); err != nil {
		return ""
	}
	raw, ok := fields[name]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimLeftFunc(value, unicode.IsSpace)
}
