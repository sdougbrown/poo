package poo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/bash"
)

// The restore: a session rebuilt from the records of an earlier one.

// header returns a session header record for a test.
func header(sessionID string) *poo.SessionCreatedRecord {
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:    sessionID,
			Mode:         "logged",
			CreatedAt:    "2026-01-01T00:00:00Z",
			WorkingDir:   "/tmp",
			Profile:      "code",
			Roles:        poo.ModelRoles{Chat: &poo.ModelInfo{Name: "fast", ModelID: "test-model", ContextLimit: 32768, Ops: []poo.Op{poo.OpChat, poo.OpCompact}}},
			Tools:        []poo.ToolInfo{{Name: "echo", Class: poo.ClassReadOnly, Background: poo.BackgroundNever}},
			SettingsHash: "settings-1", Build: poo.BuildInfo{Name: "poo", Version: "test"}},
	}
}

// restoreOptions builds the options a restore gets.
//
// The options are the ones the original session ran with, plus the
// catalog the compatibility check reads.
func restoreOptions(t *testing.T, tr poo.ChatTransport, sink poo.RecordSink) poo.Options {
	t.Helper()
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Profiles = []poo.Profile{{Name: "code", Tools: []poo.ToolInfo{{Name: "echo", Class: poo.ClassReadOnly}}}}
	opts.Models = map[string]poo.ModelInfo{
		"fast": {Name: "fast", ModelID: "test-model", ContextLimit: 32768, Ops: []poo.Op{poo.OpChat, poo.OpCompact}},
	}
	opts.SettingsHash = "settings-1"
	opts.Build = poo.BuildInfo{Name: "poo", Version: "test"}
	return opts
}

