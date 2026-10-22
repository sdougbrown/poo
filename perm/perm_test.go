package perm_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// The tests drive a policy through a session: the core installs the
// policy in its slot, the model makes the calls, and what the model
// sees says what the policy did.

// readTool is a read-only tool with a path argument.
type readTool struct {
	mu sync.Mutex
	// calls counts the calls the tool ran.
	calls int
	// prepareErr is the failure Prepare returns, when it is set.
	prepareErr error
}

// Spec describes the tool.
func (t *readTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "read",
		Description: "read a range of lines from a file",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"}}}`),
		Class:       poo.ClassReadOnly,
		PrimaryArg:  "path",
	}
}

// Prepare accepts the call.
func (t *readTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	if t.prepareErr != nil {
		return nil, t.prepareErr
	}
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, poo.InvalidArgsf("read: %v", err)
	}
	return readPrepared{path: args.Path, offset: args.Offset}, nil
}

// Call counts the call and returns the file.
func (t *readTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return poo.ToolResult{Text: "the file"}, nil
}

// Calls returns how many calls the tool ran.
func (t *readTool) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// readPrepared is a prepared read call.
type readPrepared struct {
	// path is the file the call reads.
	path string
	// offset is the first line the call shows.
	offset int
}

// Summary describes the call.
func (p readPrepared) Summary() string {
	return p.path
}

// bashTool is a side-effecting tool with a command argument.
type bashTool struct {
	mu sync.Mutex
	// calls counts the calls the tool ran.
	calls int
}

// Spec describes the tool.
func (t *bashTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "bash",
		Description: "run a shell command",
		Schema:      json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
		Class:       poo.ClassSideEffecting,
		PrimaryArg:  "command",
	}
}

// Prepare accepts the call.
func (t *bashTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, poo.InvalidArgsf("bash: %v", err)
	}
	return bashPrepared{command: args.Command}, nil
}

// Call counts the call and returns its output.
func (t *bashTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return poo.ToolResult{Text: "the output"}, nil
}

// Calls returns how many calls the tool ran.
func (t *bashTool) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// bashPrepared is a prepared bash call.
type bashPrepared struct {
	// command is the command the call runs.
	command string
}

// Summary describes the call.
func (p bashPrepared) Summary() string {
	return p.command
}

// session is a session under test.
type session struct {
	// session is the session itself.
	session *poo.Session
	// transport is the scripted transport it talks to.
	transport *pootest.Transport
	// sink is the record sink it writes to.
	sink *pootest.Sink
}

// newSession builds a session with the given policy and tools.
//
// The session is closed when the test ends.
func newSession(t *testing.T, turns []pootest.Turn, tools map[string]poo.Tool, order []string, policy *perm.Policy) *session {
	t.Helper()
	transport := pootest.NewTransport(turns...)
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport: transport,
		Tools:     tools,
		ToolOrder: order,
		Sink:      sink,
		SessionID: "s1",
		Model:     poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithPolicy(policy))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return &session{session: s, transport: transport, sink: sink}
}

// run prompts the session and waits for the run to settle.
func (s *session) run(t *testing.T) poo.Result {
	t.Helper()
	if _, err := s.session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	return res
}

// lastResult returns the text of the tool result the model saw last.
//
// The result carries the handle line the core adds, so the text is what
// follows it.
func (s *session) lastResult(t *testing.T) string {
	t.Helper()
	results := s.results(t)
	if len(results) == 0 {
		t.Fatal("the model saw no tool result")
	}
	return resultText(results[len(results)-1])
}

// resultText returns the text of a tool result without its handle line.
func resultText(text string) string {
	if _, rest, ok := strings.Cut(text, "\n"); ok {
		return rest
	}
	return text
}

// results returns the text of every tool result the model saw.
func (s *session) results(t *testing.T) []string {
	t.Helper()
	requests := s.transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the model was never asked")
	}
	var out []string
	for _, message := range requests[len(requests)-1].Messages {
		for _, c := range message.Content {
			if c.Kind == poo.ContentToolResult {
				out = append(out, c.Text)
			}
		}
	}
	return out
}

// events returns the session's events with the given name.
func (s *session) events(t *testing.T, name poo.EventName) []poo.Event {
	t.Helper()
	var out []poo.Event
	for _, ev := range s.session.Events() {
		if ev.Name == name {
			out = append(out, ev)
		}
	}
	return out
}

// waitForEvent waits for an event with the given name.
//
// The policy slot runs in the call's own goroutine, and a run that ends
// settles before the slot has finished, so the event may arrive a
// moment later. The wait is bounded, and it sleeps for a millisecond at
// a time.
func (s *session) waitForEvent(t *testing.T, name poo.EventName) poo.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range s.events(t, name) {
			return ev
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the session emitted no %s event", name)
	return poo.Event{}
}

// readCall is a scripted read call.
func readCall(id, path string, offset int) pootest.Call {
	args := map[string]any{"path": path}
	if offset > 0 {
		args["offset"] = offset
	}
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return pootest.Call{Name: "read", ID: id, Arguments: raw}
}

// bashCall is a scripted bash call.
func bashCall(id, command string) pootest.Call {
	raw, err := json.Marshal(map[string]any{"command": command})
	if err != nil {
		panic(err)
	}
	return pootest.Call{Name: "bash", ID: id, Arguments: raw}
}

// fakeAsker answers a permission ask with a fixed decision.
//
// The asker is available unless it is told otherwise, and it records
// the asks it was given.
type fakeAsker struct {
	mu sync.Mutex
	// decision is what the asker answers.
	decision perm.Decision
	// err is the failure the asker returns, when it is set.
	err error
	// available reports whether the asker can answer now.
	available bool
	// budget is how long the asker may take.
	budget time.Duration
	// asked counts the asks the asker was given.
	asked int
	// lastAsk is the ask the asker was given last.
	lastAsk perm.Ask
	// before is called before the asker answers, so a test can act at a
	// known point.
	before func(ctx context.Context)
}

// Ask answers the ask.
func (a *fakeAsker) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	a.mu.Lock()
	a.asked++
	a.lastAsk = ask
	before := a.before
	decision := a.decision
	err := a.err
	a.mu.Unlock()
	if before != nil {
		before(ctx)
	}
	if err != nil {
		return perm.DecisionDefer, err
	}
	return decision, nil
}

// Available reports whether the asker can answer now.
func (a *fakeAsker) Available() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.available
}

// CanEverAnswer reports that the asker may answer an ask.
func (a *fakeAsker) CanEverAnswer() bool { return true }

// Budget is how long the asker may take.
func (a *fakeAsker) Budget() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.budget
}

// AskerName is the name the asker's answers carry.
func (a *fakeAsker) AskerName() string { return "fake" }

// Asks returns how many asks the asker was given.
func (a *fakeAsker) Asks() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.asked
}

// LastAsk returns the ask the asker was given last.
func (a *fakeAsker) LastAsk() perm.Ask {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastAsk
}
