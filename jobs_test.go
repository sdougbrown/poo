package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/prompt"
)

// The jobs: a call that becomes a job returns at once, and its result
// arrives later as an input. A job belongs to the session, not to the
// run that started it.

// jobOptions builds the options for a session with a job tool.
func jobOptions(t *testing.T, tr poo.ChatTransport, tool poo.Tool, sink poo.RecordSink) poo.Options {
	t.Helper()
	return poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"job": tool},
		ToolOrder:    []string{"job"},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
		WorkingDir:   t.TempDir(),
	}
}

// jobCall is a call of the scripted job tool.
func jobCall(text string, background bool) poo.ToolCall {
	args := map[string]any{"text": text}
	if background {
		args["background"] = true
	}
	raw, _ := json.Marshal(args)
	return poo.ToolCall{ID: "call_" + text, Name: "job", Arguments: raw}
}

// waitFor polls a condition until it holds.
//
// The condition is a fact the test already knows, so the wait is short
// and the failure says what was missing. The job path is driven by
// channels, and this is only the last step of a chain.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestABackgroundCallReturnsStarted covers the plan's rule: a call with
// background true returns at once with a started outcome, and the turn
// carries on.
func TestABackgroundCallReturnsStarted(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")
	tool.Release()

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}

	// The call's own result is the started one.
	found := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Outcome == poo.OutcomeStarted {
			found = true
			if !strings.Contains(r.Text, "[started job j1 · job;") {
				t.Errorf("the started result is %q, want the started text", r.Text)
			}
		}
	}
	if !found {
		t.Error("the log holds no started result")
	}
	// The job's acceptance is recorded.
	if sink.count("job.started") != 1 {
		t.Errorf("the log holds %d job.started records, want 1", sink.count("job.started"))
	}
}

// TestTheBackgroundArgumentIsStrippedBeforePrepare covers the plan's
// rule: the core removes the reserved argument before the tool's own
// decoder sees it.
func TestTheBackgroundArgumentIsStrippedBeforePrepare(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.Release()

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The strict decoder accepted the call, so it never saw the
	// reserved argument.
	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d times, want 1", tool.Calls())
	}
	// The tool.call record keeps the original arguments.
	found := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolCallRecord)
		if !ok {
			continue
		}
		if strings.Contains(string(r.Arguments), "background") {
			found = true
		}
	}
	if !found {
		t.Error("the tool.call record does not keep the original arguments")
	}
}

// TestANonBooleanBackgroundArgumentIsRecoverable covers the plan's
// rule: a background argument that is not a boolean is a recoverable
// error, and the run carries on.
func TestANonBooleanBackgroundArgumentIsRecoverable(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "job",
			Arguments: json.RawMessage(`{"text":"one","background":"yes"}`),
		}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.Release()

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d times, want 0", tool.Calls())
	}
	// The model saw the error and carried on.
	found := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Outcome == poo.OutcomeError && strings.Contains(r.Text, "background") {
			found = true
		}
	}
	if !found {
		t.Error("the model did not see the argument error")
	}
}

// TestASchemaThatDefinesBackgroundFailsRegistration covers the plan's
// rule: the reserved name cannot be taken by a tool.
func TestASchemaThatDefinesBackgroundFailsRegistration(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &backgroundSchemaTool{}

	opts := jobOptions(t, tr, tool, sink)
	_, err := poo.New(opts)
	if err == nil {
		t.Fatal("the session was built with a tool that defines the reserved argument")
	}
	if !strings.Contains(err.Error(), "background") {
		t.Errorf("the error is %q, want it to name the reserved argument", err.Error())
	}
}

// TestBackgroundAlwaysMakesEveryCallAJob covers the plan's rule: a tool
// that says always makes every call a job, and it is offered no
// background argument.
func TestBackgroundAlwaysMakesEveryCallAJob(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "job",
			Arguments: json.RawMessage(`{"text":"one"}`),
		}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.Release()

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if sink.count("job.started") != 1 {
		t.Errorf("the log holds %d job.started records, want 1", sink.count("job.started"))
	}
	// The tool is offered no background property: the schema the model
	// saw is the tool's own.
	reqs := tr.Requests()
	if len(reqs) == 0 {
		t.Fatal("the transport got no request")
	}
	for _, spec := range reqs[0].Tools {
		if spec.Name != "job" {
			continue
		}
		if strings.Contains(string(spec.Schema), "background") {
			t.Error("the always tool was offered the background property")
		}
	}
}

