package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec"
)

// The limits one attempt reads under.
const (
	// maxLineBytes is the longest line a feed's stdout may carry. A
	// line is one message, so a longer one is a feed printing
	// something that is not a message: the attempt fails rather than
	// the session holding it in memory.
	maxLineBytes = 1 << 20
	// maxStderrBytes is how much of a command's stderr one attempt
	// logs. A feed that floods its diagnostics loses the rest: the log
	// is not the feed's output.
	maxStderrBytes = 64 << 10
)

// silentLogger drops what a runner that was given no logger says.
var silentLogger = slog.New(slog.DiscardHandler)

// Runner returns the runner that runs one attempt of a feed's command.
//
// Each call starts the manifest's command once with the environment the
// manifest declares, hands its stdout to the session line by line, and
// reaps the process before it returns, so nothing the attempt started
// outlives it.
//
// A line is one JSON object naming the message the feed wants
// delivered:
//
//	{"text": "the message", "from": "alice"}
//
// `text` is required and is not empty; `from` names the sender and may
// be left out. A blank line is skipped, and a field the message does
// not name is ignored, so a feed may carry its own bookkeeping in a
// line. A line the object does not describe is a protocol failure:
// silence about a message is better than a guess at what it meant.
//
// The first protocol failure — a line that is not such an object, a
// line longer than the line limit, or an input the session refuses — is
// the attempt's error. The runner then stops the command, discards
// whatever else it says, and still reaps it.
//
// The command's stderr is the runner's diagnostics: it goes to the
// attempt's log, never to the model, and never into an input.
//
// The environment follows a registered CLI's rules: the benign names,
// then the manifest's own entries, resolved through env. A reference
// that does not resolve fails the attempt, and the command does not
// start.
func Runner(spec Spec, ex exec.Executor, env func(string) (string, bool), dir string) poo.FeedRunner {
	return func(ctx context.Context, feed poo.FeedIO) error {
		resolved, err := spec.ResolveEnv(env)
		if err != nil {
			return fmt.Errorf("feed %q: %w", spec.Name, err)
		}
		// The runner cancels its own context as well as the session's:
		// a protocol failure has to stop the command, and the executor
		// stops it when the context ends.
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		stdout := &lineReader{emit: feed.Emit, cancel: cancel}
		stderr := newStderrLog(feed.Log, maxStderrBytes)
		proc, err := ex.Start(runCtx, exec.Command{
			Argv:   spec.Command,
			Dir:    dir,
			Env:    resolved,
			Stderr: stderr,
		}, stdout)
		if err != nil {
			return fmt.Errorf("feed %q: %w", spec.Name, err)
		}
		// The output is read to its end before Wait returns, so every
		// line is in by now and the last one is decoded here.
		code, _ := proc.Wait()
		stderr.finish()
		if ctx.Err() != nil {
			// The attempt was stopped, so a line the command left
			// unterminated is not a message: the session is not taking
			// input for the attempt any more.
			if err := stdout.failure(); err != nil {
				return err
			}
			return ctx.Err()
		}
		if err := stdout.finish(); err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("feed %q: the command exited with status %d", spec.Name, code)
		}
		return nil
	}
}

// message is one line of a feed's stdout: the input the feed emits.
type message struct {
	// Text is the message's text. It is required.
	Text string `json:"text"`
	// From names the sender, and is empty when the feed names none.
	From string `json:"from"`
}

// lineReader turns a command's stdout into the inputs the feed emits.
//
// The executor writes the output as it arrives and does not stop when
// the writer refuses it, so a failure is recorded rather than returned:
// every later byte is discarded, and the attempt's error is what was
// recorded first. A writer that returned its error would be a writer
// the executor keeps writing to, and the command behind a full pipe
// would never end.
type lineReader struct {
	// emit queues one input. It is the session's, and its refusal ends
	// the attempt.
	emit func(from, text string) error
	// cancel stops the command.
	cancel context.CancelFunc
	// pending holds the bytes of the line that has not ended yet.
	pending []byte
	// err is the first failure, and nil while the lines are good.
	err error
}

// Write reads the output the executor hands over, one line at a time.
//
// The write always reports every byte as taken, whatever the lines say:
// the executor has no way to stop a command that keeps writing, so
// discarding is how the attempt keeps its output from blocking the
// process.
func (r *lineReader) Write(p []byte) (int, error) {
	n := len(p)
	if r.err != nil {
		return n, nil
	}
	r.pending = append(r.pending, p...)
	for {
		end := bytes.IndexByte(r.pending, '\n')
		if end < 0 {
			break
		}
		line := string(r.pending[:end])
		r.pending = r.pending[end+1:]
		if err := r.accept(line); err != nil {
			return n, nil
		}
	}
	if len(r.pending) > maxLineBytes {
		r.fail(fmt.Errorf("a line is longer than %d bytes", maxLineBytes))
	}
	return n, nil
}

