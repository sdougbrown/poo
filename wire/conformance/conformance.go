// Package conformance proves that a poo binary speaks the public wire
// protocol.
//
// The suite is black box. It starts a binary, points it at a fake
// OpenAI-compatible endpoint through a settings file it generates, and
// drives the binary over stdin and stdout. It never reaches into the
// binary: everything it knows, it learns from the protocol.
//
// The suite is reusable. A binary that links the wire protocol may be
// checked against it, and the binary's own CI runs it. The flows
// adapt to what the binary announces: a flow that needs a tool the
// binary does not offer is skipped, and the reason is logged.
//
// The messages the binary sends are decoded under the plan's
// forward-compatibility rule: a field a known shape does not have is a
// failure, unless an announced capability covers it, and an unknown
// capability key or an unknown event type is tolerated.
package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// Target is the binary the suite runs.
type Target struct {
	// Binary is the path of the binary: cmd/poo, or a custom build.
	Binary string
	// Args are the arguments the binary is started with, e.g.
	// []string{"serve"}.
	//
	// The suite adds the settings file it generates, so the binary
	// takes a --settings flag.
	Args []string
	// Profile is the profile the sessions run under. It must be a
	// profile the binary offers in its handshake. An empty name is
	// the suite's own: the code profile.
	Profile string
	// Fixtures are the scripted provider turns and the environment
	// the settings name. A target that leaves them empty gets the
	// suite's own.
	Fixtures Fixtures
	// Env is the environment the binary is started with, appended to
	// the suite's own.
	//
	// A binary that reads a variable before it serves (an identity, the
	// path of the socket it reports to) cannot run without one, and the
	// suite sets it here rather than in the test process's environment:
	// the value is the target's business, and the suite's own
	// environment stays as it is.
	Env []string
}

// Fixtures are the scripted provider turns and the environment.
//
// The turns are what the fake provider answers with, and a turn may
// call a tool of the target's own: a caller scripts a turn that
// exercises the tool the binary offers.
type Fixtures struct {
	// Turns are the answers the provider serves, in order. The
	// provider serves the last turn again when the requests run out.
	//
	// A target that leaves them empty gets the suite's own turns: a
	// plain answer, and another one.
	Turns []Turn
	// Model is the model identifier the settings name. An empty name
	// is the suite's own.
	Model string
	// ContextLimit is the window the settings name, in tokens. Zero
	// is the suite's own.
	ContextLimit int
}

// Turn is one scripted answer of the fake provider.
type Turn struct {
	// Text is the text the assistant says.
	Text string
	// Chunks are the pieces the answer arrives in. A turn that names
	// them sends one delta per piece, so a flow can script a run that
	// streams a lot.
	Chunks []string
	// ToolCalls are the tool calls the assistant makes.
	ToolCalls []ToolCall
	// Status is the HTTP status the endpoint answers with. Zero is
	// 200.
	Status int
	// Error is the error body the endpoint answers with. It is the
	// body a rate-limited or a failing endpoint sends.
	Error *ProviderError
	// Delay is how long the endpoint waits before it answers. A turn
	// with a delay gives a flow the time to interrupt the run.
	Delay time.Duration
	// FailAfterDelta answers with the text and then breaks the stream.
	//
	// A stream that carried a delta is not retried: the model has
	// seen part of the answer.
	FailAfterDelta bool
}

// ToolCall is one tool call the assistant makes.
type ToolCall struct {
	// Name is the tool's name.
	Name string
	// Arguments are the tool's arguments.
	Arguments map[string]any
}

// ProviderError is the error body an endpoint answers with.
type ProviderError struct {
	// Type is the error's type, e.g. "insufficient_quota".
	Type string
	// Code is the error's code, e.g. "insufficient_quota".
	Code string
	// Message is the error's message.
	Message string
}

// The defaults the suite uses when the target brings no fixtures.
const (
	// defaultModel is the model identifier the settings name.
	defaultModel = "conformance-model"
	// defaultContextLimit is the window the settings name.
	defaultContextLimit = 32768
	// defaultProfile is the profile the sessions run under.
	defaultProfile = "code"
)

