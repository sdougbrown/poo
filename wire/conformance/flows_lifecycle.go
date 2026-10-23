package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// The flows that outlive a run: the jobs, the crashes, the teardown and
// the slow client.

// handoff covers the flow of a run that runs out of context: the
// compactor asks for a handoff, and the run ends with it.
func (s *suite) handoff() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	// The model calls a tool, so the run carries on and the gate asks
	// the compactor whether the window is full.
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": "echo hello"}}}},
		{Text: "the handoff note"},
	}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, handoffWindow, nil)
	c := s.start(settings, endpoint)
	s.handshake(c)
	sessionID := s.create(c)

	// The prompt fills the window: the estimate crosses the ratio the
	// compactor acts at. The session's own prompt says how long it is,
	// so the prompt the flow sends is sized to the window.
	prompt := s.promptText(c, sessionID)
	window := float64(handoffWindow)
	fill := int(window*0.925*3.5) - len(prompt) - 70
	if fill < 1 {
		fill = 1
	}
	runID := s.prompt(c, sessionID, strings.Repeat(handoffFiller, fill/len(handoffFiller)))
	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "context_exhausted" {
		s.t.Errorf("the run settled %q, want context_exhausted", settled.Reason)
	}
	if settled.Detail.Handoff == "" {
		s.t.Errorf("the settled run holds no handoff: %+v", settled.Detail)
	}

	// The handoff was asked for with a fork: the endpoint got the
	// instruction.
	requests := c.requests()
	if len(requests) < 2 {
		s.t.Fatalf("the endpoint got %d requests, want 2", len(requests))
	}
	if !requestHolds(requests[1], "Write a handoff") {
		s.t.Errorf("the second request does not ask for a handoff: %v", requests[1])
	}
}

// handoffFiller is the text the handoff flow fills the window with.
const handoffFiller = "the task is long. "

// handoffWindow is the window the handoff flow runs under.
//
// The window is small enough that a prompt fills it, and large enough
// that the first request has room for an answer.
const handoffWindow = 16384

// promptText returns the session's system prompt.
func (s *suite) promptText(c *client, sessionID string) string {
	s.t.Helper()
	raw := c.call(protocol.MethodSessionGet, map[string]any{
		"session_id": sessionID,
	}, &protocol.GetResult{})
	var state protocol.GetResult
	if err := json.Unmarshal(raw, &state); err != nil {
		s.t.Fatalf("decoding the session: %v", err)
	}
	return state.Prompt.Text
}

// jobs covers the flow of a client that runs work in the background:
// the job starts, the run waits for it, a cancelled run wakes when the
// result arrives, a job can be stopped, and a closed session leaves no
// process behind.
func (s *suite) jobs() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	if !s.hasBackground() {
		s.skip(fmt.Sprintf("the profile %q offers no tool that may run in the background", s.profile))
		return
	}
	s.jobRunsAndSettles()
	s.jobCancelWakesTheSession()
	s.jobCancelStopsTheJob()
	s.jobStopsWithTheSession()
}

// jobRunsAndSettles covers the job that ends: the run waits for it,
// the result is delivered, and the run settles.
func (s *suite) jobRunsAndSettles() {
	s.t.Helper()
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{
			"command":    "sleep 1",
			"background": true,
		}}}},
		{Text: "the answer"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "start a job")
	started := c.waitEvent("job started", func(e event) bool {
		return e.Type == "job.started" && e.Payload["run_id"] == runID
	})
	job, _ := started.Payload["job"].(string)
	if job == "" {
		s.t.Fatalf("the job names no id: %v", started.Payload)
	}

	// The run waits for the job.
	s.waitForState(c, sessionID, "waiting")

	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
	// The result was delivered, so the model was asked again.
	requests := c.requests()
	if len(requests) < 2 {
		s.t.Errorf("the endpoint got %d requests, want 2: the result was not delivered", len(requests))
	}
}

// waitForState waits for the session to reach a state.
//
// The state is a moment in the run's life, so the flow waits for it
// rather than looking once.
func (s *suite) waitForState(c *client, sessionID, want string) {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state := s.inspect(c, sessionID, nil, 0)
		if state.State == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Errorf("the session did not reach the state %q", want)
}

