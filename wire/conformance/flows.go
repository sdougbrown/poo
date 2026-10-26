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

// The flows of the suite. They mirror how a client drives a runtime:
// the binary is spawned, the session is opened, the run is prompted and
// watched, and the session is closed.

// open starts a binary with the given turns and returns a client and
// the session it opened.
func (s *suite) open(turns []Turn, extra map[string]any) (*client, string) {
	s.t.Helper()
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, extra)
	c := s.start(settings, endpoint)
	s.handshake(c)
	return c, s.create(c)
}

// handshake does the handshake and records what the binary announced.
func (s *suite) handshake(c *client) protocol.InitializeResult {
	s.t.Helper()
	raw := c.call(protocol.MethodInitialize, map[string]any{
		"protocol_versions": []int{protocol.Version},
		"client": map[string]any{
			"name":    "conformance",
			"version": "1",
			"capabilities": map[string]any{
				"permission_answers": true,
			},
		},
	}, &protocol.InitializeResult{})

	var result protocol.InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		s.t.Fatalf("decoding the handshake: %v", err)
	}
	if result.ProtocolVersion != protocol.Version {
		s.t.Errorf("the protocol version is %d, want %d", result.ProtocolVersion, protocol.Version)
	}
	if result.Build.Name == "" {
		s.t.Error("the handshake names no build")
	}
	if len(result.Profiles) == 0 {
		s.t.Error("the handshake offers no profile")
	}
	s.handshakeResult = result
	return result
}

// create opens a session and returns its id.
func (s *suite) create(c *client) string {
	s.t.Helper()
	return s.createInfo(c).SessionID
}

// createInfo opens a session and returns what the binary said about it.
func (s *suite) createInfo(c *client) protocol.SessionInfo {
	s.t.Helper()
	raw := c.call(protocol.MethodSessionCreate, map[string]any{
		"working_dir": s.dir,
		"profile":     s.profile,
	}, &protocol.SessionInfo{})
	var info protocol.SessionInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		s.t.Fatalf("decoding the session: %v", err)
	}
	if info.SessionID == "" {
		s.t.Fatalf("the session has no id: %s", string(raw))
	}
	if info.Mode != "logged" {
		s.t.Errorf("the session's mode is %q, want logged", info.Mode)
	}
	if info.SessionDir == "" {
		s.t.Error("the session names no directory")
	}
	// The session runs under the profile the client asked for: it
	// offers that profile's tools, and none of another profile's. A
	// tool the flow added (the typed result's finish, say) is not a
	// profile tool and is allowed.
	s.checkSessionTools(info.Tools, s.profileTools(), s.handshakeResult.Profiles)
	return info
}

// checkSessionTools checks the tools a created session offers against
// the profile the client asked for.
func (s *suite) checkSessionTools(got, want []protocol.ToolInfo, profiles []protocol.ProfileInfo) {
	s.t.Helper()
	byName := make(map[string]protocol.ToolInfo, len(got))
	for _, tool := range got {
		byName[tool.Name] = tool
	}
	foreign := map[string]bool{}
	for _, profile := range profiles {
		for _, tool := range profile.Tools {
			found := false
			for _, own := range want {
				if own.Name == tool.Name {
					found = true
					break
				}
			}
			if !found {
				foreign[tool.Name] = true
			}
		}
	}
	for _, tool := range want {
		gotTool, ok := byName[tool.Name]
		if !ok {
			s.t.Errorf("the session does not offer the profile's %q tool", tool.Name)
			continue
		}
		if gotTool != tool {
			s.t.Errorf("the session's %q tool is %+v, want %+v", tool.Name, gotTool, tool)
		}
	}
	for _, tool := range got {
		if foreign[tool.Name] {
			s.t.Errorf("the session offers %q, a tool of another profile", tool.Name)
		}
	}
}

