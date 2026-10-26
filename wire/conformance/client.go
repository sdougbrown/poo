package conformance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// client drives a binary over its pipes.
//
// The client is a JSON-RPC 2.0 peer: it sends the requests, reads the
// answers and the notifications, and answers the server's permission
// asks. Every message is read under the plan's forward-compatibility
// rule.
type client struct {
	// t is the test the client runs in.
	t *testing.T
	// cmd is the running binary.
	cmd *exec.Cmd
	// in is the client's end of the binary's input.
	in io.WriteCloser
	// reader reads the binary's output.
	reader *bufio.Reader
	// done is closed when the binary's output ends.
	done chan struct{}
	// lines carries the lines the binary sent.
	lines chan []byte
	// loose holds the answers that came for a request the client
	// wrote by hand.
	loose chan map[string]any
	// exited is closed when the binary ends.
	exited chan struct{}
	// stderr holds what the binary wrote to its error output.
	stderr *syncBuffer
	// endpoint is the fake endpoint the binary talks to.
	endpoint *provider

	// mu guards the fields below.
	mu sync.Mutex
	// answers are the answers the client has not read yet, by the
	// request's id.
	answers map[string]chan *answer
	// events are the events the binary sent, in order.
	events []event
	// changed is closed and replaced when an event arrives.
	changed chan struct{}
	// capabilities are the capabilities the handshake announced.
	capabilities map[string]json.RawMessage
	// ask answers the server's permission asks. An empty answer means
	// the client does not answer.
	ask func(protocol.AskParams) string
	// next is the id of the next request.
	next int
	// paused reports that the client stopped reading.
	paused bool
	// err is the binary's exit error.
	err error
}

// answer is one answer from the binary.
type answer struct {
	// result is the result of a call that succeeded.
	result json.RawMessage
	// failure is the failure of a call that was refused.
	failure *protocol.Error
	// failureRaw is the failure as it came, so the client can read it
	// under the decode rule.
	failureRaw json.RawMessage
}

// event is one event the binary sent.
//
// The event's own data is not a known shape: the payload is whatever
// the event carries, and the client reads the fields it knows.
type event struct {
	// SessionID identifies the session.
	SessionID string
	// Seq is the event's sequence number.
	Seq int64
	// Type names the event.
	Type string
	// At is the event's timestamp.
	At string
	// Payload is the event's own data.
	Payload map[string]any
}

// start starts the binary and returns the client that drives it.
//
// The settings file is the one the flow wrote, and the extra
// arguments are the ones the flow needs.
func (s *suite) start(settings string, endpoint *provider, extra ...string) *client {
	s.t.Helper()
	args := append([]string{}, s.target.Args...)
	args = append(args, "--settings", settings)
	args = append(args, extra...)

	cmd := exec.Command(s.target.Binary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.t.Fatalf("the binary's input: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.t.Fatalf("the binary's output: %v", err)
	}
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		s.t.Fatalf("starting the binary: %v", err)
	}
	c := &client{
		t:            s.t,
		cmd:          cmd,
		in:           stdin,
		reader:       bufio.NewReader(stdout),
		done:         make(chan struct{}),
		exited:       make(chan struct{}),
		stderr:       stderr,
		endpoint:     endpoint,
		answers:      map[string]chan *answer{},
		changed:      make(chan struct{}),
		lines:        make(chan []byte),
		loose:        make(chan map[string]any, 8),
		capabilities: map[string]json.RawMessage{},
		ask:          func(protocol.AskParams) string { return protocol.DecisionAllow },
	}
	go c.read()
	go c.process()
	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.exited)
	}()
	s.t.Cleanup(c.stop)
	return c
}

// read reads the binary's output until it ends.
//
// The lines go to a channel, so a client that stops reading leaves the
// pipe alone: the binary's write blocks, and the transport's watchdog
// sees the stall.
func (c *client) read() {
	defer close(c.done)
	for {
		line, err := c.reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			c.lines <- line
		}
		if err != nil {
			return
		}
	}
}

// process acts on the lines the binary sent.
//
// A client that stopped reading stops here: the line is dropped, and
// the reader blocks on the next one. The pipe fills up, and the
// binary's write stalls.
func (c *client) process() {
	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				return
			}
			if c.isPaused() {
				return
			}
			c.handle(line)
		case <-c.done:
			return
		}
	}
}

// isPaused reports that the client stopped reading.
func (c *client) isPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

// stopReading stops the client from reading the binary's output.
//
// The pipe stays open, so the binary's write blocks once the pipe is
// full: that is the slow client the transport's bounds are there for.
func (c *client) stopReading() {
	c.t.Helper()
	c.mu.Lock()
	c.paused = true
	c.mu.Unlock()
}

