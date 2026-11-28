package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// planNow is the clock the durable planner's tests run on.
var planNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// planClock returns planNow, so a deadline's meaning does not drift with
// the test's own run time.
func planClock() time.Time { return planNow }

// durableTools are the tools a durable fixture session was created with: a
// plain side-effecting tool, a read-only one, and one that declares its
// repetition safe.
func durableTools() []poo.ToolInfo {
	return []poo.ToolInfo{
		{Name: "bash", Class: poo.ClassSideEffecting},
		{Name: "read", Class: poo.ClassReadOnly},
		{Name: "write", Class: poo.ClassSideEffecting, Idempotent: true},
	}
}

// durableHeader returns a durable session's header, with the tools the
// planner's fallback lookup reads.
func durableHeader(id string, tools ...poo.ToolInfo) *poo.SessionCreatedRecord {
	rec := createdRecord(id)
	rec.Mode = string(ModeDurable)
	rec.Tools = tools
	if len(rec.Tools) == 0 {
		rec.Tools = durableTools()
	}
	return rec
}

// planSink collects the records a plan writes.
//
// A sink with failAfter set refuses every append after that many, which is
// the state a crash in the middle of a recovery leaves.
type planSink struct {
	records   []poo.Record
	failAfter int
	appends   int
}

// Append collects one record.
func (s *planSink) Append(_ context.Context, rec poo.Record) error {
	if s.failAfter > 0 && s.appends >= s.failAfter {
		return errInjected
	}
	s.appends++
	s.records = append(s.records, rec)
	return nil
}

// Flush writes nothing: the sink holds its records in memory.
func (s *planSink) Flush(context.Context) error { return nil }

// planOptions returns the options a durable plan is made with.
func planOptions() DurableOptions {
	return DurableOptions{Now: planClock, Tools: durableTools()}
}

// planOf plans the pending operations of some records.
func planOf(t *testing.T, recs []poo.Record, opts DurableOptions) (*ContinuationPlan, *planSink) {
	t.Helper()
	sink := &planSink{}
	plan, err := DurablePlanner{Options: opts}.Plan(context.Background(), sink, pendingOf("s1", recs))
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	return plan, sink
}

// recordNamed returns the records of a name a plan wrote, in order.
func recordNamed(recs []poo.Record, name string) []poo.Record {
	var out []poo.Record
	for _, rec := range recs {
		if rec.Meta().Record == name {
			out = append(out, rec)
		}
	}
	return out
}

// toolStarted returns the write-ahead record of a call that began.
func toolStarted(runID string, turn int, handle, tool, callID string) *poo.ToolStartedRecord {
	return &poo.ToolStartedRecord{
		RecordMeta: poo.NewRecordMeta("tool.started"),
		RunID:      runID,
		Turn:       turn,
		Handle:     handle,
		CallID:     callID,
		Tool:       tool,
	}
}

// runResumed returns a record of a run that resumed.
func runResumed(runID string, attempt int, from poo.State, turn int) *poo.RunResumedRecord {
	return &poo.RunResumedRecord{
		RecordMeta:        poo.NewRecordMeta("run.resumed"),
		RunID:             runID,
		ResumptionAttempt: attempt,
		FromPhase:         from,
		Turn:              turn,
	}
}

// turnHookRecord returns a turn hook's start or end record.
func turnHookRecord(runID string, turn int, phase, status string, index int, interrupted bool) *poo.TurnHookRecord {
	return &poo.TurnHookRecord{
		RecordMeta:  poo.NewRecordMeta("turn.hook"),
		RunID:       runID,
		Turn:        turn,
		Phase:       phase,
		Status:      status,
		HookIndex:   index,
		Interrupted: interrupted,
	}
}

// askedAt returns a permission ask with a deadline and a fallback.
func askedAt(runID, askID, tool, deadline, fallback string) *poo.PermissionAskedRecord {
	rec := permissionAsked(runID, askID, tool)
	rec.Summary = "the call writes a file"
	rec.Class = poo.ClassSideEffecting
	rec.Reason = poo.AskReasonPolicy
	rec.Deadline = deadline
	rec.Fallback = fallback
	return rec
}

// feedSubscribed returns the record of a feed instance that subscribed.
func feedSubscribed(feed, instance, scope, prompt string) *poo.FeedSubscribedRecord {
	return &poo.FeedSubscribedRecord{
		RecordMeta: poo.NewRecordMetaFrom("feed.subscribed", poo.FeedOrigin(feed)),
		Feed:       feed,
		Instance:   instance,
		Scope:      scope,
		Prompt:     prompt,
	}
}

// feedUnsubscribed returns the record of a feed instance that unsubscribed.
func feedUnsubscribed(feed, instance string) *poo.FeedUnsubscribedRecord {
	return &poo.FeedUnsubscribedRecord{
		RecordMeta: poo.NewRecordMetaFrom("feed.unsubscribed", poo.FeedOrigin(feed)),
		Feed:       feed,
		Instance:   instance,
	}
}

// assistantMessage returns an assistant message, partial when asked for.
func assistantMessage(runID string, turn int, callID, tool string, partial bool) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      runID,
		Turn:       turn,
		Role:       poo.RoleAssistant,
		MessageID:  "m1",
		Partial:    partial,
		Content: []poo.ContentBlock{{
			Kind:       poo.ContentToolCall,
			ToolCallID: callID,
			Name:       tool,
			Arguments:  json.RawMessage(`{"text":"one"}`),
		}},
	}
}

// compacted returns a run's compaction record.
func compacted(runID string) *poo.CompactionRecord {
	return &poo.CompactionRecord{
		RecordMeta:   poo.NewRecordMeta("compaction"),
		RunID:        runID,
		Strategy:     "window",
		Form:         "window",
		TokensBefore: 100,
		TokensAfter:  10,
	}
}