// prompt starts a run and returns its id.
func (s *suite) prompt(c *client, sessionID, text string) string {
	s.t.Helper()
	raw := c.call(protocol.MethodAgentPrompt, map[string]any{
		"session_id": sessionID,
		"text":       text,
	}, &protocol.PromptResult{})
	var result protocol.PromptResult
	if err := json.Unmarshal(raw, &result); err != nil {
		s.t.Fatalf("decoding the prompt: %v", err)
	}
	if result.RunID == "" {
		s.t.Fatalf("the prompt returned no run id: %s", string(raw))
	}
	return result.RunID
}

// inspect asks the session for its state, and waits for a run when the
// wait is given.
func (s *suite) inspect(c *client, sessionID string, wait *protocol.InspectWait, afterSeq int64) protocol.InspectResult {
	s.t.Helper()
	params := map[string]any{"session_id": sessionID}
	if wait != nil {
		params["wait"] = wait
	}
	if afterSeq > 0 {
		params["after_seq"] = afterSeq
	}
	raw := c.call(protocol.MethodAgentInspect, params, &protocol.InspectResult{})
	var result protocol.InspectResult
	if err := json.Unmarshal(raw, &result); err != nil {
		s.t.Fatalf("decoding the inspect: %v", err)
	}
	return result
}

// sendMessage sends a message with the mode the call names, and the
// result names an input.
func (s *suite) sendMessage(c *client, sessionID, text, mode string) protocol.MessageResult {
	s.t.Helper()
	raw := c.call(protocol.MethodAgentMessage, map[string]any{
		"session_id": sessionID,
		"text":       text,
		"mode":       mode,
	}, &protocol.MessageResult{})
	var message protocol.MessageResult
	if err := json.Unmarshal(raw, &message); err != nil {
		s.t.Fatalf("decoding the message: %v", err)
	}
	if message.InputID == "" {
		s.t.Errorf("the message returned no input id: %s", string(raw))
	}
	return message
}

// settled waits for a run to settle and returns the outcome.
func (s *suite) settled(c *client, sessionID, runID string) protocol.Settled {
	s.t.Helper()
	result := s.inspect(c, sessionID, &protocol.InspectWait{
		Until:     "settled",
		RunID:     runID,
		TimeoutMS: 30000,
	}, 0)
	if result.Settled == nil {
		s.t.Fatalf("the run %q did not settle: %+v", runID, result)
	}
	return *result.Settled
}

// profileTools returns the tools the profile offers.
func (s *suite) profileTools() []protocol.ToolInfo {
	for _, profile := range s.handshakeResult.Profiles {
		if profile.Name == s.profile {
			return profile.Tools
		}
	}
	return nil
}