// jobCancelWakesTheSession covers the job whose run was cancelled: the
// result arrives later and wakes the session.
func (s *suite) jobCancelWakesTheSession() {
	s.t.Helper()
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{
			"command":    "sleep 1",
			"background": true,
		}}}},
		{Text: "the answer after the wake"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "start a job")
	c.waitEvent("job started", func(e event) bool {
		return e.Type == "job.started" && e.Payload["run_id"] == runID
	})
	c.call(protocol.MethodAgentCancel, map[string]any{
		"session_id": sessionID,
		"run_id":     runID,
	}, &struct{}{})

	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "cancelled" {
		s.t.Errorf("the run settled %q, want cancelled", settled.Reason)
	}

	// The result arrived later and woke the session: a new run started.
	woken := c.waitEvent("the woken run", func(e event) bool {
		return e.Type == "run.started" && e.Payload["run_id"] != runID
	})
	wokenID, _ := woken.Payload["run_id"].(string)
	if wokenID == "" {
		s.t.Fatalf("the woken run names no id: %v", woken.Payload)
	}
	s.settled(c, sessionID, wokenID)
}

// jobCancelStopsTheJob covers the job a client stops: the job ends and
// the run carries on.
func (s *suite) jobCancelStopsTheJob() {
	s.t.Helper()
	command := s.boundedCommand()
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{
			"command":    command,
			"background": true,
		}}}},
		{Text: "the answer"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "start a job")
	started := c.waitEvent("job started", func(e event) bool {
		return e.Type == "job.started" && e.Payload["run_id"] == runID
	})
	job, _ := started.Payload["job"].(string)

	c.call(protocol.MethodJobCancel, map[string]any{
		"session_id": sessionID,
		"job":        job,
	}, &struct{}{})

	ended := c.waitEvent("job ended", func(e event) bool {
		return e.Type == "job.ended" && e.Payload["job"] == job
	})
	outcome, _ := ended.Payload["outcome"].(string)
	if outcome != "cancelled" {
		s.t.Errorf("the job ended %q, want cancelled", outcome)
	}
	s.settled(c, sessionID, runID)
}

