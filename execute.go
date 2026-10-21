package poo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// The visible results the core gives the model for the calls that did
// not run.
const (
	// SkippedTruncatedText is the result of a call in a truncated
	// response: the model was cut off before it could finish the call.
	SkippedTruncatedText = "Not run: your response was cut off at the output limit. Re-issue this call."
	// SkippedInterruptedText is the result of a call that an interrupt
	// cut short before it started.
	SkippedInterruptedText = "Not run: interrupted by the user."
	// CancelledInterruptedText is the result of a call that an
	// interrupt cut short while it ran.
	CancelledInterruptedText = "Cancelled: interrupted by the user."
)

// callPlan is one call of a batch, with everything the core needs to
// run it.
type callPlan struct {
	// handle is the call's handle, e.g. "r1".
	handle string
	// call is the model's request.
	call ToolCall
	// index is the call's position in the batch, counting from zero.
	index int
	// complete reports that the call's arguments are a complete JSON
	// object.
	complete bool
	// invalid reports that the call failed its validation, so it
	// counts toward the invalid near key.
	invalid bool
	// prepared is the validated call, when it validated.
	prepared Prepared
	// prepareErr is the failure the validation produced, when it
	// failed.
	prepareErr error
}

// executeBatch runs a turn's tool calls.
//
// A batch always runs to completion: no stop condition skips the
// remaining calls. The calls run in parallel only when every one of
// them belongs to a tool declared ReadOnly; otherwise they run
// sequentially in model order.
func (s *Session) executeBatch(run *runState, turn int, calls []ToolCall, truncated bool) {
	plans := s.planCalls(run, turn, calls, truncated)
	if len(plans) == 0 {
		return
	}

	s.mu.Lock()
	s.transitionLocked(StateExecuting)
	s.mu.Unlock()

	// The turn's summary is built from the calls the model made, so a
	// hook and a stop condition can see what the turn did.
	s.beginTurn(run, turn, plans)

	// The batch runs under the run's activity context, so an interrupt
	// cancels the calls that are running.
	s.startActivity(run)
	defer s.endActivity(run)

	// The repeat counters are asked about the calls in model order,
	// before the batch runs: the counts depend on the order the model
	// asked for the calls, and a batch that runs in parallel still has
	// that order.
	s.countRepeats(plans)

	if s.batchIsParallel(plans) {
		s.executeParallel(run, turn, plans)
		// The calls that never started because of an interrupt get a
		// visible result.
		if s.interruptPending(run) {
			s.skipInterruptedCalls(run, turn, callsFromPlans(plans))
		}
		return
	}
	for _, p := range plans {
		// An interrupt skips the calls that have not started, and the
		// run continues.
		if s.interruptPending(run) {
			s.skipInterruptedCalls(run, turn, callsFromPlans(plans))
			return
		}
		s.executeOne(run, turn, p)
	}
}

// countRepeats records every call of a batch in the model order.
//
// The counts are the doom loop's, and they depend on the order the
// model asked for the calls: a batch that runs in parallel still has
// the model's order.
//
// A call that failed its validation counts toward the invalid near key,
// because the same malformed call repeated is the same loop.
func (s *Session) countRepeats(plans []callPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repeats == nil {
		s.repeats = newRepeatCounter()
	}
	if s.repeatCounts == nil {
		s.repeatCounts = map[string]repeatCounts{}
	}
	for _, p := range plans {
		primary := s.primaryArgOf(p.call.Name)
		exact := exactKeyOf(p.call.Name, p.call.Arguments)
		near := nearKeyOf(p.call.Name, primary, p.call.Arguments, false)
		if p.invalid {
			exact = "invalid:" + p.call.Name
			near = nearKeyOf(p.call.Name, primary, p.call.Arguments, true)
		}
		counts := s.repeats.count(p.call.Name, exact, near)
		s.repeatCounts[p.handle] = counts
		s.repeatOrder = append(s.repeatOrder, p.handle)
	}
}

// callsFromPlans returns the calls a plan holds.
func callsFromPlans(plans []callPlan) []ToolCall {
	out := make([]ToolCall, 0, len(plans))
	for _, p := range plans {
		out = append(out, p.call)
	}
	return out
}

// beginTurn records the turn's calls in the turn's summary.
//
// The caller holds the session's lock.
func (s *Session) beginTurn(run *runState, turn int, plans []callPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := TurnSummary{Index: turn}
	for _, p := range plans {
		summary.Calls = append(summary.Calls, ToolCallSummary{
			Handle: p.handle,
			Tool:   p.call.Name,
		})
	}
	s.turns = append(s.turns, summary)
}