// TestTheAllowedToolIsOfferedTheBackgroundProperty covers the plan's
// rule: a tool that says allowed is offered the reserved argument.
func TestTheAllowedToolIsOfferedTheBackgroundProperty(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.Release()

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	reqs := tr.Requests()
	if len(reqs) == 0 {
		t.Fatal("the transport got no request")
	}
	found := false
	for _, spec := range reqs[0].Tools {
		if spec.Name == "job" && strings.Contains(string(spec.Schema), "background") {
			found = true
		}
	}
	if !found {
		t.Error("the allowed tool was not offered the background property")
	}
}

// TestTheJobToolsAreRegistered covers the plan's rule: a session with a
// job-capable tool offers the model the jobs and cancel_job tools.
func TestTheJobToolsAreRegistered(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.Release()

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	reqs := tr.Requests()
	if len(reqs) == 0 {
		t.Fatal("the transport got no request")
	}
	names := map[string]bool{}
	for _, spec := range reqs[0].Tools {
		names[spec.Name] = true
	}
	if !names["jobs"] {
		t.Error("the model was not offered the jobs tool")
	}
	if !names["cancel_job"] {
		t.Error("the model was not offered the cancel_job tool")
	}
}

// TestASessionWithoutAJobToolHasNoJobTools covers the plan's rule: the
// job tools are registered only when a tool may run in the background.
func TestASessionWithoutAJobToolHasNoJobTools(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	reqs := tr.Requests()
	if len(reqs) == 0 {
		t.Fatal("the transport got no request")
	}
	for _, spec := range reqs[0].Tools {
		if spec.Name == "jobs" || spec.Name == "cancel_job" {
			t.Errorf("the model was offered the %s tool", spec.Name)
		}
	}
}

// backgroundSchemaTool is a tool whose own schema takes the reserved
// background argument.
type backgroundSchemaTool struct{}

// Spec describes the tool.
func (t *backgroundSchemaTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "job",
		Description: "a tool that takes the reserved argument",
		Schema:      json.RawMessage(`{"type":"object","properties":{"background":{"type":"boolean"}}}`),
		Class:       poo.ClassSideEffecting,
		Background:  poo.BackgroundAllowed,
	}
}

// Prepare accepts the call.
func (t *backgroundSchemaTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{}, nil
}

// Call answers with a fixed text.
func (t *backgroundSchemaTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "done"}, nil
}

// TestTheJobsToolContributesItsPromptSection covers the plan's rule: the
// jobs prompt section tells the model about the background jobs.
func TestTheJobsToolContributesItsPromptSection(t *testing.T) {
	tool := &poo.JobTool{}
	section := tool.PromptSection(prompt.Env{})
	if !strings.Contains(section, "background") {
		t.Errorf("the section is %q, want it to name the background argument", section)
	}
	if !strings.Contains(section, "cancel_job") {
		t.Errorf("the section is %q, want it to name the cancel_job tool", section)
	}
}

// TestNewDoesNotMutateTheCallersTools covers the construction contract:
// the core registers its own job tools in the session's copies, not in
// the caller's map or order, so one tools map can build two sessions.
func TestNewDoesNotMutateTheCallersTools(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tools := map[string]poo.Tool{"job": tool}
	order := []string{"job"}
	opts := poo.Options{
		Transport:    scripted([]fakeResponse{{text: "hi"}}),
		Tools:        tools,
		ToolOrder:    order,
		SessionID:    "s1",
		ModelID:      "m",
		ContextLimit: 32768,
	}
	s1, err := poo.New(opts)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	defer s1.Close()
	if len(tools) != 1 {
		t.Errorf("New added %d tools to the caller's map: %v", len(tools)-1, tools)
	}
	if len(order) != 1 {
		t.Errorf("New appended to the caller's ToolOrder: %v", order)
	}
	opts.SessionID = "s2"
	s2, err := poo.New(opts)
	if err != nil {
		t.Fatalf("second New with the same map: %v", err)
	}
	defer s2.Close()
}
