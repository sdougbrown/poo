package feed_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/cli-registry"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/feed"
)

// errRefused is what a test's emit reports when it stands in for a session
// that will not take the input.
var errRefused = errors.New("the session is closed")

// emitted collects the inputs a runner's commands produce.
type emitted struct {
	mu sync.Mutex
	// inputs are the inputs, in the order they were emitted.
	inputs []input
	// refuse is what Emit reports instead of accepting an input, when
	// a test wants the session's refusal.
	refuse error
}

// input is one emitted input: who sent it, and what it says.
type input struct {
	from string
	text string
}

// emit accepts one input, or refuses it as the test asked.
func (e *emitted) emit(from, text string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.refuse != nil {
		return e.refuse
	}
	e.inputs = append(e.inputs, input{from: from, text: text})
	return nil
}

// all returns the inputs emitted so far.
func (e *emitted) all() []input {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]input(nil), e.inputs...)
}

// len is how many inputs were emitted.
func (e *emitted) len() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.inputs)
}

// silent returns a logger that drops what it is given.
func silent() *slog.Logger { return slog.New(slog.DiscardHandler) }

// start runs one attempt in the background and returns what it emits,
// the cancel that stops it, and the channel the attempt's result
// arrives on.
func start(t *testing.T, c executorCase, spec feed.Spec, log *slog.Logger) (*emitted, context.CancelFunc, chan error) {
	t.Helper()
	got := &emitted{}
	dir := t.TempDir()
	run := feed.Runner(spec, c.executor, envLookup())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, dir, got.emit, log)
	}()
	return got, cancel, done
}

// result waits for the attempt to end, and fails the test when the
// fixture outlives it: every fixture that fails waits a minute, so a
// runner that did not reap its command would hang here.
func result(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("the attempt did not end: the command was not reaped")
		return nil
	}
}

// TestTheRunnerEmitsWhatTheCommandPrints covers a long-lived feed: the
// runner reads the lines while the command runs, and cancelling the
// attempt ends the process.
func TestTheRunnerEmitsWhatTheCommandPrints(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got, cancel, done := start(t, c, feed.Spec{Name: "chat", Command: []string{script(t, "chat")}}, silent())
			waitFor(t, "the two messages", func() bool { return got.len() == 2 })
			cancel()
			if err := result(t, done); !errors.Is(err, context.Canceled) {
				t.Errorf("the cancelled attempt returned %v, want context.Canceled", err)
			}
			inputs := got.all()
			want := []input{
				{from: "alice", text: "the first message"},
				{text: "the second message"},
			}
			if len(inputs) != len(want) {
				t.Fatalf("the attempt emitted %v, want %v", inputs, want)
			}
			for i := range want {
				if inputs[i] != want[i] {
					t.Errorf("the input %d is %+v, want %+v", i, inputs[i], want[i])
				}
			}
		})
	}
}

// TestALineThatIsNotAMessageFailsTheAttempt covers the protocol: the
// first line the object does not describe ends the attempt, the
// messages before it stand, and the command is reaped anyway.
func TestALineThatIsNotAMessageFailsTheAttempt(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got, cancel, done := start(t, c, feed.Spec{Name: "badline", Command: []string{script(t, "badline")}}, silent())
			defer cancel()
			err := result(t, done)
			if err == nil {
				t.Fatal("the attempt accepted a line that is not a message")
			}
			if !strings.Contains(err.Error(), "not a feed message") {
				t.Errorf("the attempt failed with %q, want it to name the line", err)
			}
			// The fixture prints a good line after the bad one: the
			// attempt ended at the failure, so that line is discarded.
			inputs := got.all()
			want := input{text: "the message before the failure"}
			if len(inputs) != 1 || inputs[0] != want {
				t.Errorf("the attempt emitted %+v, want only %+v", inputs, want)
			}
		})
	}
}

