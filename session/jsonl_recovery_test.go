package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// TestATornTailIsTruncated covers the recovery's first step: a log
// that was cut in the middle of a record is truncated on disk.
func TestATornTailIsTruncated(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	path := writeSession(t, dir, "s1", createdRecord("s1"), runStarted("r1"))
	whole, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stating the log: %v", err)
	}
	// The process was killed in the middle of a record.
	appendBytes(t, path, []byte(`{"record":"run.settled","seq":99`))

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if !loaded.RecoveredTail {
		t.Error("the load does not report a recovered tail, want one")
	}
	// The torn bytes are gone: the file holds whole records only.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if strings.Contains(string(data), `"seq":99`) {
		t.Errorf("the log still holds the torn tail: %q", data[whole.Size():])
	}
	names := recordNames(loaded.Records)
	want := []string{"session.created", "run.started", "run.settled"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the session holds %v, want %v", names, want)
	}
}

// TestAnInterruptedRunIsMarkedInterrupted covers the recovery's second
// step: every call without a result is interrupted, every ask without
// an answer is denied, and the run's settle record comes last.
func TestAnInterruptedRunIsMarkedInterrupted(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		toolCall("r1", 1, "r2", "bash"),
		toolCall("r1", 1, "r3", "read"),
		toolResult("r1", 1, "r1", "bash", poo.OutcomeOK),
		permissionAsked("r1", "ask1", "bash"),
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	recs := loaded.Records
	names := recordNames(recs)
	want := []string{
		"session.created", "run.started", "tool.call", "tool.call", "tool.call",
		"tool.result", "permission.asked",
		"tool.result", "tool.result", "permission.answered", "run.settled",
	}
	if len(names) != len(want) {
		t.Fatalf("the session holds %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("the session holds %v, want %v", names, want)
		}
	}

	// The two calls without a result are interrupted, and the one with
	// a result is left alone.
	interrupted := map[string]*poo.ToolResultRecord{}
	for _, rec := range recs {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Outcome == poo.OutcomeInterrupted {
			interrupted[r.Handle] = r
		}
	}
	if len(interrupted) != 2 {
		t.Fatalf("the session holds %d interrupted results, want 2", len(interrupted))
	}
	for _, handle := range []string{"r2", "r3"} {
		result, ok := interrupted[handle]
		if !ok {
			t.Errorf("the call %s has no interrupted result", handle)
			continue
		}
		if want := poo.HandleLine(handle, result.Tool) + "\n" + InterruptedCallText; result.Text != want {
			t.Errorf("the call %s's text is %q, want %q", handle, result.Text, want)
		}
	}
	if _, ok := interrupted["r1"]; ok {
		t.Error("the call that had a result was interrupted, want it left alone")
	}

	// The ask is denied, and the answer says it was interrupted.
	answered, ok := recs[len(recs)-2].(*poo.PermissionAnsweredRecord)
	if !ok {
		t.Fatalf("the record before the settle is %T, want a permission answer", recs[len(recs)-2])
	}
	if answered.Decision != "deny" {
		t.Errorf("the decision is %q, want deny", answered.Decision)
	}
	if answered.Provenance != poo.ProvenanceInterrupted {
		t.Errorf("the provenance is %q, want %q", answered.Provenance, poo.ProvenanceInterrupted)
	}
	if answered.AskID != "ask1" {
		t.Errorf("the answer is for %q, want ask1", answered.AskID)
	}

	// The settle record is last.
	settled, ok := recs[len(recs)-1].(*poo.RunSettledRecord)
	if !ok {
		t.Fatalf("the last record is %T, want a settle record", recs[len(recs)-1])
	}
	if settled.Reason != poo.ReasonInterrupted {
		t.Errorf("the settle reason is %q, want %q", settled.Reason, poo.ReasonInterrupted)
	}
	if settled.RunID != "r1" {
		t.Errorf("the settle record is for %q, want r1", settled.RunID)
	}
}

// TestARecoveryThatFailsPartwayIsFinishedByTheNextLoad covers the
// recovery's idempotence: a load that failed partway is finished by
// the next one, without writing a record twice.
func TestARecoveryThatFailsPartwayIsFinishedByTheNextLoad(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		toolCall("r1", 1, "r2", "bash"),
		permissionAsked("r1", "ask1", "bash"),
	)

	// The first recovery write fails.
	store.appendFault = 1
	if _, err := store.Load(ctx, "s1"); err == nil {
		t.Fatal("the load succeeded, want the injected failure")
	}
	// The failure gave the lock up: the session is free again.
	locked, err := Locked(LockPath(dir, "s1"))
	if err != nil {
		t.Fatalf("checking the lock: %v", err)
	}
	if locked {
		t.Error("the session is still locked, want the lock released")
	}

	// The next load finishes the job.
	store.appendFault = 0
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	recs := loaded.Records
	results := 0
	answers := 0
	settles := 0
	for _, rec := range recs {
		switch r := rec.(type) {
		case *poo.ToolResultRecord:
			if r.Outcome == poo.OutcomeInterrupted {
				results++
			}
		case *poo.PermissionAnsweredRecord:
			answers++
		case *poo.RunSettledRecord:
			settles++
		}
	}
	if results != 2 {
		t.Errorf("the session holds %d interrupted results, want 2", results)
	}
	if answers != 1 {
		t.Errorf("the session holds %d answers, want 1", answers)
	}
	if settles != 1 {
		t.Errorf("the session holds %d settle records, want 1", settles)
	}
	// The settle record is still last.
	if _, ok := recs[len(recs)-1].(*poo.RunSettledRecord); !ok {
		t.Errorf("the last record is %T, want a settle record", recs[len(recs)-1])
	}
}

