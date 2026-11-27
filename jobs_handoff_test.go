package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// TestAMovedCallTimesOutUnderTheJobTimeout covers the rule: a moved
// call runs under the job timeout like any other job, so a call held
// open past the bound ends with the timeout outcome.
func TestAMovedCallTimesOutUnderTheJobTimeout(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "bash", Arguments: []byte(`{"command":"echo $$ > started; until [ -f release ]; do /bin/sleep 0.02; done"}`)}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: dir,
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithJobs(poo.JobOptions{Timeout: 300 * time.Millisecond, OnIdle: poo.OnIdleHold}))
	done := make(chan error, 1)
	go func() {
		_, err := s.Prompt(context.Background(), "run it")
		done <- err
	}()
	var runID string
	for end := time.Now().Add(10 * time.Second); runID == ""; {
		if time.Now().After(end) {
			t.Fatal("the call never started")
		}
		if data, err := os.ReadFile(filepath.Join(dir, "started")); err == nil && strings.HasSuffix(string(data), "\n") {
			for _, rec := range sink.allRecords() {
				if r, ok := rec.(*poo.ToolCallRecord); ok && r.Handle == "r1" {
					runID = r.RunID
				}
			}
		}
		if runID == "" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if _, err := s.BackgroundCall(runID, "r1"); err != nil {
		t.Fatalf("moving the call: %v", err)
	}
	var ended *poo.JobEndedRecord
	waitForSink(t, sink, "the job timeout", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.JobEndedRecord); ok && r.JobID == "j1" {
				ended = r
				return true
			}
		}
		return false
	})
	if ended.Outcome != poo.OutcomeError || !ended.TimedOut {
		t.Errorf("job.ended outcome=%s timed_out=%v; want error/timed_out", ended.Outcome, ended.TimedOut)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestBackgroundCallRefusesWhatItCannotMove covers the refusals of
// BackgroundCall a client branches on: a run the session is not
// running, a call that is no longer live, and a duplicate of a running
// job.
func TestBackgroundCallRefusesWhatItCannotMove(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	args := []byte(`{"command":"echo $$ > started; until [ -f release ]; do /bin/sleep 0.02; done"}`)
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{
				{CallID: "one", Name: "bash", Arguments: args},
				{CallID: "two", Name: "bash", Arguments: args},
			}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: dir,
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithJobs(poo.JobOptions{MaxRunning: 2, OnIdle: poo.OnIdleHold}))
	done := make(chan error, 1)
	go func() {
		_, err := s.Prompt(context.Background(), "run it")
		done <- err
	}()
	waitForFile(t, dir, "started", 10*time.Second)
	var runID string
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolCallRecord); ok && r.Handle == "r1" {
			runID = r.RunID
		}
	}
	if runID == "" {
		t.Fatal("the run recorded no call for r1")
	}
	if _, err := s.BackgroundCall("not-the-run", "r1"); !errors.Is(err, poo.ErrNotRunning) {
		t.Errorf("a move against another run: %v; want ErrNotRunning", err)
	}
	if _, err := s.BackgroundCall(runID, "r1"); err != nil {
		t.Fatalf("moving the held call: %v", err)
	}
	if _, err := s.BackgroundCall(runID, "r1"); !errors.Is(err, poo.ErrCallNotBackgroundable) {
		t.Errorf("a second move of a claimed call: %v; want ErrCallNotBackgroundable", err)
	}
	waitForFile(t, dir, "started", 10*time.Second)
	err := error(nil)
	for end := time.Now().Add(5 * time.Second); ; {
		var moveErr error
		_, moveErr = s.BackgroundCall(runID, "r2")
		if moveErr == nil || strings.Contains(moveErr.Error(), "j1") {
			err = moveErr
			break
		}
		if time.Now().After(end) {
			t.Fatalf("moving the duplicate: %v; want the duplicate refusal", moveErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !errors.Is(err, poo.ErrCallNotBackgroundable) {
		t.Errorf("a duplicate of a running job: %v; want ErrCallNotBackgroundable", err)
	}
	if !strings.Contains(err.Error(), "j1") {
		t.Errorf("the duplicate refusal %q; want it to name job j1", err.Error())
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// The settled run keeps its owner with no pending calls: a move
	// then finds no live call, the same refusal a finished call gets.
	if _, err := s.BackgroundCall(runID, "r1"); !errors.Is(err, poo.ErrCallNotBackgroundable) {
		t.Errorf("a move after the run settled: %v; want ErrCallNotBackgroundable", err)
	}
}

// TestBackgroundCallRefusesWhenTheJobSlotsAreFull covers the rule: a
// move past the running-job limit is a recoverable busy error.
func TestBackgroundCallRefusesWhenTheJobSlotsAreFull(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{
				{CallID: "one", Name: "bash", Arguments: []byte(`{"command":"echo $$ > started-a; until [ -f release-a ]; do /bin/sleep 0.02; done"}`)},
				{CallID: "two", Name: "bash", Arguments: []byte(`{"command":"echo $$ > started-b; until [ -f release-b ]; do /bin/sleep 0.02; done"}`)},
			}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bash.New()}, Sink: sink, WorkingDir: dir,
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithJobs(poo.JobOptions{MaxRunning: 1, OnIdle: poo.OnIdleHold}))
	done := make(chan error, 1)
	go func() {
		_, err := s.Prompt(context.Background(), "run it")
		done <- err
	}()
	waitForFile(t, dir, "started-a", 10*time.Second)
	var runID string
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolCallRecord); ok && r.Handle == "r1" {
			runID = r.RunID
		}
	}
	if _, err := s.BackgroundCall(runID, "r1"); err != nil {
		t.Fatalf("moving the first held call: %v", err)
	}
	waitForFile(t, dir, "started-b", 10*time.Second)
	if _, err := s.BackgroundCall(runID, "r2"); !errors.Is(err, poo.ErrBusy) {
		t.Errorf("a move past the running-job limit: %v; want ErrBusy", err)
	}
	for _, name := range []string{"release-a", "release-b"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func waitForFile(t *testing.T, dir, name string, within time.Duration) {
	t.Helper()
	for end := time.Now().Add(within); ; {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("%s never appeared", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// moveHeldCommand runs a session whose one call holds a command open,
// moves the call to a job, and waits for the job to deliver the
// command's output once it ends. The tool is the one the session
// offers, and callArgs is the call that starts the command.
func moveHeldCommand(t *testing.T, tool poo.Tool, callArgs string) {
	dir := t.TempDir()
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: tool.Spec().Name, Arguments: []byte(callArgs)}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{tool.Spec().Name: tool}, Sink: sink, WorkingDir: dir,
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	})
	done := make(chan error, 1)
	go func() {
		_, err := s.Prompt(context.Background(), "run it")
		done <- err
	}()
	waitForFile(t, dir, "started", 10*time.Second)
	var runID string
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolCallRecord); ok && r.Handle == "r1" {
			runID = r.RunID
		}
	}
	jobID, err := s.BackgroundCall(runID, "r1")
	if err != nil {
		t.Fatalf("moving the held call: %v", err)
	}
	if jobID != "j1" {
		t.Errorf("the move returned job %q; want j1", jobID)
	}
	var started bool
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Outcome == poo.OutcomeStarted {
			started = strings.Contains(r.Text, "job j1")
		}
	}
	if !started {
		t.Error("the foreground run did not record the started receipt")
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitForSink(t, sink, "the moved command's output", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.JobEndedRecord); ok && r.Outcome == poo.OutcomeOK && strings.Contains(r.Text, "held-") {
				return true
			}
		}
		return false
	})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// holdCallTool loads a catalog whose one entry holds a command open