// queuedFollowUp returns the record of a queued follow-up.
func queuedFollowUp(inputID, text string, note *poo.DeliveryNote) *poo.InputQueuedRecord {
	rec := inputQueued(inputID, poo.FollowUp)
	rec.Text = text
	rec.DeliveryNote = note
	return rec
}

// TestDurablePlanDerivesThePhaseFromTheCrashPoint covers the phase the plan
// resumes from: every crash point a durable run leaves has one reading, and
// the plan names it.
func TestDurablePlanDerivesThePhaseFromTheCrashPoint(t *testing.T) {
	deadline := planNow.Add(-time.Minute).Format(time.RFC3339Nano)
	cases := []struct {
		name  string
		recs  []poo.Record
		phase poo.State
		hook  *HookResume
		check func(t *testing.T, plan *ContinuationPlan, sink *planSink)
	}{
		{
			name: "no unsettled run",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				&poo.RunSettledRecord{
					RecordMeta: poo.NewRecordMeta("run.settled"),
					RunID:      "r1",
					Reason:     poo.ReasonEndTurn,
					Detail:     poo.SettleDetail{Source: "model"},
					SettledAt:  "2026-10-20T00:00:01Z",
				},
			},
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if plan != nil {
					t.Errorf("the plan is %+v, want nil for a settled session", plan)
				}
				if len(sink.records) != 0 {
					t.Errorf("the plan wrote %v, want nothing", recordNames(sink.records))
				}
			},
		},
		{
			name: "the run stopped in an after-turn hook",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				assistantMessage("r1", 1, "call_1", "read", false),
				toolCallWithID("r1", 1, "r1", "read", "call_1"),
				toolResult("r1", 1, "r1", "read", poo.OutcomeOK),
				turnHookRecord("r1", 1, hookPhaseAfterTurn, "started", 1, false),
			},
			phase: poo.StateExecuting,
			hook:  &HookResume{Phase: hookPhaseAfterTurn, HookIndex: 1},
		},
		{
			name: "the run stopped in a hook of an interrupted turn",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				turnHookRecord("r1", 1, hookPhaseAfterTurn, "started", 0, true),
			},
			phase: poo.StateExecuting,
			hook:  &HookResume{Phase: hookPhaseAfterTurn, Interrupted: true},
		},
		{
			name: "the run stopped in a settle hook",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				assistantMessage("r1", 1, "", "", false),
				turnHookRecord("r1", 1, hookPhaseBeforeSettle, "started", 0, false),
			},
			phase: poo.StateRequesting,
			hook:  &HookResume{Phase: hookPhaseBeforeSettle, HookIndex: 0},
		},
		{
			name: "a hook that ended leaves no hook to resume",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				turnHookRecord("r1", 1, hookPhaseAfterTurn, "started", 0, false),
				turnHookRecord("r1", 1, hookPhaseAfterTurn, "ended", 0, false),
			},
			phase: poo.StateRequesting,
		},
		{
			name: "a job holds the run open",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				jobStarted("j1", "r1", "bash", "r1"),
			},
			phase: poo.StateWaiting,
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if len(plan.ReplayCalls) != 0 {
					t.Errorf("the plan replays %d calls, want none", len(plan.ReplayCalls))
				}
				ends := recordNamed(sink.records, "job.ended")
				if len(ends) != 1 {
					t.Fatalf("the plan wrote %d job ends, want 1", len(ends))
				}
				if ended := ends[0].(*poo.JobEndedRecord); ended.Outcome != poo.OutcomeInterrupted {
					t.Errorf("the job's outcome is %q, want %q", ended.Outcome, poo.OutcomeInterrupted)
				}
			},
		},
		{
			name: "a call to replay outranks a job",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				toolCall("r1", 1, "r1", "read"),
				jobStarted("j1", "r1", "bash", "r1"),
			},
			phase: poo.StateExecuting,
		},
		{
			name: "a live ask outranks a job",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				toolCall("r1", 1, "r1", "bash"),
				askedAt("r1", "a1", "bash", planNow.Add(time.Minute).Format(time.RFC3339Nano), "deny"),
				jobStarted("j1", "r1", "bash", "r1"),
			},
			phase: poo.StateExecuting,
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if len(plan.LiveAsks) != 1 {
					t.Fatalf("the plan holds %d live asks, want 1", len(plan.LiveAsks))
				}
				ends := recordNamed(sink.records, "job.ended")
				if len(ends) != 1 {
					t.Fatalf("the plan wrote %d job ends, want 1", len(ends))
				}
				if ended := ends[0].(*poo.JobEndedRecord); ended.Outcome != poo.OutcomeInterrupted {
					t.Errorf("the job's outcome is %q, want %q", ended.Outcome, poo.OutcomeInterrupted)
				}
			},
		},
		{
			name: "a partial reply",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				assistantMessage("r1", 1, "", "", true),
			},
			phase: poo.StateStreaming,
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if !plan.PartialDiscarded {
					t.Error("the plan keeps the partial reply, want it discarded")
				}
			},
		},
		{
			name: "a partial reply the run went on from",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				assistantMessage("r1", 1, "call_1", "read", true),
				toolCallWithID("r1", 1, "r1", "read", "call_1"),
			},
			phase: poo.StateExecuting,
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if plan.PartialDiscarded {
					t.Error("the plan discards a partial reply the run's calls followed, want it kept")
				}
			},
		},
		{
			name: "an uncommitted compaction",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				assistantMessage("r1", 1, "", "", false),
				compacted("r1"),
			},
			phase: poo.StateRequesting,
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if !plan.CompactionDiscarded {
					t.Error("the plan keeps the compaction, want it discarded")
				}
			},
		},
		{
			name: "an ask the run still waits on",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				toolCall("r1", 1, "r1", "bash"),
				askedAt("r1", "a1", "bash", planNow.Add(time.Minute).Format(time.RFC3339Nano), "deny"),
				toolStarted("r1", 1, "r1", "bash", ""),
			},
			phase: poo.StateExecuting,
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if len(plan.LiveAsks) != 1 {
					t.Fatalf("the plan holds %d live asks, want 1", len(plan.LiveAsks))
				}
				if len(recordNamed(sink.records, "tool.result")) != 0 {
					t.Error("the plan answered a call it is still waiting on")
				}
			},
		},
		{
			name: "an ask whose deadline passed is answered, not resumed",
			recs: []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				toolCall("r1", 1, "r1", "bash"),
				askedAt("r1", "a1", "bash", deadline, "deny"),
			},
			phase: poo.StateRequesting,
			check: func(t *testing.T, plan *ContinuationPlan, sink *planSink) {
				if len(plan.LiveAsks) != 0 {
					t.Errorf("the plan holds %d live asks, want none", len(plan.LiveAsks))
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, sink := planOf(t, tc.recs, planOptions())
			if plan == nil {
				if tc.phase != "" {
					t.Fatalf("the plan is nil, want a plan that resumes in %q", tc.phase)
				}
				if tc.check != nil {
					tc.check(t, plan, sink)
				}
				return
			}
			if plan.Phase != tc.phase {
				t.Errorf("the run resumes in %q, want %q", plan.Phase, tc.phase)
			}
			if !reflect.DeepEqual(plan.Hook, tc.hook) {
				t.Errorf("the hook is %+v, want %+v", plan.Hook, tc.hook)
			}
			if tc.check != nil {
				tc.check(t, plan, sink)
			}
		})
	}
}

