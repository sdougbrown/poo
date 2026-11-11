package repl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// callTimeout is how long a call waits for the server's answer.
//
// The calls a session answers immediately come back in milliseconds;
// the timeout is for a slow machine, not a slow model: a run streams
// its events while the prompt's answer has been in for a while.
const callTimeout = 2 * time.Minute

// conn is the client's end of the wire: a JSON-RPC 2.0 peer over the
// server's pipes.
//
// The reader goroutine reads one object per line and demuxes it: an
// answer goes to the call that waits for it, an event notification to
// the event channel, and a permission ask to the ask channel.
type conn struct {
	// mu guards the writer and the pending map.
	mu sync.Mutex
	// w writes to the server's input.
	w io.Writer
	// next is the id of the client's next request.
	next int
	// pending holds the channel each in-flight request waits on, by
	// its id.
	pending map[int]chan rpcResult

	// events carries the session's events, in arrival order.
	events chan protocol.Event
	// asks carries the server's permission asks, with the raw request
	// id the answer echoes.
	asks chan askRequest
	// done is closed when the reader returns.
	done chan struct{}
}

// askRequest is one permission ask from the server.
//
// The id is the raw request id: the answer echoes it, and the client
// keeps no mapping of its own.
type askRequest struct {
	id  json.RawMessage
	ask protocol.AskParams
}

// rpcResult is one answer from the server.
type rpcResult struct {
	// raw is the result, for a call that succeeded.
	raw json.RawMessage
	// err is the failure, for a call the server refused.
	err *protocol.Error
}

// startConn runs the reader over the server's output. The caller
// writes requests through the returned connection.
func startConn(stdout io.Reader, stdin io.Writer) *conn {
	c := &conn{
		w:       stdin,
		pending: map[int]chan rpcResult{},
		events:  make(chan protocol.Event, 256),
		asks:    make(chan askRequest, 8),
		done:    make(chan struct{}),
	}
	go c.read(stdout)
	return c
}

// read reads the server's lines and demuxes them.
func (c *conn) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		c.line(scanner.Bytes())
	}
	c.mu.Lock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = map[int]chan rpcResult{}
	c.mu.Unlock()
	close(c.done)
}

// line demuxes one line from the server.
func (c *conn) line(line []byte) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *protocol.Error `json:"error"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return
	}
	// An id the server sent is present when it is neither absent nor
	// null: JSON-RPC's notification shape.
	hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
	switch {
	case msg.Method == protocol.MethodEvent && !hasID:
		evt, err := unmarshalEvent(msg.Params)
		if err != nil {
			return
		}
		select {
		case c.events <- evt:
		default:
			// The client stopped reading: the drop is the same
			// overflow the server's queue would report.
		}
	case msg.Method == protocol.MethodPermissionAsk && hasID:
		var ask protocol.AskParams
		if err := json.Unmarshal(msg.Params, &ask); err != nil {
			return
		}
		select {
		case c.asks <- askRequest{id: msg.ID, ask: ask}:
		default:
		}
	case hasID:
		var id int
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- rpcResult{raw: msg.Result, err: msg.Error}
		}
	}
}

// call sends a request and waits for its answer.
//
// The result, when the caller gave one, is decoded from the answer.
func (c *conn) call(method string, params, result any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("repl: encoding %s: %w", method, err)
	}
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	rawID, _ := json.Marshal(id)
	payload, err := json.Marshal(protocol.Request{
		JSONRPC: "2.0",
		ID:      rawID,
		Method:  method,
		Params:  rawParams,
	})
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("repl: encoding %s: %w", method, err)
	}
	if _, err := c.w.Write(append(payload, '\n')); err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("repl: writing %s: %w", method, err)
	}
	c.mu.Unlock()

	select {
	case r, ok := <-ch:
		if !ok {
			return fmt.Errorf("repl: %s: the server stopped answering", method)
		}
		if r.err != nil {
			return fmt.Errorf("repl: %s: %s", method, r.err.Message)
		}
		if result != nil && len(r.raw) > 0 {
			if err := json.Unmarshal(r.raw, result); err != nil {
				return fmt.Errorf("repl: %s: %w", method, err)
			}
		}
		return nil
	case <-time.After(callTimeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("repl: %s: timed out", method)
	case <-c.done:
		return fmt.Errorf("repl: %s: the server closed the connection", method)
	}
}

// answer replies to the server's permission ask.
func (c *conn) answer(req askRequest, decision protocol.AskResult) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      req.id,
		"result":  decision,
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(append(payload, '\n'))
	return err
}

// unmarshalEvent reads an event notification's params.
//
// The event's own data is spread into the object, so the fields the
// client knows are read out and the rest is the payload.
func unmarshalEvent(raw json.RawMessage) (protocol.Event, error) {
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(raw, &flat); err != nil {
		return protocol.Event{}, err
	}
	var evt protocol.Event
	read := func(key string, dst any) {
		if v, ok := flat[key]; ok {
			_ = json.Unmarshal(v, dst)
			delete(flat, key)
		}
	}
	read("session_id", &evt.SessionID)
	read("seq", &evt.Seq)
	read("type", &evt.Type)
	read("at", &evt.At)
	evt.Payload = map[string]any{}
	for key, v := range flat {
		var value any
		if err := json.Unmarshal(v, &value); err == nil {
			evt.Payload[key] = value
		}
	}
	return evt, nil
}