// jobStopsWithTheSession covers the job a closed session leaves
// behind: the job stops, and no process is left running.
func (s *suite) jobStopsWithTheSession() {
	s.t.Helper()
	beat := filepath.Join(s.dir, "job.beat")
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{
			"command":    "while true; do date +%s%N > " + beat + "; sleep 0.1; done",
			"background": true,
		}}}},
	}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, nil)
	c := s.start(settings, endpoint)
	s.handshake(c)
	sessionID := s.create(c)

	runID := s.prompt(c, sessionID, "start a job")
	c.waitEvent("job started", func(e event) bool {
		return e.Type == "job.started" && e.Payload["run_id"] == runID
	})
	// The job is running: the command writes down that it is alive.
	s.waitForTheFile(beat)

	c.call(protocol.MethodSessionClose, map[string]any{
		"session_id": sessionID,
	}, &struct{}{})

	// The job stopped with the session.
	ended := c.waitEvent("job ended", func(e event) bool {
		return e.Type == "job.ended"
	})
	if cause, _ := ended.Payload["cause"].(string); cause != "session_closed" {
		s.t.Errorf("the job ended with the cause %q, want session_closed", cause)
	}

	// The process is gone: the file it wrote stops changing.
	s.waitForTheFileToSettle(beat)

	// The closing process appended nothing after its flush: the log
	// stops growing once the close returned.
	path := filepath.Join(s.sessions, sessionID+".jsonl")
	before, err := os.Stat(path)
	if err != nil {
		s.t.Fatalf("stating the session log: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	still, err := os.Stat(path)
	if err != nil {
		s.t.Fatalf("stating the session log after the close: %v", err)
	}
	if still.Size() != before.Size() {
		s.t.Errorf("the session log grew from %d to %d bytes after the close returned", before.Size(), still.Size())
	}

	// Another process loads the session at once: the close released the
	// Authority lock.
	other := s.start(settings, endpoint)
	s.handshake(other)
	answer := other.send(protocol.MethodSessionLoad, map[string]any{
		"session_id": sessionID,
	}).wait(s.t)
	if answer.failure != nil {
		if dataCode(answer.failure) == protocol.DataSessionLocked {
			s.t.Error("the load after the close is refused with session_locked: the Authority lock was not released")
		} else {
			s.t.Errorf("the load after the close was refused: %s (%s)", answer.failure.Message, dataCode(answer.failure))
		}
		return
	}
	other.call(protocol.MethodSessionClose, map[string]any{"session_id": sessionID}, &struct{}{})
}

// waitForTheFile waits for a file to appear.
func (s *suite) waitForTheFile(path string) {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.t.Fatalf("the job wrote no %s", path)
}

// waitForTheFileToSettle waits for a file to stop changing.
//
// The command writes the file again and again, so a file that does not
// change says the command is gone.
func (s *suite) waitForTheFileToSettle(path string) {
	s.t.Helper()
	first, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatalf("reading %s: %v", path, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		again, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if string(again) != string(first) {
			first = again
			continue
		}
		// The file did not change in the last moment: the command has
		// stopped writing.
		if time.Since(s.lastChange(path)) > time.Second {
			return
		}
	}
	s.t.Errorf("the file %s is still changing: the command is still running", path)
}

// lastChange returns when the file was last written.
func (s *suite) lastChange(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// attachAndScrollback covers the flow of a client that attaches to a
// session after a crash: the scrollback holds what the session did, and
// the interrupted run is marked.
func (s *suite) attachAndScrollback() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	// The command ends by itself: the binary dies, and the command goes
	// on without it.
	command := s.boundedCommand()
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": command}}}},
	}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, nil)
	c := s.start(settings, endpoint)
	s.handshake(c)
	sessionID := s.create(c)

	runID := s.prompt(c, sessionID, "do the thing")
	s.waitToolCall(c, runID)

	// The client reads the scrollback before the crash.
	before := s.history(c, sessionID, 0, 500)
	if len(before.Items) == 0 {
		s.t.Fatal("the session's scrollback is empty")
	}

	// The process dies the way a crash does.
	c.kill()

	// A new process attaches to the session.
	other := s.start(settings, endpoint)
	s.handshake(other)
	raw := other.call(protocol.MethodSessionLoad, map[string]any{
		"session_id": sessionID,
	}, &protocol.SessionInfo{})
	var loaded protocol.SessionInfo
	if err := json.Unmarshal(raw, &loaded); err != nil {
		s.t.Fatalf("decoding the load: %v", err)
	}
	if len(loaded.InterruptedRuns) == 0 {
		s.t.Error("the load names no interrupted run")
	}
	for _, run := range loaded.InterruptedRuns {
		if run != runID {
			s.t.Errorf("the load names the interrupted run %q, want %q", run, runID)
		}
	}

	// The scrollback pages backwards: each page is older than the one
	// before it, and the union is the transcript the session recorded
	// before the crash.
	var after []protocol.HistoryItem
	beforeSeq := int64(0)
	for page := 0; ; page++ {
		if page > 10000 {
			s.t.Fatal("the scrollback never reached its oldest page")
		}
		result := s.history(other, sessionID, beforeSeq, 3)
		if len(result.Items) == 0 {
			break
		}
		for i := 1; i < len(result.Items); i++ {
			if result.Items[i].Seq <= result.Items[i-1].Seq {
				s.t.Errorf("the page is not in oldest-first order: %d after %d", result.Items[i].Seq, result.Items[i-1].Seq)
			}
		}
		after = append(result.Items, after...)
		if !result.HasMore {
			break
		}
		beforeSeq = result.Items[0].Seq
	}
	if len(after) < len(before.Items) {
		s.t.Errorf("the scrollback holds %d items, want at least %d", len(after), len(before.Items))
	}
	// The scrollback holds what the session did before the crash.
	for _, item := range before.Items {
		if !historyHas(after, item) {
			s.t.Errorf("the scrollback lost the %q item %q", item.Type, item.Text)
		}
	}
	marked := false
	for _, item := range after {
		if item.Reason == "interrupted" {
			marked = true
		}
	}
	if !marked {
		s.t.Error("the scrollback marks no interrupted run")
	}
}

// historyHas reports whether the page holds the item.
//
// The item is the same when it has the same type, the same text and
// the same handle: the scrollback is a projection, and a reader sees
// the same entry after the crash.
func historyHas(items []protocol.HistoryItem, want protocol.HistoryItem) bool {
	for _, item := range items {
		if item.Type == want.Type && item.Text == want.Text && item.Handle == want.Handle {
			return true
		}
	}
	return false
}

