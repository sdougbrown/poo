package session

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/sdougbrown/poo"
)

// The texts the recovery writes.
//
// A call or a job that was cut short has no outcome, and the model is
// told so: the effect may or may not have happened, and the model
// decides what to do about it.
const (
	// InterruptedCallText is the text of an interrupted tool result.
	InterruptedCallText = "outcome unknown; the process stopped during this call"
	// InterruptedJobText is the text of an interrupted job.
	InterruptedJobText = "outcome unknown; the process stopped during this job"
)

// Pending is the set of operations a session has not resolved.
//
// It is what a recovery resolves: a run that started and never settled,
// a tool call with no result, a permission ask with no answer, a job
// that started and never ended, a feed's attempt that started and never
// ended, and an input that was queued and never reached the model.
type Pending struct {
	// SessionID identifies the session.
	SessionID string
	// Runs are the runs that started and never settled, in the order
	// they started.
	Runs []PendingRun
	// Jobs are the jobs that started and never ended, in the order
	// they started.
	Jobs []PendingJob
	// Feeds are the feed attempts that started and never ended, in the
	// order they started.
	Feeds []PendingFeed
	// Inputs are the inputs that were queued and never delivered or
	// dropped, in the order they were queued.
	Inputs []PendingInput
	// Delivered are the inputs whose message reached the log without the
	// delivered record that follows it, in the order of their messages.
	Delivered []PendingInput
}

// IsEmpty reports whether there is nothing to resolve.
func (p *Pending) IsEmpty() bool {
	return p == nil || (len(p.Runs) == 0 && len(p.Jobs) == 0 && len(p.Feeds) == 0 && len(p.Inputs) == 0 && len(p.Delivered) == 0)
}

// PendingRun is a run that started and never settled.
type PendingRun struct {
	// RunID identifies the run.
	RunID string
	// Calls are the tool calls the run made and never saw the result
	// of, in the order the model made them.
	Calls []PendingCall
	// Asks are the permission asks the run never got an answer to, in
	// the order the run asked.
	Asks []PendingAsk
}

// PendingCall is a tool call whose result record or result message is
// missing.
type PendingCall struct {
	// RunID identifies the run the call belongs to.
	RunID string
	// Turn is the turn the call belongs to.
	Turn int
	// Handle is the call's handle. A call the model saw but whose record
	// was never written gets a fresh handle from the recovery.
	Handle string
	// Tool is the name of the tool the model called.
	Tool string
	// CallID is the provider's id of the call: the id the call's record
	// holds, or the one the message block carrying the call gives. It is
	// empty when neither pairs the call with an id, and then no result
	// message refers to it.
	CallID string
	// Nested reports that the call is one a running tool made: its
	// record names a parent call. The model never saw such a call, so
	// the recovery answers it with a result and never with a message.
	Nested bool
	// MissingResult reports that the call has no result record.
	MissingResult bool
	// MissingMessage reports that the call has no result message.
	MissingMessage bool
	// Recorded reports that the log holds a tool.call record for the
	// call, not only the message that carried it.
	Recorded bool
	// ResultText is the text of the call's existing result record, when
	// it has one. The message the recovery writes carries it.
	ResultText string
	// ResultIsError is whether the call's existing result is an error.
	ResultIsError bool
}

// PendingAsk is a permission ask with no answer.
type PendingAsk struct {
	// RunID identifies the run that is waiting.
	RunID string
	// AskID names the ask.
	AskID string
	// Tool is the name of the tool the model called.
	Tool string
}

// PendingJob is a job that started and never ended.
type PendingJob struct {
	// JobID is the job's identifier, e.g. "j3".
	JobID string
	// Handle is the handle of the call the job came from.
	Handle string
	// Tool is the name of the tool the job runs.
	Tool string
	// RunID identifies the run that started the job.
	RunID string
}

// PendingFeed is one attempt of a feed that started and never ended.
type PendingFeed struct {
	// Feed is the feed's name.
	Feed string
	// Attempt numbers the feed's attempts in this session, from 1.
	Attempt int
	// StartedAt is the recorded start time.
	StartedAt string
}

// PendingInput is an input that never reached the model.
type PendingInput struct {
	// InputID is the input's identifier.
	InputID string
	// RunID identifies the run the input belongs to.
	RunID string
	// Mode is how the input was accepted.
	Mode poo.InputMode
	// Origin is the origin of the message that delivered the input. It is
	// set only on a delivery the recovery completes.
	Origin poo.Origin
}