// TestRepeatedLoadsAddNothing covers the recovery's idempotence: a
// session that was recovered once is not recovered again.
func TestRepeatedLoadsAddNothing(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		jobStarted("j1", "r1", "bash", "r1"),
		inputQueued("in1", poo.Steer),
	)

	first, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	names := recordNames(first.Records)
	if len(names) != 9 {
		t.Fatalf("the session holds %v, want 9 records", names)
	}

	second, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session again: %v", err)
	}
	if len(second.Records) != len(first.Records) {
		t.Errorf("the second load holds %d records, want %d", len(second.Records), len(first.Records))
	}
}

// TestTheSessionIsUsableAfterARecovery covers the recovery's promise:
// the recovered session takes new records.
func TestTheSessionIsUsableAfterARecovery(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
	)
	if _, err := store.Load(ctx, "s1"); err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if err := store.Append(ctx, runStarted("r2")); err != nil {
		t.Fatalf("appending after the recovery: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	other, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer other.Close()
	loaded, err := other.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	found := false
	for _, rec := range loaded.Records {
		if started, ok := rec.(*poo.RunStartedRecord); ok && started.RunID == "r2" {
			found = true
		}
	}
	if !found {
		t.Error("the second run's record is not in the session, want it there")
	}
}

// TestAnUnfinishedJobIsEndedAndQueued covers the recovery's third step:
// a job that started and never ended is ended, and its result is queued
// again.
func TestAnUnfinishedJobIsEndedAndQueued(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		jobStarted("j1", "r1", "bash", "r1"),
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	ended, ok := loaded.Records[len(loaded.Records)-1].(*poo.JobEndedRecord)
	if !ok {
		t.Fatalf("the last record is %T, want a job's end", loaded.Records[len(loaded.Records)-1])
	}
	if ended.Outcome != poo.OutcomeInterrupted {
		t.Errorf("the job's outcome is %q, want %q", ended.Outcome, poo.OutcomeInterrupted)
	}
	if ended.Text != InterruptedJobText {
		t.Errorf("the job's text is %q, want %q", ended.Text, InterruptedJobText)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("the load queues %d job results, want 1", len(loaded.Jobs))
	}
	if loaded.Jobs[0].JobID != "j1" {
		t.Errorf("the queued result is for %q, want j1", loaded.Jobs[0].JobID)
	}
	if loaded.Jobs[0].Outcome != poo.OutcomeInterrupted {
		t.Errorf("the queued result's outcome is %q, want %q", loaded.Jobs[0].Outcome, poo.OutcomeInterrupted)
	}
}

// TestAJobThatEndedIsQueuedAgain covers a job that ended but whose
// result never reached the model.
func TestAJobThatEndedIsQueuedAgain(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		jobStarted("j1", "r1", "bash", "r1"),
		jobEnded("j1", "r1", "bash", "r1", "s1-ji1", "the job's result"),
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("the load queues %d job results, want 1", len(loaded.Jobs))
	}
	queued := loaded.Jobs[0]
	if queued.JobID != "j1" {
		t.Errorf("the queued result is for %q, want j1", queued.JobID)
	}
	if queued.Text != "the job's result" {
		t.Errorf("the queued result's text is %q, want the recorded text", queued.Text)
	}
	if queued.Outcome != poo.OutcomeOK {
		t.Errorf("the queued result's outcome is %q, want %q", queued.Outcome, poo.OutcomeOK)
	}
}

// TestADeliveredJobIsNotQueuedAgain covers a job whose result reached
// the model: there is nothing to deliver twice.
func TestADeliveredJobIsNotQueuedAgain(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		jobStarted("j1", "r1", "bash", "r1"),
		jobEnded("j1", "r1", "bash", "r1", "s1-ji1", "the job's result"),
		inputDelivered("s1-ji1", poo.InputModeJob),
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if len(loaded.Jobs) != 0 {
		t.Errorf("the load queues %d job results, want none", len(loaded.Jobs))
	}
}

// TestAJobOfAnOldRunIsStillRecovered covers a job whose run settled
// long ago: the job is the session's, not the run's.
func TestAJobOfAnOldRunIsStillRecovered(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		jobStarted("j1", "r1", "bash", "r1"),
		&poo.RunSettledRecord{
			RecordMeta: poo.NewRecordMeta("run.settled"),
			RunID:      "r1",
			Reason:     poo.ReasonEndTurn,
			Detail:     poo.SettleDetail{Source: "model"},
			SettledAt:  "2026-10-20T00:00:02Z",
		},
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	ended, ok := loaded.Records[len(loaded.Records)-1].(*poo.JobEndedRecord)
	if !ok {
		t.Fatalf("the last record is %T, want a job's end", loaded.Records[len(loaded.Records)-1])
	}
	if ended.JobID != "j1" {
		t.Errorf("the ended job is %q, want j1", ended.JobID)
	}
	if len(loaded.Jobs) != 1 {
		t.Errorf("the load queues %d job results, want 1", len(loaded.Jobs))
	}
}

// TestAQueuedInputWithNoFateIsDropped covers the recovery's fourth
// step: an input that was accepted and never reached the model is
// dropped.
func TestAQueuedInputWithNoFateIsDropped(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		inputQueued("in1", poo.Steer),
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	dropped, ok := loaded.Records[len(loaded.Records)-1].(*poo.InputDroppedRecord)
	if !ok {
		t.Fatalf("the last record is %T, want a dropped input", loaded.Records[len(loaded.Records)-1])
	}
	if dropped.InputID != "in1" {
		t.Errorf("the dropped input is %q, want in1", dropped.InputID)
	}
	if dropped.Reason != poo.ReasonInterrupted {
		t.Errorf("the reason is %q, want %q", dropped.Reason, poo.ReasonInterrupted)
	}
}

