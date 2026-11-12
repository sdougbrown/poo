package repl

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// fakeServer is a poo server for tests: it speaks the protocol over
// pipes and runs one scripted run. The decisions it received, and the
// requests it answered, are fields the test reads back.
type fakeServer struct {
	// t is the test the server runs in.
	t *testing.T

	// mu guards the fields below.
	mu sync.Mutex
	// decisions are the answers the client sent, in order.
	decisions []string
	// prompts are the prompts the client sent.
	prompts []string
	// switches are the model names the client configured, in order.
	switches []string
	// closed says the client closed the session.
	closed bool
	// cancels are the run ids the client's cancels named, in order.
	cancels []string
	// refusePrompt makes run/prompt refuse, for the refusal tests.
	refusePrompt bool
	askSummary   string
	// holdRun keeps the run open until the client cancels it, so a
	// test has a run to stop.
	holdRun bool
	// staleSettle sends a settle for a run that is not the session's
	// before the real one, so a test can watch the guard that ignores
	// it. staleSettled closes once it has gone out.
	staleSettle  bool
	staleSettled chan struct{}
	// askWhen holds the ask back until the test closes it, so the
	// test can type ahead of the ask.
	askWhen chan struct{}
	// warnings are the session warnings create reports.
	warnings []string

	in *bufio.Reader
	// w writes to the client.
	w io.Writer
	// writeMu guards w.
	writeMu sync.Mutex
}

// pipeUp starts a fake server over a pipe pair and returns the ends
// the client holds.
func pipeUp(t *testing.T, server *fakeServer) (stdin io.WriteCloser, stdout io.ReadCloser) {
	t.Helper()
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	server.in = bufio.NewReader(serverRead)
	server.w = serverWrite
	go server.serve()
	t.Cleanup(func() {
		serverWrite.Close()
		serverRead.Close()
	})
	return clientWrite, clientRead
}

// serve reads the client's lines until the pipe closes.
func (s *fakeServer) serve() {
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return
		}
		s.line([]byte(strings.TrimSpace(line)))
	}
}

// line answers one client message.
func (s *fakeServer) line(line []byte) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}
	// A message with an id and no method is the client's answer to
	// the ask the server sent.
	if req.Method == "" && len(req.ID) > 0 {
		s.answer(line)
		return
	}
	switch req.Method {
	case protocol.MethodInitialize:
		s.reply(req.ID, map[string]any{
			"protocol_version": 1,
			"build":            map[string]any{"name": "poo-test"},
			"profiles":         []any{},
			"models":           map[string]any{},
			"capabilities":     map[string]any{},
		})
	case protocol.MethodSessionCreate:
		s.mu.Lock()
		s.closed = false
		s.mu.Unlock()
		response := map[string]any{
			"session_id": "s1",
			"mode":       "logged",
			"tools":      []any{},
			"models":     map[string]any{"chat": "gemma4"},
		}
		if len(s.warnings) > 0 {
			response["warnings"] = s.warnings
		}
		s.reply(req.ID, response)
	case protocol.MethodModelsList:
		s.reply(req.ID, map[string]any{
			"models": []map[string]any{
				{"name": "gemma4", "model_id": "gemma4", "context_limit": 262144},
				{"name": "qwen", "model_id": "qwen", "context_limit": 131072},
			},
		})
	case protocol.MethodSessionConfigure:
		var params protocol.ConfigureParams
		json.Unmarshal(req.Params, &params)
		// hidden is a model the server accepts but the catalog above does
		// not list: a switch to it succeeds with no entry to read.
		if params.Models == nil || (params.Models.Chat != "gemma4" && params.Models.Chat != "qwen" && params.Models.Chat != "hidden") {
			s.fail(req.ID, "unknown_model", "poo: the session's model is unknown")
			return
		}
		s.mu.Lock()
		s.switches = append(s.switches, params.Models.Chat)
		s.mu.Unlock()
		s.reply(req.ID, map[string]any{
			"models": map[string]any{"chat": params.Models.Chat},
		})
	case protocol.MethodRunPrompt:
		var params protocol.PromptParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		s.prompts = append(s.prompts, params.Text)
		s.mu.Unlock()
		if s.refusePrompt {
			s.fail(req.ID, "busy", "the session is busy")
			return
		}
		s.reply(req.ID, map[string]any{"run_id": "r1"})
		go s.run()
	case protocol.MethodRunCancel:
		var params protocol.CancelParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		s.cancels = append(s.cancels, params.RunID)
		s.mu.Unlock()
		s.reply(req.ID, map[string]any{})
	case protocol.MethodSessionClose:
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.reply(req.ID, map[string]any{})
	default:
		s.fail(req.ID, "no_method", "there is no method "+req.Method)
	}
}