// resume covers the flow of a client that picks a session up after a
// crash: the interrupted run is recorded, and the session takes a new
// prompt.
func (s *suite) resume() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	// The command ends by itself: the binary dies, and the command goes
	// on without it.
	command := s.boundedCommand()
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": command}}}},
		{Text: "the answer after the resume"},
	}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, nil)
	c := s.start(settings, endpoint)
	s.handshake(c)
	sessionID := s.create(c)

	runID := s.prompt(c, sessionID, "do the thing")
	s.waitToolCall(c, runID)
	c.kill()

	other := s.start(settings, endpoint)
	s.handshake(other)
	raw := other.call(protocol.MethodSessionLoad, map[string]any{
		"session_id": sessionID,
	}, &protocol.SessionInfo{})
	var loaded protocol.SessionInfo
	if err := json.Unmarshal(raw, &loaded); err != nil {
		s.t.Fatalf("decoding the load: %v", err)
	}
	if len(loaded.InterruptedRuns) == 0 {
		s.t.Error("the load names no interrupted run")
	}

	// The tool that was running is recorded as interrupted.
	history := s.history(other, sessionID, 0, 500)
	found := false
	for _, item := range history.Items {
		if item.Type == "tool_result" && item.Outcome == "interrupted" {
			found = true
		}
	}
	if !found {
		s.t.Error("the scrollback holds no interrupted tool result")
	}

	// The session takes a new prompt.
	newRun := s.prompt(other, sessionID, "carry on")
	settled := s.settled(other, sessionID, newRun)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the new run settled %q, want end_turn", settled.Reason)
	}
}

// providerLimit covers the flow of a client whose endpoint runs out of
// quota: the run settles as a provider limit, the session is whole,
// and the next prompt works once the endpoint recovers.
func (s *suite) providerLimit() {
	s.t.Helper()
	turns := []Turn{
		{
			Status: 429,
			Error: &ProviderError{
				Type:    "insufficient_quota",
				Code:    "insufficient_quota",
				Message: "the account is out of quota",
			},
		},
		{Text: "the answer after the limit"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "say hello")
	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "provider_limit" {
		s.t.Errorf("the run settled %q, want provider_limit", settled.Reason)
	}
	if settled.Detail.Provider == nil {
		s.t.Fatalf("the settled run holds no provider detail: %+v", settled.Detail)
	}
	if settled.Detail.Provider.Kind != "usage_limit" {
		s.t.Errorf("the provider failure is %q, want usage_limit", settled.Detail.Provider.Kind)
	}

	// The session is not broken.
	raw := c.call(protocol.MethodSessionGet, map[string]any{
		"session_id": sessionID,
	}, &protocol.GetResult{})
	var state protocol.GetResult
	if err := json.Unmarshal(raw, &state); err != nil {
		s.t.Fatalf("decoding the session: %v", err)
	}
	if state.Broken {
		s.t.Error("the session is broken")
	}

	// The next prompt works: the endpoint recovered.
	next := s.prompt(c, sessionID, "say hello again")
	settled = s.settled(c, sessionID, next)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the next run settled %q, want end_turn", settled.Reason)
	}
}

// failures covers the flows of the refusals: a provider that fails, a
// line that is not JSON, a params object the server does not accept, a
// method called too early, a version the server does not speak, a
// second prompt, and a session another process holds.
func (s *suite) failures() {
	s.t.Helper()
	s.failureBeforeTheDelta()
	s.failureAfterTheDelta()
	s.failureMalformedLine()
	s.failureUnknownField()
	s.failureExplicitNull()
	s.failureBeforeTheHandshake()
	s.failureUnsupportedVersion()
	s.failureBusy()
	s.failureLockedSession()
}

// failureBeforeTheDelta covers the provider that fails before it says
// anything: the request is retried, and the run goes on.
func (s *suite) failureBeforeTheDelta() {
	s.t.Helper()
	turns := []Turn{
		{
			Status: 500,
			Error: &ProviderError{
				Type:    "server_error",
				Code:    "internal_error",
				Message: "the endpoint failed",
			},
		},
		{Text: "the answer after the retry"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "say hello")
	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the run settled %q, want end_turn: the request is retried", settled.Reason)
	}
	c.waitEvent("the retry", func(e event) bool {
		return e.Type == "provider.retry"
	})
}