// defaultTurns are the answers the provider gives when the target
// brings no turns of its own.
var defaultTurns = []Turn{
	{Text: "the first answer"},
	{Text: "the second answer"},
}

// suite is the suite's state: the binary, the temporary directory and
// the settings the flows write.
type suite struct {
	// t is the test the suite runs in.
	t *testing.T
	// target is the binary the suite runs.
	target Target
	// dir is the temporary directory the suite works in.
	dir string
	// sessions is the directory the session logs live in.
	sessions string
	// profile is the profile the sessions run under.
	profile string
	// model is the model identifier the settings name.
	model string
	// contextLimit is the window the settings name.
	contextLimit int
	// turns are the answers the provider gives when a flow does not
	// script its own.
	turns []Turn
	// handshakeResult is what the binary announced in the handshake.
	handshakeResult protocol.InitializeResult
	// settingsCount is how many settings files the suite wrote.
	settingsCount int
	// commands is how many commands the suite scripted.
	commands int
	// commandSeconds is how long a scripted command runs.
	commandSeconds int
	// flowName is the name of the flow that is running.
	flowName string
	// skips are the reasons the running flow did not run.
	skips []string
	// partial reports whether a flow ran before it skipped.
	partial bool
	// report is what the suite ran.
	report Report
}

// Report is what the suite ran.
type Report struct {
	// Run names the flows that ran whole, in order. A flow that ran in
	// part is not here.
	Run []string
	// Partial names the flows that ran in part and skipped the rest, in
	// order. The flow has one entry in Skipped for each part it skipped.
	//
	// A flow that has two halves reports itself here when the first
	// half ran and the second needed something the target does not
	// offer.
	Partial []string
	// Skipped is the reasons the flows did not run, in order.
	//
	// A flow is here when the target does not offer what the flow
	// scripts, so a binary with fewer tools reports more skips. A flow
	// that ran in part is here for the part it skipped and is named in
	// Partial as well, so the flow names here are the skipped flows and
	// the partly run ones: a caller that counts the flows that did not run
	// at all subtracts Partial from them.
	Skipped []Skip
}

// Skip is one reason a flow did not run.
type Skip struct {
	// Flow names the flow.
	Flow string
	// Reason says why the flow skipped.
	Reason string
}

// Run runs the suite against the target and returns what it ran.
//
// The suite runs every flow the target can run. A flow that needs
// something the target does not have is skipped, and the reason is
// logged and reported.
func Run(t *testing.T, target Target) Report {
	t.Helper()
	if target.Binary == "" {
		t.Fatal("conformance: the target has no binary")
	}
	s := &suite{
		t:              t,
		target:         target,
		dir:            t.TempDir(),
		profile:        target.Profile,
		model:          defaultModel,
		contextLimit:   defaultContextLimit,
		turns:          defaultTurns,
		commandSeconds: 2,
	}
	if s.profile == "" {
		s.profile = defaultProfile
	}
	if target.Fixtures.Model != "" {
		s.model = target.Fixtures.Model
	}
	if target.Fixtures.ContextLimit > 0 {
		s.contextLimit = target.Fixtures.ContextLimit
	}
	if len(target.Fixtures.Turns) > 0 {
		s.turns = target.Fixtures.Turns
	}
	s.sessions = filepath.Join(s.dir, "sessions")

	s.flow("spawn", s.spawn)
	s.flow("prompt and stream", s.promptAndStream)
	s.flow("status and wait", s.statusAndWait)
	s.flow("permission gate", s.permissionGate)
	s.flow("follow-up", s.followUp)
	s.flow("steer", s.steer)
	s.flow("interrupt", s.interrupt)
	s.flow("follow-up message", s.followUpMessage)
	s.flow("typed result", s.typedResult)
	s.flow("heard and said", s.heardAndSaid)
	s.flow("handoff", s.handoff)
	s.flow("kill", s.kill)
	s.flow("jobs", s.jobs)
	s.flow("attach and scrollback", s.attachAndScrollback)
	s.flow("resume", s.resume)
	s.flow("provider limit", s.providerLimit)
	s.flow("failures", s.failures)
	s.flow("teardown", s.teardown)
	s.flow("slow client", s.slowClient)
	s.flow("ephemeral", s.ephemeral)
	s.t.Logf("flows: %d ran, %d ran in part, %d skipped, %d skip reasons",
		len(s.report.Run), len(s.report.Partial), skippedFlows(s.report.Skipped), len(s.report.Skipped))
	return s.report
}