// finish decodes the line the command left unterminated and returns the
// attempt's first failure.
func (r *lineReader) finish() error {
	if r.err != nil {
		return r.err
	}
	if len(r.pending) == 0 {
		return nil
	}
	line := string(r.pending)
	r.pending = nil
	_ = r.accept(line)
	return r.err
}

// failure returns the attempt's first failure.
func (r *lineReader) failure() error {
	return r.err
}

// accept decodes one line and emits the input it names.
//
// A blank line is skipped: a feed that separates its messages with one
// is still describing itself, not sending an empty message.
func (r *lineReader) accept(line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	var msg message
	// A line holds one object and nothing else: a second value, or
	// anything after the first, is a feed whose output the session
	// cannot read, and following the first object would deliver
	// something other than what the feed wrote.
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return r.fail(fmt.Errorf("a line is not a feed message: %w", err))
	}
	if msg.Text == "" {
		return r.fail(fmt.Errorf("a feed message has no text"))
	}
	if err := r.emit(msg.From, msg.Text); err != nil {
		return r.fail(err)
	}
	return nil
}

// fail records the attempt's failure and stops the command.
//
// The first failure is the attempt's: what the command says after it is
// the noise of a process on its way out.
func (r *lineReader) fail(err error) error {
	if r.err != nil {
		return r.err
	}
	r.err = err
	r.pending = nil
	r.cancel()
	return err
}

// stderrLog writes a command's stderr to the attempt's log, a line at a
// time.
//
// The budget bounds one attempt: a feed whose diagnostics run past it
// loses the rest, with one line saying so. A nil logger drops the
// output.
type stderrLog struct {
	// log receives the lines.
	log *slog.Logger
	// left is how many bytes of this attempt's stderr may still be
	// logged.
	left int
	// dropped reports that the budget is spent.
	dropped bool
	// pending holds the bytes of the line that has not ended yet.
	pending []byte
}

// newStderrLog returns the writer for one attempt's stderr.
func newStderrLog(log *slog.Logger, budget int) *stderrLog {
	if log == nil {
		log = silentLogger
	}
	return &stderrLog{log: log, left: budget}
}

// Write logs the output the executor hands over, one line at a time.
//
// It never fails: the log is not the process's to stop, and an
// attempt's failure is the feed's own, not its diagnostics'.
func (w *stderrLog) Write(p []byte) (int, error) {
	n := len(p)
	if w.dropped || w.left <= 0 {
		w.drop()
		return n, nil
	}
	w.pending = append(w.pending, p...)
	for {
		end := bytes.IndexByte(w.pending, '\n')
		if end < 0 {
			break
		}
		line := string(w.pending[:end])
		w.pending = w.pending[end+1:]
		w.line(line)
		if w.dropped {
			return n, nil
		}
	}
	// A line that outgrows what the attempt may log is logged in part:
	// waiting for its end would keep the rest of the output in memory
	// for a command that may never end it.
	if len(w.pending) > w.left {
		w.line(string(w.pending))
		w.pending = nil
	}
	return n, nil
}

// finish logs the line the command left unterminated, if any, and
// returns.
func (w *stderrLog) finish() {
	if w.dropped || len(w.pending) == 0 {
		return
	}
	line := string(w.pending)
	w.pending = nil
	w.line(line)
}

// line logs one line, within the attempt's budget.
//
// A line the budget cannot hold is logged in part, and the rest of the
// attempt's stderr is dropped.
func (w *stderrLog) line(line string) {
	if w.dropped {
		return
	}
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return
	}
	if len(line) > w.left {
		if w.left > 0 {
			w.log.Info("feed stderr", slog.String("line", line[:w.left]))
		}
		w.drop()
		return
	}
	w.left -= len(line) + 1
	w.log.Info("feed stderr", slog.String("line", line))
}

// drop says once that the attempt's stderr is past its budget, and
// stops logging it.
func (w *stderrLog) drop() {
	if w.dropped {
		return
	}
	w.dropped = true
	w.pending = nil
	w.log.Warn("feed stderr dropped: the attempt's stderr is past its limit")
}