// TestAQueuedFeedInputIsNeverDropped covers the recovery's treatment of
// a feed's input: it has no terminal record of its own, so the recovery
// leaves it for the restore to deliver.
func TestAQueuedFeedInputIsNeverDropped(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		&poo.InputQueuedRecord{
			RecordMeta: poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("inbox")),
			SessionID:  "s1",
			InputID:    "s1-ji1",
			Mode:       poo.InputModeFeed,
			Text:       "[feed inbox: hello]",
			QueuedAt:   "2026-10-20T00:00:00Z",
		},
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	for _, rec := range loaded.Records {
		if rec.Meta().Record == "input.dropped" {
			t.Fatalf("the recovery dropped the feed's input: %+v", rec)
		}
	}
	if len(loaded.Jobs) != 1 {
		t.Errorf("the load queues %d inputs, want 1", len(loaded.Jobs))
	}
}

// TestThePendingOperationsAreReported covers the pending query: it says
// what a recovery would resolve.
func TestThePendingOperationsAreReported(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
		jobStarted("j1", "r1", "bash", "r1"),
		inputQueued("in1", poo.Steer),
	)

	pending, err := store.Pending(ctx, "s1")
	if err != nil {
		t.Fatalf("reading the pending operations: %v", err)
	}
	if len(pending.Runs) != 1 {
		t.Fatalf("the session has %d unsettled runs, want 1", len(pending.Runs))
	}
	if len(pending.Runs[0].Calls) != 1 {
		t.Errorf("the run has %d calls without a result, want 1", len(pending.Runs[0].Calls))
	}
	if len(pending.Jobs) != 1 {
		t.Errorf("the session has %d unfinished jobs, want 1", len(pending.Jobs))
	}
	if len(pending.Inputs) != 1 {
		t.Errorf("the session has %d inputs without a fate, want 1", len(pending.Inputs))
	}
	if pending.IsEmpty() {
		t.Error("the pending operations are empty, want some")
	}
}

// TestTheRecoveryKeepsTheFileWhole covers a recovery that leaves the
// log readable: every line is a whole record.
func TestTheRecoveryKeepsTheFileWhole(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r1", "bash"),
	)
	if _, err := store.Load(ctx, "s1"); err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}
	recs := logRecords(t, filepath.Join(dir, "s1.jsonl"))
	if len(recs) != 5 {
		t.Errorf("the log holds %d records, want 5", len(recs))
	}
}

// TestANestedCallWithoutAResultIsRecovered covers the nested seam's
// torn tail: a nested tool.call with no result is recovered with an
// interrupted result, and no message record — the model never saw the
// call, so nothing of it enters the history.
//
// The parent is what says the model never saw the call, not the absence
// of a provider id: a nested call whose record carries an id is closed
// without a message too.
func TestANestedCallWithoutAResultIsRecovered(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	nested := func(handle, callID string) *poo.ToolCallRecord {
		rec := toolCallWithID("r1", 1, handle, "read", callID)
		rec.Mode = poo.CallModeNested
		rec.Parent = "r2"
		return rec
	}
	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		toolCall("r1", 1, "r2", "codemode"),
		nested("r5", ""),
		nested("r6", "call_nested_2"),
	)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}

	results := map[string]*poo.ToolResultRecord{}
	messages := 0
	for _, rec := range loaded.Records {
		switch r := rec.(type) {
		case *poo.ToolResultRecord:
			results[r.Handle] = r
		case *poo.MessageRecord:
			messages++
		}
	}
	for _, handle := range []string{"r5", "r6"} {
		result := results[handle]
		if result == nil {
			t.Fatalf("the nested call %s has no recovered result", handle)
		}
		if result.Outcome != poo.OutcomeInterrupted {
			t.Errorf("the recovered outcome of %s is %q, want interrupted", handle, result.Outcome)
		}
	}
	// No message at all: the model never saw either nested call, so
	// neither gets a result message — the parent keeps them out of the
	// history, with or without a provider id.
	if messages != 0 {
		t.Errorf("the recovered log holds %d messages, want 0 (the recovery writes none for a nested call)", messages)
	}
}

// Review scratch test: CallIDs pairs calls with ids by ordinal, so a
// batch whose records landed only in part pairs the wrong id.

// A parallel batch made three calls; the process was killed after the
// record of the second one (r2) landed. The assistant message holds all
// three call blocks (id1, id2, id3). The k-th call record of the turn is
// the k-th call block, so r2 is paired with id1 — the first call's id —
// and the recovery's interrupted answer names the wrong call.
func TestAPartialBatchPairsEachSurvivingCallWithItsOwnID(t *testing.T) {
	dir, store := newTestStore(t)
	_ = dir
	msg := &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      "r1",
		Turn:       1,
		Role:       poo.RoleAssistant,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentToolCall, ToolCallID: "id1", Name: "blocker", Arguments: json.RawMessage(`{}`)},
			{Kind: poo.ContentToolCall, ToolCallID: "id2", Name: "blocker", Arguments: json.RawMessage(`{}`)},
			{Kind: poo.ContentToolCall, ToolCallID: "id3", Name: "blocker", Arguments: json.RawMessage(`{}`)},
		},
	}
	writeSession(t, dir, "s1",
		testHeader("s1", newBlockingTool()),
		runStarted("r1"),
		msg,
		toolCallWithID("r1", 1, "r2", "blocker", "id2"),
	)
	loaded, err := store.Load(context.Background(), "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// The surviving call record is r2, and r2 is the second call the
	// model made: its id is id2, not the first call's.
	answered := map[string]bool{}
	for _, rec := range loaded.Records {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.Handle != "r2" {
			continue
		}
		if r.CallID != "id2" {
			t.Errorf("the result for r2 carries the call id %q, want id2", r.CallID)
		}
	}
	for _, rec := range loaded.Records {
		m, ok := rec.(*poo.MessageRecord)
		if !ok {
			continue
		}
		for _, b := range m.Content {
			if b.Kind == poo.ContentToolResult {
				answered[b.ToolCallID] = true
			}
		}
	}
	// Every call the model saw is answered, and each answer names its own
	// call.
	for _, id := range []string{"id1", "id2", "id3"} {
		if !answered[id] {
			t.Errorf("the recovery did not answer %q", id)
		}
	}
}