// TestDurablePlanContinuesTheLastRunAndSettlesTheEarlierOnes covers a log
// with more than one run left open: only the last one is the run the process
// was in, and the ones before it are settled.
func TestDurablePlanContinuesTheLastRunAndSettlesTheEarlierOnes(t *testing.T) {
	plan, sink := planOf(t, []poo.Record{
		durableHeader("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		runStarted("r2"),
		toolCall("r2", 1, "r1", "read"),
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if plan.RunID != "r2" {
		t.Errorf("the plan continues %q, want the last run r2", plan.RunID)
	}
	settles := recordNamed(sink.records, "run.settled")
	if len(settles) != 1 {
		t.Fatalf("the plan wrote %d settles, want the earlier run's", len(settles))
	}
	if settled := settles[0].(*poo.RunSettledRecord); settled.RunID != "r1" {
		t.Errorf("the plan settled %q, want r1", settled.RunID)
	}
}

// TestDurablePlanResolvesTheSessionOwnWork covers the operations that belong
// to the session rather than to the run: a feed attempt, a job and a message
// that reached the log without its delivered record are ended, and the run
// still continues.
func TestDurablePlanResolvesTheSessionOwnWork(t *testing.T) {
	plan, sink := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		toolCall("r1", 1, "r1", "read"),
		&poo.FeedStartedRecord{
			RecordMeta: poo.NewRecordMetaFrom("feed.started", poo.FeedOrigin("inbox")),
			SessionID:  "s1",
			Feed:       "inbox",
			Attempt:    1,
			StartedAt:  "2026-10-20T00:00:00Z",
		},
		jobStarted("j1", "r9", "bash", "r9"),
		// A follow-up whose message landed and whose delivered record did not.
		queuedFollowUp("in1", "carry on", nil),
		&poo.MessageRecord{
			RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginUser),
			RunID:      "r1",
			InputID:    "in1",
			Role:       poo.RoleUser,
			Content:    []poo.ContentBlock{{Kind: poo.ContentText, Text: "carry on"}},
		},
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	ended := recordNamed(sink.records, "feed.ended")
	if len(ended) != 1 {
		t.Fatalf("the plan wrote %d feed ends, want 1", len(ended))
	}
	if feed := ended[0].(*poo.FeedEndedRecord); feed.Outcome != poo.FeedInterrupted {
		t.Errorf("the feed's outcome is %q, want %q", feed.Outcome, poo.FeedInterrupted)
	}
	if len(recordNamed(sink.records, "job.ended")) != 1 {
		t.Errorf("the plan wrote %d job ends, want 1", len(recordNamed(sink.records, "job.ended")))
	}
	delivered := recordNamed(sink.records, "input.delivered")
	if len(delivered) != 1 {
		t.Fatalf("the plan wrote %d deliveries, want the message's", len(delivered))
	}
	if rec := delivered[0].(*poo.InputDeliveredRecord); rec.InputID != "in1" {
		t.Errorf("the delivery is for %q, want in1", rec.InputID)
	}
	if len(recordNamed(sink.records, "run.settled")) != 0 {
		t.Error("the plan settled the run, want it continued")
	}
}

// TestDurablePlanAnswersOrReplaysEachToolClass covers the tool classes: a
// call the planner cannot replay is answered interrupted, and one it can is
// left for the continuation.
func TestDurablePlanAnswersOrReplaysEachToolClass(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		start  bool
		reason string
		// answered reports that the plan writes the call's result, and
		// handled that the plan writes its message.
		answered bool
		outcome  poo.Outcome
		replay   bool
	}{
		{name: "read-only call that started", tool: "read", start: true, reason: PlanReasonReadOnly, replay: true},
		{name: "read-only call that did not start", tool: "read", reason: PlanReasonReadOnly, replay: true},
		{name: "idempotent call that started", tool: "write", start: true, reason: PlanReasonIdempotent, replay: true},
		{name: "idempotent call that did not start", tool: "write", reason: PlanReasonIdempotent, replay: true},
		{name: "side-effecting call that started", tool: "bash", start: true, answered: true, outcome: poo.OutcomeInterrupted},
		{name: "side-effecting call that did not start", tool: "bash", reason: PlanReasonNotStarted, replay: true},
		{name: "call of an unknown tool that started", tool: "gone", start: true, answered: true, outcome: poo.OutcomeInterrupted},
		{name: "call of an unknown tool that did not start", tool: "gone", reason: PlanReasonNotStarted, replay: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				assistantMessage("r1", 1, "call_1", tc.tool, false),
				toolCallWithID("r1", 1, "r1", tc.tool, "call_1"),
			}
			if tc.start {
				recs = append(recs, toolStarted("r1", 1, "r1", tc.tool, "call_1"))
			}
			plan, sink := planOf(t, recs, planOptions())
			if plan == nil {
				t.Fatal("the plan is nil, want a continuation")
			}
			replayed := len(plan.ReplayCalls) == 1
			if replayed != tc.replay {
				t.Fatalf("the plan replays %d calls, want replay=%v (%+v)", len(plan.ReplayCalls), tc.replay, plan.ReplayCalls)
			}
			if tc.replay {
				call := plan.ReplayCalls[0]
				if call.Reason != tc.reason {
					t.Errorf("the call is replayed because %q, want %q", call.Reason, tc.reason)
				}
				if call.Handle != "r1" || call.CallID != "call_1" || call.Tool != tc.tool {
					t.Errorf("the planned call is %+v, want r1/call_1/%s", call, tc.tool)
				}
				if string(call.Arguments) != `{}` {
					t.Errorf("the planned call's arguments are %q, want the call record's", call.Arguments)
				}
			}
			results := recordNamed(sink.records, "tool.result")
			if tc.answered {
				if len(results) != 1 {
					t.Fatalf("the plan wrote %d results, want 1", len(results))
				}
				result := results[0].(*poo.ToolResultRecord)
				if result.Outcome != tc.outcome {
					t.Errorf("the result's outcome is %q, want %q", result.Outcome, tc.outcome)
				}
				if result.CallID != "call_1" {
					t.Errorf("the result is for %q, want call_1", result.CallID)
				}
				want := poo.HandleLine("r1", tc.tool) + "\n" + InterruptedCallText
				if result.Text != want {
					t.Errorf("the result's text is %q, want %q", result.Text, want)
				}
				// The model saw the call, so the result reaches it.
				messages := recordNamed(sink.records, "message")
				if len(messages) != 1 {
					t.Fatalf("the plan wrote %d messages, want the result's", len(messages))
				}
				block := messages[0].(*poo.MessageRecord).Content[0]
				if block.ToolCallID != "call_1" || !block.IsError {
					t.Errorf("the message answers %q (error %v), want call_1 with an error", block.ToolCallID, block.IsError)
				}
			} else if len(results) != 0 {
				t.Errorf("the plan wrote %d results, want none", len(results))
			}
		})
	}
}

