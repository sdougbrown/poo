package poo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
)

// The hook decisions are the plan's: a before-tool hook allows, denies or
// short-circuits; an after-tool hook keeps, annotates, replaces or
// redacts; a before-settle hook accepts, continues or stops.

// TestTheToolDecisions covers the before-tool decisions: the zero value
// allows, and each named decision reads the way the plan says.
func TestTheToolDecisions(t *testing.T) {
	allow := poo.Allow()
	if !allow.Allowed || allow.Denied {
		t.Errorf("the allow decision is %+v, want an allowance", allow)
	}

	deny := poo.Deny("the command is not allowed")
	if !deny.Denied {
		t.Errorf("the deny decision is %+v, want a denial", deny)
	}
	if deny.Detail != "the command is not allowed" {
		t.Errorf("the denial's detail is %q, want the explanation", deny.Detail)
	}

	short := poo.ShortCircuit(poo.ToolResult{Text: "the answer"})
	if !short.Allowed {
		t.Errorf("the short circuit is %+v, want an allowance", short)
	}
	if short.Result == nil {
		t.Fatal("the short circuit holds no result")
	}
	if short.Result.Text != "the answer" {
		t.Errorf("the short circuit's result is %q, want the answer", short.Result.Text)
	}
}

// TestTheResultDecisions covers the after-tool decisions.
func TestTheResultDecisions(t *testing.T) {
	keep := poo.Keep()
	if !keep.Kept || keep.Redacted || keep.Replaced != nil {
		t.Errorf("the keep decision is %+v, want a plain keep", keep)
	}

	annotate := poo.Annotate("and it passed")
	if !annotate.Kept {
		t.Errorf("the annotate decision is %+v, want a keep with text", annotate)
	}
	if annotate.Annotated != "and it passed" {
		t.Errorf("the annotation is %q, want the text", annotate.Annotated)
	}

	replace := poo.Replace(poo.ToolResult{Text: "the replacement"})
	if replace.Replaced == nil {
		t.Fatal("the replace decision holds no result")
	}
	if replace.Replaced.Text != "the replacement" {
		t.Errorf("the replacement is %q, want the replacement", replace.Replaced.Text)
	}

	redact := poo.Redact()
	if !redact.Redacted {
		t.Errorf("the redact decision is %+v, want a redaction", redact)
	}
}

// TestTheSettleDecisions covers the before-settle decisions.
func TestTheSettleDecisions(t *testing.T) {
	accept := poo.Accept()
	if !accept.Accepted {
		t.Errorf("the accept decision is %+v, want an acceptance", accept)
	}

	msg := poo.NewTextMessage(poo.RoleUser, "keep going")
	cont := poo.Continue(msg)
	if cont.Continue == nil {
		t.Fatal("the continue decision holds no message")
	}
	if cont.Continue.Text() != "keep going" {
		t.Errorf("the continuation is %q, want the message", cont.Continue.Text())
	}

	stop := poo.StopSettle("the answer is good enough")
	if !stop.Stop {
		t.Errorf("the stop decision is %+v, want a stop", stop)
	}
	if stop.Detail != "the answer is good enough" {
		t.Errorf("the stop's detail is %q, want the explanation", stop.Detail)
	}
}

// TestTheTurnDecisions covers the after-turn decisions.
func TestTheTurnDecisions(t *testing.T) {
	append := poo.AppendMessages(poo.NewTextMessage(poo.RoleUser, "more context"))
	if len(append.Append) != 1 {
		t.Fatalf("the decision appends %d messages, want 1", len(append.Append))
	}
	if append.Stop {
		t.Error("the append decision stops the run")
	}

	stop := poo.StopTurn("the task is done")
	if !stop.Stop {
		t.Errorf("the stop decision is %+v, want a stop", stop)
	}
}