// echoTool is the read-only tool the crash sweep's session calls.
type echoTool struct{}

// Spec describes the tool.
func (echoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "the crash sweep's echo",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"additionalProperties":false}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return blockingArgs{}, nil
}

// Call answers at once.
func (echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "echoed"}, nil
}

// denyPolicy denies the echo call whose arguments say the word.
type denyPolicy struct{}

// BeforeTool denies one call and allows the rest.
func (denyPolicy) BeforeTool(_ poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	if info.Spec.Name == "echo" && strings.Contains(string(info.Arguments), "deny") {
		return poo.Deny("the crash sweep denies this call"), nil
	}
	return poo.Allow(), nil
}

// crashSession returns the tools and header a crash sweep session is
// built with.
func crashSession(t *testing.T) (poo.Tool, poo.Tool, *poo.SessionCreatedRecord) {
	t.Helper()
	echo := echoTool{}
	job := pootest.NewJobTool("job", poo.BackgroundAlways)
	job.SetResult("the job's output")
	model := testModel()
	header := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:    "s1",
			Mode:         string(ModeLogged),
			CreatedAt:    "2026-10-20T00:00:00Z",
			WorkingDir:   "/tmp",
			Profile:      testProfile,
			Roles:        poo.ModelRoles{Chat: &model},
			Tools:        []poo.ToolInfo{testToolInfo(echo), testToolInfo(job)},
			SettingsHash: testSettingsHash, Build: poo.BuildInfo{Name: "poo", Version: "test"}, Model: poo.SessionModel{ModelID: model.ModelID, Endpoint: model.Endpoint},
			ContextLimit: model.ContextLimit,
			ToolsMode:    "native"},
	}
	return echo, job, header
}

// crashOptions returns the options a restore of the crash sweep's
// session takes.
func crashOptions(echo, job poo.Tool, transport poo.ChatTransport) poo.Options {
	model := testModel()
	return poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{"echo": echo, "job": job},
		ToolOrder:    []string{"echo", "job"},
		SessionID:    "s1",
		Model:        model,
		Models:       map[string]poo.ModelInfo{model.Name: model},
		Profiles:     []poo.Profile{{Name: testProfile, Tools: []poo.ToolInfo{testToolInfo(echo), testToolInfo(job)}}},
		SettingsHash: testSettingsHash,
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
		SystemPrompt: "the prompt",
		Jobs:         poo.JobOptions{OnIdle: poo.OnIdleHold},
	}
}

// crashLog drives one session through tool calls, a parallel batch,
// a denied call, a job and an ask, and returns its log's lines.
//
// The ask belongs to a run that started and never settled, the state a
// crash while asking leaves.
func crashLog(t *testing.T) [][]byte {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	echo, jobTool, header := crashSession(t)
	if _, err := store.Create(context.Background(), header); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	transport := pootest.NewTransport(
		pootest.Turn{
			Finish: "tool_calls",
			Calls: []pootest.Call{
				{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`), ID: "c1"},
				{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`), ID: "c2"},
				{Name: "echo", Arguments: json.RawMessage(`{"text":"deny"}`), ID: "c3"},
				{Name: "job", Arguments: json.RawMessage(`{"text":"work"}`), ID: "c4"},
			},
		},
		pootest.Turn{Text: "done", Finish: "stop"},
	)
	model := testModel()
	sess, err := poo.New(poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{"echo": echo, "job": jobTool},
		ToolOrder:    []string{"echo", "job"},
		SessionID:    "s1",
		Model:        model,
		Models:       map[string]poo.ModelInfo{model.Name: model},
		Profiles:     []poo.Profile{{Name: testProfile, Tools: []poo.ToolInfo{testToolInfo(echo), testToolInfo(jobTool)}}},
		SettingsHash: testSettingsHash,
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
		SystemPrompt: "the prompt",
		Sink:         store,
		Policy:       denyPolicy{},
		Jobs:         poo.JobOptions{OnIdle: poo.OnIdleHold},
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	// The job's gate is open before the run starts, so the job finishes
	// while the run is still around to hear about it.
	jobTool.(*pootest.JobTool).ReleaseAll()
	if _, err := sess.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// The run that crashed while asking, and a job it started and never
	// ended. Both are the session's, so the recovery resolves them even
	// though an earlier run settled.
	path := filepath.Join(dir, "s1.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	for _, rec := range []poo.Record{
		runStarted("r2"),
		&poo.PermissionAskedRecord{
			RecordMeta: poo.NewRecordMeta("permission.asked"),
			RunID:      "r2",
			AskID:      "a1",
			Tool:       "echo",
			Reason:     "policy",
		},
		jobStarted("j9", "r9", "job", "r2"),
	} {
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("encoding the record: %v", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("writing the record: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing the log: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	lines := bytesLines(data)
	// The log must hold what the sweep says it drives: calls, a parallel
	// batch, a denied call, a job and an ask.
	calls := 0
	batch := 0
	denied := false
	jobs := 0
	ask := false
	for _, line := range lines {
		rec, err := NewCodec().Decode(line)
		if err != nil {
			t.Fatalf("decoding the log: %v", err)
		}
		switch r := rec.(type) {
		case *poo.ToolCallRecord:
			calls++
		case *poo.ToolResultRecord:
			if r.Outcome == poo.OutcomeDenied {
				denied = true
			}
		case *poo.MessageRecord:
			callsInMessage := 0
			for _, block := range r.Content {
				if block.Kind == poo.ContentToolCall {
					callsInMessage++
				}
			}
			if callsInMessage > batch {
				batch = callsInMessage
			}
		case *poo.JobStartedRecord:
			jobs++
		case *poo.PermissionAskedRecord:
			ask = true
		}
	}
	if calls < 4 {
		t.Fatalf("the log holds %d tool calls, want the batch, the denied call and the job", calls)
	}
	if batch < 2 {
		t.Fatalf("the log's largest batch holds %d calls, want a parallel batch", batch)
	}
	if !denied {
		t.Fatal("the log holds no denied call")
	}
	if jobs < 1 {
		t.Fatal("the log holds no job")
	}
	if !ask {
		t.Fatal("the log holds no ask")
	}
	return lines
}

// unanswered returns the first call id an assistant message carries
// with no result message, or the empty string when every call is paired.
func unanswered(recs []poo.Record) string {
	var calls []string
	results := map[string]bool{}
	for _, rec := range recs {
		m, ok := rec.(*poo.MessageRecord)
		if !ok {
			continue
		}
		for _, block := range m.Content {
			switch block.Kind {
			case poo.ContentToolCall:
				calls = append(calls, block.ToolCallID)
			case poo.ContentToolResult:
				results[block.ToolCallID] = true
			}
		}
	}
	for _, id := range calls {
		if id != "" && !results[id] {
			return id
		}
	}
	return ""
}

// TestEveryCrashBoundaryRecoversAPairedRequest sweeps the log's record
// boundaries: at each one the log is truncated, recovered twice,
// restored, and asked for a request. Every call is paired, the second
// recovery adds nothing, and the request the provider receives pairs
// every call with its result.
func TestEveryCrashBoundaryRecoversAPairedRequest(t *testing.T) {
	lines := crashLog(t)
	for boundary := 1; boundary <= len(lines); boundary++ {
		t.Run("boundary", func(t *testing.T) {
			dir := t.TempDir()
			var data []byte
			for _, line := range lines[:boundary] {
				data = append(data, line...)
				data = append(data, '\n')
			}
			if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), data, 0o600); err != nil {
				t.Fatalf("writing the log: %v", err)
			}
			checkBoundary(t, dir)
		})
	}
	// Once in the middle of a line: a process killed during a write.
	t.Run("mid-line", func(t *testing.T) {
		dir := t.TempDir()
		var data []byte
		for _, line := range lines {
			data = append(data, line...)
			data = append(data, '\n')
		}
		data = data[:len(data)-len(lines[len(lines)-1])/2]
		if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), data, 0o600); err != nil {
			t.Fatalf("writing the log: %v", err)
		}
		checkBoundary(t, dir)
	})
}