// skippedFlows returns how many flows the skip reasons name.
//
// A flow can have several reasons, and a flow that ran in part is among
// them.
func skippedFlows(skips []Skip) int {
	seen := make(map[string]struct{}, len(skips))
	for _, skip := range skips {
		seen[skip.Flow] = struct{}{}
	}
	return len(seen)
}

// flow runs one flow and says which one it was.
//
// The log names the flow, so a failure says which one it was.
func (s *suite) flow(name string, run func()) {
	s.t.Helper()
	s.flowName = name
	s.skips = nil
	s.partial = false
	start := time.Now()
	s.t.Logf("flow: %s", name)
	run()
	if len(s.skips) == 0 {
		s.report.Run = append(s.report.Run, name)
	} else if s.partial {
		s.report.Partial = append(s.report.Partial, name)
	}
	for _, reason := range s.skips {
		s.report.Skipped = append(s.report.Skipped, Skip{Flow: name, Reason: reason})
	}
	s.t.Logf("flow: %s took %s", name, time.Since(start).Round(time.Millisecond))
}

// boundedCommand returns a command that ends by itself.
//
// A command that waits for the suite to end it is the one a killed
// binary leaves behind: the binary dies, and the command goes on
// without it. A command that ends by itself leaves nothing behind.
func (s *suite) boundedCommand() string {
	s.t.Helper()
	s.commands++
	return fmt.Sprintf("sleep %d", s.commandSeconds)
}

// markerWaitSteps is how many 50ms steps a marker wait takes before it
// ends by itself: about ten seconds.
const markerWaitSteps = 200

// markerCommand returns a command that waits, bounded, for a marker in
// the working directory and then ends.
//
// The wait holds the run inside its tool call while a flow sends its
// message: the marker is written once the message is accepted. Like
// boundedCommand, the command ends by itself, so a binary that dies
// leaves nothing behind.
func (s *suite) markerCommand(marker string) string {
	s.t.Helper()
	s.commands++
	return fmt.Sprintf("i=0; while [ \"$i\" -lt %d ] && [ ! -e %s ]; do sleep 0.05; i=$((i+1)); done",
		markerWaitSteps, marker)
}

// settings writes a settings file for a flow and returns its path.
//
// The file points the binary at the fake endpoint, names the model and
// the profile, and puts the session logs in the suite's directory. The
// extra document holds the settings a flow needs.
func (s *suite) settings(endpoint *provider, contextLimit int, extra map[string]any) string {
	s.t.Helper()
	document := map[string]any{
		"version": 1,
		"endpoints": map[string]any{
			"fake": map[string]any{"base_url": endpoint.server.URL + "/v1"},
		},
		"models": map[string]any{
			s.model: map[string]any{
				"endpoint":      "fake",
				"model_id":      s.model,
				"api":           "chat_completions",
				"tools_mode":    "native",
				"ops":           []string{"chat"},
				"context_limit": contextLimit,
			},
		},
		"roles": map[string]any{"chat": s.model},
		"session": map[string]any{
			"mode": "logged",
			"dir":  s.sessions,
		},
		"permission": map[string]any{
			// A side-effecting call no rule covers is asked about, and
			// the client answers it. The chain gives up after two
			// seconds, so a client that does not answer does not hold
			// the run up.
			"unmatched":       "ask",
			"fallback":        "allow",
			"askers":          []string{"wire"},
			"chain_timeout_s": 2,
		},
	}
	for name, value := range extra {
		document[name] = value
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		s.t.Fatalf("encoding the settings: %v", err)
	}
	// Every flow has a settings file of its own: a binary that starts
	// late must not read the settings of the flow that came after it.
	s.settingsCount++
	path := filepath.Join(s.dir, fmt.Sprintf("settings-%d.json", s.settingsCount))
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		s.t.Fatalf("writing the settings: %v", err)
	}
	return path
}
