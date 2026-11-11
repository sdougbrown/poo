package repl

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// wireHarness is a conn wired to pipes a test drives: feed writes the
// server's lines, taken reads the client's.
type wireHarness struct {
	// stdout is the reader the conn reads.
	stdout *io.PipeReader
	// stdin is the writer the conn writes.
	stdin io.Writer

	feed  func(line string)
	taken func() string
	// drain unblocks the conn's writes when no test reads them: a
	// dead server's pipe errors instead of blocking.
	drain func()
}

// newWireHarness wires the pipes and returns the ends the test holds.
func newWireHarness(t *testing.T) *wireHarness {
	t.Helper()
	// The server's output: the test feeds lines in, the conn reads.
	stdoutRead, stdoutWrite := io.Pipe()
	// The server's input: the conn writes, the test takes lines out.
	stdinRead, stdinWrite := io.Pipe()
	t.Cleanup(func() {
		stdoutWrite.Close()
		stdinRead.Close()
	})
	reader := bufio.NewReader(stdinRead)
	return &wireHarness{
		stdout: stdoutRead,
		stdin:  stdinWrite,
		feed: func(line string) {
			stdoutWrite.Write([]byte(line + "\n"))
		},
		drain: func() {
			_, _ = io.Copy(io.Discard, stdinRead)
		},
		taken: func() string {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("reading the conn's request: %v", err)
			}
			return line
		},
	}
}

// TestTheDemuxSendsEachKindWhereItBelongs covers the reader: an
// answer reaches the call, an event reaches the event channel, and a
// permission ask reaches the ask channel with the id it came with.
func TestTheDemuxSendsEachKindWhereItBelongs(t *testing.T) {
	h := newWireHarness(t)
	conn := startConn(h.stdout, h.stdin)

	go func() {
		// The feed waits for the request to be on the wire first: a
		// response that outruns the call has no channel to land in.
		h.taken()
		h.feed(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
		h.feed(`{"jsonrpc":"2.0","method":"event","params":{"session_id":"s1","seq":1,"type":"tool.call","tool":"bash"}}`)
		h.feed(`{"jsonrpc":"2.0","id":"a1","method":"permission/ask","params":{"session_id":"s1","tool":"bash"}}`)
	}()

	if err := conn.call("session/create", map[string]any{}, nil); err != nil {
		t.Fatalf("the call failed: %v", err)
	}
	evt := <-conn.events
	if evt.Type != "tool.call" || evt.Payload["tool"] != "bash" {
		t.Errorf("the event read back as %+v", evt)
	}
	ask := <-conn.asks
	if ask.ask.Tool != "bash" || string(ask.id) != `"a1"` {
		t.Errorf("the ask read back as %+v", ask)
	}
}

// TestARequestHoldsTheMethodAndTheParams covers what the client puts
// on the wire: a request with an id, the method and the params.
func TestARequestHoldsTheMethodAndTheParams(t *testing.T) {
	h := newWireHarness(t)
	conn := startConn(h.stdout, h.stdin)

	// The pipe is synchronous: the request is read as it is written,
	// and the answer goes back while the call waits.
	linec := make(chan string, 1)
	go func() {
		linec <- h.taken()
		h.feed(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	}()
	if err := conn.call("run/prompt", protocol.PromptParams{SessionID: "s1"}, nil); err != nil {
		t.Fatalf("the call failed: %v", err)
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  struct {
			SessionID string `json:"session_id"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(<-linec)), &req); err != nil {
		t.Fatalf("the request did not decode: %v", err)
	}
	if req.JSONRPC != "2.0" || req.Method != "run/prompt" || req.Params.SessionID != "s1" {
		t.Errorf("the request read back as %+v", req)
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		t.Errorf("the request carries no id: %s", req.ID)
	}
}

// TestACallCarriesTheRefusalItGot covers a call the server refused:
// the failure names the method and the server's message.
func TestACallCarriesTheRefusalItGot(t *testing.T) {
	h := newWireHarness(t)
	conn := startConn(h.stdout, h.stdin)

	go func() {
		h.taken()
		h.feed(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"the session is busy"}}`)
	}() // The pipe is synchronous, so the taken read unblocks the write.

	err := conn.call("run/prompt", map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("the refusal read back as %v", err)
	}
}

// TestAConnFailsWhenTheServerLeaves covers the reader's end: a server
// that closed its output fails every call that is still waiting.
func TestAConnFailsWhenTheServerLeaves(t *testing.T) {
	h := newWireHarness(t)
	conn := startConn(h.stdout, h.stdin)
	h.stdout.Close()
	go h.drain()

	if err := conn.call("run/prompt", map[string]any{}, nil); err == nil {
		t.Error("the call succeeded after the server left")
	}
}

// TestAnAskTheClientCannotTakeIsDenied covers the ask's failure mode: a
// client that is not taking asks answers the server with a denial,
// because the server blocks on the answer and a dropped ask would hang
// the session.
func TestAnAskTheClientCannotTakeIsDenied(t *testing.T) {
	h := newWireHarness(t)
	conn := startConn(h.stdout, h.stdin)
	// The harness takes lines the conn writes; the ask feed fills the
	// channel past its buffer, and the overflow is answered denied.
	go func() {
		for i := 0; i < 9; i++ {
			h.feed(`{"jsonrpc":"2.0","id":"a` + string(rune('0'+i)) + `","method":"permission/ask","params":{"session_id":"s1","tool":"bash"}}`)
		}
	}()

	// The channel holds eight; the ninth overflows and is denied.
	line := strings.TrimSpace(h.taken())
	if !strings.Contains(line, "deny") || !strings.Contains(line, `"id":"a8"`) {
		t.Errorf("the overflowed ask was not denied: %s", line)
	}
	// The first eight are still waiting to be read.
	ask := <-conn.asks
	if ask.ask.Tool != "bash" || string(ask.id) != `"a0"` {
		t.Errorf("the first ask read back as %+v", ask)
	}
}

// TestTheReaderClosesTheEndsOfTheWire covers a server that goes away
// while a run is streaming: the event and ask channels close, so the run
// waiting on them returns instead of waiting for what will never come.
func TestTheReaderClosesTheEndsOfTheWire(t *testing.T) {
	h := newWireHarness(t)
	conn := startConn(h.stdout, h.stdin)
	h.stdout.Close()
	go h.drain()

	select {
	case _, ok := <-conn.events:
		if ok {
			t.Error("the event channel carried an event after the server left")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event channel stayed open after the server left")
	}
	select {
	case _, ok := <-conn.asks:
		if ok {
			t.Error("the ask channel carried a request after the server left")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the ask channel stayed open after the server left")
	}
}