// checkBoundary recovers and restores one truncated log and checks
// the invariant.
func checkBoundary(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("the first load: %v", err)
	}
	if id := unanswered(loaded.Records); id != "" {
		t.Fatalf("the recovered session leaves the call %q unanswered", id)
	}
	before := len(loaded.Records)
	again, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("the second load: %v", err)
	}
	if len(again.Records) != before {
		t.Fatalf("the second recovery added %d records, want none", len(again.Records)-before)
	}
	if id := unanswered(again.Records); id != "" {
		t.Fatalf("the session leaves the call %q unanswered", id)
	}

	echo, jobTool, _ := crashSession(t)
	transport := pootest.NewTransport(
		pootest.Turn{Text: "one", Finish: "stop"},
		pootest.Turn{Text: "two", Finish: "stop"},
		pootest.Turn{Text: "three", Finish: "stop"},
	)
	sess, _, err := poo.Restore(crashOptions(echo, jobTool, transport), again.Records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	defer sess.Close()
	if _, err := sess.Prompt(ctx, "carry on"); err != nil {
		t.Fatalf("prompting the restored session: %v", err)
	}
	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("waiting for the restored session: %v", err)
	}
	requests := transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the restored session made no request")
	}
	for _, req := range requests {
		calls := map[string]bool{}
		results := map[string]bool{}
		for _, msg := range req.Messages {
			for _, block := range msg.Content {
				switch block.Kind {
				case poo.ContentToolCall:
					calls[block.ToolCallID] = true
				case poo.ContentToolResult:
					results[block.ToolCallID] = true
				}
			}
		}
		for id := range calls {
			if !results[id] {
				t.Errorf("the request carries the call %q with no result", id)
			}
		}
	}
}

// Review scratch tests: the restored session's job ids restart at j1,
// because the job counter is not seeded from the records.

// backgroundTool runs every call as a background job.
type backgroundTool struct {
	blockingTool
}

// Spec describes the tool, with the background mode that makes every
// call a job.
func (t *backgroundTool) Spec() poo.ToolSpec {
	spec := t.blockingTool.Spec()
	spec.Background = poo.BackgroundAlways
	return spec
}