// planCalls turns a turn's calls into the plan the core runs.
//
// A call in a truncated response is complete when its arguments parse as
// a complete JSON object and pass Prepare. An incomplete call is
// skipped with a visible result.
//
// The plan validates every call, so the repeat counters are asked about
// the calls in the model order and a call that failed its validation is
// counted toward the near key before the batch runs.
func (s *Session) planCalls(run *runState, turn int, calls []ToolCall, truncated bool) []callPlan {
	var plans []callPlan
	for i, call := range calls {
		handle := fmt.Sprintf("r%d", i+1)
		plan := callPlan{handle: handle, call: call, index: i, complete: true}
		if truncated && !isCompleteJSONObject(call.Arguments) {
			plan.complete = false
			plans = append(plans, plan)
			continue
		}
		// The call is validated now, so the repeat counters know
		// which calls failed before the batch runs.
		tool, ok := s.opts.Tools[call.Name]
		if !ok {
			plans = append(plans, plan)
			continue
		}
		prepared, err := tool.Prepare(call.Arguments, s.prepareEnv())
		if err != nil {
			plan.invalid = true
			plan.prepareErr = err
		} else {
			plan.prepared = prepared
		}
		plans = append(plans, plan)
	}
	return plans
}

// isCompleteJSONObject reports that the arguments are a complete JSON
// object.
//
// A truncated response leaves the last call's arguments half-written,
// and a half-written object does not parse.
func isCompleteJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil
}

// batchIsParallel reports that every call in the batch belongs to a
// read-only tool.
//
// Parallel execution happens only when every call is read-only,
// because a side-effecting call may depend on what ran before it.
func (s *Session) batchIsParallel(plans []callPlan) bool {
	if len(plans) < 2 {
		return false
	}
	for _, p := range plans {
		tool, ok := s.opts.Tools[p.call.Name]
		if !ok {
			return false
		}
		if tool.Spec().Class != ClassReadOnly {
			return false
		}
	}
	return true
}

// executeParallel runs a read-only batch at the same time.
func (s *Session) executeParallel(run *runState, turn int, plans []callPlan) {
	var wg sync.WaitGroup
	for _, p := range plans {
		wg.Add(1)
		go func(p callPlan) {
			defer wg.Done()
			// An interrupt that arrived before the call started skips
			// it.
			if s.interruptPending(run) {
				return
			}
			s.executeOne(run, turn, p)
		}(p)
	}
	wg.Wait()
}

// executeOne runs one call and records its result.
//
// A call that was interrupted before it started is skipped; a call that
// was interrupted while it ran is cancelled. A call in a truncated
// response whose arguments are incomplete is skipped with a visible
// result.
func (s *Session) executeOne(run *runState, turn int, plan callPlan) {
	handle := plan.handle
	call := plan.call

	// The call is recorded before it runs, so a log shows what the
	// model asked for even when the call never finished.
	if err := s.write(run.ctx, &ToolCallRecord{
		RecordMeta: NewRecordMetaFrom("tool.call", OriginModel),
		RunID:      run.id,
		Turn:       turn,
		Handle:     handle,
		Tool:       call.Name,
		Arguments:  recordArgs(call.Arguments),
	}); err != nil {
		s.addCandidate(run, newCandidate(ReasonStoreError, SettleDetail{
			Source: "store",
			Text:   err.Error(),
		}))
		return
	}
	s.events.emit(EventToolCall, map[string]any{
		"run_id": run.id,
		"handle": handle,
		"tool":   call.Name,
	})

	// An incomplete call in a truncated response is skipped: the model
	// was cut off before it could finish the call.
	if !plan.complete {
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    SkippedTruncatedText,
			IsError: true,
		}, OutcomeSkipped)
		return
	}

	tool, ok := s.opts.Tools[call.Name]
	if !ok {
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    fmt.Sprintf("unknown tool %q", call.Name),
			IsError: true,
		}, OutcomeError)
		return
	}

	// The call's outcome is decided by the core, not by the tool.
	prepared := plan.prepared
	if prepared == nil {
		// The call failed its validation. The failure is a recoverable
		// error: the model sees the message and may correct the call.
		text := "the call was not prepared"
		if plan.prepareErr != nil {
			text = plan.prepareErr.Error()
		}
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    text,
			IsError: true,
		}, OutcomeError)
		return
	}

	// The repeat counters were asked about the call before the batch
	// ran, so the counts the policy slot sees are the ones the plan
	// names.

	// The call has been issued, so it needs a result even if the run
	// settles before it finishes.
	s.issueCall(run, turn, handle, call.Name, call.ID)

	result, err := tool.Call(s.activityContextFor(run), prepared, s.toolEnv(run, handle))
	if err == nil && s.wasInterrupted(run) {
		// The call was cut short by an interrupt, even though the tool
		// returned a result: the tool saw the cancellation and gave
		// back what it had. The outcome says the call was cancelled.
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    result.Text,
			IsError: true,
		}, OutcomeCancelled)
		return
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, errInterrupted) {
			// The call was cut short. The outcome says which: a call
			// that never started is skipped, and one that was running
			// is cancelled.
			if s.wasInterrupted(run) {
				s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
					Text:    CancelledInterruptedText,
					IsError: true,
				}, OutcomeCancelled)
				return
			}
			s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
				Text:    err.Error(),
				IsError: true,
			}, OutcomeCancelled)
			return
		}
		// A tool error that wraps ErrFatal is the environment being
		// unusable: the run settles tool_fatal.
		if errors.Is(err, ErrFatal) {
			s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
				Text:    err.Error(),
				IsError: true,
			}, OutcomeError)
			s.addCandidate(run, newCandidate(ReasonToolFatal, SettleDetail{
				Source: "tool:" + call.Name,
				Text:   err.Error(),
			}))
			return
		}
		// Any other error is recoverable: it goes back to the model
		// and the run continues.
		s.recordResult(run, turn, handle, call.Name, call.ID, ToolResult{
			Text:    err.Error(),
			IsError: true,
		}, OutcomeError)
		return
	}

	// A valid finish call settles the run as finished, with the result.
	if call.Name == FinishToolName {
		if raw, ok := prepared.(FinishResult); ok {
			s.addCandidate(run, newCandidate(ReasonFinished, SettleDetail{
				Source: "finish",
				Text:   "the model called finish with a valid result",
				Result: raw.FinishResult(),
			}))
		}
	}

	s.recordResult(run, turn, handle, call.Name, call.ID, result, OutcomeOK)
}

