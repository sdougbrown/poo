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

// The store and the restore, end to end.
//
// A process is killed in the middle of a tool call, and another process
// loads the session, recovers it and restores it. The restored session
// runs again, and the model sees a history whose calls are all
// answered.

// The names the sessions in this test are built with.
const (
	// testProfile is the profile the session was created with.
	testProfile = "test"
	// testSettingsHash is the hash of the settings the session was
	// created with.
	testSettingsHash = "settings-1"
	// testCallID is the provider's id of the call the model made.
	testCallID = "call-1"
)

// testModel returns the model the sessions in this test talk to.
func testModel() poo.ModelInfo {
	return poo.ModelInfo{
		Name:         "test-model",
		Endpoint:     "http://127.0.0.1:1/v1",
		ModelID:      "test-model",
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: 8192,
	}
}

// testToolInfo returns the record of a tool the session was built
// with.
func testToolInfo(tool poo.Tool) poo.ToolInfo {
	spec := tool.Spec()
	return poo.ToolInfo{Name: spec.Name, Class: spec.Class, Background: spec.Background}
}

// testOptions returns the options a session in this test is built with.
//
// The options are the ones the caller of a restore has: the tools it
// links, the models it offers and the profile it knows.
func testOptions(id string, tool poo.Tool, transport poo.ChatTransport) poo.Options {
	model := testModel()
	return poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{tool.Spec().Name: tool},
		ToolOrder:    []string{tool.Spec().Name},
		SessionID:    id,
		Model:        model,
		Models:       map[string]poo.ModelInfo{model.Name: model},
		Profiles:     []poo.Profile{{Name: testProfile, Tools: []poo.ToolInfo{testToolInfo(tool)}}},
		SettingsHash: testSettingsHash,
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
	}
}

// testHeader returns the header of the session the helper creates.
func testHeader(id string, tool poo.Tool) *poo.SessionCreated {
	model := testModel()
	return &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    id,
		Mode:         string(ModeLogged),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		WorkingDir:   "/tmp",
		Profile:      testProfile,
		Roles:        poo.ModelRoles{Chat: &model},
		Tools:        []poo.ToolInfo{testToolInfo(tool)},
		SettingsHash: testSettingsHash,
		Model:        poo.SessionModel{ID: model.ModelID, BaseURL: model.Endpoint},
		ContextLimit: model.ContextLimit,
		ToolsMode:    "native",
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
	}
}

// blockingTool is a side-effecting tool whose call never finishes.
//
// The call blocks until the test releases it, so a process can be
// killed in the middle of it: the log holds the call and no result.
type blockingTool struct {
	// started is signalled when a call starts.
	started chan struct{}
	// release lets a call finish.
	release chan struct{}
}

// newBlockingTool returns a tool that blocks.
func newBlockingTool() *blockingTool {
	return &blockingTool{started: make(chan struct{}, 1), release: make(chan struct{})}
}

// Spec describes the tool.
func (t *blockingTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "blocker",
		Description: "a tool that blocks until the test releases it",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Class:       poo.ClassSideEffecting,
	}
}

// Prepare accepts the call.
func (t *blockingTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return blockingArgs{}, nil
}

// Call blocks until the tool is released.
func (t *blockingTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	select {
	case t.started <- struct{}{}:
	default:
	}
	select {
	case <-t.release:
	case <-ctx.Done():
		return poo.ToolResult{Text: "the call was stopped"}, ctx.Err()
	}
	return poo.ToolResult{Text: "done"}, nil
}

// blockingArgs is a prepared call of the blocking tool.
type blockingArgs struct{}

// Summary describes the call.
func (blockingArgs) Summary() string { return "the blocking call" }

// runBlockedTurn creates a session and runs a turn that stops in the
// middle of a tool call.
//
// The session is left open: the process is about to be killed, and the
// call is what it is killed in the middle of.
func runBlockedTurn(ctx context.Context, store *FileStore, id string) error {
	tool := newBlockingTool()
	if _, err := store.Create(ctx, testHeader(id, tool)); err != nil {
		return err
	}
	transport := pootest.NewTransport(pootest.Turn{
		Calls: []pootest.Call{{Name: tool.Spec().Name, Arguments: json.RawMessage(`{}`), ID: testCallID}},
	})
	opts := testOptions(id, tool, transport)
	opts.Sink = store
	sess, err := poo.New(opts)
	if err != nil {
		return err
	}
	if _, err := sess.Prompt(ctx, "hello"); err != nil {
		return err
	}
	<-tool.started
	return nil
}

// TestARecoveredJobIsQueuedAgain covers the job a crash cut short: the
// recovery ends it, and the restore queues its result again, so the
// model hears how the job ended.
func TestARecoveredJobIsQueuedAgain(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	tool := newBlockingTool()
	writeSession(t, dir, "s1",
		testHeader("s1", tool),
		runStarted("r1"),
		jobStarted("j1", "r1", tool.Spec().Name, "r1"),
	)
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	transport := pootest.NewTransport(pootest.Turn{Text: "ok", Finish: "stop"})
	resumed, err := Resume(ctx, store, "s1", testOptions("s1", tool, transport))
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	t.Cleanup(func() { resumed.Session.Close() })

	// The restore queued the result the recovery ended.
	if len(resumed.Restored.Jobs) != 1 {
		t.Fatalf("the restore queued %d job results, want 1", len(resumed.Restored.Jobs))
	}
	queued := resumed.Restored.Jobs[0]
	if queued.Job != "j1" {
		t.Errorf("the queued result is for %q, want j1", queued.Job)
	}
	if queued.Outcome != poo.OutcomeInterrupted {
		t.Errorf("the queued result's outcome is %q, want %q", queued.Outcome, poo.OutcomeInterrupted)
	}
	if !strings.Contains(queued.Text, "outcome unknown") {
		t.Errorf("the queued result's text is %q, want it to say the outcome is unknown", queued.Text)
	}

	// The result reached the model: the idle session woke a run, and
	// the run's request carried the result.
	waitForRequest(t, transport)
	requests := transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the restored session made no request, want the job result delivered")
	}
	carried := false
	for _, msg := range requests[0].Messages {
		if strings.Contains(msg.Text(), "outcome unknown") {
			carried = true
		}
	}
	if !carried {
		t.Error("the request does not carry the job result, want the model to hear how the job ended")
	}
}

