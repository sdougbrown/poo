package pootest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/sdougbrown/poo"
)

// JobTool is a scripted tool that may run in the background.
//
// The tool is the test double the jobs stage needs: it can be declared
// Allowed, so the model chooses per call, or Always, so every call is a
// job.
//
// A call blocks on a gate the test controls, so a test can hold a job
// open and release it at a known point. Nothing sleeps: the gate is a
// channel, and the test decides when the job finishes.
type JobTool struct {
	mu sync.Mutex
	// name is the tool's name.
	name string
	// background is the tool's background mode.
	background poo.Background
	// result is the text the tool returns.
	result string
	// err is the failure the tool returns, when it is set.
	err error
	// started is signalled when a call starts, so a test knows the job
	// is running.
	started chan struct{}
	// gate holds the releases, one per call.
	gate chan struct{}
	// closed is closed when every call may finish.
	closed chan struct{}
	// rounds counts the releases that let the waiting calls finish.
	rounds int
	// calls counts the calls the tool served.
	calls int
	// envs holds the environment of every call, in order.
	envs []poo.ToolEnv
}

// NewJobTool returns a job tool.
//
// The background mode is BackgroundAllowed by default, so the model
// chooses per call. The tool blocks until Release is called, so a test
// controls when the job finishes.
func NewJobTool(name string, background poo.Background) *JobTool {
	if background == "" {
		background = poo.BackgroundAllowed
	}
	return &JobTool{
		name:       name,
		background: background,
		started:    make(chan struct{}, 64),
		gate:       make(chan struct{}, 64),
		closed:     make(chan struct{}),
	}
}

// Release lets one blocked call finish.
//
// A call that is not blocked yet takes the release when it starts, so a
// test may release before the call begins.
func (t *JobTool) Release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case t.gate <- struct{}{}:
	default:
	}
}

// ReleaseAll lets every blocked call finish, and every call that starts
// after it.
func (t *JobTool) ReleaseAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
}

// ReleaseWaiting lets every call that is blocked right now finish.
//
// The release is a round: a call that starts after it is blocked again,
// so a test can release one round of calls without opening the tool for
// good.
func (t *JobTool) ReleaseWaiting() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rounds++
	// The calls that are waiting now are released at once; a call that
	// starts after the release captures the new round and is blocked
	// again.
	for {
		select {
		case t.gate <- struct{}{}:
		default:
			return
		}
	}
}

// Started returns the channel that is signalled when a call starts.
//
// A test waits on it to know that the job is running, and it never
// sleeps.
func (t *JobTool) Started() <-chan struct{} { return t.started }

// Calls returns how many calls the tool served.
func (t *JobTool) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// Envs returns the environment of every call, in order.
func (t *JobTool) Envs() []poo.ToolEnv {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]poo.ToolEnv(nil), t.envs...)
}

// SetResult sets the text the tool returns.
func (t *JobTool) SetResult(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.result = text
}

// SetError makes the tool fail with an error.
func (t *JobTool) SetError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

// Spec describes the tool.
func (t *JobTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        t.name,
		Description: "a scripted tool that may run in the background",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "text": {"type": "string"}
  }
}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "text",
		Background: t.background,
	}
}

// Prepare accepts the call.
func (t *JobTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("%s: the arguments are not valid: %v", t.name, err)
	}
	return jobPrepared{text: a.Text}, nil
}

// Call blocks until the test releases it, and then answers.
//
// A call that arrives after a release round started is not blocked: the
// round releases the calls that were waiting when it began.
func (t *JobTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.mu.Lock()
	t.calls++
	t.envs = append(t.envs, env)
	gate := t.gate
	closed := t.closed
	round := t.rounds
	t.mu.Unlock()

	select {
	case t.started <- struct{}{}:
	default:
	}

	t.mu.Lock()
	released := t.rounds > round
	t.mu.Unlock()
	if !released {
		select {
		case <-gate:
		case <-closed:
		case <-ctx.Done():
			// The stop wraps the context's cause, so a caller can
			// recognize the cancellation either way.
			return poo.ToolResult{Text: "the call was stopped"}, fmt.Errorf("pootest: the job was stopped: %w", ctx.Err())
		}
	}

	t.mu.Lock()
	result := t.result
	err := t.err
	t.mu.Unlock()
	if err != nil {
		return poo.ToolResult{}, err
	}
	return poo.ToolResult{Text: result}, nil
}

// jobPrepared is a prepared job-tool call.
type jobPrepared struct {
	// text is the call's text.
	text string
}

// Summary describes the call.
func (p jobPrepared) Summary() string { return p.text }

// ErrJobStopped is the error a job tool returns when it is stopped.
var ErrJobStopped = errors.New("pootest: the job was stopped")