// TestDurablePlanReplaysACallTheLogHoldsOnlyInTheMessage covers a call whose
// own record was never written: the model's message is the only place its
// arguments are, so the planned call carries them from there.
func TestDurablePlanReplaysACallTheLogHoldsOnlyInTheMessage(t *testing.T) {
	plan, _ := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		assistantMessage("r1", 1, "call_1", "read", false),
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if len(plan.ReplayCalls) != 1 {
		t.Fatalf("the plan replays %d calls, want the model's call", len(plan.ReplayCalls))
	}
	call := plan.ReplayCalls[0]
	if string(call.Arguments) != `{"text":"one"}` {
		t.Errorf("the planned call's arguments are %q, want the message's", call.Arguments)
	}
	if call.Reason != PlanReasonReadOnly {
		t.Errorf("the call is replayed because %q, want %q", call.Reason, PlanReasonReadOnly)
	}
}

// TestDurablePlanClosesANestedCallWithoutAMessage covers a call the model
// never saw: its result is written and no message carries it.
func TestDurablePlanClosesANestedCallWithoutAMessage(t *testing.T) {
	nested := toolCallWithID("r1", 1, "r5", "bash", "")
	nested.Mode = poo.CallModeNested
	nested.Parent = "r1"
	plan, sink := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		toolCall("r1", 1, "r1", "read"),
		nested,
		toolStarted("r1", 1, "r5", "bash", ""),
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if len(plan.ReplayCalls) != 1 || plan.ReplayCalls[0].Handle != "r1" {
		t.Errorf("the plan replays %+v, want the parent call alone", plan.ReplayCalls)
	}
	if len(recordNamed(sink.records, "tool.result")) != 1 {
		t.Errorf("the plan wrote %d results, want the nested call's", len(recordNamed(sink.records, "tool.result")))
	}
	if len(recordNamed(sink.records, "message")) != 0 {
		t.Errorf("the plan wrote %d messages, want none for a nested call", len(recordNamed(sink.records, "message")))
	}
}

// TestDurablePlanWritesTheMessageOfAResultThatLanded covers a call whose
// result reached the log and whose message did not: the call is answered,
// and the model gets the answer.
func TestDurablePlanWritesTheMessageOfAResultThatLanded(t *testing.T) {
	result := toolResult("r1", 1, "r1", "read", poo.OutcomeOK)
	result.CallID = "call_1"
	result.Text = "the file's text"
	plan, sink := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		assistantMessage("r1", 1, "call_1", "read", false),
		toolCallWithID("r1", 1, "r1", "read", "call_1"),
		result,
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if len(plan.ReplayCalls) != 0 {
		t.Errorf("the plan replays %d calls, want none", len(plan.ReplayCalls))
	}
	if len(recordNamed(sink.records, "tool.result")) != 0 {
		t.Error("the plan wrote a second result for a call that has one")
	}
	messages := recordNamed(sink.records, "message")
	if len(messages) != 1 {
		t.Fatalf("the plan wrote %d messages, want 1", len(messages))
	}
	block := messages[0].(*poo.MessageRecord).Content[0]
	if block.Text != "the file's text" || block.IsError {
		t.Errorf("the message carries %q (error %v), want the recorded result", block.Text, block.IsError)
	}
}