// message is one message from the binary.
type message struct {
	// JSONRPC is the protocol version of the message.
	JSONRPC string `json:"jsonrpc"`
	// ID is the message's id, when it has one.
	ID json.RawMessage `json:"id"`
	// Method is the method the message names, when it names one.
	Method string `json:"method"`
	// Params is the method's params object.
	Params json.RawMessage `json:"params"`
	// Result is the result of a response.
	Result json.RawMessage `json:"result"`
	// Error is the failure of a response.
	Error json.RawMessage `json:"error"`
}

// handle acts on one message from the binary.
func (c *client) handle(line []byte) {
	var msg message
	if err := json.Unmarshal(line, &msg); err != nil {
		c.t.Errorf("the binary sent a line that is not JSON: %q", strings.TrimSpace(string(line)))
		return
	}
	switch {
	case msg.Method != "" && len(msg.ID) > 0:
		// A request from the server: the permission ask.
		c.answerAsk(msg)
	case msg.Method != "":
		// A notification: an event.
		c.record(msg.Params)
	default:
		// A response to one of the client's requests.
		c.deliver(line, msg)
	}
}

// answerAsk answers the server's permission ask.
//
// The client answers what the flow told it to answer, and a flow that
// wants the ask to go unanswered says so.
func (c *client) answerAsk(msg message) {
	var params protocol.AskParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		c.t.Errorf("the ask's params are not what the contract says: %v", err)
		return
	}
	decision := c.ask(params)
	if decision == "" {
		// The client does not answer: the ask runs out of time.
		return
	}
	c.writeRaw(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(msg.ID),
		"result":  map[string]any{"decision": decision},
	})
}

// record files an event the binary sent.
func (c *client) record(raw json.RawMessage) {
	evt, err := decodeEvent(raw)
	if err != nil {
		c.t.Errorf("the event is not what the contract says: %v", err)
		return
	}
	c.mu.Lock()
	c.events = append(c.events, evt)
	changed := c.changed
	c.changed = make(chan struct{})
	c.mu.Unlock()
	close(changed)
}

// deliver hands an answer to the request that is waiting for it.
func (c *client) deliver(line []byte, msg message) {
	key := idKey(msg.ID)
	c.mu.Lock()
	ch, ok := c.answers[key]
	if ok {
		delete(c.answers, key)
	}
	c.mu.Unlock()
	if !ok {
		// The answer is for a request the client wrote by hand.
		var loose map[string]any
		if err := json.Unmarshal(line, &loose); err == nil {
			select {
			case c.loose <- loose:
			default:
			}
		}
		return
	}
	ans := &answer{result: msg.Result}
	if len(msg.Error) > 0 {
		var failure protocol.Error
		if err := json.Unmarshal(msg.Error, &failure); err == nil {
			ans.failure = &failure
			ans.failureRaw = msg.Error
		}
	}
	ch <- ans
}

// writeRaw writes a message to the binary.
func (c *client) writeRaw(msg any) {
	c.t.Helper()
	line, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("encoding the message: %v", err)
	}
	if _, err := c.in.Write(append(line, '\n')); err != nil {
		c.t.Fatalf("writing to the binary: %v", err)
	}
}

// writeLine writes a raw line to the binary.
//
// A flow uses it to send something the contract does not allow, e.g. a
// line that is not JSON.
func (c *client) writeLine(line string) {
	c.t.Helper()
	if _, err := c.in.Write([]byte(line + "\n")); err != nil {
		c.t.Fatalf("writing to the binary: %v", err)
	}
}

// request is an outstanding request.
type request struct {
	// id is the request's id.
	id string
	// ch carries the answer.
	ch chan *answer
	// client is the client that sent the request.
	client *client
}