// run plays the scripted run: a reply that streams, a tool call whose
// permission the client must answer, and the settle.
func (s *fakeServer) run() {
	s.event("turn.started", map[string]any{"run_id": "r1", "turn": 1})
	s.event("message.delta", map[string]any{
		"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Hello, ",
	})
	s.event("message.delta", map[string]any{
		"run_id": "r1", "message_id": "m1", "kind": "thinking", "text": "hmm",
	})
	s.event("message.delta", map[string]any{
		"run_id": "r1", "message_id": "m1", "kind": "text", "text": "world.",
	})
	s.event("turn.ended", map[string]any{"run_id": "r1", "turn": 1})
	s.event("turn.started", map[string]any{"run_id": "r1", "turn": 2})
	if s.staleSettle {
		s.event("run.settled", map[string]any{"run_id": "r0", "reason": "cancelled", "turns": 1})
		if s.staleSettled != nil {
			close(s.staleSettled)
		}
	}
	if s.holdRun {
		// The run waits for a cancel of its own: an earlier run's
		// cancel is not an answer.
		s.waitCancel(s.cancelCount() + 1)
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "cancelled", "turns": 1})
		return
	}
	s.event("tool.call", map[string]any{
		"run_id": "r1", "handle": "h1", "tool": "bash", "summary": "ls -la",
	})
	// The ask goes out as a request, and the run waits for it.
	if s.askWhen != nil {
		select {
		case <-s.askWhen:
		case <-time.After(10 * time.Second):
			return
		}
	}
	summary := s.askSummary
	if summary == "" {
		summary = "ls -la"
	}
	askID := s.request(protocol.MethodPermissionAsk, protocol.AskParams{
		SessionID: "s1",
		RunID:     "r1",
		AskID:     "a1",
		Tool:      "bash",
		Summary:   summary,
		Class:     "side_effecting",
		Reason:    "policy",
	})
	s.waitDecision(askID)
	s.event("tool.result", map[string]any{
		"run_id": "r1", "handle": "h1", "tool": "bash", "outcome": "ok",
		"text": "file one\nfile two\n",
	})
	s.event("turn.ended", map[string]any{"run_id": "r1", "turn": 2})
	s.event("run.settled", map[string]any{
		"run_id": "r1", "reason": "end_turn", "turns": 2,
		"detail": map[string]any{
			"usage": map[string]any{"input_tokens": 12300, "output_tokens": 1100},
		},
	})
}

// cancelCount is how many cancels the client has sent.
func (s *fakeServer) cancelCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cancels)
}

// waitCancel waits for want cancels. A run nobody cancels ends the wait
// quietly: the test that wanted it fails on its own assertions, and a
// late t.Error would land after the test.
func (s *fakeServer) waitCancel(want int) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s.cancelCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitDecision blocks until the client answers the ask, or gives up
// after a wait no test could need: a goroutine that spins forever is a
// leaked one, and the test that wanted the decision fails on its own
// assertions anyway.
func (s *fakeServer) waitDecision(askID json.RawMessage) {
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			s.t.Error("the ask waited 10s with no decision")
			return
		default:
		}
		s.mu.Lock()
		n := len(s.decisions)
		s.mu.Unlock()
		if n > 0 {
			return
		}
		// The client writes the answer on its own; a short sleep
		// keeps the wait from spinning.
		time.Sleep(5 * time.Millisecond)
	}
}

// reply writes one response.
func (s *fakeServer) reply(id json.RawMessage, result any) {
	payload, _ := json.Marshal(result)
	s.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  json.RawMessage(payload),
	})
}

// fail writes one refusal.
func (s *fakeServer) fail(id json.RawMessage, code, message string) {
	s.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": -32000, "message": message, "data": map[string]any{"code": code}},
	})
}

// request writes one request to the client and returns its id.
func (s *fakeServer) request(method string, params any) json.RawMessage {
	payload, _ := json.Marshal(params)
	s.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      "a1",
		"method":  method,
		"params":  json.RawMessage(payload),
	})
	return json.RawMessage(`"a1"`)
}

// event writes one event notification.
func (s *fakeServer) event(name string, payload map[string]any) {
	payload["type"] = name
	payload["session_id"] = "s1"
	s.write(map[string]any{
		"jsonrpc": "2.0",
		"method":  protocol.MethodEvent,
		"params":  payload,
	})
}

// write writes one line to the client.
func (s *fakeServer) write(msg any) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.w.Write(append(payload, '\n'))
}

// recordDecision records an answer the client sent. The line reader
// hands the answer here, because an answer is a response: it has an
// id and no method.
func (s *fakeServer) answer(line []byte) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Result struct {
			Decision string `json:"decision"`
		} `json:"result"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decisions = append(s.decisions, msg.Result.Decision)
}
