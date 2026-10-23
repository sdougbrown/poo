package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
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

// ownBackgroundTool declares its own background argument and never runs
// as a job: the core's reserved argument belongs only to tools that
// allow jobs.
type ownBackgroundTool struct {
	got string
}

// Spec declares the tool's own background property.
func (t *ownBackgroundTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:   "own",
		Schema: json.RawMessage(`{"type":"object","properties":{"background":{"type":"boolean"}}}`),
		Class:  poo.ClassReadOnly,
	}
}

// ownPrepared is the prepared call.
type ownPrepared struct{ raw string }

// Summary describes the call.
func (p ownPrepared) Summary() string { return p.raw }

// Prepare keeps the arguments it saw.
func (t *ownBackgroundTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	t.got = string(raw)
	return ownPrepared{raw: string(raw)}, nil
}

// Call answers.
func (t *ownBackgroundTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "ok"}, nil
}

// TestANeverToolKeepsItsOwnBackgroundArgument covers the plan's rule:
// the reserved argument is stripped only for a tool that may become a
// job. A never-background tool may define its own background property.
func TestANeverToolKeepsItsOwnBackgroundArgument(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "own", Arguments: []byte(`{"background":true}`)}}},
		{text: "done"},
	})
	tool := &ownBackgroundTool{}
	opts := poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"own": tool},
		ToolOrder:    []string{"own"},
		Sink:         &recordingSink{},
		SessionID:    "s1",
		ModelID:      "m",
		ContextLimit: 32768,
		WorkingDir:   t.TempDir(),
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(tool.got, "background") {
		t.Errorf("the never-background tool's Prepare saw %q, want its own background argument", tool.got)
	}
}

// TestANullBackgroundArgumentIsRefused covers the plan's rule: the
// reserved argument is a boolean. Null is not a boolean, so the call is
// a recoverable argument error and the tool never runs.
func TestANullBackgroundArgumentIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{
			ID:        "null",
			Name:      "job",
			Arguments: json.RawMessage(`{"text":"one","background":null}`),
		}},
	}})
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.ReleaseAll()
	opts := jobOptions(t, tr, tool, &recordingSink{})
	opts.MaxTurns = 1
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if tool.Calls() != 0 {
		t.Fatalf("background:null invoked the tool %d times; a non-boolean must be a recoverable argument error", tool.Calls())
	}
}

// TestWaitJobsWaitsForACompletionBeingRecorded covers the completion
// handoff: a job whose job.ended record is being written is still
// outstanding work, so WaitJobs cannot return in the gap before its
// result is queued.
func TestWaitJobsWaitsForACompletionBeingRecorded(t *testing.T) {
	sink := &blockingRecordSink{
		kind:    "job.ended",
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "waiting"},
		{text: "woken answer"},
	})
	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the run to wait", func() bool { return s.State() == poo.StateWaiting })
	s.Cancel("probe")
	tool.Release()
	<-sink.entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := s.WaitJobs(ctx)
	close(sink.release)
	if err == nil {
		t.Fatal("WaitJobs returned while the job.ended record was still being written")
	}
}

// unlaunchedJobSink holds the job.started record until the test releases
// it, so the run settles while the job's acceptance is in flight.
type unlaunchedJobSink struct {
	recordingSink
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Append holds the job.started record.
func (s *unlaunchedJobSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "job.started" {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestAnUnlaunchedJobCompletesThroughTheQueue covers the plan's jobs
// rule: every accepted job completes with a job.ended that names the
// input its result is delivered under. A job the run settles before
// launching is cancelled and its result is queued like any other, so the
// idle decision holds it.
func TestAnUnlaunchedJobCompletesThroughTheQueue(t *testing.T) {
	sink := &unlaunchedJobSink{entered: make(chan struct{}), release: make(chan struct{})}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the cancelled result woke a run"},
	})
	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s := newTestSession(t, opts)
	id, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	cancelDone := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelDone)
	}()
	// The job's acceptance is in flight when the finalization begins, so
	// the run settles before the job launches.
	waitFor(t, "the settling event", func() bool {
		for _, e := range s.Events() {
			if e.Name == poo.EventRunSettling {
				return true
			}
		}
		return false
	})
	close(sink.release)
	if _, err := s.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	<-cancelDone

	names := sink.Names()
	started, ended := -1, -1
	var end *poo.JobEnded
	for i, name := range names {
		switch name {
		case "job.started":
			started = i
		case "job.ended":
			ended = i
		}
	}
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.JobEnded); ok {
			end = r
		}
	}
	if started < 0 || ended < 0 || started > ended {
		t.Fatalf("records are %v; want job.started before job.ended", names)
	}
	if end.Outcome != poo.JobOutcomeCancelled {
		t.Errorf("the unlaunched job ended %q, want cancelled", end.Outcome)
	}
	if end.InputID == "" {
		t.Errorf("the unlaunched job's job.ended names no input_id, so its result has no fate")
	}
	if end.Text != "the run settled before the job started" {
		t.Errorf("the unlaunched job's text is %q, want the settling note", end.Text)
	}
	if got := s.QueuedJobResults(); got != 1 {
		t.Errorf("%d job result(s) queued, want the unlaunched job's cancelled result", got)
	}
	if got := sink.count("input.held"); got != 1 {
		t.Errorf("the log holds %d input.held records, want 1", got)
	}
}