// failureAfterTheDelta covers the provider that fails after it said
// something: the answer is not retried, and the run settles as a
// provider error.
func (s *suite) failureAfterTheDelta() {
	s.t.Helper()
	turns := []Turn{
		{Text: "the first part", FailAfterDelta: true},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "say hello")
	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "provider_error" {
		s.t.Errorf("the run settled %q, want provider_error", settled.Reason)
	}
}

// failureMalformedLine covers the line that is not JSON: the server
// answers with a parse error.
func (s *suite) failureMalformedLine() {
	s.t.Helper()
	c, _ := s.open(s.turns, nil)
	c.writeLine("this is not JSON")

	answer := c.nextAnswer(s.t)
	failure, ok := answer["error"].(map[string]any)
	if !ok {
		s.t.Fatalf("the malformed line was not refused: %v", answer)
	}
	data, _ := failure["data"].(map[string]any)
	if data["code"] != "parse_error" {
		s.t.Errorf("the failure carries %v, want parse_error", data["code"])
	}
}

// failureUnknownField covers the params object that holds a field the
// method does not know: the server refuses it with unknown_field.
func (s *suite) failureUnknownField() {
	s.t.Helper()
	c, _ := s.open(s.turns, nil)
	failure := c.refuse(protocol.MethodSessionCreate, map[string]any{
		"working_dir": s.dir,
		"not_a_field": true,
	})
	if dataCode(failure) != protocol.DataUnknownField {
		s.t.Errorf("the failure carries %q, want %q", dataCode(failure), protocol.DataUnknownField)
	}
}

// failureExplicitNull covers the params object that is null: the server
// refuses it with invalid_params.
func (s *suite) failureExplicitNull() {
	s.t.Helper()
	c, _ := s.open(s.turns, nil)
	c.writeRaw(map[string]any{
		"jsonrpc": "2.0",
		"id":      "null-params",
		"method":  protocol.MethodSessionCreate,
		"params":  nil,
	})
	answer := c.nextAnswer(s.t)
	failure, ok := answer["error"].(map[string]any)
	if !ok {
		s.t.Fatalf("the null params were not refused: %v", answer)
	}
	data, _ := failure["data"].(map[string]any)
	if data["code"] != protocol.DataInvalidParams {
		s.t.Errorf("the failure carries %v, want invalid_params", data["code"])
	}
}

// failureBeforeTheHandshake covers the method called before the
// handshake: the server refuses it with not_initialized.
func (s *suite) failureBeforeTheHandshake() {
	s.t.Helper()
	endpoint := newProvider(s.t, s.turns)
	settings := s.settings(endpoint, s.contextLimit, nil)
	// The client does not shake hands: the method comes first.
	c := s.start(settings, endpoint)
	failure := c.refuse(protocol.MethodSessionCreate, map[string]any{
		"working_dir": s.dir,
	})
	if dataCode(failure) != protocol.DataNotInitialized {
		s.t.Errorf("the failure carries %q, want %q", dataCode(failure), protocol.DataNotInitialized)
	}
}

// failureUnsupportedVersion covers the handshake with a version the
// server does not speak.
func (s *suite) failureUnsupportedVersion() {
	s.t.Helper()
	c, _ := s.open(s.turns, nil)
	failure := c.refuse(protocol.MethodInitialize, map[string]any{
		"protocol_versions": []int{99},
		"client":            map[string]any{"name": "conformance", "version": "1"},
	})
	if dataCode(failure) != protocol.DataProtocolVersionUnsupported {
		s.t.Errorf("the failure carries %q, want %q", dataCode(failure), protocol.DataProtocolVersionUnsupported)
	}
}

// failureBusy covers the second prompt of a running session: the server
// refuses it with busy.
func (s *suite) failureBusy() {
	s.t.Helper()
	turns := []Turn{{Text: "the answer", Delay: time.Second}}
	c, sessionID := s.open(turns, nil)
	s.prompt(c, sessionID, "say hello")

	failure := c.refuse(protocol.MethodAgentPrompt, map[string]any{
		"session_id": sessionID,
		"text":       "and again",
	})
	if dataCode(failure) != protocol.DataBusy {
		s.t.Errorf("the failure carries %q, want %q", dataCode(failure), protocol.DataBusy)
	}
}

