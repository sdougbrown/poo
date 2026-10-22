package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The restore: a session rebuilt from the records of an earlier one.

// header returns a session header record for a test.
func header(sessionID string) *poo.SessionCreated {
	return &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    sessionID,
		Mode:         "logged",
		CreatedAt:    "2026-01-01T00:00:00Z",
		WorkingDir:   "/tmp",
		Profile:      "code",
		Roles:        poo.ModelRoles{Chat: &poo.ModelInfo{Name: "fast", ModelID: "test-model", ContextLimit: 32768}},
		Tools:        []poo.ToolInfo{{Name: "echo", Class: poo.ClassReadOnly}},
		SettingsHash: "settings-1",
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
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
		"fast": {Name: "fast", ModelID: "test-model", ContextLimit: 32768},
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
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
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
}

// TestTheRestoreRebuildsTheHistory covers the history a restore holds:
// the messages, the hook-appended messages and the tool results.
func TestTheRestoreRebuildsTheHistory(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
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
		&poo.JobStarted{RecordMeta: poo.NewRecordMeta("job.started"), SessionID: "s1", Job: "j1", Handle: "r1", Tool: "echo", RunID: "run-1"},
		&poo.JobEnded{RecordMeta: poo.NewRecordMeta("job.ended"), SessionID: "s1", Job: "j1", Handle: "r1", Tool: "echo", RunID: "run-1", Outcome: poo.JobOutcomeOK, InputID: "s1-ji1", Text: "the job said something"},
	}
	pending := poo.PendingJobResults(records)
	if len(pending) != 1 {
		t.Fatalf("the records hold %d pending job results, want 1", len(pending))
	}
	if pending[0].Job != "j1" || pending[0].Text != "the job said something" {
		t.Errorf("the pending result is %+v, want the job's result", pending[0])
	}

	// A delivered result is in the history already.
	records = append(records, &poo.InputDelivered{RecordMeta: poo.NewRecordMeta("input.delivered"), SessionID: "s1", RunID: "run-1", InputID: "s1-ji1", Mode: poo.InputModeJob})
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
