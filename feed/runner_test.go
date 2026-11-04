package feed_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/cli-registry"
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/feed"
)

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
	run := feed.Runner(spec, c.executor, envLookup(), t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, poo.FeedIO{Emit: got.emit, Log: log})
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
			if n := got.len(); n != 1 {
				t.Errorf("the attempt emitted %d inputs, want the one before the failure", n)
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
// process sees: the manifest's literal, and a reference the session's
// environment resolves.
func TestTheManifestEnvironmentReachesTheCommand(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			spec := feed.Spec{
				Name:    "echoenv",
				Command: []string{script(t, "echoenv")},
				Env: []cliregistry.EnvBinding{
					{Name: "FIXTURE_ECHO", Value: "the literal"},
					{Name: "FIXTURE_REFERENCE", From: "FIXTURE_HOST", Reference: true},
				},
			}
			run := feed.Runner(spec, c.executor, envLookup("FIXTURE_HOST=the reference"), t.TempDir())
			var mu sync.Mutex
			var texts []string
			err := run(context.Background(), poo.FeedIO{
				Emit: func(from, text string) error {
					mu.Lock()
					defer mu.Unlock()
					texts = append(texts, text)
					return nil
				},
				Log: silent(),
			})
			if err != nil {
				t.Fatalf("the attempt failed: %v", err)
			}
			if len(texts) != 1 || texts[0] != "the literal and the reference" {
				t.Errorf("the command read %v, want the literal and the resolved reference", texts)
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
			got := &emitted{refuse: poo.ErrSessionClosed}
			run := feed.Runner(feed.Spec{Name: "chat", Command: []string{script(t, "chat")}}, c.executor, envLookup(), t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- run(ctx, poo.FeedIO{Emit: got.emit, Log: silent()})
			}()
			err := result(t, done)
			if !errors.Is(err, poo.ErrSessionClosed) {
				t.Errorf("the attempt returned %v, want the session's refusal", err)
			}
		})
	}
}