// prepareEnv builds the environment a tool validates its arguments in.
func (s *Session) prepareEnv() PrepareEnv {
	return PrepareEnv{
		WorkingDir:     s.opts.WorkingDir,
		ProtectedPaths: s.opts.ProtectedPaths,
	}
}

// toolEnv builds the environment a tool runs in.
func (s *Session) toolEnv(run *runState, handle string) ToolEnv {
	return ToolEnv{
		PrepareEnv: s.prepareEnv(),
		SessionID:  s.opts.SessionID,
		RunID:      run.id,
		Handle:     handle,
		Env:        s.opts.Env,
		Settings:   s.opts.Settings,
	}
}

// recordResult records a tool result and appends it to the history.
//
// The result goes back to the model under the call's provider-side id,
// because that is the id the provider matches it against.
func (s *Session) recordResult(run *runState, turn int, handle, tool, callID string, result ToolResult, outcome Outcome) {
	// The call has its result now, so it no longer needs a synthetic
	// one from the finalization.
	s.completeCall(run, handle)

	if err := s.write(run.ctx, &ToolResultRecord{
		RecordMeta: NewRecordMetaFrom("tool.result", OriginTool),
		RunID:      run.id,
		Turn:       turn,
		Handle:     handle,
		Tool:       tool,
		Outcome:    outcome,
		Text:       result.Text,
		Structured: result.Structured,
	}); err != nil {
		s.addCandidate(run, newCandidate(ReasonStoreError, SettleDetail{
			Source: "store",
			Text:   err.Error(),
		}))
	}

	// The result goes into the model's view and the turn's summary.
	msg := ToolResultMessage(callID, tool, result.Text, result.IsError)
	msg.Origin = OriginTool
	s.appendMessages(msg)
	s.rememberResult(run, turn, handle, tool, outcome)

	_ = s.write(run.ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", OriginTool),
		RunID:      run.id,
		Turn:       turn,
		Role:       msg.Role,
		Content:    msg.Content,
	})

	s.events.emit(EventToolResult, map[string]any{
		"run_id":  run.id,
		"handle":  handle,
		"tool":    tool,
		"outcome": string(outcome),
		"text":    result.Text,
	})
}

// rememberResult records a call's outcome in the turn's summary.
func (s *Session) rememberResult(run *runState, turn int, handle, tool string, outcome Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[handle] = ToolResult{}
	for i := range s.turns {
		if s.turns[i].Index != turn {
			continue
		}
		for j := range s.turns[i].Calls {
			if s.turns[i].Calls[j].Handle == handle {
				s.turns[i].Calls[j].Outcome = outcome
				return
			}
		}
	}
}

// issueCall records that a call is running, so the finalization can
// give it a synthetic result if it never finishes.
func (s *Session) issueCall(run *runState, turn int, handle, tool, callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.pendingCalls == nil {
		run.pendingCalls = map[string]*issuedCall{}
	}
	run.pendingCalls[handle] = &issuedCall{
		handle:  handle,
		tool:    tool,
		id:      callID,
		turn:    turn,
		started: true,
	}
	if run.workDone == nil {
		run.workDone = make(chan struct{}, 1)
	}
}