// hasTool reports whether the profile offers a tool by name.
func (s *suite) hasTool(name string) bool {
	for _, tool := range s.profileTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// skip records that the current flow does not run, and says why.
func (s *suite) skip(reason string) {
	s.t.Logf("skipping %s: %s", s.flowName, reason)
	s.skips = append(s.skips, reason)
}

// skipWithoutTool reports whether the current flow must skip.
//
// A flow scripts a tool's call, so it needs the tool the binary offers
// under that name: a profile that offers no bash cannot run a flow that
// scripts one.
func (s *suite) skipWithoutTool(name string) bool {
	if s.hasTool(name) {
		return false
	}
	s.skip(fmt.Sprintf("the profile %q offers no %q tool", s.profile, name))
	return true
}

// hasBackground reports whether the profile offers a tool that may run
// in the background.
func (s *suite) hasBackground() bool {
	for _, tool := range s.profileTools() {
		if tool.Background != "" && tool.Background != "never" {
			return true
		}
	}
	return false
}

// spawn covers the flow a client starts with: the process comes up, the
// handshake says what the binary can do, and a session opens.
func (s *suite) spawn() {
	s.t.Helper()
	c, sessionID := s.open(s.turns, nil)

	result := s.handshakeResult
	found := false
	for _, profile := range result.Profiles {
		if profile.Name != s.profile {
			continue
		}
		found = true
		if len(profile.Tools) == 0 && s.profile != "base" {
			s.t.Errorf("the profile %q offers no tool", s.profile)
		}
	}
	if !found {
		s.t.Errorf("the handshake does not offer the profile %q", s.profile)
	}
	if result.Models.Chat == "" {
		s.t.Error("the handshake names no chat model")
	}
	if len(result.Capabilities.Persistence) == 0 {
		s.t.Error("the handshake announces no persistence mode")
	}

	state := s.inspect(c, sessionID, nil, 0)
	if state.State == "" {
		s.t.Error("the session has no state")
	}
}

// promptAndStream covers the flow of a run: the prompt starts it, the
// events stream in, and the run settles.
func (s *suite) promptAndStream() {
	s.t.Helper()
	c, sessionID := s.open(s.turns, nil)
	runID := s.prompt(c, sessionID, "say hello")

	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
	if settled.RunID != runID {
		s.t.Errorf("the settled run is %q, want %q", settled.RunID, runID)
	}

	events := c.eventList()
	if len(events) == 0 {
		s.t.Fatal("the binary sent no event")
	}
	var last int64
	sawDelta := false
	for _, evt := range events {
		if evt.Seq <= last {
			s.t.Errorf("the event %q has the sequence number %d, want more than %d", evt.Type, evt.Seq, last)
		}
		last = evt.Seq
		// The notification names the session the event belongs to.
		if evt.SessionID != sessionID {
			s.t.Errorf("the event %q names the session %q, want %q", evt.Type, evt.SessionID, sessionID)
		}
		if evt.Type == "message.delta" {
			sawDelta = true
		}
	}
	if !sawDelta {
		s.t.Error("the binary sent no message delta")
	}
}

// statusAndWait covers the flow of a client that watches a run: the
// inspect waits for the settle, it pages through the events, and a wait
// that runs out is not a failure.
func (s *suite) statusAndWait() {
	s.t.Helper()
	turns := []Turn{{Text: "the answer", Delay: 300 * time.Millisecond}}
	c, sessionID := s.open(turns, nil)
	runID := s.prompt(c, sessionID, "say hello")

	// A wait that runs out is not a failure: the client asks again.
	timedOut := s.inspect(c, sessionID, &protocol.InspectWait{
		Until:     "settled",
		RunID:     runID,
		TimeoutMS: 50,
	}, 0)
	if !timedOut.TimedOut {
		s.t.Error("the wait did not run out")
	}
	if timedOut.Settled != nil {
		s.t.Error("the wait that ran out settled the run")
	}

	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}

	// The events page: the page after the last one is empty.
	first := s.inspect(c, sessionID, nil, 0)
	if len(first.Events) == 0 {
		s.t.Fatal("the inspect returned no event")
	}
	second := s.inspect(c, sessionID, nil, first.NextSeq)
	if len(second.Events) != 0 {
		s.t.Errorf("the page after the last event holds %d events, want none", len(second.Events))
	}
}

// permissionGate covers the flow of a client that answers the server's
// asks: it allows a call, it refuses a call, and it stays quiet when
// the ask runs out of time.
func (s *suite) permissionGate() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": "echo allowed"}}}},
		{Text: "the first answer"},
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": "echo refused"}}}},
		{Text: "the second answer"},
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": "echo unanswered"}}}},
		{Text: "the third answer"},
	}
	c, sessionID := s.open(turns, nil)

	// The client allows the call.
	c.ask = func(protocol.AskParams) string { return protocol.DecisionAllow }
	runID := s.prompt(c, sessionID, "run the command")
	s.settled(c, sessionID, runID)
	if outcome := s.toolOutcome(c, runID); outcome != "ok" {
		s.t.Errorf("the allowed call ended %q, want ok", outcome)
	}

	// The client refuses the call.
	c.ask = func(protocol.AskParams) string { return protocol.DecisionDeny }
	runID = s.prompt(c, sessionID, "run the command")
	s.settled(c, sessionID, runID)
	if outcome := s.toolOutcome(c, runID); outcome != "denied" {
		s.t.Errorf("the refused call ended %q, want denied", outcome)
	}

	// The client says nothing: the ask runs out of time and the
	// fallback decides.
	c.ask = func(protocol.AskParams) string { return "" }
	runID = s.prompt(c, sessionID, "run the command")
	s.settled(c, sessionID, runID)
	if outcome := s.toolOutcome(c, runID); outcome != "ok" {
		s.t.Errorf("the unanswered call ended %q, want ok: the fallback allows it", outcome)
	}
}