// TestDurablePlanResolvesAnExpiredAsk covers the ask whose deadline passed:
// it is answered the way it was going to be, and the call follows.
func TestDurablePlanResolvesAnExpiredAsk(t *testing.T) {
	expired := planNow.Add(-time.Minute).Format(time.RFC3339Nano)
	cases := []struct {
		name     string
		fallback string
		decision string
		// allows reports that the call is left to be run again.
		allows bool
	}{
		{name: "the fallback denies", fallback: "deny", decision: decisionDeny},
		{name: "the fallback allows", fallback: "allow", decision: decisionAllow, allows: true},
		{name: "the ask names no fallback", decision: decisionDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := []poo.Record{
				durableHeader("s1"), runStarted("r1"),
				assistantMessage("r1", 1, "call_1", "bash", false),
				toolCallWithID("r1", 1, "r1", "bash", "call_1"),
				askedAt("r1", "a1", "bash", expired, tc.fallback),
			}
			plan, sink := planOf(t, recs, planOptions())
			if plan == nil {
				t.Fatal("the plan is nil, want a continuation")
			}
			answers := recordNamed(sink.records, "permission.answered")
			if len(answers) != 1 {
				t.Fatalf("the plan wrote %d answers, want 1", len(answers))
			}
			answer := answers[0].(*poo.PermissionAnsweredRecord)
			if answer.Decision != tc.decision {
				t.Errorf("the decision is %q, want %q", answer.Decision, tc.decision)
			}
			if answer.AnsweredBy != "recovery" {
				t.Errorf("the answer names %q, want recovery", answer.AnsweredBy)
			}
			if answer.Provenance != poo.ProvenanceTimeout {
				t.Errorf("the provenance is %q, want %q", answer.Provenance, poo.ProvenanceTimeout)
			}
			if answer.AskID != "a1" {
				t.Errorf("the answer is for %q, want a1", answer.AskID)
			}
			results := recordNamed(sink.records, "tool.result")
			if tc.allows {
				if len(results) != 0 {
					t.Errorf("the plan wrote %d results, want none: the call is replayed", len(results))
				}
				if len(plan.ReplayCalls) != 1 || plan.ReplayCalls[0].Handle != "r1" {
					t.Errorf("the plan replays %+v, want the allowed call", plan.ReplayCalls)
				}
				if plan.Phase != poo.StateExecuting {
					t.Errorf("the run resumes in %q, want %q", plan.Phase, poo.StateExecuting)
				}
				return
			}
			if len(plan.ReplayCalls) != 0 {
				t.Errorf("the plan replays %+v, want none: the call is denied", plan.ReplayCalls)
			}
			if len(results) != 1 {
				t.Fatalf("the plan wrote %d results, want the denied call's", len(results))
			}
			denied := results[0].(*poo.ToolResultRecord)
			if denied.Outcome != poo.OutcomeDenied {
				t.Errorf("the result's outcome is %q, want %q", denied.Outcome, poo.OutcomeDenied)
			}
			want := poo.HandleLine("r1", "bash") + "\n" + DeniedCallText
			if denied.Text != want {
				t.Errorf("the result's text is %q, want %q", denied.Text, want)
			}
			// The model's call is answered, so its result reaches it.
			if len(recordNamed(sink.records, "message")) != 1 {
				t.Errorf("the plan wrote %d messages, want the denied result's", len(recordNamed(sink.records, "message")))
			}
			if plan.Phase != poo.StateRequesting {
				t.Errorf("the run resumes in %q, want %q", plan.Phase, poo.StateRequesting)
			}
		})
	}
}

// TestDurablePlanKeepsALiveAskWithItsDeadline covers the ask that has not
// expired: the plan hands it to the continuation with its own deadline, and
// writes nothing about it.
func TestDurablePlanKeepsALiveAskWithItsDeadline(t *testing.T) {
	deadline := planNow.Add(90 * time.Second)
	plan, sink := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		askedAt("r1", "a1", "bash", deadline.Format(time.RFC3339Nano), "allow"),
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if len(recordNamed(sink.records, "permission.answered")) != 0 {
		t.Error("the plan answered a live ask")
	}
	if len(plan.LiveAsks) != 1 {
		t.Fatalf("the plan holds %d live asks, want 1", len(plan.LiveAsks))
	}
	ask := plan.LiveAsks[0]
	if !ask.Deadline.Equal(deadline) {
		t.Errorf("the ask's deadline is %s, want %s", ask.Deadline, deadline)
	}
	if ask.Fallback != "allow" {
		t.Errorf("the ask's fallback is %q, want allow", ask.Fallback)
	}
	if ask.Summary != "the call writes a file" {
		t.Errorf("the ask's summary is %q, want the recorded one", ask.Summary)
	}
	if ask.Class != poo.ClassSideEffecting || ask.Reason != poo.AskReasonPolicy {
		t.Errorf("the ask is %q/%q, want the recorded class and reason", ask.Class, ask.Reason)
	}
}

// TestDurablePlanKeepsAnAskWithNoDeadlineLive covers an ask that named no
// deadline: nothing decided to expire it, so the continuation waits on it.
func TestDurablePlanKeepsAnAskWithNoDeadlineLive(t *testing.T) {
	plan, _ := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		permissionAsked("r1", "a1", "bash"),
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if len(plan.LiveAsks) != 1 {
		t.Fatalf("the plan holds %d live asks, want 1", len(plan.LiveAsks))
	}
	if !plan.LiveAsks[0].Deadline.IsZero() {
		t.Errorf("the ask's deadline is %s, want none", plan.LiveAsks[0].Deadline)
	}
}

