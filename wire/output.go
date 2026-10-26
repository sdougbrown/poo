package wire

import (
	"errors"
	"sync"
)

// The failures the output queue reports.
var (
	// errOutputOverflow means the queue is full: the client stopped
	// reading.
	errOutputOverflow = errors.New("wire: the output queue overflowed")
	// errOutputClosed means the queue is closed and takes no more.
	errOutputClosed = errors.New("wire: the output queue is closed")
)

// output is the server's bounded queue of messages.
//
// One writer goroutine drains it, so the messages reach the client in
// the order they were queued. The queue is bounded in messages and in
// bytes: a client that stopped reading fills it, and the overflow is a
// transport failure.
type output struct {
	// mu guards the queue.
	mu sync.Mutex
	// cond wakes the writer when a message arrives.
	cond *sync.Cond
	// queue holds the messages, oldest first.
	queue [][]byte
	// bytes is how many bytes the queue holds.
	bytes int
	// maxMessages is how many messages the queue holds.
	maxMessages int
	// maxBytes is how many bytes the queue holds.
	maxBytes int
	// closed reports that the queue takes no more messages.
	closed bool
}

// newOutput returns a queue with the given bounds.
func newOutput(maxMessages, maxBytes int) *output {
	o := &output{maxMessages: maxMessages, maxBytes: maxBytes}
	o.cond = sync.NewCond(&o.mu)
	return o
}

// push queues a message.
//
// The queue is full when it holds too many messages or too many bytes.
// The message is refused then, and the caller treats the refusal as a
// transport failure.
func (o *output) push(line []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errOutputClosed
	}
	if len(o.queue) >= o.maxMessages || o.bytes+len(line) > o.maxBytes {
		return errOutputOverflow
	}
	o.queue = append(o.queue, line)
	o.bytes += len(line)
	o.cond.Signal()
	return nil
}

// pop takes the next message, waiting for one.
//
// It reports false when the queue is closed and empty: the writer has
// nothing left to do.
func (o *output) pop() ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.queue) == 0 && !o.closed {
		o.cond.Wait()
	}
	if len(o.queue) == 0 {
		return nil, false
	}
	line := o.queue[0]
	o.queue = o.queue[1:]
	o.bytes -= len(line)
	return line, true
}

// close ends the queue.
//
// The writer drains what is left and stops.
func (o *output) close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	o.cond.Broadcast()
}