// toolOutcome returns the outcome of the first tool call of a run.
func (s *suite) toolOutcome(c *client, runID string) string {
	s.t.Helper()
	evt := c.waitEvent("tool result", func(e event) bool {
		return e.Type == "tool.result" && e.Payload["run_id"] == runID
	})
	outcome, _ := evt.Payload["outcome"].(string)
	return outcome
}

// followUp covers the flow of a client that asks the same session
// again: the second run carries on where the first one stopped.
func (s *suite) followUp() {
	s.t.Helper()
	c, sessionID := s.open(s.turns, nil)

	first := s.prompt(c, sessionID, "the first question")
	s.settled(c, sessionID, first)
	second := s.prompt(c, sessionID, "the second question")
	settled := s.settled(c, sessionID, second)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the second run settled %q, want end_turn", settled.Reason)
	}

	history := s.history(c, sessionID, 0, 100)
	if len(history.Items) == 0 {
		s.t.Fatal("the session's scrollback is empty")
	}
	questions := 0
	for _, item := range history.Items {
		if item.Role == "user" && strings.Contains(item.Text, "question") {
			questions++
		}
	}
	if questions != 2 {
		s.t.Errorf("the scrollback holds %d questions, want 2", questions)
	}
}

// history asks the session for its history.
func (s *suite) history(c *client, sessionID string, beforeSeq int64, limit int) protocol.HistoryResult {
	s.t.Helper()
	params := map[string]any{"session_id": sessionID}
	if beforeSeq > 0 {
		params["before_seq"] = beforeSeq
	}
	if limit > 0 {
		params["limit"] = limit
	}
	raw := c.call(protocol.MethodSessionHistory, params, &protocol.HistoryResult{})
	var result protocol.HistoryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		s.t.Fatalf("decoding the history: %v", err)
	}
	return result
}

// steer covers the flow of a client that redirects a run: the message
// arrives at the next turn.
//
// The command waits for the marker the flow writes once the message is
// accepted, and acknowledges it; the flow waits for that acknowledgement
// before it settles, so the run is still inside its tool call when the
// steer arrives.
func (s *suite) steer() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	marker := filepath.Join(s.dir, "steer.marker")
	s.resetMarker(marker)
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": s.markerCommand(marker)}}}},
		{Text: "the answer"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "do the thing")
	s.waitToolCall(c, runID)
	s.sendMessage(c, sessionID, "the steered message", "steer")
	// The message is accepted, so the tool may see the marker and end.
	s.markMarker(marker)
	// The tool confirms it saw the marker while its call was open, or the
	// flow fails here instead of testing a post-tool-call message.
	s.waitMarkerSeen(marker)

	s.settled(c, sessionID, runID)

	// The steered message is in the request the next turn made.
	requests := c.requests()
	if len(requests) < 2 {
		s.t.Fatalf("the endpoint got %d requests, want 2", len(requests))
	}
	if !requestHolds(requests[1], "the steered message") {
		s.t.Errorf("the second request does not carry the steered message: %v", requests[1])
	}
}