// TestDurablePlanKeepsTheActiveSubscriptions covers the feed instances: the
// last subscription of each instance that is still active is what the
// continuation takes on again, and an instance that unsubscribed is not.
func TestDurablePlanKeepsTheActiveSubscriptions(t *testing.T) {
	plan, _ := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		feedSubscribed("inbox", "i1", "scope-one", ""),
		feedSubscribed("inbox", "i2", "scope-two", "watch the two"),
		feedUnsubscribed("inbox", "i2"),
		// Stage 2 re-emits the subscribe on a prompt-only change: the
		// instance's current scope and prompt are the last record's.
		feedSubscribed("inbox", "i1", "scope-one", "watch one"),
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if len(plan.Subscriptions) != 1 {
		t.Fatalf("the plan holds %d subscriptions, want 1", len(plan.Subscriptions))
	}
	sub := plan.Subscriptions[0]
	if sub.Feed != "inbox" || sub.Instance != "i1" {
		t.Errorf("the subscription is %s/%s, want inbox/i1", sub.Feed, sub.Instance)
	}
	if sub.Prompt != "watch one" || sub.Scope != "scope-one" {
		t.Errorf("the subscription is %q in %q, want the last record's", sub.Prompt, sub.Scope)
	}
	if sub.Origin != poo.FeedOrigin("inbox") {
		t.Errorf("the subscription's origin is %q, want the recorded one", sub.Origin)
	}
}

// TestDurablePlanKeepsQueuedInputs covers the inputs the session accepted
// and never delivered: the plan hands them to the continuation and writes no
// dropped record, so a crash does not lose the user's message.
func TestDurablePlanKeepsQueuedInputs(t *testing.T) {
	note := &poo.DeliveryNote{MessageID: "m1", UpToChars: 3, Text: "the reply was cut short"}
	plan, sink := planOf(t, []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		toolCall("r1", 1, "r1", "read"),
		queuedFollowUp("in1", "carry on", note),
	}, planOptions())
	if plan == nil {
		t.Fatal("the plan is nil, want a continuation")
	}
	if len(recordNamed(sink.records, "input.dropped")) != 0 {
		t.Error("the plan dropped a queued input, want it kept")
	}
	if len(plan.PendingInputs) != 1 {
		t.Fatalf("the plan holds %d inputs, want 1", len(plan.PendingInputs))
	}
	input := plan.PendingInputs[0]
	if input.InputID != "in1" {
		t.Errorf("the input is %q, want in1", input.InputID)
	}
	if input.Text != "carry on" {
		t.Errorf("the input's text is %q, want the queued text", input.Text)
	}
	if input.Mode != poo.FollowUp {
		t.Errorf("the input's mode is %q, want %q", input.Mode, poo.FollowUp)
	}
	if !reflect.DeepEqual(input.DeliveryNote, note) {
		t.Errorf("the input's delivery note is %+v, want %+v", input.DeliveryNote, note)
	}
}

// TestDurablePlanGivesUpAtTheResumptionLimit covers the crash loop: a run
// that was resumed as often as it may be is settled interrupted, with a
// detail that names the limit.
func TestDurablePlanGivesUpAtTheResumptionLimit(t *testing.T) {
	records := func(resumptions int) []poo.Record {
		recs := []poo.Record{
			durableHeader("s1"), runStarted("r1"),
			assistantMessage("r1", 1, "call_1", "read", false),
			toolCallWithID("r1", 1, "r1", "read", "call_1"),
		}
		for i := 1; i <= resumptions; i++ {
			recs = append(recs, runResumed("r1", i, poo.StateExecuting, 1))
		}
		return recs
	}
	// One resumption short of the limit: the run continues.
	plan, _ := planOf(t, records(DefaultMaxResumptions-1), planOptions())
	if plan == nil {
		t.Fatalf("the plan is nil after %d resumptions, want a continuation", DefaultMaxResumptions-1)
	}
	if plan.Resumptions != DefaultMaxResumptions-1 {
		t.Errorf("the plan counts %d resumptions, want %d", plan.Resumptions, DefaultMaxResumptions-1)
	}

	plan, sink := planOf(t, records(DefaultMaxResumptions), planOptions())
	if plan != nil {
		t.Errorf("the plan is %+v, want nil at the resumption limit", plan)
	}
	settles := recordNamed(sink.records, "run.settled")
	if len(settles) != 1 {
		t.Fatalf("the plan wrote %d settles, want 1", len(settles))
	}
	settled := settles[0].(*poo.RunSettledRecord)
	if settled.Reason != poo.ReasonInterrupted {
		t.Errorf("the settle reason is %q, want %q", settled.Reason, poo.ReasonInterrupted)
	}
	if settled.Detail.Text != ResumptionLimitText {
		t.Errorf("the settle detail is %q, want %q", settled.Detail.Text, ResumptionLimitText)
	}
	if len(recordNamed(sink.records, "tool.result")) != 1 {
		t.Errorf("the plan wrote %d results, want the interrupted call's", len(recordNamed(sink.records, "tool.result")))
	}

	// The limit is the options', not the default.
	custom := planOptions()
	custom.MaxResumptions = 2
	if plan, _ := planOf(t, records(2), custom); plan != nil {
		t.Errorf("the plan is %+v, want nil at the configured limit", plan)
	}
}