// failureLockedSession covers the session another process holds: the
// server refuses the load with session_locked.
func (s *suite) failureLockedSession() {
	s.t.Helper()
	turns := []Turn{{Text: "the answer"}}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, nil)

	// The first process holds the session: it created it, and it has
	// not closed it.
	first := s.start(settings, endpoint)
	s.handshake(first)
	sessionID := s.create(first)

	// The second process cannot open it.
	second := s.start(settings, endpoint)
	s.handshake(second)
	failure := second.refuse(protocol.MethodSessionLoad, map[string]any{
		"session_id": sessionID,
	})
	if dataCode(failure) != protocol.DataSessionLocked {
		s.t.Errorf("the failure carries %q, want %q", dataCode(failure), protocol.DataSessionLocked)
	}
}

// teardown covers the flows of a client that goes away: a session that
// is closed while its run settles, and a client that hangs up with a
// run and an ask in flight.
func (s *suite) teardown() {
	s.t.Helper()
	s.teardownCloseRacingARun()
	// The close race needs no tool, so a hang-up that skips leaves the
	// flow half run rather than wholly skipped.
	s.partial = true
	s.teardownHangUp()
}

// teardownCloseRacingARun covers the close that races a settling run:
// the waiter of the run gets the run's outcome.
func (s *suite) teardownCloseRacingARun() {
	s.t.Helper()
	turns := []Turn{{Text: "the answer", Delay: 500 * time.Millisecond}}
	c, sessionID := s.open(turns, nil)
	runID := s.prompt(c, sessionID, "say hello")

	// The client waits for the run, and closes the session while it
	// waits. The wait is given a moment to start, so the close finds
	// a waiter and not a request that has not been read yet.
	waiting := c.send(protocol.MethodAgentInspect, map[string]any{
		"session_id": sessionID,
		"wait": map[string]any{
			"until":      "settled",
			"run_id":     runID,
			"timeout_ms": 30000,
		},
	})
	time.Sleep(200 * time.Millisecond)
	c.call(protocol.MethodSessionClose, map[string]any{
		"session_id": sessionID,
	}, &struct{}{})

	answer := waiting.wait(s.t)
	if answer.failure != nil {
		s.t.Errorf("the waiter was refused: %s", answer.failure.Message)
	}
	if len(answer.result) == 0 {
		s.t.Error("the waiter got no result")
	}
}

// teardownHangUp covers the client that hangs up with a run and an ask
// in flight: the binary closes the session and ends cleanly.
func (s *suite) teardownHangUp() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	// The command is a short one: the run waits for the ask, not for
	// the command, and the binary hangs up while the ask is pending.
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": "true"}}}},
	}
	c, sessionID := s.open(turns, nil)
	// The client does not answer the ask, and the run waits for it.
	asked := make(chan struct{})
	c.ask = func(protocol.AskParams) string {
		select {
		case <-asked:
		default:
			close(asked)
		}
		return ""
	}
	s.prompt(c, sessionID, "do the thing")
	select {
	case <-asked:
	case <-time.After(10 * time.Second):
		s.t.Fatal("the binary asked nothing")
	}

	// The client hangs up.
	c.closeInput()
	code := c.waitExit(30 * time.Second)
	if code != 0 {
		s.t.Errorf("the binary exited %d, want 0; it wrote: %s", code, c.stderrText())
	}
}

// slowClient covers the client that stops reading: the transport fails
// and the binary ends with a failure.
func (s *suite) slowClient() {
	s.t.Helper()
	s.slowClientQueueOverflow()
	s.slowClientWriteStall()
}

// slowClientQueueOverflow covers the client that stops reading while
// the binary keeps talking: the queue overflows and the transport
// fails.
func (s *suite) slowClientQueueOverflow() {
	s.t.Helper()
	// The answer is bigger than the queue, and it comes in pieces, so
	// the queue fills while the writer waits for the client.
	turns := []Turn{{Chunks: chunks(80, 256<<10)}}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, nil)
	c := s.start(settings, endpoint)
	s.handshake(c)
	sessionID := s.create(c)
	s.prompt(c, sessionID, "say hello")

	// The client stops reading.
	c.stopReading()

	code := c.waitExit(60 * time.Second)
	if code != 1 {
		s.t.Errorf("the binary exited %d, want 1; it wrote: %s", code, c.stderrText())
	}
	if !strings.Contains(c.stderrText(), "the output queue overflowed") {
		s.t.Errorf("the binary wrote %q, want it to say the queue overflowed", c.stderrText())
	}
	s.reloadAfterAFailure(settings, endpoint, sessionID)
}

