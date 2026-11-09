package exec

import (
	"errors"
	"os"
	"sync"
)

// StdinPipe writes a command's input through a pipe.
//
// The executor owns the write rather than handing the runtime a reader to
// copy from. Once the command has ended, Wait closes the writer. A descendant
// holding the read end cannot hold the call.
//
// NewStdinPipe starts the write. The caller gives Reader to the command and
// closes the parent's copy with CloseReader after the command starts. The caller
// invokes Wait when the command ends, or Abort if it never started.
type StdinPipe struct {
	reader *os.File
	writer *os.File
	done   chan error
	once   sync.Once
	err    error
}

// NewStdinPipe starts writing input through a pipe.
func NewStdinPipe(input string) (*StdinPipe, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p := &StdinPipe{reader: reader, writer: writer, done: make(chan error, 1)}
	go func() {
		_, err := writer.WriteString(input)
		writer.Close()
		p.done <- err
	}()
	return p, nil
}

// Reader is the pipe's read end, for the command's standard input.
func (p *StdinPipe) Reader() *os.File { return p.reader }

// CloseReader closes the parent's copy after the command inherits the read end.
func (p *StdinPipe) CloseReader() { _ = p.reader.Close() }

// Wait stops a write the command is no longer reading and reports whether
// every byte was written. It is called once the command has ended.
func (p *StdinPipe) Wait() error {
	p.once.Do(func() {
		// A descendant can retain the read end after the command exits.
		// Closing the writer releases any write still blocked on that reader.
		_ = p.writer.Close()
		p.err = <-p.done
		_ = p.reader.Close()
	})
	return p.err
}

// Abort releases the pipe for a command that never started.
func (p *StdinPipe) Abort() {
	p.once.Do(func() {
		_ = p.writer.Close()
		_ = p.reader.Close()
		p.err = <-p.done
	})
}

// JoinStdinIncomplete folds a child wait error and a stdin write error into
// the error an executor's Wait reports.
//
// A stopped command keeps its wait error alone: a cancellation is never
// reported as an input failure. Any other command joins an incomplete stdin
// write alongside its own outcome, so a declared-success exit code cannot hide
// an incomplete write. errors.Is finds ErrStdinIncomplete in the result, and
// errors.As still finds the child's own error through it.
func JoinStdinIncomplete(waitErr, stdinErr error, stopped bool) error {
	if stopped || stdinErr == nil {
		return waitErr
	}
	if waitErr == nil {
		return ErrStdinIncomplete
	}
	return errors.Join(waitErr, ErrStdinIncomplete)
}