// TestDurablePlanThatFailedPartwayIsFinishedByTheNextPlan covers the
// planner's idempotence: a plan that stopped after writing some of its
// records is finished by the next one, which names the same continuation and
// duplicates nothing.
func TestDurablePlanThatFailedPartwayIsFinishedByTheNextPlan(t *testing.T) {
	expired := planNow.Add(-time.Minute).Format(time.RFC3339Nano)
	recs := []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		// A started side-effecting call, which the plan answers.
		assistantMessage("r1", 1, "call_1", "bash", false),
		toolCallWithID("r1", 1, "r1", "bash", "call_1"),
		toolStarted("r1", 1, "r1", "bash", "call_1"),
		// A call to replay.
		toolCallWithID("r1", 1, "r2", "read", "call_2"),
		// An expired ask whose fallback denies.
		toolCallWithID("r1", 1, "r3", "bash", "call_3"),
		askedAt("r1", "a1", "bash", expired, "deny"),
		// A job, a feed attempt and an input of the session's.
		jobStarted("j1", "r9", "bash", "r1"),
		queuedFollowUp("in1", "carry on", nil),
	}
	// What the plan is, with no failure at all.
	clean, cleanSink := planOf(t, recs, planOptions())
	if clean == nil {
		t.Fatal("the clean plan is nil, want a continuation")
	}
	if len(cleanSink.records) < 3 {
		t.Fatalf("the clean plan wrote %d records, want at least three", len(cleanSink.records))
	}

	for fault := 1; fault < len(cleanSink.records); fault++ {
		t.Run("crash", func(t *testing.T) {
			first := &planSink{failAfter: fault}
			if _, err := (DurablePlanner{Options: planOptions()}).Plan(context.Background(), first, pendingOf("s1", recs)); err == nil {
				t.Fatal("the plan succeeded, want the injected failure")
			}
			if len(first.records) != fault {
				t.Fatalf("the failed plan wrote %d records, want %d", len(first.records), fault)
			}
			// The log now holds what the failed plan managed to write.
			partial := append(append([]poo.Record(nil), recs...), first.records...)
			second := &planSink{}
			again, err := DurablePlanner{Options: planOptions()}.Plan(context.Background(), second, pendingOf("s1", partial))
			if err != nil {
				t.Fatalf("the second plan: %v", err)
			}
			if !reflect.DeepEqual(again, clean) {
				t.Errorf("the second plan is %+v, want %+v", again, clean)
			}
			// Nothing is written twice.
			counts := map[string]int{}
			for _, rec := range append(partial, second.records...) {
				counts[rec.Meta().Record]++
			}
			if counts["tool.result"] != 2 {
				t.Errorf("the log holds %d results, want 2 (the interrupted call's and the denied one's)", counts["tool.result"])
			}
			if counts["permission.answered"] != 1 {
				t.Errorf("the log holds %d answers, want 1", counts["permission.answered"])
			}
			if counts["job.ended"] != 1 {
				t.Errorf("the log holds %d job ends, want 1", counts["job.ended"])
			}
			if counts["input.dropped"] != 0 {
				t.Errorf("the log holds %d dropped inputs, want none", counts["input.dropped"])
			}
			if counts["run.settled"] != 0 {
				t.Errorf("the log holds %d settles, want none: the run continues", counts["run.settled"])
			}
			// And a third plan has nothing left to write.
			third := &planSink{}
			final, err := DurablePlanner{Options: planOptions()}.Plan(context.Background(), third, pendingOf("s1", append(partial, second.records...)))
			if err != nil {
				t.Fatalf("the third plan: %v", err)
			}
			if len(third.records) != 0 {
				t.Errorf("the third plan wrote %v, want nothing", recordNames(third.records))
			}
			if !reflect.DeepEqual(final, clean) {
				t.Errorf("the third plan is %+v, want %+v", final, clean)
			}
		})
	}
}

// TestDurablePlanReportsTheRunItContinues covers the plan's own report: the
// run, its turn, its counters, and the phase it leaves on the Pending.
func TestDurablePlanReportsTheRunItContinues(t *testing.T) {
	recs := []poo.Record{
		durableHeader("s1"), runStarted("r1"),
		assistantMessage("r1", 1, "call_1", "read", false),
		toolCallWithID("r1", 1, "r1", "read", "call_1"),
		// The turn was cut short once and asked again.
		&poo.TurnInterruptedRecord{
			RecordMeta:    poo.NewRecordMeta("turn.interrupted"),
			SessionID:     "s1",
			RunID:         "r1",
			Turn:          1,
			InputIDs:      []string{"in1"},
			InterruptedAt: "2026-10-20T00:00:02Z",
		},
	}
	pending := pendingOf("s1", recs)
	if len(pending.Runs) != 1 {
		t.Fatalf("the session has %d unsettled runs, want 1", len(pending.Runs))
	}
	if pending.Runs[0].Phase != "" {
		t.Errorf("the scan set the phase to %q, want it left to the planner", pending.Runs[0].Phase)
	}
	sink := &planSink{}
	plan, err := DurablePlanner{Options: planOptions()}.Plan(context.Background(), sink, pending)
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if plan.RunID != "r1" || plan.Turn != 1 {
		t.Errorf("the plan continues %q turn %d, want r1 turn 1", plan.RunID, plan.Turn)
	}
	if plan.SessionID != "s1" {
		t.Errorf("the plan names the session %q, want s1", plan.SessionID)
	}
	if pending.Runs[0].Phase != plan.Phase {
		t.Errorf("the run's phase is %q, want the plan's %q", pending.Runs[0].Phase, plan.Phase)
	}
	if pending.Runs[0].RetryCount != 1 {
		t.Errorf("the run's retry count is %d, want 1", pending.Runs[0].RetryCount)
	}
	if pending.Runs[0].ContinuationCount != 0 {
		t.Errorf("the run's continuation count is %d, want 0", pending.Runs[0].ContinuationCount)
	}

	// A turn the run went back to the model from has more than one
	// assistant message, and the ones past the first are continuations.
	again := append(append([]poo.Record(nil), recs...),
		assistantMessage("r1", 1, "call_2", "read", false),
	)
	pending = pendingOf("s1", again)
	if len(pending.Runs) != 1 {
		t.Fatalf("the session has %d unsettled runs, want 1", len(pending.Runs))
	}
	if pending.Runs[0].ContinuationCount != 1 {
		t.Errorf("the run's continuation count is %d, want 1", pending.Runs[0].ContinuationCount)
	}
}