// waitToolCall waits for the tool call of a run.
func (s *suite) waitToolCall(c *client, runID string) {
	s.t.Helper()
	c.waitEvent("tool call", func(e event) bool {
		return e.Type == "tool.call" && e.Payload["run_id"] == runID
	})
}

// interrupt covers the flow of a client that stops a run: the tool is
// cancelled, the turn is interrupted, and the run carries on.
func (s *suite) interrupt() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	command := s.boundedCommand()
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": command}}}},
		{Text: "the answer after the interrupt"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "do the thing")
	s.waitToolCall(c, runID)
	s.sendMessage(c, sessionID, "stop that", "interrupt")

	c.waitEvent("the interrupted turn", func(e event) bool {
		return e.Type == "turn.interrupted" && e.Payload["run_id"] == runID
	})
	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the run settled %q, want end_turn: the run carries on after the interrupt", settled.Reason)
	}

	// The interrupted message reached the model: the request the next
	// turn made carries it.
	requests := c.requests()
	if len(requests) < 2 {
		s.t.Fatalf("the endpoint got %d requests, want 2", len(requests))
	}
	if !requestHolds(requests[1], "stop that") {
		s.t.Errorf("the second request does not carry the interrupt: %v", requests[1])
	}
}

// followUpMessage covers the flow of a client that queues a message for
// the run's natural stop: the message is delivered after the model
// stopped, and the run carries on.
//
// The command waits for the marker the flow writes once the message is
// accepted, and acknowledges it; the flow waits for that acknowledgement
// before it settles, so the run is still inside its tool call when the
// follow-up arrives.
func (s *suite) followUpMessage() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	marker := filepath.Join(s.dir, "follow-up.marker")
	s.resetMarker(marker)
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": s.markerCommand(marker)}}}},
		{Text: "the answer"},
		{Text: "the answer after the follow-up"},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "do the thing")
	s.waitToolCall(c, runID)
	s.sendMessage(c, sessionID, "the queued message", "follow_up")
	// The message is accepted, so the tool may see the marker and end.
	s.markMarker(marker)
	// The tool confirms it saw the marker while its call was open, or the
	// flow fails here instead of testing a post-tool-call message.
	s.waitMarkerSeen(marker)

	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "end_turn" {
		s.t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}

	requests := c.requests()
	if len(requests) < 3 {
		s.t.Fatalf("the endpoint got %d requests, want 3", len(requests))
	}
	if requestHolds(requests[1], "the queued message") {
		s.t.Errorf("the second request carries the follow-up: it waits for the natural stop: %v", requests[1])
	}
	if !requestHolds(requests[2], "the queued message") {
		s.t.Errorf("the third request does not carry the follow-up: %v", requests[2])
	}
}

// typedResult covers the flow of a headless client: the model finishes
// with a typed result, and the run settles with it.
func (s *suite) typedResult() {
	s.t.Helper()
	schema := filepath.Join(s.dir, "result.schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`), 0o600); err != nil {
		s.t.Fatalf("writing the schema: %v", err)
	}
	extra := map[string]any{
		"finish": map[string]any{"schema_file": schema},
	}
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "finish", Arguments: map[string]any{
			"result": map[string]any{"answer": "the result"},
		}}}},
	}
	endpoint := newProvider(s.t, turns)
	settings := s.settings(endpoint, s.contextLimit, extra)
	c := s.start(settings, endpoint)
	s.handshake(c)
	// The finish tool is the settings' opt-in, so the binary announces
	// it with the session and not with the profile.
	info := s.createInfo(c)
	sessionID := info.SessionID
	offers := false
	for _, tool := range info.Tools {
		if tool.Name == "finish" {
			offers = true
			break
		}
	}
	if !offers {
		s.skip(`the session offers no "finish" tool`)
		return
	}

	runID := s.prompt(c, sessionID, "finish it")
	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "finished" {
		s.t.Errorf("the run settled %q, want finished", settled.Reason)
	}
	if len(settled.Detail.Result) == 0 {
		s.t.Fatalf("the settled run holds no result: %+v", settled.Detail)
	}
	if !strings.Contains(string(settled.Detail.Result), "the result") {
		s.t.Errorf("the result is %s, want the model's answer", string(settled.Detail.Result))
	}
}