// TestTheRestoredSessionSendsTheSameNextRequest covers the restore's
// promise: a restored session's next request is the request the
// original session would have sent.
func TestTheRestoredSessionSendsTheSameNextRequest(t *testing.T) {
	// The original session runs twice: the second run's request is the
	// one the restored session has to reproduce.
	original := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
		{text: "done"},
		{text: "ok"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, original, &echoTool{}, sink)
	s := newTestSession(t, opts)
	runSession(t, s, "hello")
	// The records of the first run are what the restored session is
	// built from.
	firstRun := append([]poo.Record{header("s1")}, sink.allRecords()...)
	runSession(t, s, "more")

	requests := original.Requests()
	if len(requests) != 3 {
		t.Fatalf("the original made %d requests, want 3", len(requests))
	}

	// The restored session is built from the records of the first run.
	restoredTransport := scripted([]fakeResponse{{text: "ok"}})
	records := firstRun
	session, _, err := poo.Restore(restoreOptions(t, restoredTransport, &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	if _, err := session.Prompt(context.Background(), "more"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}

	restoredRequests := restoredTransport.Requests()
	if len(restoredRequests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(restoredRequests))
	}
	// The original's next request is the one its second run made.
	want := requests[2].Messages
	got := restoredRequests[0].Messages
	if len(want) != len(got) {
		t.Fatalf("the restored request holds %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if want[i].Role != got[i].Role {
			t.Errorf("the message %d has the role %q, want %q", i, got[i].Role, want[i].Role)
		}
		if want[i].Text() != got[i].Text() {
			t.Errorf("the message %d holds %q, want %q", i, got[i].Text(), want[i].Text())
		}
		if len(want[i].Content) != len(got[i].Content) {
			t.Errorf("the message %d holds %d blocks, want %d", i, len(got[i].Content), len(want[i].Content))
		}
	}
	// The bodies are the same, byte for byte.
	originalBodies := original.Rendered()
	restoredBodies := restoredTransport.Rendered()
	if len(originalBodies) < 3 || len(restoredBodies) < 1 {
		t.Fatalf("the original rendered %d bodies and the restored %d", len(originalBodies), len(restoredBodies))
	}
	if !bytes.Equal(originalBodies[2].Body, restoredBodies[0].Body) {
		t.Errorf("the restored body is\n%s\nwant\n%s", restoredBodies[0].Body, originalBodies[2].Body)
	}
}

// TestTheRestoreRebuildsTheHistory covers the history a restore holds:
// the messages, the hook-appended messages and the tool results.
func TestTheRestoreRebuildsTheHistory(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.BeforeRun = []poo.BeforeRunHook{appendBeforeRun{}}
	s := newTestSession(t, opts)
	runSession(t, s, "hello")

	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	session, restored, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	var texts []string
	for _, m := range poo.Messages(session) {
		texts = append(texts, string(m.Role)+":"+m.Text())
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "the hook said something") {
		t.Errorf("the restored history holds no hook message: %s", joined)
	}
	if !strings.Contains(joined, "hello") {
		t.Errorf("the restored history holds no prompt: %s", joined)
	}
	if !strings.Contains(joined, "done") {
		t.Errorf("the restored history holds no reply: %s", joined)
	}
	if !strings.Contains(joined, "tool:") {
		t.Errorf("the restored history holds no tool result: %s", joined)
	}
	if restored.Messages == 0 {
		t.Errorf("the restore reported no messages")
	}
}

// TestTheRestoreKeepsTheCallsSummary checks that a restored turn keeps the
// tool call summary the records hold, as the after-turn hook reads it.
func TestTheRestoreKeepsTheCallsSummary(t *testing.T) {
	const description = "list the working directory"
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "bash", Arguments: []byte(`{"command":"true","description":` + jsonQuote(description) + `}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{"bash": bash.New()},
		ToolOrder:  []string{"bash"},
		Sink:       sink,
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
	}
	s := newTestSession(t, opts)
	runSession(t, s, "hello")

	recorded := firstCallRecord(t, sink.Records())
	if recorded.Summary != description {
		t.Fatalf("the recorded call's summary is %q, want %q", recorded.Summary, description)
	}

	created := header("s1")
	created.Tools = []poo.ToolInfo{{Name: "bash", Class: poo.ClassSideEffecting, Background: poo.BackgroundAllowed}}
	records := append([]poo.Record{created}, sink.allRecords()...)

	// The restore invokes no hook, so a later turn is where a hook reads
	// the rebuilt turn summary.
	turns := &turnCapture{}
	restoreOpts := restoreOptions(t, scripted([]fakeResponse{{text: "again"}}), &recordingSink{})
	restoreOpts.Tools = map[string]poo.Tool{"bash": bash.New()}
	restoreOpts.ToolOrder = []string{"bash"}
	restoreOpts.Profiles = []poo.Profile{{Name: "code", Tools: []poo.ToolInfo{{Name: "bash", Class: poo.ClassSideEffecting}}}}
	restoreOpts.AfterTurn = []poo.AfterTurnHook{turns}
	session, _, err := poo.Restore(restoreOpts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	runSession(t, session, "more")

	found := false
	for _, snapshot := range turns.captured() {
		for _, turn := range snapshot {
			for _, call := range turn.Calls {
				if call.Handle != recorded.Handle {
					continue
				}
				found = true
				if call.Summary != recorded.Summary {
					t.Errorf("the restored turn summary is %q, want the recorded %q", call.Summary, recorded.Summary)
				}
			}
		}
	}
	if !found {
		t.Fatalf("the restored turn holds no call for %s", recorded.Handle)
	}
}

// TestTheRestoreInvokesNoHook covers the restore's rule: the records
// already hold what the hooks said, so no hook runs again.
func TestTheRestoreInvokesNoHook(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}})
	sink := &recordingSink{}
	start := &countingStart{}
	before := &countingBeforeRun{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SessionStart = []poo.SessionStartHook{start}
	opts.BeforeRun = []poo.BeforeRunHook{before}
	s := newTestSession(t, opts)
	runSession(t, s, "hello")
	if start.calls != 1 || before.calls != 1 {
		t.Fatalf("the original ran the hooks %d and %d times, want 1 and 1", start.calls, before.calls)
	}

	restoreOpts := restoreOptions(t, scripted(nil), &recordingSink{})
	restoreOpts.SessionStart = []poo.SessionStartHook{start}
	restoreOpts.BeforeRun = []poo.BeforeRunHook{before}
	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	session, _, err := poo.Restore(restoreOpts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	if start.calls != 1 || before.calls != 1 {
		t.Errorf("the restore ran the hooks %d and %d times, want 1 and 1", start.calls, before.calls)
	}
}

// TestTheRestoreRejectsAnUnknownProfile covers the compatibility check:
// a profile this build does not offer is an unknown profile.
func TestTheRestoreRejectsAnUnknownProfile(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Profiles = []poo.Profile{{Name: "base"}}
	_, _, err := poo.Restore(opts, records)
	if err == nil {
		t.Fatal("the restore accepted a profile this build does not offer")
	}
	if !errors.Is(err, poo.ErrUnknownProfile) {
		t.Errorf("the restore returned %v, want an unknown profile", err)
	}
	var incompatible *poo.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("the restore returned %T, want an incompatible error", err)
	}
	if incompatible.Code != poo.CodeUnknownProfile {
		t.Errorf("the error's code is %q, want %q", incompatible.Code, poo.CodeUnknownProfile)
	}
}

// TestTheRestoreRejectsAnUnknownModel covers the compatibility check:
// a role that names a model the settings do not offer is an unknown
// model.
func TestTheRestoreRejectsAnUnknownModel(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	delete(opts.Models, "fast")
	_, _, err := poo.Restore(opts, records)
	if err == nil {
		t.Fatal("the restore accepted a model the settings do not offer")
	}
	if !errors.Is(err, poo.ErrUnknownModel) {
		t.Errorf("the restore returned %v, want an unknown model", err)
	}
}

// TestTheRestoreRejectsAnIncompatibleTool covers the compatibility
// check: a tool that is gone, or that changed its class, does not fit.
func TestTheRestoreRejectsAnIncompatibleTool(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Tools = map[string]poo.Tool{}
	_, _, err := poo.Restore(opts, records)
	if err == nil {
		t.Fatal("the restore accepted a tool this build does not link")
	}
	if !errors.Is(err, poo.ErrSessionIncompatible) {
		t.Errorf("the restore returned %v, want an incompatible session", err)
	}
	var incompatible *poo.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("the restore returned %T, want an incompatible error", err)
	}
	if len(incompatible.Differences) == 0 {
		t.Error("the error names no difference")
	}
	if !strings.Contains(strings.Join(incompatible.Differences, ";"), "echo") {
		t.Errorf("the differences do not name the tool: %v", incompatible.Differences)
	}
}

// TestTheRestoreWarnsAboutAChangedBuild covers the compatibility
// check's warnings: a changed build or settings are allowed.
func TestTheRestoreWarnsAboutAChangedBuild(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.SettingsHash = "settings-2"
	opts.Build = poo.BuildInfo{Name: "poo", Version: "other"}
	_, restored, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	if len(restored.Warnings) != 2 {
		t.Errorf("the restore reported %d warnings, want 2: %v", len(restored.Warnings), restored.Warnings)
	}
}

// TestTheRestoreWarnsAboutADeclaredFeedTheFeedsDoNotRun covers the
// compatibility check's warnings: a session whose profile declares a
// feed this build's feeds do not run restores without it, and says so.
func TestTheRestoreWarnsAboutADeclaredFeedTheFeedsDoNotRun(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Profiles[0].Feeds = []string{"broker"}
	session, restored, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	if len(restored.Warnings) != 1 {
		t.Fatalf("the restore reported %d warnings, want 1: %v", len(restored.Warnings), restored.Warnings)
	}
	if !strings.Contains(restored.Warnings[0], `"broker"`) {
		t.Errorf("the warning does not name the missing feed: %q", restored.Warnings[0])
	}
}

// TestTheRestoreStaysQuietWhenADeclaredFeedRuns covers the quiet side:
// a declared feed the build's feeds do run warns about nothing.
func TestTheRestoreStaysQuietWhenADeclaredFeedRuns(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Profiles[0].Feeds = []string{"broker"}
	opts.Feeds = []poo.FeedSpec{{Name: "broker", Run: func(context.Context, poo.FeedIO) error { return nil }}}
	session, restored, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	if len(restored.Warnings) != 0 {
		t.Errorf("the restore reported %v, want no warnings", restored.Warnings)
	}
}

// TestTheRestoreRejectsAnotherFormat covers the codec: a record format
// this build cannot read is a typed error.
func TestTheRestoreRejectsAnotherFormat(t *testing.T) {
	created := header("s1")
	created.Format = "poo-session-v9"
	_, _, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), []poo.Record{created})
	if err == nil {
		t.Fatal("the restore accepted another format")
	}
	if !errors.Is(err, poo.ErrCodecVersion) {
		t.Errorf("the restore returned %v, want a codec error", err)
	}
}

// TestTheRestoreRequeuesAnUndeliveredJobResult covers the job results:
// a result that was never delivered is queued again.
func TestTheRestoreRequeuesAnUndeliveredJobResult(t *testing.T) {
	records := []poo.Record{
		header("s1"),
		&poo.JobStartedRecord{RecordMeta: poo.NewRecordMeta("job.started"), SessionID: "s1", JobID: "j1", Handle: "r1", Tool: "echo", RunID: "run-1"},
		&poo.JobEndedRecord{RecordMeta: poo.NewRecordMeta("job.ended"), SessionID: "s1", JobID: "j1", Handle: "r1", Tool: "echo", RunID: "run-1", Outcome: poo.OutcomeOK, InputID: "s1-ji1", Text: "the job said something"},
	}
	pending := poo.PendingJobResults(records)
	if len(pending) != 1 {
		t.Fatalf("the records hold %d pending job results, want 1", len(pending))
	}
	if pending[0].JobID != "j1" || pending[0].Text != "the job said something" {
		t.Errorf("the pending result is %+v, want the job's result", pending[0])
	}

	// A delivered result is in the history already.
	records = append(records, &poo.InputDeliveredRecord{RecordMeta: poo.NewRecordMeta("input.delivered"), SessionID: "s1", RunID: "run-1", InputID: "s1-ji1", Mode: poo.InputModeJob})
	if pending := poo.PendingJobResults(records); len(pending) != 0 {
		t.Errorf("the records hold %d pending job results, want none", len(pending))
	}
}

// appendBeforeRun is a before-run hook that appends a message.
type appendBeforeRun struct{}

// BeforeRun appends a message to the history.
func (h appendBeforeRun) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	return poo.StartDecision{Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the hook said something")}}, nil
}

// countingStart is a session-start hook that counts its calls.
type countingStart struct {
	// calls counts how many times the hook ran.
	calls int
}

// SessionStart counts the call.
func (h *countingStart) SessionStart(ctx poo.HookContext) (poo.StartDecision, error) {
	h.calls++
	return poo.StartDecision{}, nil
}

// countingBeforeRun is a before-run hook that counts its calls.
type countingBeforeRun struct {
	// calls counts how many times the hook ran.
	calls int
}

// BeforeRun counts the call.
func (h *countingBeforeRun) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	h.calls++
	return poo.StartDecision{}, nil
}

// TestTheRestoreHoldsTheSteeringAndTheCompactions covers the history a
// restore rebuilds: the steering the run took, and the compaction the
// view went through.
func TestTheRestoreHoldsTheSteeringAndTheCompactions(t *testing.T) {
	compactor := pootest.NewCompactor(true, poo.Compaction{
		Strategy: "the scripted one",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: 1, Last: 1},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary of the first turn")},
		},
	})
	compactor.SetShouldAfter(1)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	tool := &echoTool{}
	hold := newNoteHold(tool)
	// The held call still answers with the echo tool's text, so the check
	// below that compaction replaced the first turn has something to find.
	held := tool.call
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		result, err := held(ctx)
		result.Text = "echoed"
		return result, err
	}
	opts := testOptions(t, tr, tool, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The tool call holds the run open, so the steer reaches a running
	// run rather than one that already settled.
	hold.wait(t)
	if _, err := s.Send("also do this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	session, restored, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	if restored.Compactions != 1 {
		t.Errorf("the restore applied %d compactions, want 1", restored.Compactions)
	}

	var texts []string
	for _, m := range poo.Messages(session) {
		texts = append(texts, m.Text())
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "also do this") {
		t.Errorf("the restored history holds no steering: %s", joined)
	}
	if !strings.Contains(joined, "the summary of the first turn") {
		t.Errorf("the restored history holds no compaction: %s", joined)
	}
	if strings.Contains(joined, "echoed") {
		t.Errorf("the restored history holds the replaced turn: %s", joined)
	}
}

// TestTheRestoredSessionHoldsTheUndeliveredJobResults covers the job
// results a restore queues: they follow the session's idle policy, so
// a session that holds them delivers them at the next run.
func TestTheRestoredSessionHoldsTheUndeliveredJobResults(t *testing.T) {
	records := []poo.Record{
		header("s1"),
		&poo.JobStartedRecord{RecordMeta: poo.NewRecordMeta("job.started"), SessionID: "s1", JobID: "j1", Handle: "r1", Tool: "echo", RunID: "run-1"},
		&poo.JobEndedRecord{RecordMeta: poo.NewRecordMeta("job.ended"), SessionID: "s1", JobID: "j1", Handle: "r1", Tool: "echo", RunID: "run-1", Outcome: poo.OutcomeOK, InputID: "s1-ji1", Text: "the job said something"},
	}
	tr := scripted([]fakeResponse{{text: "ok"}})
	sink := &recordingSink{}
	opts := restoreOptions(t, tr, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	session, restored, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	if len(restored.Jobs) != 1 {
		t.Fatalf("the restore queued %d job results, want 1", len(restored.Jobs))
	}
	if sink.count("input.held") != 1 {
		t.Errorf("the log holds %d input.held records, want 1", sink.count("input.held"))
	}

	// The held result is delivered at the next run.
	if _, err := session.Prompt(context.Background(), "more"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	requests := tr.Requests()
	if len(requests) == 0 {
		t.Fatal("the restored session made no request")
	}
	found := false
	for _, m := range requests[0].Messages {
		if strings.Contains(m.Text(), "the job said something") {
			found = true
		}
	}
	if !found {
		t.Error("the model did not read the job's result")
	}
}

// backgroundTool is a tool with a background mode.
type backgroundTool struct {
	echoTool
	// background is the tool's background mode.
	background poo.Background
}

// Spec describes the tool with its background mode.
func (t *backgroundTool) Spec() poo.ToolSpec {
	spec := t.echoTool.Spec()
	spec.Background = t.background
	return spec
}

// TestTheRestoreRejectsAToolThatChangedItsClass covers the
// compatibility check: a tool that changed its class does not fit.
func TestTheRestoreRejectsAToolThatChangedItsClass(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	// The recorded tool is read-only, and the linked one is not.
	opts.Tools["echo"] = &echoTool{class: poo.ClassSideEffecting}
	_, _, err := poo.Restore(opts, records)
	if err == nil {
		t.Fatal("the restore accepted a tool that changed its class")
	}
	var incompatible *poo.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("the restore returned %T, want an incompatible error", err)
	}
	if len(incompatible.Differences) != 1 {
		t.Fatalf("the error names %d differences, want 1: %v", len(incompatible.Differences), incompatible.Differences)
	}
	if !strings.Contains(incompatible.Differences[0], "read_only") {
		t.Errorf("the difference does not name the class: %q", incompatible.Differences[0])
	}
}

// TestTheRestoreRejectsAToolThatChangedItsBackgroundMode covers the
// compatibility check: a tool that changed its background mode does
// not fit.
func TestTheRestoreRejectsAToolThatChangedItsBackgroundMode(t *testing.T) {
	created := header("s1")
	// The recorded mode is non-empty: a record written before the header
	// carried a mode says nothing about it, and is not a difference.
	created.Tools[0].Background = poo.BackgroundAllowed
	records := []poo.Record{created}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Tools["echo"] = &backgroundTool{background: poo.BackgroundAlways}
	_, _, err := poo.Restore(opts, records)
	if err == nil {
		t.Fatal("the restore accepted a tool that changed its background mode")
	}
	var incompatible *poo.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("the restore returned %T, want an incompatible error", err)
	}
	if len(incompatible.Differences) != 1 {
		t.Fatalf("the error names %d differences, want 1: %v", len(incompatible.Differences), incompatible.Differences)
	}
	if !strings.Contains(incompatible.Differences[0], "background") {
		t.Errorf("the difference does not name the background mode: %q", incompatible.Differences[0])
	}
}

// TestTheRestoreKeepsTheRecordedProfileAndRole covers the rule: the
// effective profile and the chat role come from the metadata, not from
// the current defaults.
func TestTheRestoreKeepsTheRecordedProfileAndRole(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Profile = "base"
	opts.Model = poo.ModelInfo{Name: "other", ModelID: "other-model", ContextLimit: 4096}
	session, _, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	if got := poo.SessionProfile(session); got != "code" {
		t.Errorf("the restored session's profile is %q, want the recorded one", got)
	}
	if got := poo.SessionChatModel(session); got.Name != "fast" {
		t.Errorf("the restored session's model is %q, want the recorded one", got.Name)
	}
}

// restoredMaxTokens restores the header's session under the options and
// returns the max_tokens of its first request.
func restoredMaxTokens(t *testing.T, created *poo.SessionCreatedRecord, opts poo.Options, tr *pootest.Transport) int {
	t.Helper()
	sess, _, err := poo.Restore(opts, []poo.Record{created})
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	runSession(t, sess, "again")
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(requests))
	}
	return requests[0].MaxTokens
}

// TestTheRestoreFillsAnUnknownMaxOutputFromTheSettings covers a model
// whose recorded chat role has no output limit: the restored session
// sends the limit the current settings declare for the same model, then
// the one the header recorded, and never one declared for another model.
func TestTheRestoreFillsAnUnknownMaxOutputFromTheSettings(t *testing.T) {
	t.Run("the same model", func(t *testing.T) {
		tr := scripted([]fakeResponse{{text: "done"}})
		opts := restoreOptions(t, tr, &recordingSink{})
		fast := opts.Models["fast"]
		fast.MaxOutput = 16384
		opts.Models["fast"] = fast
		if got := restoredMaxTokens(t, header("s1"), opts, tr); got != 16384 {
			t.Errorf("the restored request's max_tokens is %d, want the settings' 16384", got)
		}
	})
	t.Run("the header's limit", func(t *testing.T) {
		tr := scripted([]fakeResponse{{text: "done"}})
		opts := restoreOptions(t, tr, &recordingSink{})
		created := header("s1")
		created.Model = poo.SessionModel{ModelID: "test-model", MaxOutput: 4096}
		if got := restoredMaxTokens(t, created, opts, tr); got != 4096 {
			t.Errorf("the restored request's max_tokens is %d, want the header's 4096", got)
		}
	})
	t.Run("the name now serves another model_id", func(t *testing.T) {
		tr := scripted([]fakeResponse{{text: "done"}})
		opts := restoreOptions(t, tr, &recordingSink{})
		opts.Models["fast"] = poo.ModelInfo{Name: "fast", ModelID: "other-model", ContextLimit: 32768, MaxOutput: 16384}
		if got := restoredMaxTokens(t, header("s1"), opts, tr); got != poo.DefaultMaxOutputFor(opts.Model.ContextLimit) {
			t.Errorf("the restored request's max_tokens is %d, want the default %d: the entry names another model_id", got, poo.DefaultMaxOutputFor(opts.Model.ContextLimit))
		}
	})
	t.Run("an unknown context limit", func(t *testing.T) {
		tr := scripted([]fakeResponse{{text: "done"}})
		opts := restoreOptions(t, tr, &recordingSink{})
		opts.Model = poo.ModelInfo{Name: "other", ModelID: "other-model", ContextLimit: 4096}
		created := header("s1")
		created.Roles.Chat.ContextLimit = 0
		created.ContextLimit = 0
		if got := restoredMaxTokens(t, created, opts, tr); got != poo.DefaultMaxOutputFor(opts.Model.ContextLimit) {
			t.Errorf("the restored request's max_tokens is %d, want %d: the room is the settings' window, not another model's", got, poo.DefaultMaxOutputFor(opts.Model.ContextLimit))
		}
	})
	t.Run("a recorded limit", func(t *testing.T) {
		tr := scripted([]fakeResponse{{text: "done"}})
		opts := restoreOptions(t, tr, &recordingSink{})
		fast := opts.Models["fast"]
		fast.MaxOutput = 16384
		opts.Models["fast"] = fast
		created := header("s1")
		created.Roles.Chat.MaxOutput = 1024
		if got := restoredMaxTokens(t, created, opts, tr); got != 1024 {
			t.Errorf("the restored request's max_tokens is %d, want the recorded 1024", got)
		}
	})
}

// TestTheRestoreKeepsARecordedBackgroundModeItDoesNotKnow covers a log
// written before the header carried a tool's background mode: the empty
// record says nothing about the mode, so it is not a difference.
func TestTheRestoreKeepsARecordedBackgroundModeItDoesNotKnow(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Tools["echo"] = &backgroundTool{background: poo.BackgroundAllowed}
	if _, _, err := poo.Restore(opts, records); err != nil {
		t.Fatalf("the restore refused a tool whose recorded mode is unknown: %v", err)
	}
}

// TestTheRestoreRejectsAToolThatIsNoLongerLinked covers the
// compatibility check: a tool the records name and this build does not
// link is a difference.
func TestTheRestoreRejectsAToolThatIsNoLongerLinked(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	delete(opts.Tools, "echo")
	opts.ToolOrder = nil
	_, _, err := poo.Restore(opts, records)
	if err == nil {
		t.Fatal("the restore accepted a tool that is no longer linked")
	}
	var incompatible *poo.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("the restore returned %T, want an incompatible error", err)
	}
	if len(incompatible.Differences) != 1 || !strings.Contains(incompatible.Differences[0], "no longer linked") {
		t.Errorf("the differences are %v, want the missing tool", incompatible.Differences)
	}
}

// TestTheRestoreKeepsASessionWhoseOptionalFeedIsGone covers the
// subscribe_feed tool's absence: it is the core's tool for starting
// optional feeds, so a build that links no optional feed does not link it.
// The session restores, and the changed feed set is a warning.
func TestTheRestoreKeepsASessionWhoseOptionalFeedIsGone(t *testing.T) {
	created := header("s1")
	created.Tools = append(created.Tools, poo.ToolInfo{Name: poo.SubscribeFeedToolName, Class: poo.ClassReadOnly, Background: poo.BackgroundNever})
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Profiles = []poo.Profile{{Name: "code", Feeds: []string{"chat"}}}
	opts.Feeds = nil
	_, restored, err := poo.Restore(opts, []poo.Record{created})
	if err != nil {
		t.Fatalf("the restore refused a session whose optional feed is gone: %v", err)
	}
	want := poo.MissingFeedWarning("code", "chat")
	found := false
	for _, warning := range restored.Warnings {
		if warning == want {
			found = true
		}
	}
	if !found {
		t.Errorf("the warnings are %v, want %q", restored.Warnings, want)
	}
}

// TestTheRestoreSendsTheRecordedToolsModeAndReasoning covers the
// request's shape: the recorded tools mode and reasoning effort are what
// the restored session sends, not the caller's defaults.
func TestTheRestoreSendsTheRecordedToolsModeAndReasoning(t *testing.T) {
	created := header("s1")
	created.ToolsMode = poo.ToolsText
	created.ReasoningEffort = "low"
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := restoreOptions(t, tr, &recordingSink{})
	opts.ToolsMode = string(poo.ToolsNative)
	opts.ReasoningEffort = ""
	sess, _, err := poo.Restore(opts, []poo.Record{created})
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	runSession(t, sess, "again")
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(requests))
	}
	if got := requests[0].ModelInfo.Tools.Mode; got != poo.ToolsText {
		t.Errorf("the restored request's tools mode is %q, want %q", got, poo.ToolsText)
	}
	if got := requests[0].ModelInfo.Reasoning.Mode; got != poo.ReasoningEffort {
		t.Errorf("the restored request's reasoning mode is %q, want %q", got, poo.ReasoningEffort)
	}
}

// TestTheRestoreKeepsTheResolvedControlWhenTheRecordedLevelIsBesideTheList
// covers the forcing rule's other side: a recorded "off" is beside the
// declared levels, so the resolved model keeps its own reasoning control,
// and the restored request shapes itself by that control, not by an
// effort one. A regression that forces on "off" sends an effort field to
// a toggle or budget model, which this test fails on.
func TestTheRestoreKeepsTheResolvedControlWhenTheRecordedLevelIsBesideTheList(t *testing.T) {
	for _, recorded := range []string{"off", "default", "OFF"} {
		t.Run(recorded, func(t *testing.T) {
			created := header("s1")
			created.ReasoningEffort = recorded
			// The recorded role is the model the session resolved to, and
			// this one declares a toggle control, the way a gemma-family
			// model does.
			created.Roles.Chat.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningToggle, Param: poo.ReasoningPathToggle}
			tr := scripted([]fakeResponse{{text: "done"}})
			opts := restoreOptions(t, tr, &recordingSink{})
			sess, _, err := poo.Restore(opts, []poo.Record{created})
			if err != nil {
				t.Fatalf("restoring the session: %v", err)
			}
			t.Cleanup(func() { sess.Close() })
			runSession(t, sess, "again")
			requests := tr.Requests()
			if len(requests) != 1 {
				t.Fatalf("the restored session made %d requests, want 1", len(requests))
			}
			if got := requests[0].ModelInfo.Reasoning.Mode; got != poo.ReasoningToggle {
				t.Errorf("the restored request's reasoning mode is %q, want %q", got, poo.ReasoningToggle)
			}
		})
	}
}

// TestTheRestoreRefusesADurableSession covers the mode check on the core
// path: a durable session is not one this build continues.
func TestTheRestoreRefusesADurableSession(t *testing.T) {
	created := header("s1")
	created.Mode = "durable"
	_, _, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), []poo.Record{created})
	if !errors.Is(err, poo.ErrUnsupportedMode) {
		t.Fatalf("the restore returned %v, want an unsupported mode", err)
	}
}

// TestTheRestoreAppliesOptionFuncsAfterTheRecordedPrompt covers the order
// around the recorded prompt: the record replaces the construction-time
// prompt before the option functions run, so their edits land on the
// recorded text, and a resume edit is never cleared after it was applied.
func TestTheRestoreAppliesOptionFuncsAfterTheRecordedPrompt(t *testing.T) {
	for _, recorded := range []bool{false, true} {
		name := "without_record"
		if recorded {
			name = "with_record"
		}
		t.Run(name, func(t *testing.T) {
			opts := restoreOptions(t, scripted(nil), &recordingSink{})
			opts.SystemPrompt = "construction base"
			opts.Prompt = []func(*prompt.Builder){func(b *prompt.Builder) { b.Set("construction", "construction section") }}
			records := []poo.Record{header("s1")}
			if recorded {
				records = append(records, &poo.SystemPromptRecord{Text: "recorded base"})
			}
			seen := false
			observer := func(o *poo.Options) {
				seen = true
				if recorded && (o.SystemPrompt != "recorded base" || len(o.Prompt) != 0) {
					t.Errorf("option saw prompt=%q edits=%d; want recorded prompt and cleared edits", o.SystemPrompt, len(o.Prompt))
				}
				if !recorded && (o.SystemPrompt != "construction base" || len(o.Prompt) != 1) {
					t.Errorf("without a record, option lost construction state: %q edits=%d", o.SystemPrompt, len(o.Prompt))
				}
			}
			s, _, err := poo.Restore(opts, records, observer, poo.WithPrompt(func(b *prompt.Builder) { b.Append("base", "explicit resume edit") }))
			if err != nil {
				t.Fatalf("restoring the session: %v", err)
			}
			t.Cleanup(func() { s.Close() })
			if !seen {
				t.Fatal("option did not run")
			}
			text := s.PromptState().Text
			if !strings.Contains(text, "explicit resume edit") {
				t.Errorf("explicit resume edit lost: %q", text)
			}
			if recorded && (strings.Contains(text, "construction") || !strings.Contains(text, "recorded base")) {
				t.Errorf("recorded prompt not preserved: %q", text)
			}
			if !recorded && !strings.Contains(text, "construction section") {
				t.Errorf("construction edits lost without a record: %q", text)
			}
		})
	}
}

// TestTheHeaderTheRunCommandWritesCanBeRestored builds the
// session header the way cmd/poo/main.go builds it (a tool's name and
// class, and no background mode) and restores it with the tool that
// binary links.
func TestTheHeaderTheRunCommandWritesCanBeRestored(t *testing.T) {
	tool := bash.New()
	spec := tool.Spec()
	if spec.Background != poo.BackgroundAllowed {
		t.Fatalf("the bash tool's background mode is %q, want %q", spec.Background, poo.BackgroundAllowed)
	}
	// cmd/poo/main.go: sessionTools = append(sessionTools,
	// poo.ToolInfo{Name: spec.Name, Class: spec.Class})
	recorded := []poo.ToolInfo{{Name: spec.Name, Class: spec.Class}}
	// the header the run command writes for this tool
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			WorkingDir: "/tmp",
			Tools:      recorded, Model: poo.SessionModel{ModelID: "m"},
			ContextLimit: 32768,
			ToolsMode:    "native"},
	}
	records := []poo.Record{created}

	opts := poo.Options{
		Transport: scripted(nil),
		Tools:     map[string]poo.Tool{spec.Name: tool},
		ToolOrder: []string{spec.Name},
		Model:     poo.ModelInfo{Name: "m", ModelID: "m", ContextLimit: 32768},
		Models:    map[string]poo.ModelInfo{"m": {Name: "m", ModelID: "m", ContextLimit: 32768}},
	}
	_, _, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("the restore of a session this binary wrote failed: %v", err)
	}
}

// TestTheRestoreNumbersTheTurnsAcrossRuns covers the rule that a
// restored session keeps the turn numbers the records were written
// with: a compaction names a range of turns, so a number that shifts
// with the run a turn is in cuts the wrong work.
func TestTheRestoreNumbersTheTurnsAcrossRuns(t *testing.T) {
	original := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: "done"},
		{text: "ok"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, original, &echoTool{}, sink))
	runSession(t, s, "do the thing")
	runSession(t, s, "again")

	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	session, _, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	turns := poo.SessionView(session).Turns()
	if len(turns) != 3 {
		t.Fatalf("the restored session holds %d turns, want 3", len(turns))
	}
	for i, turn := range turns {
		if turn.TurnIndex != i+1 {
			t.Errorf("the restored turn %d is numbered %d, want %d", i+1, turn.TurnIndex, i+1)
		}
	}
}

// TestARestoredWindowLeavesTheTurnsAfterItInTheView covers the rule that a
// replayed window compaction marks the turns it swallowed and not the ones
// recorded after it. The summary strategy reads that flag to decide what it
// may cut, so a restored session has to be able to compact the turns the
// window never saw.
func TestARestoredWindowLeavesTheTurnsAfterItInTheView(t *testing.T) {
	// One window compaction once a turn has happened, and then the
	// strategy leaves the run alone, so the turns after the window are
	// recorded.
	var fired atomic.Bool
	compactor := &compactionCompactor{
		should: func(view poo.View, budget poo.ContextBudget) bool {
			if len(view.Turns()) < 1 || fired.Load() {
				return false
			}
			fired.Store(true)
			return true
		},
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "window", Window: &poo.ProviderWindow{}}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "the answer"},
	})
	tr.SetWindow(poo.ProviderWindow{Items: []poo.ContentBlock{{
		Kind:     poo.ContentProvider,
		Provider: &poo.ProviderBlock{API: poo.APIChatCompletions, Required: true, Data: []byte(`{"w":1}`)},
	}}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	session, restored, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	if restored.Compactions != 1 {
		t.Fatalf("the restore applied %d compactions, want 1", restored.Compactions)
	}

	turns := poo.SessionView(session).Turns()
	if len(turns) != 3 {
		t.Fatalf("the restored session holds %d turns, want 3", len(turns))
	}
	if !turns[0].Removed {
		t.Error("the turn the window swallowed is not marked removed")
	}
	for _, turn := range turns[1:] {
		if turn.Removed {
			t.Errorf("the turn %d, recorded after the window, is marked removed", turn.TurnIndex)
		}
	}
}

// TestARestoredSessionSendsTheLearnedOutputLimit covers the restore's
// promise for a model that declares none: the cap the original session
// learned is applied to the restored model, so its first request carries
// the cap and takes no second rejection.
func TestARestoredSessionSendsTheLearnedOutputLimit(t *testing.T) {
	original := scripted([]fakeResponse{
		{err: outputLimitRejection(4096)},
		{text: "answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, original, &echoTool{}, sink))
	runSession(t, s, "hello")
	records := append([]poo.Record{header("s1")}, sink.allRecords()...)

	restoredTransport := scripted([]fakeResponse{{text: "ok"}})
	session, _, err := poo.Restore(restoreOptions(t, restoredTransport, &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	runSession(t, session, "more")
	requests := restoredTransport.Requests()
	if len(requests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(requests))
	}
	if got := requests[0].MaxTokens; got != 4096 {
		t.Errorf("the restored request carried max_tokens %d, want the learned 4096", got)
	}
}

// TestTheRestoredModelIgnoresAnotherModelsOutputLimit covers the model id:
// a learned cap belongs to the model that was rejected, so a record that
// names another model leaves the restored model's own settings alone.
func TestTheRestoredModelIgnoresAnotherModelsOutputLimit(t *testing.T) {
	records := []poo.Record{
		header("s1"),
		&poo.OutputLimitLearnedRecord{
			RecordMeta: poo.NewRecordMeta("model.output_limit"),
			ModelID:    "other-model",
			Previous:   8192,
			Limit:      4096,
		},
	}
	tr := scripted([]fakeResponse{{text: "ok"}})
	session, _, err := poo.Restore(restoreOptions(t, tr, &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	if got := session.ChatModel().MaxOutput; got != 0 {
		t.Errorf("the restored model carries max_output %d, want 0", got)
	}
	runSession(t, session, "hello")
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(requests))
	}
	if got := requests[0].MaxTokens; got != 8192 {
		t.Errorf("the restored request carried max_tokens %d, want the default 8192", got)
	}
}

// TestARestoredInputCapComesFromTheSameModel covers the fallback for a record
// written before the cap existed: the restore takes the cap from the
// settings' entry for the model the record names, and only when that entry
// describes the same model id. A role the settings now point at another
// model keeps the limits it ran with.
func TestARestoredInputCapComesFromTheSameModel(t *testing.T) {
	// The recorded view is far over the 2000-token input cap
	// and well under it for the 32768-token window, so a restore that
	// picks the cap up settles context_exhausted before its first
	// request.
	prompt := strings.Repeat("x", 14000)
	records := func() []poo.Record {
		return []poo.Record{
			header("s1"),
			&poo.MessageRecord{
				RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginUser),
				RunID:      "r1",
				Role:       poo.RoleUser,
				Content:    poo.NewTextMessage(poo.RoleUser, prompt).Content,
			},
		}
	}
	for _, tc := range []struct {
		name    string
		modelID string
		want    poo.Reason
	}{
		{"the entry describes the recorded model", "test-model", poo.ReasonContextExhausted},
		{"the entry describes another model", "other-model", poo.ReasonEndTurn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := restoreOptions(t, scripted([]fakeResponse{{text: "ok"}}), &recordingSink{})
			opts.Models["fast"] = poo.ModelInfo{
				Name:         "fast",
				ModelID:      tc.modelID,
				ContextLimit: 32768,
				InputLimit:   2000,
				Ops:          []poo.Op{poo.OpChat, poo.OpCompact},
			}
			session, _, err := poo.Restore(opts, records())
			if err != nil {
				t.Fatalf("restoring the session: %v", err)
			}
			t.Cleanup(func() { session.Close() })

			if got := runSession(t, session, "hello"); got.Reason != tc.want {
				t.Errorf("the run settled %q, want %q", got.Reason, tc.want)
			}
		})
	}
}

// TestTheRestoredSessionKeepsTheCompactedInput covers the guard a resumed
// session needs: the input the first report after the last compaction gave is
// the input the strategy compares the new model's window against, and a restore
// that forgets it would let a compactor that helped nothing run again.
func TestTheRestoredSessionKeepsTheCompactedInput(t *testing.T) {
	const after = 1234
	compactor := pootest.NewCompactor(true, summaryCompaction())
	compactor.SetShouldAfter(1)
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer", usage: &poo.UsageReport{InputTokens: after, OutputTokens: 5}},
	}), &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	if got := sink.count("compaction"); got != 1 {
		t.Fatalf("the log holds %d compactions, want 1", got)
	}

	probe := &budgetProbe{}
	restoredOpts := restoreOptions(t, scripted([]fakeResponse{{text: "ok"}}), &recordingSink{})
	restoredOpts.Compactor = probe
	restored, _, err := poo.Restore(restoredOpts, append([]poo.Record{header("s1")}, sink.allRecords()...))
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	runSession(t, restored, "carry on")
	if probe.last.CompactedInput != after {
		t.Errorf("the restored budget carries a compacted input of %d, want the report's %d",
			probe.last.CompactedInput, after)
	}
}

// compactedFixture returns records of a run that compacts once and reports the
// input the compaction left, with the positions of the compaction record and
// of that report in the slice.
func compactedFixture(t *testing.T) (records []poo.Record, compaction, report int) {
	t.Helper()
	compactor := pootest.NewCompactor(true, summaryCompaction())
	compactor.SetShouldAfter(1)
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer", usage: &poo.UsageReport{InputTokens: 1234, OutputTokens: 5}},
	}), &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	records = append([]poo.Record{header("s1")}, sink.allRecords()...)
	compaction, report = -1, -1
	for i, rec := range records {
		switch rec.(type) {
		case *poo.CompactionRecord:
			compaction = i
		case *poo.UsageRecord:
			if compaction >= 0 && report < 0 {
				report = i
			}
		}
	}
	if compaction < 0 || report < 0 {
		t.Fatalf("the fixture holds no compaction at %d and no report at %d", compaction, report)
	}
	return records, compaction, report
}

// insertRecords returns the records with recs inserted at index.
func insertRecords(records []poo.Record, at int, recs ...poo.Record) []poo.Record {
	out := make([]poo.Record, 0, len(records)+len(recs))
	out = append(out, records[:at]...)
	out = append(out, recs...)
	return append(out, records[at:]...)
}

// plainFixture returns the records of a run that compacts nothing.
func plainFixture(t *testing.T) []poo.Record {
	t.Helper()
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer", usage: &poo.UsageReport{InputTokens: 1234, OutputTokens: 5}},
	}), &echoTool{result: "echoed"}, sink)
	s := newTestSession(t, opts)
	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	return append([]poo.Record{header("s1")}, sink.allRecords()...)
}

// usageLike returns a usage record shaped like another one, with its own input
// count and origin.
func usageLike(usage *poo.UsageRecord, input int, origin poo.Origin) *poo.UsageRecord {
	return &poo.UsageRecord{
		RecordMeta:   poo.NewRecordMetaFrom("usage", origin),
		RunID:        usage.RunID,
		Turn:         usage.Turn,
		Request:      usage.Request,
		InputTokens:  input,
		OutputTokens: usage.OutputTokens,
	}
}

// TestTheRestoredCompactedInputFollowsTheRecords covers the guard a restore
// rebuilds. The report a compaction made is not the view's input, a compaction
// that left no report keeps the guard waiting, a compaction the endpoint ended
// the run with armed nothing, and a model switch cleared what it measured.
func TestTheRestoredCompactedInputFollowsTheRecords(t *testing.T) {
	base, _, report := compactedFixture(t)
	plain := plainFixture(t)
	baseReport := base[report].(*poo.UsageRecord)
	handoff := &poo.CompactionRecord{
		RecordMeta: poo.NewRecordMeta("compaction"),
		RunID:      baseReport.RunID,
		Strategy:   "handoff",
		Form:       "end",
		Compaction: poo.Compaction{Strategy: "handoff", End: &poo.EndRun{Handoff: "the note"}},
	}
	switched := &poo.ModelsChangedRecord{
		RecordMeta: poo.NewRecordMeta("session.models_changed"),
		From:       "fast",
		To:         "fast",
	}

	cases := []struct {
		name    string
		records []poo.Record
		want    int
	}{
		{
			name:    "the report after the compaction",
			records: base,
			want:    1234,
		},
		{
			name: "the compaction's own report is not the view's input",
			records: insertRecords(base, report,
				usageLike(baseReport, 50, poo.OriginCompaction)),
			want: 1234,
		},
		{
			name:    "a compaction with no report after it stays pending",
			records: append(append([]poo.Record(nil), base[:report]...), base[report+1:]...),
			want:    999,
		},
		{
			name:    "a model switch after the compaction arms nothing",
			records: insertRecords(base, len(base), switched),
			want:    0,
		},
		{
			name:    "a handoff after an armed compaction keeps its report",
			records: insertRecords(base, len(base), handoff, usageLike(baseReport, 777, "")),
			want:    1234,
		},
		{
			name:    "a handoff with no armed compaction before it arms nothing",
			records: insertRecords(plain, len(plain), handoff, usageLike(baseReport, 777, "")),
			want:    0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			probe := &budgetProbe{}
			opts := restoreOptions(t, scripted([]fakeResponse{
				{calls: []poo.ToolCall{{CallID: "c9", Name: "echo", Arguments: []byte(`{"text":"again"}`)}}, usage: &poo.UsageReport{InputTokens: 999, OutputTokens: 5}},
				{text: "the answer"},
			}), &recordingSink{})
			opts.Compactor = probe
			restored, _, err := poo.Restore(opts, c.records)
			if err != nil {
				t.Fatalf("restoring the session: %v", err)
			}
			t.Cleanup(func() { restored.Close() })
			if res := runSession(t, restored, "carry on"); res.Reason != poo.ReasonEndTurn {
				t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
			}
			if probe.last.CompactedInput != c.want {
				t.Errorf("the restored budget carries a compacted input of %d, want %d",
					probe.last.CompactedInput, c.want)
			}
		})
	}
}