// TestDurableModeLeavesALoggedCrashSettledInterrupted covers the mode
// boundary: the store plans a durable session's continuation only, and a
// logged session's crash is still settled interrupted.
func TestDurableModeLeavesALoggedCrashSettledInterrupted(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		permissionAsked("r1", "a1", "bash"),
	)
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if loaded.Continuation != nil {
		t.Errorf("the load plans a continuation for a logged session: %+v", loaded.Continuation)
	}
	settles := 0
	answers := 0
	for _, rec := range loaded.Records {
		switch r := rec.(type) {
		case *poo.RunSettledRecord:
			settles++
			if r.Reason != poo.ReasonInterrupted {
				t.Errorf("the settle reason is %q, want %q", r.Reason, poo.ReasonInterrupted)
			}
		case *poo.PermissionAnsweredRecord:
			answers++
			if r.Provenance != poo.ProvenanceInterrupted {
				t.Errorf("the answer's provenance is %q, want %q", r.Provenance, poo.ProvenanceInterrupted)
			}
		}
	}
	if settles != 1 {
		t.Errorf("the session holds %d settles, want 1", settles)
	}
	if answers != 1 {
		t.Errorf("the session holds %d answers, want 1", answers)
	}
}

// TestADurableLoadWarnsWhenTheBuildDiffers covers the durable path's build
// identity: the store records the build it opens under, and a session
// written by another build loads with the warning the logged path gives.
func TestADurableLoadWarnsWhenTheBuildDiffers(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s1", durableHeader("s1"), runStarted("r1"))
	ctx := context.Background()

	store, err := NewStore(dir, WithBuild(poo.BuildInfo{Name: "poo", Version: "0.2.0"}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		store.Close()
		t.Fatalf("loading the durable session: %v", err)
	}
	var warned bool
	for _, warning := range loaded.Warnings {
		if strings.Contains(warning, "made by poo 0.1.0") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("the load warns %v, want the other build's warning", loaded.Warnings)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// A store that names no build compares nothing, so it warns nothing.
	quiet, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the quiet store: %v", err)
	}
	t.Cleanup(func() { quiet.Close() })
	quietLoaded, err := quiet.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the durable session quietly: %v", err)
	}
	for _, warning := range quietLoaded.Warnings {
		if strings.Contains(warning, "made by") {
			t.Errorf("the quiet store warns %q, want nothing: a store that names no build compares nothing", warning)
		}
	}
}

// TestDurableLoadPlansAContinuation covers where the load's wiring is: a
// durable session's load plans the continuation and returns it, and the same
// records under the logged mode are settled interrupted.
func TestDurableLoadPlansAContinuation(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	header := durableHeader("s1")
	recs := []poo.Record{header, runStarted("r1"), toolCall("r1", 1, "r1", "read")}
	writeSession(t, dir, "s1", recs...)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the durable session: %v", err)
	}
	if loaded.Continuation == nil {
		t.Fatal("the load planned nothing, want a continuation")
	}
	if len(loaded.Continuation.ReplayCalls) != 1 ||
		loaded.Continuation.ReplayCalls[0].Reason != PlanReasonReadOnly {
		t.Errorf("the plan replays %+v, want the header's read-only call",
			loaded.Continuation.ReplayCalls)
	}
	if loaded.Continuation.RunID != "r1" {
		t.Errorf("the plan continues the run %q, want %q", loaded.Continuation.RunID, "r1")
	}
	for _, rec := range loaded.Records {
		if settled, ok := rec.(*poo.RunSettledRecord); ok {
			t.Errorf("the load settled the run %s, want it continued", settled.RunID)
		}
	}
	meta := MetadataOfRecord(header)

	// The same records under the logged mode are settled interrupted, and
	// the interrupt policy is what the resolver picks for them.
	settled := &planSink{}
	plan, err := store.recovery.Resolve(ctx, settled, pendingOf("s1", recs), ModeLogged, meta)
	if err != nil {
		t.Fatalf("resolving the logged session: %v", err)
	}
	if plan != nil {
		t.Errorf("the logged resolve planned %+v, want nothing", plan)
	}
	if len(recordNamed(settled.records, "run.settled")) != 1 {
		t.Errorf("the logged resolve wrote %v, want one settle", recordNames(settled.records))
	}

	// A load's settle runs on the store's clock, the one WithClock
	// sets: the run's settle is stamped from it, not the wall clock.
	clk := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	clockStore, err := NewStore(dir, WithClock(func() time.Time { return clk }))
	if err != nil {
		t.Fatalf("opening the clocked store: %v", err)
	}
	defer clockStore.Close()
	writeSession(t, dir, "s2", createdRecord("s2"), runStarted("r2"))
	clockLoaded, err := clockStore.Load(ctx, "s2")
	if err != nil {
		t.Fatalf("loading the logged session on the clock: %v", err)
	}
	var clockSettled *poo.RunSettledRecord
	for _, rec := range clockLoaded.Records {
		if s, ok := rec.(*poo.RunSettledRecord); ok {
			clockSettled = s
		}
	}
	if clockSettled == nil {
		t.Fatal("the clocked load settled no run")
	}
	if got := clockSettled.SettledAt; got != clk.UTC().Format(time.RFC3339Nano) {
		t.Errorf("the run settled at %s, want the store's clock %s", got, clk.UTC().Format(time.RFC3339Nano))
	}

	// A store given a planner of its own reads it at load time.
	other, err := NewStore(dir, WithDurableOptions(DurableOptions{MaxResumptions: 1}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer other.Close()
	if other.recovery.planner(meta).Options.MaxResumptions != 1 {
		t.Error("the store's planner does not carry its options")
	}
	if len(other.recovery.planner(meta).Options.Tools) != len(meta.Tools) {
		t.Error("the store's planner does not carry the session's tools")
	}

	// The check mode accepts the mode the load now serves, and still
	// refuses one it does not know.
	if err := CheckMode(ModeDurable); err != nil {
		t.Errorf("the check refuses the durable mode: %v", err)
	}
	if err := CheckMode(Mode("nonsense")); !errors.Is(err, ErrUnsupportedMode) {
		t.Errorf("the check on an unknown mode returned %v, want %v", err, ErrUnsupportedMode)
	}
}