// slowClientWriteStall covers the client that stops reading with less
// output than the queue holds: the write stalls and the transport
// fails.
func (s *suite) slowClientWriteStall() {
	s.t.Helper()
	// The answer is bigger than the pipe and smaller than the queue.
	turns := []Turn{{Chunks: chunks(200, 1<<10)}}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, nil)
	c := s.start(settings, endpoint)
	s.handshake(c)
	sessionID := s.create(c)
	s.prompt(c, sessionID, "say hello")

	c.stopReading()

	code := c.waitExit(60 * time.Second)
	if code != 1 {
		s.t.Errorf("the binary exited %d, want 1; it wrote: %s", code, c.stderrText())
	}
	if !strings.Contains(c.stderrText(), "the write to the client took more than") {
		s.t.Errorf("the binary wrote %q, want it to say the write stalled", c.stderrText())
	}
	s.reloadAfterAFailure(settings, endpoint, sessionID)
}

// reloadAfterAFailure proves the transport failure released the
// sessions: another process loads the session that the failed process
// served.
func (s *suite) reloadAfterAFailure(settings string, endpoint *provider, sessionID string) {
	s.t.Helper()
	second := s.start(settings, endpoint)
	s.handshake(second)
	pending := second.send(protocol.MethodSessionLoad, map[string]any{
		"session_id": sessionID,
	})
	answer := pending.wait(s.t)
	if answer.failure != nil {
		if dataCode(answer.failure) == protocol.DataSessionLocked {
			s.t.Error("the load after the transport failure is refused with session_locked: the Authority lock was not released")
			return
		}
		s.t.Errorf("the load after the transport failure was refused: %s (%s)", answer.failure.Message, dataCode(answer.failure))
		return
	}
	// The load took the session: closing it releases the second
	// process's authority too.
	second.call(protocol.MethodSessionClose, map[string]any{"session_id": sessionID}, &struct{}{})
}

// chunks returns the pieces an answer arrives in.
func chunks(count, size int) []string {
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, strings.Repeat("x", size))
	}
	return out
}

// ephemeral covers the pass that keeps the sessions in memory: the
// session runs, the scrollback is there, no file is written, and the
// session cannot be loaded.
func (s *suite) ephemeral() {
	s.t.Helper()
	turns := []Turn{{Text: "the answer"}}
	endpoint := newProvider(s.t, turns)
	// The pass has a directory of its own, so the check for the files
	// it wrote is about its own files.
	directory := filepath.Join(s.dir, "ephemeral")
	settings := s.settings(endpoint, s.contextLimit, map[string]any{
		"session": map[string]any{"mode": "logged", "dir": directory},
	})
	c := s.start(settings, endpoint, "--ephemeral")
	s.handshake(c)

	raw := c.call(protocol.MethodSessionCreate, map[string]any{
		"working_dir": s.dir,
		"profile":     s.profile,
	}, &protocol.SessionInfo{})
	var info protocol.SessionInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		s.t.Fatalf("decoding the session: %v", err)
	}
	if info.Mode != "ephemeral" {
		s.t.Fatalf("the session's mode is %q, want ephemeral", info.Mode)
	}
	if info.SessionDir != "" {
		s.t.Errorf("the session names the directory %q, want none", info.SessionDir)
	}

	runID := s.prompt(c, info.SessionID, "say hello")
	s.settled(c, info.SessionID, runID)

	// The scrollback is there, within the process.
	history := s.history(c, info.SessionID, 0, 100)
	if len(history.Items) == 0 {
		s.t.Error("the session's scrollback is empty")
	}

	// No file was written.
	entries, err := os.ReadDir(directory)
	if err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".jsonl") {
				s.t.Errorf("the session directory holds %q, want no session", entry.Name())
			}
		}
	}

	// The session cannot be loaded: it was never written.
	failure := c.refuse(protocol.MethodSessionLoad, map[string]any{
		"session_id": info.SessionID,
	})
	if dataCode(failure) != protocol.DataSessionNotFound {
		s.t.Errorf("the failure carries %q, want %q", dataCode(failure), protocol.DataSessionNotFound)
	}
}