// TestTheHookContextCarriesTheRun covers the hook context: the run's
// identity, the turn, the history as a view, the budget and the decider.
func TestTheHookContextCarriesTheRun(t *testing.T) {
	ctx := poo.HookContext{
		Context:     context.Background(),
		SessionID:   "s1",
		RunID:       "r1",
		Turn:        3,
		Interrupted: true,
		History:     poo.NewHistory(nil, nil, nil),
		Budget:      poo.Budget{ContextLimit: 32768, EstimatedInput: 100, CharsPerToken: 3.5},
		Decider:     decide.DeciderFunc(nil),
	}
	if ctx.SessionID != "s1" || ctx.RunID != "r1" {
		t.Errorf("the context names %q and %q, want s1 and r1", ctx.SessionID, ctx.RunID)
	}
	if ctx.Turn != 3 {
		t.Errorf("the context's turn is %d, want 3", ctx.Turn)
	}
	if !ctx.Interrupted {
		t.Error("the context does not report the interruption")
	}
	if ctx.History == nil {
		t.Error("the context holds no history")
	}
	if ctx.Budget.ContextLimit != 32768 {
		t.Errorf("the budget's limit is %d, want 32768", ctx.Budget.ContextLimit)
	}
	if ctx.Decider == nil {
		t.Error("the context holds no decider")
	}
}

// TestTheHookInterfacesAreThePlanShapes covers the hook points: a value
// that implements the plan's interface satisfies it.
func TestTheHookInterfacesAreThePlanShapes(t *testing.T) {
	var (
		_ poo.SessionStartHook = sessionStartHook{}
		_ poo.BeforeRunHook    = beforeRunHook{}
		_ poo.BeforeToolHook   = beforeToolHook{}
		_ poo.AfterToolHook    = afterToolHook{}
		_ poo.AfterTurnHook    = afterTurnHook{}
		_ poo.BeforeSettleHook = beforeSettleHook{}
		_ poo.Observer         = observer{}
		// The policy slot is a before-tool hook.
		_ poo.PolicyHook = beforeToolHook{}
	)
}

// sessionStartHook is a session-start hook.
type sessionStartHook struct{}

func (sessionStartHook) SessionStart(poo.HookContext) (poo.StartDecision, error) {
	return poo.StartDecision{}, nil
}

// beforeRunHook is a before-run hook.
type beforeRunHook struct{}

func (beforeRunHook) BeforeRun(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
	return poo.StartDecision{}, nil
}

// beforeToolHook is a before-tool hook.
type beforeToolHook struct{}

func (beforeToolHook) BeforeTool(poo.HookContext, poo.ToolCallInfo) (poo.ToolDecision, error) {
	return poo.Allow(), nil
}

// afterToolHook is an after-tool hook.
type afterToolHook struct{}

func (afterToolHook) AfterTool(poo.HookContext, poo.ToolCallInfo, poo.ToolResult) (poo.ResultDecision, error) {
	return poo.Keep(), nil
}

// afterTurnHook is an after-turn hook.
type afterTurnHook struct{}

func (afterTurnHook) AfterTurn(poo.HookContext) (poo.TurnDecision, error) {
	return poo.TurnDecision{}, nil
}

// beforeSettleHook is a before-settle hook.
type beforeSettleHook struct{}

func (beforeSettleHook) BeforeSettle(poo.HookContext, poo.Reason) (poo.SettleDecision, error) {
	return poo.Accept(), nil
}

// observer is an observer.
type observer struct{}

func (observer) Observe(poo.Event) error { return nil }

// TestTheStartDecisionCarriesThePromptEdit covers the system-prompt edit
// a start decision may carry: it edits the named sections.
func TestTheStartDecisionCarriesThePromptEdit(t *testing.T) {
	sections := &fakeSections{}
	decision := poo.StartDecision{
		SystemPrompt: func(s prompt.Sections) {
			s.Set("base", "the new base")
			s.Remove("project")
		},
	}
	decision.SystemPrompt(sections)

	if sections.set["base"] != "the new base" {
		t.Errorf("the base section is %q, want the new base", sections.set["base"])
	}
	if !sections.removed["project"] {
		t.Error("the project section was not removed")
	}
}

// fakeSections records the edits made to it.
type fakeSections struct {
	// set holds the sections that were set.
	set map[string]string
	// removed holds the sections that were removed.
	removed map[string]bool
	// names holds the section names, in order.
	names []string
}

// Set records a section.
func (s *fakeSections) Set(name, text string) {
	if s.set == nil {
		s.set = map[string]string{}
	}
	s.set[name] = text
}

// Append records an appended section.
func (s *fakeSections) Append(name, text string) {
	if s.set == nil {
		s.set = map[string]string{}
	}
	s.set[name] += text
}