// until a release file appears beside it.
func holdCallTool(t *testing.T) *cliregistry.CallTool {
	t.Helper()
	body, err := json.Marshal(map[string]any{"version": 1, "tools": map[string]any{
		"hold": map[string]any{
			"description": "Hold until released",
			"command":     []string{"/bin/bash", "-c", "echo $$ > started; until [ -f release ]; do /bin/sleep 0.02; done; echo held-$$"},
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
	}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	manifest := filepath.Join(t.TempDir(), "hold.json")
	if err := os.WriteFile(manifest, body, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := cliregistry.Load(manifest)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	return cliregistry.NewCall(catalog)
}

// TestBackgroundCallMovesARunningCLI covers the rule: a call_cli call
// moves to a job while it runs.
func TestBackgroundCallMovesARunningCLI(t *testing.T) {
	moveHeldCommand(t, holdCallTool(t), `{"command":"hold"}`)
}

// TestBackgroundCallMovesARunningEntry covers the rule: the same
// catalog entry offered directly as its own tool moves to a job while
// it runs, the way the call over the catalog does.
func TestBackgroundCallMovesARunningEntry(t *testing.T) {
	tool := holdCallTool(t)
	entries := tool.Tools()
	if len(entries) == 0 {
		t.Fatal("the catalog offered no direct entry tool")
	}
	moveHeldCommand(t, entries[0], `{}`)
}

// TestAMovedCallRunsAsTheJob covers the rule: a call the run moves to a
// job runs as the job's own — the env its tool holds names the job it
// became, so a tool that keys on the job id sees it from the start.
func TestAMovedCallRunsAsTheJob(t *testing.T) {
	release := make(chan struct{})
	tool := &echoTool{
		background: poo.BackgroundAllowed,
		call: func(ctx context.Context) (poo.ToolResult, error) {
			<-release
			return poo.ToolResult{Text: "held"}, nil
		},
	}
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "echo", Arguments: []byte(`{"text":"hold"}`)}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"echo": movableEcho{tool}},
		Sink:  sink, WorkingDir: t.TempDir(),
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithJobs(poo.JobOptions{OnIdle: poo.OnIdleHold}))
	go func() {
		_, _ = s.Prompt(context.Background(), "run it")
	}()
	var runID string
	for end := time.Now().Add(10 * time.Second); ; {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.ToolCallRecord); ok && r.Handle == "r1" {
				runID = r.RunID
			}
		}
		if runID != "" && len(tool.Envs()) == 1 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("the held call never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	jobID, err := s.BackgroundCall(runID, "r1")
	if err != nil {
		t.Fatalf("moving the held call: %v", err)
	}
	close(release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	envs := tool.Envs()
	if len(envs) != 1 {
		t.Fatalf("the tool ran %d times, want 1", len(envs))
	}
	if envs[0].JobID != jobID {
		t.Errorf("the tool's env names job %q, want the job the call moved to (%q)", envs[0].JobID, jobID)
	}
}

// movableEcho is an echo tool whose running call may move to a job.
type movableEcho struct{ *echoTool }

// CanBackgroundRunningCall reports that the echo's running call can
// finish under a job without restarting.
func (movableEcho) CanBackgroundRunningCall() bool { return true }

// TestAMovedCallKeepsTheJobTimeout covers the rule: a moved call runs
// under the job timeout, not the per-call one — a bash call held past
// its own timeout survives until the job's bound ends it, and the
// job.ended says the job timed out.
func TestAMovedCallKeepsTheJobTimeout(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	bt := bash.New()
	bt.Timeout = 200 * time.Millisecond
	s := newTestSession(t, poo.Options{
		Transport: scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "one", Name: "bash", Arguments: []byte(`{"command":"echo $$ > started; until [ -f release ]; do /bin/sleep 0.02; done"}`)}}},
			{text: "done"},
		}),
		Tools: map[string]poo.Tool{"bash": bt}, Sink: sink, WorkingDir: dir,
		Model: poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithJobs(poo.JobOptions{Timeout: time.Second, OnIdle: poo.OnIdleHold}))
	done := make(chan error, 1)
	go func() {
		_, err := s.Prompt(context.Background(), "run it")
		done <- err
	}()
	waitForFile(t, dir, "started", 10*time.Second)
	var runID string
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolCallRecord); ok && r.Handle == "r1" {
			runID = r.RunID
		}
	}
	if runID == "" {
		t.Fatal("the run recorded no call for r1")
	}
	if _, err := s.BackgroundCall(runID, "r1"); err != nil {
		t.Fatalf("moving the held call: %v", err)
	}
	var ended *poo.JobEndedRecord
	waitForSink(t, sink, "the moved call's job to end", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.JobEndedRecord); ok && r.JobID == "j1" {
				ended = r
				return true
			}
		}
		return false
	})
	if !ended.TimedOut {
		t.Errorf("the moved call's job ended timed_out=%v; the job timeout, not the per-call one, owns the deadline", ended.TimedOut)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
