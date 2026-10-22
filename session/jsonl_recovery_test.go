package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
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
		if result.Text != InterruptedCallText {
			t.Errorf("the call %s's text is %q, want %q", handle, result.Text, InterruptedCallText)
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
	settled, ok := recs[len(recs)-1].(*poo.RunSettled)
	if !ok {
		t.Fatalf("the last record is %T, want a settle record", recs[len(recs)-1])
	}
	if settled.Reason != ReasonInterrupted {
		t.Errorf("the settle reason is %q, want %q", settled.Reason, ReasonInterrupted)
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
		case *poo.RunSettled:
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
	if _, ok := recs[len(recs)-1].(*poo.RunSettled); !ok {
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
		if started, ok := rec.(*poo.RunStarted); ok && started.RunID == "r2" {
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
	ended, ok := loaded.Records[len(loaded.Records)-1].(*poo.JobEnded)
	if !ok {
		t.Fatalf("the last record is %T, want a job's end", loaded.Records[len(loaded.Records)-1])
	}
	if ended.Outcome != poo.JobOutcomeInterrupted {
		t.Errorf("the job's outcome is %q, want %q", ended.Outcome, poo.JobOutcomeInterrupted)
	}
	if ended.Text != InterruptedJobText {
		t.Errorf("the job's text is %q, want %q", ended.Text, InterruptedJobText)
	}
	if len(loaded.Requeue) != 1 {
		t.Fatalf("the load queues %d job results, want 1", len(loaded.Requeue))
	}
	if loaded.Requeue[0].Job != "j1" {
		t.Errorf("the queued result is for %q, want j1", loaded.Requeue[0].Job)
	}
	if loaded.Requeue[0].Outcome != poo.JobOutcomeInterrupted {
		t.Errorf("the queued result's outcome is %q, want %q", loaded.Requeue[0].Outcome, poo.JobOutcomeInterrupted)
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
	if len(loaded.Requeue) != 1 {
		t.Fatalf("the load queues %d job results, want 1", len(loaded.Requeue))
	}
	queued := loaded.Requeue[0]
	if queued.InputID != "s1-ji1" {
		t.Errorf("the queued result's input id is %q, want s1-ji1", queued.InputID)
	}
	if queued.Text != "the job's result" {
		t.Errorf("the queued result's text is %q, want the recorded text", queued.Text)
	}
	if queued.Outcome != poo.JobOutcomeOK {
		t.Errorf("the queued result's outcome is %q, want %q", queued.Outcome, poo.JobOutcomeOK)
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
	if len(loaded.Requeue) != 0 {
		t.Errorf("the load queues %d job results, want none", len(loaded.Requeue))
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
		&poo.RunSettled{
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
	ended, ok := loaded.Records[len(loaded.Records)-1].(*poo.JobEnded)
	if !ok {
		t.Fatalf("the last record is %T, want a job's end", loaded.Records[len(loaded.Records)-1])
	}
	if ended.Job != "j1" {
		t.Errorf("the ended job is %q, want j1", ended.Job)
	}
	if len(loaded.Requeue) != 1 {
		t.Errorf("the load queues %d job results, want 1", len(loaded.Requeue))
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
	dropped, ok := loaded.Records[len(loaded.Records)-1].(*poo.InputDropped)
	if !ok {
		t.Fatalf("the last record is %T, want a dropped input", loaded.Records[len(loaded.Records)-1])
	}
	if dropped.InputID != "in1" {
		t.Errorf("the dropped input is %q, want in1", dropped.InputID)
	}
	if dropped.Reason != ReasonInterrupted {
		t.Errorf("the reason is %q, want %q", dropped.Reason, ReasonInterrupted)
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