// TestARestoredSessionContinuesTheJobIDs checks that a job the restored
// session starts continues the recorded job ids.
func TestARestoredSessionContinuesTheJobIDs(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := &backgroundTool{blockingTool: *newBlockingTool()}
	writeSession(t, dir, "s1",
		testHeader("s1", tool),
		runStarted("r1"),
		jobStarted("j1", "r1", tool.Spec().Name, "r1"),
		jobEnded("j1", "r1", tool.Spec().Name, "r1", "s1-ji1", "the job's result"),
		inputDelivered("s1-ji1", poo.InputModeJob),
	)
	// A second process would hold the store; Resume re-opens it here.
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	t.Cleanup(func() { store2.Close() })

	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{Name: tool.Spec().Name, Arguments: json.RawMessage(`{}`), ID: "c1"}}},
		pootest.Turn{Text: "ok", Finish: "stop"},
	)
	res, err := Resume(ctx, store2, "s1", testOptions("s1", tool, transport))
	if err != nil {
		t.Fatalf("resuming: %v", err)
	}
	if _, err := res.Session.Prompt(ctx, "run a job"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	sawStart := false
	for time.Now().Before(deadline) {
		recs := logRecords(t, dir+"/s1.jsonl")
		for _, rec := range recs {
			if started, ok := rec.(*poo.JobStartedRecord); ok {
				sawStart = true
				if started.JobID != "j1" {
					// A job id past the recorded one: the counter continued.
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sawStart {
		t.Fatalf("control failed: the restored session started no job at all")
	}
	t.Errorf("the restored session started no job with a new id: its first job reuses j1")
}

// TestAJobOfARestoredSessionIsRecoveredOnTheNextLoad shows the loss:
// the restored session's job was cut short, and its id collides with a
// job the records already ended, so the recovery sees nothing pending.
func TestAJobOfARestoredSessionIsRecoveredOnTheNextLoad(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	writeSession(t, dir, "s1",
		testHeader("s1", newBlockingTool()),
		runStarted("r1"),
		jobStarted("j1", "r1", "blocker", "r1"),
		jobEnded("j1", "r1", "blocker", "r1", "s1-ji1", "the job's result"),
		inputDelivered("s1-ji1", poo.InputModeJob),
		// The restored session's own first job: j2, and the process died
		// while it ran.
		jobStarted("j2", "r2", "blocker", "r2"),
	)
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// The second j1 has no end: the recovery should end it and queue
	// its result again.
	ends := 0
	for _, rec := range loaded.Records {
		if e, ok := rec.(*poo.JobEndedRecord); ok && e.JobID == "j1" {
			ends++
		}
	}
	if len(loaded.Jobs) != 1 {
		t.Errorf("the load queues %d job results, want the recovered job's result (job.ended records for j1: %d)", len(loaded.Jobs), ends)
	}
}

// TestAFaultAtEveryRecoveryBoundaryIsFinished sweeps the injected append
// failure over every record the recovery writes, and checks the second
// load finishes the job with no duplicate and no missing record.
func TestAFaultAtEveryRecoveryBoundaryIsFinished(t *testing.T) {
	for fault := 1; fault <= 5; fault++ {
		t.Run("", func(t *testing.T) {
			dir, store := newTestStore(t)
			ctx := context.Background()
			writeSession(t, dir, "s1",
				createdRecord("s1"),
				runStarted("r1"),
				&poo.MessageRecord{
					RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
					RunID:      "r1",
					Turn:       1,
					Role:       poo.RoleAssistant,
					MessageID:  "m1",
					Content: []poo.ContentBlock{{
						Kind:       poo.ContentToolCall,
						ToolCallID: "call_1",
						Name:       "bash",
						Arguments:  []byte(`{}`),
					}},
				},
				toolCallWithID("r1", 1, "r1", "bash", "call_1"),
				jobStarted("j1", "r1", "bash", "r1"),
				inputQueued("in1", poo.FollowUp),
			)
			store.appendFault = fault
			if _, err := store.Load(ctx, "s1"); err == nil {
				t.Fatal("the first load succeeded, want the injected failure")
			}
			store.appendFault = 0
			loaded, err := store.Load(ctx, "s1")
			if err != nil {
				t.Fatalf("the second load: %v", err)
			}
			counts := map[string]int{}
			for _, rec := range loaded.Records {
				counts[rec.Meta().Record]++
			}
			if counts["tool.result"] != 1 {
				t.Errorf("the session holds %d tool results, want 1", counts["tool.result"])
			}
			if counts["message"] != 2 { // the assistant call and the result's message
				t.Errorf("the session holds %d messages, want 2", counts["message"])
			}
			if counts["run.settled"] != 1 {
				t.Errorf("the session holds %d settles, want 1", counts["run.settled"])
			}
			if counts["job.ended"] != 1 {
				t.Errorf("the session holds %d job ends, want 1", counts["job.ended"])
			}
			if counts["input.dropped"] != 1 {
				t.Errorf("the session holds %d dropped inputs, want 1", counts["input.dropped"])
			}
			if len(loaded.Jobs) != 1 {
				t.Errorf("the load queues %d job results, want 1", len(loaded.Jobs))
			}
			// A third load adds nothing.
			before := len(loaded.Records)
			third, err := store.Load(ctx, "s1")
			if err != nil {
				t.Fatalf("the third load: %v", err)
			}
			if len(third.Records) != before {
				t.Errorf("the third load holds %d records, want %d", len(third.Records), before)
			}
		})
	}
}

// bashTool is a side-effecting tool named the way the fixtures name
// theirs, so a restored session's compatibility check passes.
type bashTool struct{}

// Spec describes the tool.
func (bashTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "bash",
		Description: "the fixture tool",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Class:       poo.ClassSideEffecting,
	}
}

// Prepare accepts the call.
func (bashTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return blockingArgs{}, nil
}

// Call returns at once.
func (bashTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "done"}, nil
}

// readTool is the read-only tool the fixture header names.
type readTool struct{ bashTool }

// Spec describes the tool.
func (readTool) Spec() poo.ToolSpec {
	spec := bashTool{}.Spec()
	spec.Name = "read"
	spec.Class = poo.ClassReadOnly
	return spec
}

// assistantCall returns the assistant message that carries a tool call.
func assistantCall(runID string, turn int, callID, tool string) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      runID,
		Turn:       turn,
		Role:       poo.RoleAssistant,
		MessageID:  "m1",
		Content: []poo.ContentBlock{{
			Kind:       poo.ContentToolCall,
			ToolCallID: callID,
			Name:       tool,
			Arguments:  json.RawMessage(`{}`),
		}},
	}
}

// recoveryOptions returns the options a restore of the fixture session
// takes.
func recoveryOptions(id string, tool poo.Tool, transport poo.ChatTransport) poo.Options {
	return poo.Options{
		Transport: transport,
		Tools: map[string]poo.Tool{
			tool.Spec().Name: tool,
			"read":           readTool{},
		},
		ToolOrder:    []string{tool.Spec().Name, "read"},
		SessionID:    id,
		Model:        poo.ModelInfo{ModelID: "m", ContextLimit: 32768},
		Sink:         nil,
		SystemPrompt: "the prompt",
	}
}