// TestAQueuedFollowUpDoesNotKeepARunFromWaiting covers the waiting
// entry: a follow-up is delivered only at a natural stop, so it is not
// queued work that keeps a run with a job running from waiting.
func TestAQueuedFollowUpDoesNotKeepARunFromWaiting(t *testing.T) {
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "waiting for the job"},
	})
	opts := jobOptions(t, tr, tool, sink)
	opts.MaxTurns = 4
	s := newTestSession(t, opts)
	var once sync.Once
	var sendErr error
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{BeforeWaiting: func(poo.WaitForResult) {
		once.Do(func() {
			_, sendErr = s.Send("follow up later", poo.FollowUp)
		})
	}})
	defer restore()
	id, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "waiting or settlement", func() bool { return s.State() == poo.StateWaiting || !s.Running() })
	if s.State() != poo.StateWaiting {
		res, err := s.WaitRun(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("the follow-up caused %d request(s) and %s with jobs=%v, queued=%d delivered=%d dropped=%d; want waiting (Send error=%v)",
			len(tr.Requests()), res.Reason, s.JobsRunning(), sink.count("input.queued"), sink.count("input.delivered"), sink.count("input.dropped"), sendErr)
	}
	s.Cancel("cleanup")
}

// TestTheCancelJobToolRejectsTrailingData covers the strict decode: a
// call's arguments are one JSON value, and bytes after it are refused,
// not silently ignored.
func TestTheCancelJobToolRejectsTrailingData(t *testing.T) {
	tool := pootest.NewJobTool(poo.CancelJobToolName, poo.BackgroundAllowed)
	if _, err := tool.Prepare(json.RawMessage(`{"job":"j1"} trailing`), poo.PrepareEnv{}); err == nil {
		t.Error("the call with trailing data was accepted")
	}
	if _, err := tool.Prepare(json.RawMessage(`{"job":"j1"}`), poo.PrepareEnv{}); err != nil {
		t.Errorf("the clean call was refused: %v", err)
	}
	if _, err := tool.Prepare(json.RawMessage(`  {"job":"j1"}  `), poo.PrepareEnv{}); err != nil {
		t.Errorf("the clean call with surrounding space was refused: %v", err)
	}
}

// panicPrepared is a prepared panic call.
type panicPrepared struct{}

// Summary describes the call.
func (panicPrepared) Summary() string { return "explode" }

// panicTool always panics in Call, once the test releases it.
type panicTool struct {
	// release holds the panic until the test closes it. A nil channel
	// panics at once.
	release chan struct{}
}

// Spec describes the tool: every call is a background job.
func (t *panicTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "explode",
		Description: "explode",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Class:       poo.ClassSideEffecting,
		Background:  poo.BackgroundAlways,
	}
}

// Prepare accepts anything.
func (t *panicTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return panicPrepared{}, nil
}

// Call panics, the way a broken tool does.
//
// The panic waits for the test's release, so a test can put it at a
// point of its own choosing instead of racing the run's turns.
func (t *panicTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if t.release != nil {
		select {
		case <-t.release:
		case <-ctx.Done():
			return poo.ToolResult{}, ctx.Err()
		}
	}
	panic("the tool exploded")
}

// TestAPanickingJobEndsWithAFatalError covers the promise the log makes:
// a tool that panics as a background job does not bring the process
// down. The job ends exactly once, with the panic as its error result,
// and the queued result reaches the model like any job's.
func TestAPanickingJobEndsWithAFatalError(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	release := make(chan struct{})
	tool := &panicTool{release: release}

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The job panics when the test releases it, and not before: the run
	// has finished its own turns and is waiting for the job, so the
	// settle the panic's fatal result produces is this run's settle.
	waitFor(t, "the run to wait for the job", func() bool { return s.State() == poo.StateWaiting })
	close(release)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// A fatal tool error settles the receiving run, the way a
	// foreground panicking tool does.
	if res.Reason != poo.ReasonToolFatal {
		t.Errorf("the run settled %q, want tool_fatal", res.Reason)
	}

	ended := 0
	for _, rec := range sink.allRecords() {
		e, ok := rec.(*poo.JobEnded)
		if !ok {
			continue
		}
		ended++
		if e.Outcome != poo.JobOutcomeError {
			t.Errorf("the job ended %q, want error", e.Outcome)
		}
		if !strings.Contains(e.Text, "panic: the tool exploded") {
			t.Errorf("the job's result is %q, want the panic", e.Text)
		}
	}
	if ended != 1 {
		t.Errorf("the log holds %d job.ended records, want 1", ended)
	}
}