// completeCall forgets a call that has its result.
func (s *Session) completeCall(run *runState, handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(run.pendingCalls, handle)
	if run.workDone != nil {
		select {
		case run.workDone <- struct{}{}:
		default:
		}
	}
}

// addCandidate adds a settle candidate to the run.
//
// A candidate arriving after the commit point is logged and ignored: it
// never mutates a settled run.
func (s *Session) addCandidate(run *runState, c candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.committed {
		return
	}
	run.candidates = append(run.candidates, c)
}

// FinishToolName is the name the typed-result tool is called by.
//
// The core does not build the tool: a consumer links poo/tool/finish and
// adds it to the session's tools. The core recognizes a finished call by
// this name and by the prepared call carrying a result.
const FinishToolName = "finish"

// FinishResult is implemented by a prepared call that carries a typed
// result.
//
// The core reads the result off the prepared call when the batch
// completes, and the run settles as finished with it.
type FinishResult interface {
	// FinishResult returns the validated result.
	FinishResult() json.RawMessage
}

// ErrFatal marks a tool error as fatal.
//
// A fatal error means the environment is unusable — the working
// directory is gone, for instance — and the run settles tool_fatal.
var ErrFatal = errors.New("poo: the environment is unusable")

// Fatalf wraps a message in a fatal error.
func Fatalf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFatal, fmt.Sprintf(format, args...))
}

// repeatCounter counts the consecutive repeats of a call in a run.
//
// The counters are the doom loop's: an exact key is the tool plus its
// canonical input, and a near key is the tool plus its primary
// argument. A Prepare failure counts toward the near key, because the
// same malformed call repeated is the same loop.
//
// The counters count consecutive repeats, and a call to another tool
// does not break the streak: a run that reads the same file five times
// with another tool in between is still a loop.
type repeatCounter struct {
	// exact counts the repeats of each exact key.
	exact map[string]int
	// near counts the repeats of each near key.
	near map[string]int
	// lastTool is the tool the last call was for.
	lastTool string
}

// repeatCounts is what a call's repeat counters hold.
type repeatCounts struct {
	// exact is how many times the model made this exact call.
	exact int
	// near is how many times the model made a call with the same
	// primary argument.
	near int
}

// newRepeatCounter returns an empty counter.
func newRepeatCounter() *repeatCounter {
	return &repeatCounter{exact: map[string]int{}, near: map[string]int{}}
}

// count records a call and returns the counts it had.
//
// The counts are the number of times the model has made this call, and
// the number of times it has made a call with the same primary
// argument, since the last call to another tool.
func (c *repeatCounter) count(tool, exactKey, nearKey string) repeatCounts {
	if tool != c.lastTool {
		// A call to another tool breaks the streak.
		c.exact = map[string]int{}
		c.near = map[string]int{}
		c.lastTool = tool
	}
	c.exact[exactKey]++
	c.near[nearKey]++
	return repeatCounts{exact: c.exact[exactKey], near: c.near[nearKey]}
}

// primaryArgOf returns the name of a tool's primary argument.
func (s *Session) primaryArgOf(name string) string {
	tool, ok := s.opts.Tools[name]
	if !ok {
		return ""
	}
	return tool.Spec().PrimaryArg
}

// exactKeyOf returns the exact repeat key of a call.
//
// The key is the tool plus its canonical arguments.
func exactKeyOf(name string, args json.RawMessage) string {
	return name + ":" + canonicalJSON(args)
}

// nearKeyOf returns the near repeat key of a call.
//
// The key is the tool plus its primary argument. A call that failed its
// validation has no valid primary argument, and the key says so.
func nearKeyOf(name, primary string, args json.RawMessage, invalid bool) string {
	if invalid {
		value := ""
		if primary != "" {
			value = primaryArgValue(args, primary)
		}
		if value == "" {
			return name + ":invalid"
		}
		return name + ":invalid:" + value
	}
	if primary == "" {
		return name
	}
	return name + ":" + primaryArgValue(args, primary)
}

// primaryArgValue returns the raw value of a top-level string argument.
//
// The value is the raw text, so two calls that spell the same argument
// the same way share a key.
func primaryArgValue(args json.RawMessage, name string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return ""
	}
	raw, ok := obj[name]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// canonicalJSON returns the arguments in a canonical form.
//
// The keys are sorted, so two calls that spell the same arguments in a
// different order share an exact key.
func canonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// sortedCalls returns the calls of a turn, in order.
func sortedCalls(calls []ToolCallSummary) []ToolCallSummary {
	out := append([]ToolCallSummary(nil), calls...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Handle < out[j].Handle })
	return out
}

// joinSummary joins the summaries of the calls of a turn.
func joinSummary(calls []ToolCallSummary) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, c.Tool)
	}
	return strings.Join(parts, ", ")
}
