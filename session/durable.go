package session

import (
	"context"
	"time"

	"github.com/sdougbrown/poo"
)

// DefaultMaxResumptions is how many times a run may be resumed before the
// durable planner gives up on it.
//
// A run that keeps stopping in the same place is a fault, not a crash: the
// planner settles it interrupted rather than replaying the same effect for
// ever.
const DefaultMaxResumptions = 3

// ResumptionLimitText is the settle detail of a run that hit its
// resumption limit.
const ResumptionLimitText = "resumption_limit: the run was resumed as often as it may be"

// DeniedCallText is the text of a call an expired ask's fallback refused.
const DeniedCallText = "the permission ask expired, and its fallback denied the call"

// The reasons a pending call is replayed.
const (
	// PlanReasonReadOnly is a call that cannot change anything, so running
	// it again is free.
	PlanReasonReadOnly = "read_only"
	// PlanReasonIdempotent is a side-effecting call whose repetition is
	// safe.
	PlanReasonIdempotent = "idempotent"
	// PlanReasonNotStarted is a side-effecting call whose start record
	// never landed, so no effect began.
	PlanReasonNotStarted = "not_started"
)

// hookPhaseAfterTurn and hookPhaseBeforeSettle are the two hook phases a
// run records.
const (
	hookPhaseAfterTurn    = "after_turn"
	hookPhaseBeforeSettle = "before_settle"
)

// decisionAllow and decisionDeny are the two answers a permission ask
// resolves to.
const (
	decisionAllow = "allow"
	decisionDeny  = "deny"
)

// The continuation's own types live in the core, because the core is what
// carries one out: the plan a store makes is the plan a restore resumes.
// The names are the session package's, so a caller of the store and a
// caller of the core talk about the same values.
type (
	// ContinuationPlan is what a durable session does next.
	ContinuationPlan = poo.ContinuationPlan
	// PlannedCall is a call the continuation runs again.
	PlannedCall = poo.PlannedCall
	// PlannedAsk is a permission ask the continuation waits on again.
	PlannedAsk = poo.PlannedAsk
	// PlannedInput is an input the continuation delivers.
	PlannedInput = poo.PlannedInput
	// HookResume is the turn hook a run resumes inside.
	HookResume = poo.HookResume
)

// DurableOptions configures the durable planner.
type DurableOptions struct {
	// MaxResumptions is how many times a run may be resumed before the
	// planner gives up on it. Zero or less means DefaultMaxResumptions.
	MaxResumptions int
	// Now is the planner's clock. The default is time.Now.
	//
	// The clock is the wall clock: an ask's deadline is a wall-clock time,
	// so a restart reads it against the clock, not against a monotonic
	// reading the process no longer has.
	Now func() time.Time
	// ToolLookup resolves a tool's class and idempotency from the
	// session's live tool set. When it is nil the planner reads the tools
	// the session's header recorded.
	ToolLookup PendingToolLookup
	// Tools are the tools the session's header recorded, which are the
	// lookup's fallback.
	Tools []poo.ToolInfo
}