// TestARecoveredCallIsAnsweredInTheNextRequest is the
// consequence of the recovery's two-record write: the result lands, its
// message does not, and the next request carries a call the provider
// cannot match.
func TestARecoveredCallIsAnsweredInTheNextRequest(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := bashTool{}
	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		assistantCall("r1", 1, "call_1", "bash"),
		toolCallWithID("r1", 1, "r1", "bash", "call_1"),
	)

	// The result is appended, the message that carries it fails.
	store.appendFault = 2
	if _, err := store.Load(ctx, "s1"); err == nil {
		t.Fatal("the load succeeded, want the injected failure")
	}
	store.appendFault = 0

	transport := pootest.NewTransport(pootest.Turn{Text: "ok", Finish: "stop"})
	resumed, err := Resume(ctx, store, "s1", recoveryOptions("s1", tool, transport))
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	t.Cleanup(func() { resumed.Session.Close() })
	if _, err := resumed.Session.Prompt(ctx, "carry on"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := resumed.Session.Wait(ctx); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}
	requests := transport.Requests()
	if len(requests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(requests))
	}
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, msg := range requests[0].Messages {
		for _, block := range msg.Content {
			switch block.Kind {
			case poo.ContentToolCall:
				calls[block.ToolCallID] = true
			case poo.ContentToolResult:
				results[block.ToolCallID] = true
			}
		}
	}
	var unanswered []string
	for id := range calls {
		if !results[id] {
			unanswered = append(unanswered, id)
		}
	}
	if len(unanswered) > 0 {
		t.Errorf("the next request carries the calls %s with no result", strings.Join(unanswered, ","))
	}
}

// Review scratch tests: crash windows that leave a tool call the model
// saw without a result, which the recovery never repairs.

// assistantCallMessage returns the assistant message record that carries
// one tool call, the way the core records a model response with calls.
func assistantCallMessage(runID string, turn int, callID, handle, tool string) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      runID,
		Turn:       turn,
		Role:       poo.RoleAssistant,
		Content: []poo.ContentBlock{{
			Kind:       poo.ContentToolCall,
			ToolCallID: callID,
			Name:       tool,
			Arguments:  json.RawMessage(`{}`),
		}},
	}
}

// the log holds the assistant message and the tool.call record, but the
// process was killed before the tool.result record: the recovery answers
// the call.
func TestRecoveryAnswersACallWithNoResult(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		assistantCallMessage("r1", 1, "call-1", "r1", "bash"),
		toolCall("r1", 1, "r1", "bash"),
	)
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	hasResult := false
	hasMessage := false
	for _, rec := range loaded.Records {
		switch r := rec.(type) {
		case *poo.ToolResultRecord:
			if r.Handle == "r1" {
				hasResult = true
			}
		case *poo.MessageRecord:
			for _, b := range r.Content {
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					hasMessage = true
				}
			}
		}
	}
	if !hasResult || !hasMessage {
		t.Fatalf("control: the recovery wrote result=%v message=%v, want both", hasResult, hasMessage)
	}
}

// A kill between the tool.result record and the message record that
// carries it leaves the call unanswered for good: the recovery keys on
// the result record, so the next load writes no message, and the
// restored session sends a request whose tool call has no result.
func TestAKillBetweenTheResultAndItsMessageIsRepaired(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := newBlockingTool()
	writeSession(t, dir, "s1",
		testHeader("s1", tool),
		runStarted("r1"),
		assistantCallMessage("r1", 1, "call-1", "r1", "blocker"),
		toolCallWithID("r1", 1, "r1", "blocker", "call-1"),
	)

	// The recovery's second write (the message) fails: the state a kill
	// leaves after the tool.result record landed.
	store.appendFault = 2
	if _, err := store.Load(ctx, "s1"); err == nil {
		t.Fatal("the load succeeded, want the injected failure")
	}
	store.appendFault = 0
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	hasResult := false
	hasMessage := false
	for _, rec := range loaded.Records {
		switch r := rec.(type) {
		case *poo.ToolResultRecord:
			if r.Handle == "r1" {
				hasResult = true
			}
		case *poo.MessageRecord:
			for _, b := range r.Content {
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					hasMessage = true
				}
			}
		}
	}
	if !hasResult {
		t.Fatal("the log holds no tool.result record, want one")
	}
	if !hasMessage {
		t.Fatal("the log holds no message for the result, want it repaired")
	}

	transport := pootest.NewTransport(pootest.Turn{Text: "ok"})
	opts := testOptions("s1", tool, transport)
	res, err := Resume(ctx, store, "s1", opts)
	if err != nil {
		t.Fatalf("resuming: %v", err)
	}
	sess := res.Session
	if _, err := sess.Prompt(ctx, "continue"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	requests := transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the resumed session made no request")
	}
	unpaired := false
	for _, req := range requests {
		for _, msg := range req.Messages {
			for _, b := range msg.Content {
				if b.Kind == poo.ContentToolCall && b.ToolCallID == "call-1" {
					unpaired = true
				}
			}
		}
	}
	if !unpaired {
		t.Fatal("control failed: the request has no tool call")
	}
	// The call is in the request. Does any message answer it?
	answered := false
	for _, req := range requests {
		for _, msg := range req.Messages {
			for _, b := range msg.Content {
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					answered = true
				}
			}
		}
	}
	if !answered {
		t.Errorf("the resumed request carries the call call-1 with no result")
	}
}