// RecoveryPolicy resolves a session's pending operations.
//
// v1 has one policy: every pending operation is marked interrupted. A
// durable session would resolve them differently, and the policy is
// where that difference lives.
type RecoveryPolicy interface {
	// Name names the policy.
	Name() string
	// Resolve appends the records that resolve the pending operations.
	//
	// The resolution appends only what is missing, so it is
	// idempotent: a resolution that failed partway is finished by the
	// next one.
	Resolve(ctx context.Context, sink poo.RecordSink, p *Pending) error
}

// InterruptPolicy marks every pending operation as interrupted.
//
// An interrupted tool call is never re-executed: the effect may have
// happened, and only the model can decide what to do about that. An
// unanswered ask is denied, a run is settled as interrupted, a job's
// outcome is unknown, a feed's attempt is ended without running the feed
// again, and an input that never reached the model is dropped.
type InterruptPolicy struct {
	// Now is the policy's clock. The default is time.Now.
	Now func() time.Time
}

// Name names the policy.
func (InterruptPolicy) Name() string { return "interrupt" }

// Resolve appends the records that resolve the pending operations.
//
// The order is the plan's: a run's results, then its answers, then its
// settle record, then the jobs and the feeds, then the inputs. The settle
// record is last for its run, so a reader of the log sees the run's
// outcome after everything that led to it.
func (p InterruptPolicy) Resolve(ctx context.Context, sink poo.RecordSink, pend *Pending) error {
	if pend == nil {
		return nil
	}
	now := p.Now
	if now == nil {
		now = time.Now
	}
	at := now().UTC().Format(time.RFC3339Nano)
	for _, run := range pend.Runs {
		for _, call := range run.Calls {
			text := call.ResultText
			isError := call.ResultIsError
			if call.MissingResult {
				text = poo.HandleLine(call.Handle, call.Tool) + "\n" + InterruptedCallText
				isError = true
				rec := &poo.ToolResultRecord{
					RecordMeta: poo.NewRecordMeta("tool.result"),
					RunID:      call.RunID,
					Turn:       call.Turn,
					Handle:     call.Handle,
					CallID:     call.CallID,
					Tool:       call.Tool,
					Outcome:    poo.OutcomeInterrupted,
					Text:       text,
					IsError:    true,
				}
				if err := sink.Append(ctx, rec); err != nil {
					return err
				}
			}
			// The result goes into the model's view, so the call the
			// model made is answered. A call the model never saw has
			// no message to carry the result.
			if call.Nested || !call.MissingMessage {
				continue
			}
			msg := poo.ToolResultMessage(call.CallID, call.Tool, text, isError, nil)
			if err := sink.Append(ctx, &poo.MessageRecord{
				RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginTool),
				RunID:      call.RunID,
				Turn:       call.Turn,
				Role:       msg.Role,
				Content:    msg.Content,
			}); err != nil {
				return err
			}
		}
		for _, ask := range run.Asks {
			rec := &poo.PermissionAnsweredRecord{
				RecordMeta: poo.NewRecordMeta("permission.answered"),
				RunID:      ask.RunID,
				AskID:      ask.AskID,
				Decision:   "deny",
				AnsweredBy: "recovery",
				Provenance: poo.ProvenanceInterrupted,
			}
			if err := sink.Append(ctx, rec); err != nil {
				return err
			}
		}
		rec := &poo.RunSettledRecord{
			RecordMeta: poo.NewRecordMeta("run.settled"),
			RunID:      run.RunID,
			Reason:     poo.ReasonInterrupted,
			Detail: poo.SettleDetail{
				Source: "recovery",
				Text:   "the process stopped during this run",
			},
			SettledAt: at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	for _, job := range pend.Jobs {
		rec := &poo.JobEndedRecord{
			RecordMeta: poo.NewRecordMeta("job.ended"),
			SessionID:  pend.SessionID,
			JobID:      job.JobID,
			Handle:     job.Handle,
			Tool:       job.Tool,
			RunID:      job.RunID,
			Outcome:    poo.OutcomeInterrupted,
			InputID:    recoveredInputID(pend.SessionID, job.JobID),
			Text:       InterruptedJobText,
			EndedAt:    at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	for _, feed := range pend.Feeds {
		rec := &poo.FeedEndedRecord{
			RecordMeta: poo.NewRecordMetaFrom("feed.ended", poo.FeedOrigin(feed.Feed)),
			SessionID:  pend.SessionID,
			Feed:       feed.Feed,
			Attempt:    feed.Attempt,
			Outcome:    poo.FeedInterrupted,
			EndedAt:    at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	for _, input := range pend.Delivered {
		// A live session stamps an origin only on a feed's delivery.
		meta := poo.NewRecordMeta("input.delivered")
		if poo.IsFeedOrigin(input.Origin) {
			meta = poo.NewRecordMetaFrom("input.delivered", input.Origin)
		}
		rec := &poo.InputDeliveredRecord{
			RecordMeta:  meta,
			SessionID:   pend.SessionID,
			RunID:       input.RunID,
			InputID:     input.InputID,
			Mode:        input.Mode,
			DeliveredAt: at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	for _, input := range pend.Inputs {
		rec := &poo.InputDroppedRecord{
			RecordMeta: poo.NewRecordMeta("input.dropped"),
			SessionID:  pend.SessionID,
			RunID:      input.RunID,
			InputID:    input.InputID,
			Mode:       input.Mode,
			Reason:     poo.ReasonInterrupted,
			DroppedAt:  at,
		}
		if err := sink.Append(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

// recoveredInputID returns the id of the input a recovered job's result
// is queued under.
//
// The id names the recovery, so it cannot be the id of an input the
// session queued itself: the two never collide.
func recoveredInputID(sessionID, job string) string {
	return fmt.Sprintf("%s-ji-recovered-%s", sessionID, job)
}

// pendingOf computes the operations a session has not resolved.
//
// The calls are keyed by the provider's call id, never by the order the
// records landed in. A call the model saw but whose record was never
// written is recovered from the message alone.
func pendingOf(id string, recs []poo.Record) *Pending {
	state := &pendingState{
		runs:       map[string]*PendingRun{},
		settled:    map[string]bool{},
		calls:      map[string]*PendingCall{},
		results:    map[string]*poo.ToolResultRecord{},
		messages:   map[string]bool{},
		asks:       map[string]*PendingAsk{},
		answered:   map[string]bool{},
		inputs:     map[string]poo.InputQueuedRecord{},
		fate:       map[string]bool{},
		messaged:   map[string]bool{},
		callIDs:    poo.CallIDs(recs),
		feedStarts: map[string][]int{},
	}
	for _, rec := range recs {
		switch r := rec.(type) {
		case *poo.RunStartedRecord:
			state.run(r.RunID)
		case *poo.RunSettledRecord:
			state.run(r.RunID)
			state.settled[r.RunID] = true
		case *poo.ToolCallRecord:
			state.run(r.RunID)
			id := r.CallID
			if id == "" {
				id = state.callIDs[r.Handle]
			}
			call := state.call(callIdentity(id, r.RunID, r.Handle))
			call.RunID = r.RunID
			call.Turn = r.Turn
			call.Handle = r.Handle
			call.Tool = r.Tool
			call.CallID = id
			call.Nested = r.Parent != ""
			call.Recorded = true
		case *poo.ToolResultRecord:
			state.run(r.RunID)
			id := r.CallID
			if id == "" {
				id = state.callIDs[r.Handle]
			}
			state.results[callIdentity(id, r.RunID, r.Handle)] = r
		case *poo.MessageRecord:
			if r.RunID != "" {
				state.run(r.RunID)
			}
			// A message that names its input is that input's delivery,
			// even when the delivered record after it was never written.
			if r.InputID != "" && !state.messaged[r.InputID] {
				state.messaged[r.InputID] = true
				state.delivering = append(state.delivering, *r)
			}
			switch r.Role {
			case poo.RoleAssistant:
				for _, block := range r.Content {
					if block.Kind != poo.ContentToolCall || block.ToolCallID == "" {
						continue
					}
					call := state.call(callIdentity(block.ToolCallID, r.RunID, ""))
					call.RunID = r.RunID
					call.Turn = r.Turn
					call.Tool = block.Name
					call.CallID = block.ToolCallID
				}
			case poo.RoleTool:
				for _, block := range r.Content {
					if block.Kind != poo.ContentToolResult || block.ToolCallID == "" {
						continue
					}
					state.messages[callIdentity(block.ToolCallID, r.RunID, "")] = true
				}
			}
		case *poo.PermissionAskedRecord:
			key := askIdentity(r.RunID, r.AskID)
			if _, ok := state.asks[key]; !ok {
				state.asks[key] = &PendingAsk{RunID: r.RunID, AskID: r.AskID, Tool: r.Tool}
				state.askOrder = append(state.askOrder, key)
			}
		case *poo.PermissionAnsweredRecord:
			state.answered[askIdentity(r.RunID, r.AskID)] = true
		case *poo.JobStartedRecord:
			state.jobs = append(state.jobs, *r)
			state.jobEnded = append(state.jobEnded, false)
		case *poo.JobEndedRecord:
			state.endJob(r.JobID)
		case *poo.FeedStartedRecord:
			state.feeds = append(state.feeds, *r)
			state.feedEnded = append(state.feedEnded, false)
			state.feedStarts[r.Feed] = append(state.feedStarts[r.Feed], len(state.feeds)-1)
		case *poo.FeedEndedRecord:
			for _, i := range state.feedStarts[r.Feed] {
				if state.feedEnded[i] {
					continue
				}
				state.feedEnded[i] = true
				break
			}
		case *poo.InputQueuedRecord:
			if _, ok := state.inputs[r.InputID]; !ok {
				state.inputs[r.InputID] = *r
				state.inputOrder = append(state.inputOrder, r.InputID)
			}
		case *poo.InputDeliveredRecord:
			state.fate[r.InputID] = true
		case *poo.InputDroppedRecord:
			state.fate[r.InputID] = true
		}
	}

	// A call the model saw but whose record was never written has no
	// handle. It gets the next one, so its result record is placed like
	// any other.
	next := 0
	for _, call := range state.calls {
		next = max(next, handleNumber(call.Handle))
	}
	for _, result := range state.results {
		next = max(next, handleNumber(result.Handle))
	}
	for _, key := range state.callOrder {
		call := state.calls[key]
		if call.Handle != "" {
			continue
		}
		if _, ok := state.results[key]; ok {
			continue
		}
		next++
		call.Handle = fmt.Sprintf("r%d", next)
	}

	pend := &Pending{SessionID: id}
	for _, runID := range state.runOrder {
		if state.settled[runID] {
			continue
		}
		pending := PendingRun{RunID: runID}
		for _, key := range state.callOrder {
			call := state.calls[key]
			if call.RunID != runID {
				continue
			}
			result, hasResult := state.results[key]
			missingMessage := call.CallID != "" && !state.messages[key]
			if hasResult && !missingMessage {
				continue
			}
			called := *call
			called.MissingResult = !hasResult
			called.MissingMessage = missingMessage
			if hasResult {
				called.ResultText = result.Text
				called.ResultIsError = result.IsError || (!result.IsError && result.Outcome != poo.OutcomeOK && result.Outcome != poo.OutcomeStarted)
			}
			pending.Calls = append(pending.Calls, called)
		}
		sort.SliceStable(pending.Calls, func(i, j int) bool {
			// The calls the session recorded are written after the
			// calls only the model's message held: the session's own
			// work is the last thing before the settle.
			if pending.Calls[i].Recorded != pending.Calls[j].Recorded {
				return !pending.Calls[i].Recorded
			}
			return handleNumber(pending.Calls[i].Handle) < handleNumber(pending.Calls[j].Handle)
		})
		for _, key := range state.askOrder {
			ask := state.asks[key]
			if ask.RunID != runID || state.answered[key] {
				continue
			}
			pending.Asks = append(pending.Asks, *ask)
		}
		pend.Runs = append(pend.Runs, pending)
	}
	for i, job := range state.jobs {
		if state.jobEnded[i] {
			continue
		}
		pend.Jobs = append(pend.Jobs, PendingJob{
			JobID:  job.JobID,
			Handle: job.Handle,
			Tool:   job.Tool,
			RunID:  job.RunID,
		})
	}
	for i, feed := range state.feeds {
		if state.feedEnded[i] {
			continue
		}
		pend.Feeds = append(pend.Feeds, PendingFeed{
			Feed:      feed.Feed,
			Attempt:   feed.Attempt,
			StartedAt: feed.StartedAt,
		})
	}
	for _, inputID := range state.inputOrder {
		if state.fate[inputID] || state.messaged[inputID] {
			continue
		}
		input := state.inputs[inputID]
		if input.Mode == poo.InputModeFeed {
			// A feed's input has no terminal record to recover: its own
			// acceptance record is what a restore delivers, so the
			// recovery leaves it alone rather than dropping it.
			continue
		}
		pend.Inputs = append(pend.Inputs, PendingInput{
			InputID: input.InputID,
			RunID:   input.RunID,
			Mode:    input.Mode,
		})
	}
	for _, msg := range state.delivering {
		if state.fate[msg.InputID] {
			continue
		}
		// A job result is the only input with no queued record.
		mode := poo.InputModeJob
		if queued, ok := state.inputs[msg.InputID]; ok {
			mode = queued.Mode
		}
		pend.Delivered = append(pend.Delivered, PendingInput{
			InputID: msg.InputID,
			RunID:   msg.RunID,
			Mode:    mode,
			Origin:  msg.Origin,
		})
	}
	return pend
}

// pendingState collects what the records say about a session's
// unresolved operations.
type pendingState struct {
	// runs are the runs that started, by id.
	runs map[string]*PendingRun
	// runOrder is the order the runs started in.
	runOrder []string
	// settled are the runs that settled.
	settled map[string]bool
	// calls are the tool calls, by call id.
	calls map[string]*PendingCall
	// callOrder is the order the calls were recorded in.
	callOrder []string
	// results are the tool results, by call id.
	results map[string]*poo.ToolResultRecord
	// messages are the calls that have a result message, by call id.
	messages map[string]bool
	// asks are the permission asks, by run and id.
	asks map[string]*PendingAsk
	// askOrder is the order the asks were made in.
	askOrder []string
	// answered are the asks that have an answer.
	answered map[string]bool
	// jobs are the jobs that started, in order. A job id may repeat in
	// a log written before the counter continued, so the order is what
	// pairs each end with a start.
	jobs []poo.JobStartedRecord
	// jobEnded reports which of the started jobs have ended.
	jobEnded []bool
	// feeds are the feed attempts that started, in record order.
	feeds []poo.FeedStartedRecord
	// feedEnded reports which of the started attempts have ended.
	feedEnded []bool
	// feedStarts are the indexes of one feed's starts, by name. One
	// feed's attempts never overlap, so the first start without an end
	// is the one an end belongs to.
	feedStarts map[string][]int
	// inputs are the inputs that were queued, by id.
	inputs map[string]poo.InputQueuedRecord
	// inputOrder is the order the inputs were queued in.
	inputOrder []string
	// fate are the inputs that have a delivered or dropped record.
	fate map[string]bool
	// messaged are the inputs a message names as delivered.
	messaged map[string]bool
	// delivering are the messages that name an input, in record order,
	// one per input.
	delivering []poo.MessageRecord
	// callIDs are the provider's ids of the calls, by handle.
	callIDs map[string]string
}

// run returns a run's state, creating it when it is new.
func (s *pendingState) run(id string) *PendingRun {
	run, ok := s.runs[id]
	if !ok {
		run = &PendingRun{RunID: id}
		s.runs[id] = run
		s.runOrder = append(s.runOrder, id)
	}
	return run
}

// call returns a call's state, creating it when it is new.
func (s *pendingState) call(key string) *PendingCall {
	call, ok := s.calls[key]
	if !ok {
		call = &PendingCall{}
		s.calls[key] = call
		s.callOrder = append(s.callOrder, key)
	}
	return call
}

// endJob marks the first start of a job id as ended.
func (s *pendingState) endJob(id string) {
	for i := range s.jobs {
		if s.jobs[i].JobID == id && !s.jobEnded[i] {
			s.jobEnded[i] = true
			return
		}
	}
}

// callIdentity names a call. A call with a provider id is named by it;
// a call the model never saw is named by its run and handle.
func callIdentity(callID, runID, handle string) string {
	if callID != "" {
		return "id\x00" + callID
	}
	return callKey(runID, handle)
}

// askIdentity names an ask within its run.
func askIdentity(runID, askID string) string {
	return runID + "\x00" + askID
}

// callKey returns the key a tool call's result is filed under.
func callKey(runID, handle string) string {
	return "handle\x00" + runID + "\x00" + handle
}

// handleNumber returns the number a handle carries, or zero when it is
// not one of the session's handles.
func handleNumber(handle string) int {
	if len(handle) < 2 || handle[0] != 'r' {
		return 0
	}
	n, err := strconv.Atoi(handle[1:])
	if err != nil || n < 0 {
		return 0
	}
	return n
}
