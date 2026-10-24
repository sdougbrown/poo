package session

// Review scratch tests: the restored session's job ids restart at j1,
// because the job counter is not seeded from the records.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// fix1BackgroundTool runs every call as a background job.
type fix1BackgroundTool struct {
	blockingTool
}

// Spec describes the tool, with the background mode that makes every
// call a job.
func (t *fix1BackgroundTool) Spec() poo.ToolSpec {
	spec := t.blockingTool.Spec()
	spec.Background = poo.BackgroundAlways
	return spec
}

// TestARestoredSessionContinuesTheJobIDs checks that a job the restored
// session starts continues the recorded job ids.
func TestARestoredSessionContinuesTheJobIDs(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := &fix1BackgroundTool{blockingTool: *newBlockingTool()}
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
			if started, ok := rec.(*poo.JobStarted); ok {
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
		if e, ok := rec.(*poo.JobEnded); ok && e.JobID == "j1" {
			ends++
		}
	}
	if len(loaded.Jobs) != 1 {
		t.Errorf("the load queues %d job results, want the recovered job's result (job.ended records for j1: %d)", len(loaded.Jobs), ends)
	}
}