// A kill between the assistant message record and the tool.call record
// leaves a call the model saw with no record at all: the recovery sees
// no call, settles the run, and the restored session sends the call
// with no result.
func TestAKillBeforeTheCallRecordIsRepaired(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := newBlockingTool()
	writeSession(t, dir, "s1",
		testHeader("s1", tool),
		runStarted("r1"),
		assistantCallMessage("r1", 1, "call-1", "r1", "blocker"),
	)
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// The recovery wrote the result the model never saw a record for.
	hasResult := false
	for _, rec := range loaded.Records {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			hasResult = true
			if r.CallID != "call-1" {
				t.Errorf("the recovered result carries the call id %q, want call-1", r.CallID)
			}
		}
	}
	if !hasResult {
		t.Fatal("the recovery wrote no result record, want one")
	}

	transport := pootest.NewTransport(pootest.Turn{Text: "ok"})
	opts := testOptions("s1", tool, transport)
	res, err := Resume(ctx, store, "s1", opts)
	if err != nil {
		t.Fatalf("resuming: %v", err)
	}
	sess := res.Session
	if _, err := sess.Prompt(ctx, "continue"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	sawCall := false
	answered := false
	for _, req := range transport.Requests() {
		for _, msg := range req.Messages {
			for _, b := range msg.Content {
				if b.Kind == poo.ContentToolCall && b.ToolCallID == "call-1" {
					sawCall = true
				}
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					answered = true
				}
			}
		}
	}
	if sawCall && !answered {
		t.Errorf("the resumed request carries the call call-1 with no result")
	}
	if !sawCall {
		t.Fatal("control failed: the request has no tool call")
	}
}

// TestACorruptMiddleLineDoesNotHideLaterActivity covers the summary
// scan: one record that does not decode does not stop the activity that
// followed it.
func TestACorruptMiddleLineDoesNotHideLaterActivity(t *testing.T) {
	dir, store := newTestStore(t)
	at := pruneNow.Add(-time.Hour)
	path := sessionAt(t, dir, "s1", pruneNow.Add(-3*time.Hour))
	appendBytes(t, path, []byte("this is not a record\n"))
	started := runStarted("r2")
	started.TS = at.Format(time.RFC3339Nano)
	started.StartedAt = at.Format(time.RFC3339Nano)
	b, err := jsonBytes(started)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	appendBytes(t, path, append(b, '\n'))
	summaries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("the store lists %d sessions, want 1", len(summaries))
	}
	if !summaries[0].LastActivity.Equal(at) {
		t.Errorf("the session's last activity is %s, want %s", summaries[0].LastActivity, at)
	}
}

// TestResumeReportsEachWarningOnce covers the two warning lists: the
// store and the restore say the same thing, and a resumed session
// reports it once.
func TestResumeReportsEachWarningOnce(t *testing.T) {
	dir, store := newTestStore(t)
	tool := newBlockingTool()
	created := testHeader("s1", tool)
	created.Build = poo.BuildInfo{Name: "poo", Version: "old"}
	created.SettingsHash = "old-settings"
	writeSession(t, dir, "s1", created, runStarted("r1"), &poo.RunSettledRecord{
		RecordMeta: poo.NewRecordMeta("run.settled"),
		RunID:      "r1",
		Reason:     poo.ReasonEndTurn,
		Detail:     poo.SettleDetail{Source: "model"},
		SettledAt:  "2026-10-20T00:00:01Z",
	})
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}
	other, err := NewStore(dir,
		WithBuild(poo.BuildInfo{Name: "poo", Version: "new"}),
		WithSettingsHash("new-settings"))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer other.Close()
	tr := pootest.NewTransport(pootest.Turn{Text: "done"})
	opts := testOptions("s1", tool, tr)
	opts.Build = poo.BuildInfo{Name: "poo", Version: "new"}
	opts.SettingsHash = "new-settings"
	resumed, err := Resume(context.Background(), other, "s1", opts)
	if err != nil {
		t.Fatalf("resuming: %v", err)
	}
	defer resumed.Session.Close()
	if len(resumed.Loaded.Warnings) != 0 {
		t.Errorf("the load holds %v, want the warnings merged into the restore", resumed.Loaded.Warnings)
	}
	seen := map[string]bool{}
	for _, warning := range resumed.Restored.Warnings {
		if seen[warning] {
			t.Errorf("the warning %q is reported twice", warning)
		}
		seen[warning] = true
	}
	if len(resumed.Restored.Warnings) != 2 {
		t.Errorf("the resume reports %v, want two warnings", resumed.Restored.Warnings)
	}
}

// jsonBytes encodes a record.
func jsonBytes(rec poo.Record) ([]byte, error) {
	return json.Marshal(rec)
}

// Review scratch test: Pending reads a session it holds no authority
// over, and the read truncates the file.

// A store whose session is held by another store (another process, in
// the terms) still truncates the session's file when Pending
// reads a torn tail. The truncation belongs to the recovery, under
// the Authority lock; Pending holds no authority.
func TestPendingDoesNotTruncateAFileItHasNoAuthorityOver(t *testing.T) {
	dir, storeA := newTestStore(t)
	ctx := context.Background()
	if _, err := storeA.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating: %v", err)
	}

	// The lock holder's file: a complete record, and then a write that
	// is on its way (the bytes a kill in the middle of a write leaves).
	path := filepath.Join(dir, "s1.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	appendBytes(t, path, []byte(`{"record":"run.started","run_id`))
	mid, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	// Another store asks about the pending operations. It holds no
	// authority: the session is locked by store A.
	storeB, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the second store: %v", err)
	}
	defer storeB.Close()
	pend, err := storeB.Pending(ctx, "s1")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if pend == nil || len(pend.Runs) != 0 {
		t.Logf("pending: %+v", pend)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(after) != len(mid) {
		t.Errorf("the file shrank from %d to %d bytes: Pending truncated a session it holds no authority over (the holder's lock is still held)", len(mid), len(after))
	}
	_ = before
}