// send sends a request and returns a handle to wait for its answer.
//
// A flow that races two requests sends them both and waits for the
// answers in the order it cares about.
func (c *client) send(method string, params any) *request {
	c.t.Helper()
	id := c.nextID()
	ch := make(chan *answer, 1)
	c.mu.Lock()
	c.answers[idKey(mustJSON(c.t, id))] = ch
	c.mu.Unlock()
	c.writeRaw(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	return &request{id: id, ch: ch, client: c}
}

// wait waits for the answer to a request.
func (r *request) wait(t *testing.T) *answer {
	t.Helper()
	select {
	case ans := <-r.ch:
		return ans
	case <-time.After(30 * time.Second):
		t.Fatalf("the binary did not answer the request %s; it wrote: %s", r.id, r.client.stderrText())
		return nil
	}
}

// call sends a request and returns its result.
//
// The result is read under the plan's forward-compatibility rule: a
// field the shape does not have is a failure, unless an announced
// capability covers it.
func (c *client) call(method string, params any, shape any) json.RawMessage {
	c.t.Helper()
	pending := c.send(method, params)
	ans := pending.wait(c.t)
	if ans.failure != nil {
		c.t.Fatalf("%s was refused: %s (%s)", method, ans.failure.Message, dataCode(ans.failure))
	}
	if shape != nil {
		if method == protocol.MethodInitialize {
			// The handshake's result carries the capabilities, and the
			// capabilities say which fields the result may hold: they
			// are read before the result is decoded.
			c.announce(ans.result)
		}
		c.decode(ans.result, shape)
	}
	return ans.result
}

// refuse sends a request and returns the failure it answers with.
func (c *client) refuse(method string, params any) *protocol.Error {
	c.t.Helper()
	pending := c.send(method, params)
	ans := pending.wait(c.t)
	if ans.failure == nil {
		c.t.Fatalf("%s was not refused: %s", method, string(ans.result))
	}
	c.decodeFailure(ans.failureRaw)
	return ans.failure
}

// nextID returns the id of the next request.
func (c *client) nextID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	return fmt.Sprintf("c%d", c.next)
}

// closeInput closes the binary's input, which is the client's way of
// hanging up.
func (c *client) closeInput() {
	_ = c.in.Close()
}

// kill ends the binary the way a crash does.
func (c *client) kill() {
	c.t.Helper()
	if c.cmd.Process == nil {
		return
	}
	_ = c.cmd.Process.Kill()
	<-c.exited
}

// exitError returns the binary's exit error.
func (c *client) exitError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// waitExit waits for the binary to end and returns its exit code.
//
// A binary that does not end in time is a failure: the suite leaves no
// process behind.
func (c *client) waitExit(timeout time.Duration) int {
	c.t.Helper()
	select {
	case <-c.exited:
		return exitCode(c.exitError())
	case <-time.After(timeout):
		c.t.Fatalf("the binary did not end within %s; it wrote: %s", timeout, c.stderrText())
		return -1
	}
}

// stop ends the client: the binary is killed if it is still running.
func (c *client) stop() {
	c.closeInput()
	select {
	case <-c.exited:
		return
	case <-time.After(5 * time.Second):
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	<-c.exited
}

// stderrText returns what the binary wrote to its error output.
func (c *client) stderrText() string {
	return c.stderr.String()
}

// nextAnswer reads the next answer the binary sends.
//
// A flow uses it when it wrote a request by hand: the answer has no
// request of its own to wait for.
func (c *client) nextAnswer(t *testing.T) map[string]any {
	t.Helper()
	select {
	case answer := <-c.loose:
		return answer
	case <-time.After(30 * time.Second):
		t.Fatalf("the binary sent no answer; it wrote: %s", c.stderrText())
		return nil
	}
}

// requests returns the bodies the fake endpoint received.
func (c *client) requests() []map[string]any {
	if c.endpoint == nil {
		return nil
	}
	return c.endpoint.requestBodies()
}

// events returns the events the binary sent.
func (c *client) eventList() []event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]event, 0, len(c.events))
	out = append(out, c.events...)
	return out
}

// waitEvent waits for an event that matches the predicate.
//
// The events that arrived before the wait are looked at first, so a
// flow that waits for an event the binary has already sent does not
// wait for another one.
func (c *client) waitEvent(what string, match func(event) bool) event {
	c.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		c.mu.Lock()
		for _, evt := range c.events {
			if match(evt) {
				c.mu.Unlock()
				return evt
			}
		}
		changed := c.changed
		c.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			c.t.Fatalf("the binary sent no %s; it wrote: %s", what, c.stderrText())
			return event{}
		}
		timer := time.NewTimer(remaining)
		select {
		case <-changed:
			timer.Stop()
		case <-timer.C:
			c.t.Fatalf("the binary sent no %s; it wrote: %s", what, c.stderrText())
			return event{}
		}
	}
}

// syncBuffer is a buffer several goroutines may write to.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// Write appends to the buffer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

// String returns what the buffer holds.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// idKey returns the key an id is filed under.
func idKey(id json.RawMessage) string {
	var text string
	if err := json.Unmarshal(id, &text); err == nil {
		return "s:" + text
	}
	return "r:" + string(id)
}

// mustJSON encodes a value, and fails the test when it cannot.
func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding %v: %v", value, err)
	}
	return encoded
}

// dataCode returns the code a failure carries.
func dataCode(failure *protocol.Error) string {
	if failure.Data == nil {
		return ""
	}
	return failure.Data.Code
}

// exitCode returns the code a process ended with.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}