// waitForRequest waits until the transport has served a request.
func waitForRequest(t *testing.T, transport *pootest.Transport) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(transport.Requests()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestAKilledSessionIsRestored covers the whole seam: a process that
// was killed in the middle of a tool call, a load that recovers the
// session, a restore that rebuilds it, and a new prompt the model can
// answer.
func TestAKilledSessionIsRestored(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	helper, _ := startHelper(t, "run", dir, "s1")
	killHelper(t, helper)

	path := filepath.Join(dir, "s1.jsonl")
	// The process was killed in the middle of a write: the tail of the
	// log is a record that was never finished.
	appendBytes(t, path, []byte(`{"record":"tool.result","seq":42,"torn`))

	// The killed process left the lock free, so the load takes it.
	locked, err := Locked(LockPath(dir, "s1"))
	if err != nil {
		t.Fatalf("checking the lock: %v", err)
	}
	if locked {
		t.Fatal("the session is still locked, want the lock released")
	}

	transport := pootest.NewTransport(pootest.Turn{Text: "ok", Finish: "stop"})
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	resumed, err := Resume(ctx, store, "s1", testOptions("s1", newBlockingTool(), transport))
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	t.Cleanup(func() { resumed.Session.Close() })

	// The load took the authority, and it holds it until the store
	// closes.
	locked, err = Locked(LockPath(dir, "s1"))
	if err != nil {
		t.Fatalf("checking the lock: %v", err)
	}
	if !locked {
		t.Error("the session is not locked, want the load to hold it")
	}

	// The torn tail is gone.
	if !resumed.Loaded.RecoveredTail {
		t.Error("the load did not report a torn tail, want the tail truncated")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if strings.Contains(string(data), "torn") {
		t.Error("the log still holds the torn tail, want it truncated")
	}

	// The synthetic records are there, and the settle is last.
	recs := resumed.Loaded.Records
	settled, ok := recs[len(recs)-1].(*poo.RunSettled)
	if !ok {
		t.Fatalf("the last record is %T, want a settle record", recs[len(recs)-1])
	}
	if settled.Reason != poo.ReasonInterrupted {
		t.Errorf("the settle reason is %q, want %q", settled.Reason, poo.ReasonInterrupted)
	}
	var interrupted *poo.ToolResultRecord
	for _, rec := range recs {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Outcome == poo.OutcomeInterrupted {
			interrupted = r
		}
	}
	if interrupted == nil {
		t.Fatal("the call has no interrupted result")
	}
	if interrupted.Handle != "r1" {
		t.Errorf("the interrupted result is for the handle %q, want r1", interrupted.Handle)
	}
	if !strings.Contains(interrupted.Text, "outcome unknown") {
		t.Errorf("the interrupted result's text is %q, want it to say the outcome is unknown", interrupted.Text)
	}
	// The result is in the model's view: the message that carries it
	// comes right before the settle.
	carried, ok := recs[len(recs)-2].(*poo.MessageRecord)
	if !ok {
		t.Fatalf("the record before the settle is %T, want the result's message", recs[len(recs)-2])
	}
	if len(carried.Content) != 1 || carried.Content[0].Kind != poo.ContentToolResult {
		t.Fatalf("the message holds %v, want the call's result", carried.Content)
	}
	if carried.Content[0].ToolCallID != testCallID {
		t.Errorf("the result answers the call %q, want %q", carried.Content[0].ToolCallID, testCallID)
	}

	// The session is restored, and the run that was cut short is the
	// one it knows about.
	if len(resumed.Restored.Interrupted) != 1 {
		t.Errorf("the restore knows of %d interrupted runs, want 1", len(resumed.Restored.Interrupted))
	}

	// The restored session runs again.
	if _, err := resumed.Session.Prompt(ctx, "more"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := resumed.Session.Wait(ctx); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}

	// The request the model sees holds every call with its result.
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
	if len(calls) != 1 {
		t.Fatalf("the request holds %d calls, want 1", len(calls))
	}
	for id := range calls {
		if !results[id] {
			t.Errorf("the call %s has no result in the request", id)
		}
	}

	// The restored session writes to the same store.
	if err := resumed.Session.Close(); err != nil {
		t.Fatalf("closing the restored session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// The second load finds nothing to recover: it adds no record.
	before := len(logRecords(t, path))
	again, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store again: %v", err)
	}
	defer again.Close()
	loaded, err := again.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session again: %v", err)
	}
	if len(loaded.Records) != before {
		t.Errorf("the second load holds %d records, want %d", len(loaded.Records), before)
	}
	if len(logRecords(t, path)) != before {
		t.Errorf("the second load wrote records, want %d", before)
	}
}