// TestALinePastTheLimitFailsTheAttempt covers the buffer: a line longer
// than a line may be is a failure, not something the session holds in
// memory.
func TestALinePastTheLimitFailsTheAttempt(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got, cancel, done := start(t, c, feed.Spec{Name: "oversize", Command: []string{script(t, "oversize")}}, silent())
			defer cancel()
			err := result(t, done)
			if err == nil {
				t.Fatal("the attempt accepted a line past the limit")
			}
			if !strings.Contains(err.Error(), "longer than") {
				t.Errorf("the attempt failed with %q, want it to name the limit", err)
			}
			if n := got.len(); n != 0 {
				t.Errorf("the attempt emitted %d inputs for a line past the limit", n)
			}
		})
	}
}

// TestTheStderrGoesToTheLogAndNotToTheModel covers the two streams: a
// command's diagnostics are the log's, and only a message reaches the
// session.
func TestTheStderrGoesToTheLogAndNotToTheModel(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			log := &logLines{}
			got, cancel, done := start(t, c, feed.Spec{Name: "noisy", Command: []string{script(t, "noisy")}}, log.logger())
			waitFor(t, "the message", func() bool { return got.len() == 1 })
			cancel()
			_ = result(t, done)
			waitFor(t, "the diagnostic in the log", func() bool {
				return strings.Contains(log.text(), "the diagnostic the model must not see")
			})
			for _, in := range got.all() {
				if strings.Contains(in.text, "diagnostic") {
					t.Errorf("the diagnostics reached the model: %+v", in)
				}
			}
		})
	}
}

// TestACommandThatExitsWithAnErrorFailsTheAttempt covers the exit
// status: a command that ends without a message and without a zero
// status is a failure that names the status.
func TestACommandThatExitsWithAnErrorFailsTheAttempt(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got, cancel, done := start(t, c, feed.Spec{Name: "failing", Command: []string{script(t, "failing")}}, silent())
			defer cancel()
			err := result(t, done)
			if err == nil {
				t.Fatal("the attempt accepted a command that exited with an error")
			}
			if !strings.Contains(err.Error(), "exited with status 3") {
				t.Errorf("the attempt failed with %q, want it to name the exit status", err)
			}
			if n := got.len(); n != 0 {
				t.Errorf("the attempt emitted %d inputs for a command that printed none", n)
			}
		})
	}
}

// TestAPartialLineAtCancelIsDropped covers the end of a stopped
// attempt: a line the command left unterminated is not a message, so a
// cancelled attempt delivers nothing.
func TestAPartialLineAtCancelIsDropped(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			log := &logLines{}
			got, cancel, done := start(t, c, feed.Spec{Name: "partial", Command: []string{script(t, "partial")}}, log.logger())
			defer cancel()
			// The diagnostic says the unterminated bytes are written,
			// so the cancel comes after them rather than before.
			waitFor(t, "the partial line to be written", func() bool {
				return strings.Contains(log.text(), "the partial line is out")
			})
			cancel()
			if err := result(t, done); !errors.Is(err, context.Canceled) {
				t.Errorf("the cancelled attempt returned %v, want context.Canceled", err)
			}
			if inputs := got.all(); len(inputs) != 0 {
				t.Errorf("the cancelled attempt emitted %+v, want nothing from a partial line", inputs)
			}
		})
	}
}

// TestTheLastLineWithoutANewlineIsDelivered covers the end of the
// output: a line a command ends without a newline is a message like any
// other.
func TestTheLastLineWithoutANewlineIsDelivered(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got, cancel, done := start(t, c, feed.Spec{Name: "unterminated", Command: []string{script(t, "unterminated")}}, silent())
			defer cancel()
			if err := result(t, done); err != nil {
				t.Fatalf("the attempt failed: %v", err)
			}
			inputs := got.all()
			if len(inputs) != 1 || inputs[0].text != "the last message" {
				t.Errorf("the attempt emitted %+v, want the unterminated line", inputs)
			}
		})
	}
}