// Remove records a removed section.
func (s *fakeSections) Remove(name string) {
	if s.removed == nil {
		s.removed = map[string]bool{}
	}
	s.removed[name] = true
}

// Insert records an inserted section.
func (s *fakeSections) Insert(after, name, text string) {
	s.names = append(s.names, name)
}

// Replace records a replaced prompt.
func (s *fakeSections) Replace(text string) {
	s.set = map[string]string{"": text}
}

// Names returns the section names.
func (s *fakeSections) Names() []string { return s.names }

// TestTheToolCallInfoCarriesTheCall covers the call info a tool hook is
// given: the spec, the prepared call, the handle and the repeat counts.
func TestTheToolCallInfoCarriesTheCall(t *testing.T) {
	info := poo.ToolCallInfo{
		Spec:            poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting},
		Handle:          "r1",
		RepeatCount:     2,
		NearRepeatCount: 5,
		BatchIndex:      0,
	}
	if info.Spec.Name != "bash" {
		t.Errorf("the spec's name is %q, want bash", info.Spec.Name)
	}
	if info.Handle != "r1" {
		t.Errorf("the handle is %q, want r1", info.Handle)
	}
	if info.RepeatCount != 2 || info.NearRepeatCount != 5 {
		t.Errorf("the repeat counts are %d and %d, want 2 and 5", info.RepeatCount, info.NearRepeatCount)
	}
}

// TestTheEventNamesAreThePlanNames covers the event names: the set the
// plan names, spelled the way the wire spells them.
func TestTheEventNamesAreThePlanNames(t *testing.T) {
	cases := map[poo.EventName]string{
		poo.EventRunStarted:      "run.started",
		poo.EventRunSettled:      "run.settled",
		poo.EventInputDelivered:  "input.delivered",
		poo.EventJobStarted:      "job.started",
		poo.EventMessageDelta:    "message.delta",
		poo.EventToolResult:      "tool.result",
		poo.EventPermissionAsked: "permission.asked",
		poo.EventDecision:        "decision",
		poo.EventUsage:           "usage",
		poo.EventContextNotice:   "context.notice",
		poo.EventCompactionEnded: "compaction.ended",
		poo.EventObserverDropped: "observer.dropped",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("the event name is %q, want %q", got, want)
		}
	}
}

// TestTheRunTriggerCarriesTheJobs covers the trigger: a prompt run and a
// job run, and the job results that woke the session.
func TestTheRunTriggerCarriesTheJobs(t *testing.T) {
	prompt := poo.RunTrigger{Kind: poo.TriggerPrompt, Prompt: "do the thing"}
	if prompt.Kind != "prompt" {
		t.Errorf("the trigger's kind is %q, want prompt", prompt.Kind)
	}

	job := poo.RunTrigger{
		Kind: poo.TriggerJob,
		Jobs: []poo.JobResult{{
			Job:     "j3",
			Handle:  "j3",
			Tool:    "bash",
			Text:    "the result",
			Outcome: poo.OutcomeOK,
		}},
	}
	if job.Kind != "job" {
		t.Errorf("the trigger's kind is %q, want job", job.Kind)
	}
	if len(job.Jobs) != 1 {
		t.Fatalf("the trigger carries %d jobs, want 1", len(job.Jobs))
	}
	if job.Jobs[0].Job != "j3" {
		t.Errorf("the job is %q, want j3", job.Jobs[0].Job)
	}
}

// TestTheJobResultRecordsRoundTrip covers the job result as a record
// field: it reaches the log.
func TestTheJobResultRecordsRoundTrip(t *testing.T) {
	started := &poo.RunStarted{
		RecordMeta: poo.NewRecordMeta("run.started"),
		RunID:      "r1",
		Trigger:    poo.TriggerJob,
		Jobs: []poo.JobResult{{
			Job:     "j3",
			Handle:  "j3",
			Tool:    "bash",
			Text:    "the result",
			Outcome: poo.OutcomeOK,
		}},
	}
	raw, err := json.Marshal(started)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var back poo.RunStarted
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if back.Trigger != poo.TriggerJob {
		t.Errorf("the trigger is %q, want job", back.Trigger)
	}
	if len(back.Jobs) != 1 || back.Jobs[0].Job != "j3" {
		t.Errorf("the jobs are %+v, want one j3", back.Jobs)
	}
}