// heardAndSaid covers the flow of a client that shows the answer as it
// arrives: the note says what the user had seen, and the answer the
// model gave is unchanged.
func (s *suite) heardAndSaid() {
	s.t.Helper()
	c, sessionID := s.open(s.turns, nil)

	runID := s.prompt(c, sessionID, "say hello")
	s.settled(c, sessionID, runID)

	// The client read the answer as it arrived, and it knows the
	// message's id.
	delta := c.waitEvent("message delta", func(e event) bool {
		return e.Type == "message.delta" && e.Payload["run_id"] == runID
	})
	messageID, _ := delta.Payload["message_id"].(string)
	if messageID == "" {
		s.t.Fatalf("the delta names no message: %v", delta.Payload)
	}

	raw := c.call(protocol.MethodAgentPrompt, map[string]any{
		"session_id": sessionID,
		"text":       "and again",
		"delivered": map[string]any{
			"message_id": messageID,
			"upto_chars": 4,
		},
	}, &protocol.PromptResult{})
	var prompted protocol.PromptResult
	if err := json.Unmarshal(raw, &prompted); err != nil {
		s.t.Fatalf("decoding the prompt: %v", err)
	}
	s.settled(c, sessionID, prompted.RunID)

	// The next request shows the note on the user's message, and the
	// assistant's message is the one the model gave.
	requests := c.requests()
	if len(requests) == 0 {
		s.t.Fatal("the endpoint got no request")
	}
	last := requests[len(requests)-1]
	if !requestHolds(last, "The user saw or heard your reply") {
		s.t.Errorf("the last request holds no delivery note: %v", last)
	}
	answer := requestText(last, "assistant")
	if answer != s.turns[0].Text {
		s.t.Errorf("the assistant's message is %q, want the answer the model gave: %q", answer, s.turns[0].Text)
	}
}

// kill covers the flow of a client that stops a run: the tool is
// cancelled, the run settles as cancelled, and the waiter wakes.
func (s *suite) kill() {
	s.t.Helper()
	if s.skipWithoutTool("bash") {
		return
	}
	command := s.boundedCommand()
	turns := []Turn{
		{ToolCalls: []ToolCall{{Name: "bash", Arguments: map[string]any{"command": command}}}},
	}
	c, sessionID := s.open(turns, nil)

	runID := s.prompt(c, sessionID, "do the thing")
	s.waitToolCall(c, runID)
	c.call(protocol.MethodAgentCancel, map[string]any{
		"session_id": sessionID,
		"run_id":     runID,
	}, &struct{}{})

	settled := s.settled(c, sessionID, runID)
	if settled.Reason != "cancelled" {
		s.t.Errorf("the run settled %q, want cancelled", settled.Reason)
	}
	// The tool got a result, so the session's history is whole.
	c.waitEvent("tool result", func(e event) bool {
		return e.Type == "tool.result" && e.Payload["run_id"] == runID
	})
}

// requestHolds reports whether a request holds the text.
func requestHolds(request map[string]any, text string) bool {
	messages, _ := request["messages"].([]any)
	for _, entry := range messages {
		message, _ := entry.(map[string]any)
		content, _ := message["content"].(string)
		if strings.Contains(content, text) {
			return true
		}
	}
	return false
}

// requestText returns the text of the message with the role.
func requestText(request map[string]any, role string) string {
	messages, _ := request["messages"].([]any)
	for _, entry := range messages {
		message, _ := entry.(map[string]any)
		if message["role"] != role {
			continue
		}
		content, _ := message["content"].(string)
		return content
	}
	return ""
}