// TestAnEnvironmentReferenceThatDoesNotResolveFailsBeforeTheCommand
// covers the environment: a manifest that names a variable the session
// does not have is a failure, and the command does not start.
func TestAnEnvironmentReferenceThatDoesNotResolveFailsBeforeTheCommand(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			spec := feed.Spec{
				Name:    "chat",
				Command: []string{script(t, "chat")},
				Env:     []cliregistry.EnvBinding{{Name: "TOKEN", From: "FIXTURE_MISSING", Reference: true}},
			}
			got, cancel, done := start(t, c, spec, silent())
			defer cancel()
			err := result(t, done)
			if err == nil {
				t.Fatal("the attempt started a command whose environment does not resolve")
			}
			if !strings.Contains(err.Error(), "FIXTURE_MISSING") {
				t.Errorf("the attempt failed with %q, want it to name the variable", err)
			}
			if n := got.len(); n != 0 {
				t.Errorf("the command ran anyway: it emitted %d inputs", n)
			}
		})
	}
}

// TestTheManifestEnvironmentReachesTheCommand covers what a feed's
// process sees: the manifest's literal, a reference the session's
// environment resolves, and nothing the manifest did not declare.
func TestTheManifestEnvironmentReachesTheCommand(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FIXTURE_UNDECLARED", "a variable the feed did not declare")
			spec := feed.Spec{
				Name:    "echoenv",
				Command: []string{script(t, "echoenv")},
				Env: []cliregistry.EnvBinding{
					{Name: "FIXTURE_ECHO", Value: "the literal"},
					{Name: "FIXTURE_REFERENCE", From: "FIXTURE_HOST", Reference: true},
				},
			}
			run := feed.Runner(spec, c.executor, envLookup("FIXTURE_HOST=the reference"))
			var mu sync.Mutex
			var texts []string
			err := run(context.Background(), t.TempDir(), func(from, text string) error {
				mu.Lock()
				defer mu.Unlock()
				texts = append(texts, text)
				return nil
			}, silent())
			if err != nil {
				t.Fatalf("the attempt failed: %v", err)
			}
			want := "the literal and the reference and []"
			if len(texts) != 1 || texts[0] != want {
				t.Errorf("the command read %v, want %q", texts, want)
			}
		})
	}
}

// TestAnInputTheSessionRefusesEndsTheAttempt covers the acceptance: the
// runner stops when the session will not take its input, and the
// session's error is the attempt's.
func TestAnInputTheSessionRefusesEndsTheAttempt(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got := &emitted{refuse: errRefused}
			run := feed.Runner(feed.Spec{Name: "chat", Command: []string{script(t, "chat")}}, c.executor, envLookup())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- run(ctx, t.TempDir(), got.emit, silent())
			}()
			err := result(t, done)
			if !errors.Is(err, errRefused) {
				t.Errorf("the attempt returned %v, want the session's refusal", err)
			}
		})
	}
}

// recordingExecutor keeps the command it was asked to run, so a test can
// read what the runner handed the executor.
type recordingExecutor struct {
	mu      sync.Mutex
	command exec.Command
}

// Name is the executor's name.
func (r *recordingExecutor) Name() string { return "recording" }

// Start keeps the command and returns a process that has already
// finished.
func (r *recordingExecutor) Start(_ context.Context, command exec.Command, _ io.Writer) (exec.Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.command = command
	return finished{}, nil
}

// finished is a process that has already exited.
type finished struct{}

// Wait reports a clean exit.
func (finished) Wait() (int, error) { return 0, nil }

// Kill does nothing: the process has ended.
func (finished) Kill() {}

// Stopped reports that the process exited on its own.
func (finished) Stopped() bool { return false }

// TestAFeedWithNoEnvironmentIsGivenAnEmptyOne covers the executor
// contract: a nil environment means the command inherits the executor's
// own, so a feed whose environment resolves to nothing is handed an
// empty one rather than the host's.
func TestAFeedWithNoEnvironmentIsGivenAnEmptyOne(t *testing.T) {
	recorder := &recordingExecutor{}
	run := feed.Runner(feed.Spec{Name: "quiet", Command: []string{"/bin/true"}}, recorder, envLookup())
	if err := run(context.Background(), t.TempDir(), func(string, string) error { return nil }, silent()); err != nil {
		t.Fatalf("the attempt failed: %v", err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.command.Env == nil {
		t.Error("the command was handed a nil environment, which inherits the executor's own")
	}
}