// options returns the options with their defaults filled in.
func (o DurableOptions) options() DurableOptions {
	if o.MaxResumptions <= 0 {
		o.MaxResumptions = DefaultMaxResumptions
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// lookup resolves a tool's class and idempotency.
//
// A tool the lookup does not know is not a tool this build can run: the
// planner reads it as side-effecting and not idempotent, which is the
// reading that never repeats an effect it cannot account for.
func (o DurableOptions) lookup(tool string) (poo.ToolInfo, bool) {
	if o.ToolLookup != nil {
		return o.ToolLookup(tool)
	}
	for _, info := range o.Tools {
		if info.Name == tool {
			return info, true
		}
	}
	return poo.ToolInfo{}, false
}

// DurablePlanner plans what a durable session does next, instead of
// settling its run interrupted.
//
// It is the durable mode's recovery: the interrupt policy settles every
// pending operation, and the planner settles only what a continuation
// cannot account for and leaves the rest to the continuation.
type DurablePlanner struct {
	// Options configures the planner.
	Options DurableOptions
}

// Plan resolves what a durable session cannot continue, and returns where
// it continues from.
//
// The records the plan writes are the ones a continuation cannot make up
// for: a call whose effect may have begun is answered with an interrupted
// result, an ask whose deadline passed is answered the way it was going to
// be, and the session's jobs, feed attempts and undelivered messages are
// ended. Everything else is the continuation's, and the plan names it.
//
// A nil plan means the session does not continue: no run was left
// unsettled, or the run hit its resumption limit. The records that settle
// whatever was left are written before Plan returns.
//
// The phase is read from the pending set Plan was given, so a run held open
// by a job reads StateWaiting even though the plan ends that job: the
// queued result is what the continuation wakes on, and a plan made after
// the result landed reads StateRequesting. Both put the run through the
// request gate.
//
// The plan is deterministic, and the records it writes are idempotent: a
// plan that failed partway leaves exactly the work it did not do, so the
// next plan writes the rest and names the same continuation.
func (p DurablePlanner) Plan(ctx context.Context, sink poo.RecordSink, pend *Pending) (*ContinuationPlan, error) {
	opts := p.Options.options()
	now := opts.Now()
	if pend == nil || len(pend.Runs) == 0 {
		// Nothing to continue. What is left is the session's own work,
		// which died with the process; the inputs stay queued, because
		// they are still the user's messages.
		return nil, resolveSession(ctx, sink, pend, opts.Now)
	}
	// The last unsettled run is the run the process was in. A run left
	// unsettled before it was left behind long ago: it is settled, so a
	// reader of the log sees one run to continue.
	if stale := pend.Runs[:len(pend.Runs)-1]; len(stale) > 0 {
		if err := (InterruptPolicy{Now: opts.Now}).Resolve(ctx, sink, &Pending{SessionID: pend.SessionID, Runs: stale}); err != nil {
			return nil, err
		}
	}
	run := &pend.Runs[len(pend.Runs)-1]
	// A run that keeps stopping is not continued, and the settle detail
	// says which limit it hit. Giving up is the interrupt policy's: it is
	// what settles the run and the rest of the pending operations.
	if run.Resumptions >= opts.MaxResumptions {
		policy := InterruptPolicy{Now: opts.Now, SettleText: ResumptionLimitText}
		if err := policy.Resolve(ctx, sink, lastOf(pend)); err != nil {
			return nil, err
		}
		return nil, nil
	}

	plan := &ContinuationPlan{
		SessionID:           pend.SessionID,
		RunID:               run.RunID,
		Turn:                run.Turn,
		Resumptions:         run.Resumptions,
		Subscriptions:       pend.Subscriptions,
		PartialDiscarded:    run.PartialAssistant,
		CompactionDiscarded: run.UncommittedCompaction,
	}
	// What the asks decide is read before the calls are, because a call an
	// ask is about belongs to the ask: the run waits on the answer before
	// it runs the call. The records are written after the calls', so the
	// call's result lands first and a crash between the two leaves the plan
	// a re-scan reads the same way.
	claimed := map[string]bool{}
	type expiredAsk struct {
		ask  PendingAsk
		call PendingCall
		// refused reports that the call's own result is the plan's,
		// because the fallback denied it.
		refused bool
	}
	var expired []expiredAsk
	for _, ask := range run.Asks {
		if live(now, ask) {
			// A live ask carries the call it is about, so the
			// continuation asks about the same call
			// (pendingAskCall) and so the restore knows the call the
			// resumed run answers.
			planned := PlannedAsk{
				RunID:    ask.RunID,
				AskID:    ask.AskID,
				Handle:   ask.Handle,
				CallID:   ask.CallID,
				Tool:     ask.Tool,
				Summary:  ask.Summary,
				Class:    ask.Class,
				Reason:   ask.Reason,
				Deadline: ask.Deadline,
				Fallback: ask.Fallback,
			}
			if call, ok := claimCall(run, ask, claimed); ok {
				planned.Arguments = call.Arguments
			}
			plan.LiveAsks = append(plan.LiveAsks, planned)
			continue
		}
		if askDecision(ask) == decisionAllow {
			// A fallback that allows leaves the call to the calls' own
			// rules: an ask comes before its call runs, so the call never
			// started and they replay it.
			expired = append(expired, expiredAsk{ask: ask})
			continue
		}
		call, refused := claimCall(run, ask, claimed)
		expired = append(expired, expiredAsk{ask: ask, call: call, refused: refused})
	}
	for _, call := range run.Calls {
		if claimed[callIdentity(call.CallID, call.RunID, call.Handle)] {
			continue
		}
		if !call.MissingResult {
			// The result landed and only the message that carries it was
			// lost: the call is answered, and the model needs the answer.
			if err := appendResultMessage(ctx, sink, call, call.ResultText, call.ResultIsError); err != nil {
				return nil, err
			}
			continue
		}
		info, _ := opts.lookup(call.Tool)
		if reason, replay := replayReason(info, call); replay && !call.Nested {
			plan.ReplayCalls = append(plan.ReplayCalls, PlannedCall{
				RunID:      call.RunID,
				Turn:       call.Turn,
				Handle:     call.Handle,
				CallID:     call.CallID,
				Tool:       call.Tool,
				Arguments:  call.Arguments,
				Class:      info.Class,
				Idempotent: info.Idempotent,
				Reason:     reason,
			})
			continue
		}
		// The effect may have happened, and only the model can decide what
		// to do about that. A nested call is closed the same way and
		// without a message: the model never saw it, and its parent keeps
		// it out of the history.
		text := poo.HandleLine(call.Handle, call.Tool) + "\n" + InterruptedCallText
		if err := appendCallResult(ctx, sink, call, poo.OutcomeInterrupted, text); err != nil {
			return nil, err
		}
	}
	// An ask whose deadline passed is answered the way it was going to be.
	// A fallback that denies answers the call too, so the model reads the
	// refusal instead of finding a call nothing answered.
	for _, due := range expired {
		if due.refused {
			text := poo.HandleLine(due.call.Handle, due.call.Tool) + "\n" + DeniedCallText
			if err := appendCallResult(ctx, sink, due.call, poo.OutcomeDenied, text); err != nil {
				return nil, err
			}
		}
		if err := sink.Append(ctx, &poo.PermissionAnsweredRecord{
			RecordMeta: poo.NewRecordMeta("permission.answered"),
			RunID:      due.ask.RunID,
			AskID:      due.ask.AskID,
			Decision:   askDecision(due.ask),
			AnsweredBy: "recovery",
			Provenance: poo.ProvenanceTimeout,
			Reason:     due.ask.Reason,
		}); err != nil {
			return nil, err
		}
	}
	if err := resolveSession(ctx, sink, pend, opts.Now); err != nil {
		return nil, err
	}
	for _, input := range pend.Inputs {
		plan.PendingInputs = append(plan.PendingInputs, PlannedInput{
			InputID:      input.InputID,
			RunID:        input.RunID,
			Mode:         input.Mode,
			Text:         input.Text,
			DeliveryNote: input.DeliveryNote,
			Attachments:  input.Attachments,
		})
	}
	phase := derivePhase(run, plan, pend)
	run.Phase = phase
	plan.Phase = phase
	if run.HookPhase != nil {
		plan.Hook = &HookResume{
			Phase:       run.HookPhase.Phase,
			HookIndex:   run.HookPhase.HookIndex,
			Interrupted: run.HookPhase.Interrupted,
		}
	}
	return plan, nil
}

// lastOf returns a session's pending set with only the run it continues.
//
// The session's own work is the session's, not the run's, so it comes with
// the last run: a job's result is queued for the session, whatever run
// started the job.
func lastOf(pend *Pending) *Pending {
	return &Pending{
		SessionID: pend.SessionID,
		Runs:      pend.Runs[len(pend.Runs)-1:],
		Jobs:      pend.Jobs,
		Feeds:     pend.Feeds,
		Inputs:    pend.Inputs,
		Delivered: pend.Delivered,
	}
}

// resolveSession ends the session's own unfinished work: its jobs, its feed
// attempts and the messages that never got their delivered record.
//
// It writes no run's records, so a run's continuation is unaffected. A
// job's outcome is unknown to the session that restarts, and a feed's
// attempt is gone with the process that ran it: neither can be continued,
// so both are ended interrupted, which is what tells the model the outcome
// is unknown and queues the job's result again.
func resolveSession(ctx context.Context, sink poo.RecordSink, pend *Pending, now func() time.Time) error {
	if pend == nil {
		return nil
	}
	return InterruptPolicy{Now: now}.Resolve(ctx, sink, &Pending{
		SessionID: pend.SessionID,
		Jobs:      pend.Jobs,
		Feeds:     pend.Feeds,
		Delivered: pend.Delivered,
	})
}

// live reports whether an ask still waits on an answer.
//
// An ask with no deadline waits for ever: an ask that never named one is
// one no one decided to expire.
func live(now time.Time, ask PendingAsk) bool {
	return ask.Deadline.IsZero() || now.Before(ask.Deadline)
}

// askDecision returns the decision an expired ask resolves to.
//
// The decision is the request's effective fallback, so an ask that names
// none denies: the log only ever carries the fallback the ask would have
// taken.
func askDecision(ask PendingAsk) string {
	if ask.Fallback == decisionAllow {
		return decisionAllow
	}
	return decisionDeny
}

// claimCall returns the call an ask is about, and claims it so no other
// rule answers it.
//
// The log pairs the two by position: the ask follows the record of the call
// it asks about, so the scan names that call on the ask. A log that holds no
// call for the ask pairs it with nothing, and the answer is then all the
// plan writes.
func claimCall(run *PendingRun, ask PendingAsk, claimed map[string]bool) (PendingCall, bool) {
	for _, call := range run.Calls {
		if !call.MissingResult || call.Handle != ask.Handle || call.CallID != ask.CallID {
			continue
		}
		key := callIdentity(call.CallID, call.RunID, call.Handle)
		if claimed[key] {
			continue
		}
		claimed[key] = true
		return call, true
	}
	return PendingCall{}, false
}

// replayReason says whether a pending call is run again, and why.
//
// A read-only call changes nothing and an idempotent tool's repetition is
// safe, so both are replayed whatever the log says. A side-effecting call
// that never started began no effect, so replaying it is the same as
// running it once. Only a side-effecting call whose start record landed is
// left to the model: the effect may have happened.
func replayReason(info poo.ToolInfo, call PendingCall) (string, bool) {
	switch {
	case info.Class == poo.ClassReadOnly:
		return PlanReasonReadOnly, true
	case info.Idempotent:
		return PlanReasonIdempotent, true
	case !call.Started:
		return PlanReasonNotStarted, true
	}
	return "", false
}

// derivePhase says where a run resumes, from what the plan left for the
// continuation.
//
// The order is the plan's. A hook the log cut short is where the run
// resumes, before the turn's own work. A job holds the run open when
// nothing else does, and the job's result is queued to wake it. A call to
// run again, or an ask to wait on, is the run executing. A partial reply
// is the run streaming, and the partial is dropped. Anything else, a
// compaction included, is the run at its next request: the gate re-reads
// the budget and the compaction before it opens one.
func derivePhase(run *PendingRun, plan *ContinuationPlan, pend *Pending) poo.State {
	if run.HookPhase != nil {
		if run.HookPhase.Phase == hookPhaseBeforeSettle {
			// The settle hooks run when the model stopped on its own:
			// the chain either settles the run or continues it at the
			// next request.
			return poo.StateRequesting
		}
		return poo.StateExecuting
	}
	if len(plan.ReplayCalls) == 0 && len(plan.LiveAsks) == 0 && openJob(pend, run.RunID) {
		return poo.StateWaiting
	}
	if len(plan.ReplayCalls) > 0 || len(plan.LiveAsks) > 0 {
		return poo.StateExecuting
	}
	if run.PartialAssistant {
		return poo.StateStreaming
	}
	return poo.StateRequesting
}

// openJob reports whether the run has a job that started and never ended.
func openJob(pend *Pending, runID string) bool {
	for _, job := range pend.Jobs {
		if job.RunID == runID {
			return true
		}
	}
	return false
}

// appendCallResult appends a pending call's result, and the message that
// carries it.
//
// The result is written only when the log has none, so a resolution that
// failed partway is finished by the next one.
func appendCallResult(ctx context.Context, sink poo.RecordSink, call PendingCall, outcome poo.Outcome, text string) error {
	if call.MissingResult {
		rec := &poo.ToolResultRecord{
			RecordMeta: poo.NewRecordMeta("tool.result"),
			RunID:      call.RunID,
			Turn:       call.Turn,
			Handle:     call.Handle,
			CallID:     call.CallID,
			Tool:       call.Tool,
			Outcome:    outcome,
			Text:       text,
			IsError:    true,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	return appendResultMessage(ctx, sink, call, text, true)
}

// appendResultMessage appends the message that carries a call's result.
//
// The model never saw a nested call, so its parent keeps it out of the
// history and no message carries its result.
func appendResultMessage(ctx context.Context, sink poo.RecordSink, call PendingCall, text string, isError bool) error {
	if call.Nested || !call.MissingMessage || call.CallID == "" {
		return nil
	}
	msg := poo.ToolResultMessage(call.CallID, call.Tool, text, isError, nil)
	return sink.Append(ctx, &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginTool),
		RunID:      call.RunID,
		Turn:       call.Turn,
		Role:       msg.Role,
		Content:    msg.Content,
	})
}
