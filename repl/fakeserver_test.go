package repl

import (
	"bufio"
	"encoding/json"
	"io"
	"slices"
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
	// levels are the reasoning levels the client configured, in order.
	levels []string
	// closed says the client closed the session.
	closed bool
	// methods are the request methods the client sent, in order.
	methods []string
	// history is what session/history pages, oldest first.
	history []protocol.HistoryItem
	// historyPageSize is the page size session/history answers with,
	// ignoring the limit the request named: the server does not honor
	// the request. Zero honors it.
	historyPageSize int
	// recoveredTail and interrupted are what session/load reports it
	// recovered.
	recoveredTail bool
	interrupted   []string
	// cancels are the run ids the client's cancels named, in order.
	cancels []string
	// refusePrompt makes run/prompt refuse, for the refusal tests.
	refusePrompt bool
	// refuseHistory makes session/history refuse, for the refusal tests.
	refuseHistory bool
	// refuseLoad makes session/load refuse, for the refusal tests.
	refuseLoad bool
	// messages are the run/message texts the client sent, in order.
	messages []string
	// refuseMessage makes run/message refuse, for the refusal tests.
	refuseMessage bool
	// askSummary is the ask's summary the last tool.call named.
	askSummary string
	// changed is the server's signal: each record a wait reads — a
	// prompt, a message, a cancel, a decision — closes it when it
	// lands, so the wait ends on the record instead of a poll. Nil
	// until a wait asks for it.
	changedCh chan struct{}
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
	// chat is the model the session talks to.
	chat string
	// level is the reasoning level the session runs at.
	level string
	// models is the catalog the server lists. A test sets it before
	// pipeUp to put more names in front of the client; a nil catalog is
	// the shared one, fakeModels.
	models []fakeModel
	// replyAs answers a switch with this model name instead of the one
	// asked for, the way a server that normalises or aliases a name
	// would: the client's catalog may hold no entry for the answer.
	replyAs string
	// script is the run the server plays for a prompt. A test sets it
	// before pipeUp; a nil script plays run, the script the shared
	// cases read.
	script func(*fakeServer)

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
	if server.chat == "" {
		server.chat = "gemma4"
	}
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
	s.mu.Lock()
	s.methods = append(s.methods, req.Method)
	s.mu.Unlock()
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
		chat := s.chat
		s.mu.Unlock()
		response := map[string]any{
			"session_id": "s1",
			"mode":       "logged",
			"tools":      []any{},
			"models":     map[string]any{"chat": chat},
		}
		if len(s.warnings) > 0 {
			response["warnings"] = s.warnings
		}
		s.reply(req.ID, response)
	case protocol.MethodSessionLoad:
		if s.refuseLoad {
			s.fail(req.ID, "no_load", "the session's load is not readable")
			return
		}
		var params protocol.LoadParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		s.closed = false
		response := map[string]any{
			"session_id": params.SessionID,
			"mode":       "logged",
			"tools":      []any{},
			"models":     map[string]any{"chat": s.chat},
		}
		if len(s.warnings) > 0 {
			response["warnings"] = s.warnings
		}
		if s.recoveredTail {
			response["recovered_tail"] = true
		}
		if len(s.interrupted) > 0 {
			response["interrupted_runs"] = s.interrupted
		}
		s.mu.Unlock()
		s.reply(req.ID, response)
	case protocol.MethodSessionHistory:
		if s.refuseHistory {
			s.fail(req.ID, "no_history", "the session's history is not readable")
			return
		}
		var params protocol.HistoryParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		known := append([]protocol.HistoryItem(nil), s.history...)
		pageSize := s.historyPageSize
		s.mu.Unlock()
		limit := params.Limit
		if pageSize > 0 {
			limit = pageSize
		}
		if limit <= 0 {
			limit = 100
		}
		var page []protocol.HistoryItem
		for _, item := range known {
			if params.BeforeSeq > 0 && item.Seq >= params.BeforeSeq {
				continue
			}
			page = append(page, item)
		}
		hasMore := len(page) > limit
		if hasMore {
			page = page[len(page)-limit:]
		}
		s.reply(req.ID, map[string]any{"items": page, "has_more": hasMore})
	case protocol.MethodSessionGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reply(req.ID, map[string]any{
			"state":     "idle",
			"jobs":      []any{},
			"prompt":    map[string]any{},
			"reasoning": s.level,
			"broken":    false,
		})
	case protocol.MethodModelsList:
		models := make([]map[string]any, 0, len(s.catalog()))
		for _, model := range s.catalog() {
			models = append(models, map[string]any{
				"name":             model.name,
				"model_id":         model.modelID,
				"context_limit":    model.window,
				"input_limit":      model.input,
				"reasoning_levels": model.levels,
				"reasoning_mode":   model.mode,
			})
		}
		s.reply(req.ID, map[string]any{"models": models})
	case protocol.MethodSessionConfigure:
		var params protocol.ConfigureParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		defer s.mu.Unlock()
		if params.Models != nil && params.Models.Chat != "" {
			if _, ok := s.modelLevels(params.Models.Chat); !ok {
				s.fail(req.ID, "unknown_model", "poo: the session's model is unknown")
				return
			}
			s.chat = params.Models.Chat
			s.switches = append(s.switches, params.Models.Chat)
			// The server the repl talks to takes the level back to one the
			// new model can run: off when the model declares it, its lowest
			// declared level when it cannot be told off.
			levels, _ := s.modelLevels(s.chat)
			if !s.acceptsLocked(s.level) {
				s.level = "off"
				if len(levels) > 0 && !slices.Contains(levels, "off") {
					s.level = levels[0]
				}
			}
		}
		if params.Reasoning != "" {
			// The session lowercases the level, and the result says what it
			// runs at, so a client that typed High reads high back.
			level := strings.ToLower(strings.TrimSpace(params.Reasoning))
			if !s.acceptsLocked(level) {
				levels, _ := s.modelLevels(s.chat)
				s.fail(req.ID, "invalid_params", "the model \""+s.chat+"\" takes the levels "+strings.Join(levels, ", "))
				return
			}
			s.level = level
			s.levels = append(s.levels, level)
		}
		chat := s.chat
		if s.replyAs != "" {
			chat = s.replyAs
		}
		s.reply(req.ID, map[string]any{
			"models":    map[string]any{"chat": chat},
			"reasoning": s.level,
		})
	case protocol.MethodRunPrompt:
		var params protocol.PromptParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		s.prompts = append(s.prompts, params.Text)
		s.signal()
		s.mu.Unlock()
		if s.refusePrompt {
			s.fail(req.ID, "busy", "the session is busy")
			return
		}
		// The run the server plays is the test's own when it named one,
		// and the shared script otherwise. It reads script before the
		// goroutine starts, which the test's write precedes.
		script := s.script
		if script == nil {
			script = (*fakeServer).run
		}
		s.reply(req.ID, map[string]any{"run_id": "r1"})
		go script(s)
	case protocol.MethodRunMessage:
		var params protocol.MessageParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		s.messages = append(s.messages, params.Text)
		refuse := s.refuseMessage
		s.signal()
		s.mu.Unlock()
		if refuse {
			s.fail(req.ID, "not_running", "the session is not running")
			return
		}
		s.reply(req.ID, map[string]any{"input_id": "i1"})
	case protocol.MethodRunCancel:
		var params protocol.CancelParams
		json.Unmarshal(req.Params, &params)
		s.mu.Lock()
		s.cancels = append(s.cancels, params.RunID)
		s.signal()
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

// fakeModel is one entry of the catalog the fake server lists.
type fakeModel struct {
	// name is the name the client switches by.
	name string
	// modelID is the name the provider answers to.
	modelID string
	// window is the model's context limit in tokens.
	window int
	// input is the largest prompt the model's endpoint takes, in tokens.
	// Zero says the endpoint takes the whole window.
	input int
	// levels are the reasoning levels the model takes, lowest first.
	levels []string
	// mode is how the model's reasoning is controlled, empty when the
	// model takes named levels.
	mode string
}

// fakeModels is the catalog the shared cases read: gemma4 takes three
// levels, and qwen takes two it does not share, so a switch is where the
// two disagree.
var fakeModels = []fakeModel{
	{name: "gemma4", modelID: "gemma4", window: 262144, levels: []string{"off", "low", "high"}},
	{name: "qwen", modelID: "qwen", window: 131072, levels: []string{"off", "medium"}},
	{name: "mystery", modelID: "mystery", window: 8192, levels: []string{"low", "high"}},
}

// catalog returns the models the server lists.
func (s *fakeServer) catalog() []fakeModel {
	if s.models != nil {
		return s.models
	}
	return fakeModels
}

// modelLevels returns the levels the named model takes.
//
// The caller holds the server's lock, or is the only goroutine reading the
// catalog, which a test is before pipeUp.
func (s *fakeServer) modelLevels(name string) ([]string, bool) {
	for _, model := range s.catalog() {
		if model.name == name {
			return model.levels, true
		}
	}
	return nil, false
}

// acceptsLocked reports whether the session's model takes a level.
//
// The caller holds the server's lock.
func (s *fakeServer) acceptsLocked(level string) bool {
	if level == "" {
		return true
	}
	levels, _ := s.modelLevels(s.chat)
	for _, accepted := range levels {
		if accepted == level {
			return true
		}
	}
	return false
}

// run plays the scripted run: a reply that streams, a tool call whose
// permission the client must answer, and the settle.
func (s *fakeServer) run() {
	// The cancel the hold waits for is one that arrives after the run
	// started, not one the count already answered: under load the
	// Escape's cancel can land while the run's own deltas are still
	// going out, and a hold that wants count+1 from the hold point
	// waits for a second cancel nobody sends.
	wantCancels := s.cancelCount() + 1
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
	// The first request's accounting, the way the wire reports it: a
	// usage event per request, and the settle carries the run's sum.
	s.event("usage", map[string]any{
		"run_id": "r1", "turn": 1, "request": 1,
		"input_tokens": 8000, "output_tokens": 500,
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
		s.waitCancel(wantCancels)
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
	// The second request's accounting, and the settle's run total: the
	// sum of both requests. A statusline that read the sum for the
	// snapshot would end the run at 20.3k when the last request's
	// window held 12.3k.
	s.event("usage", map[string]any{
		"run_id": "r1", "turn": 2, "request": 2,
		"input_tokens": 12300, "output_tokens": 600,
	})
	s.event("run.settled", map[string]any{
		"run_id": "r1", "reason": "end_turn", "turns": 2,
		"detail": map[string]any{
			"usage": map[string]any{"input_tokens": 20300, "output_tokens": 1100},
		},
	})
}

// quietRun plays a run with no permission ask: a reply that streams, a
// tool call and its result, the two requests' accounting, and a settle
// the test can hold back. The hold keeps the run working, so a line
// the test types lands while the run is still active. The tool's
// result is what a non-streaming test waits for: it renders at once,
// where the reply waits for the flush the settle brings.
func quietRun(hold chan struct{}) func(*fakeServer) {
	return func(s *fakeServer) {
		s.event("turn.started", map[string]any{"run_id": "r1", "turn": 1})
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Hello, world.",
		})
		s.event("tool.call", map[string]any{
			"run_id": "r1", "handle": "h1", "tool": "grep", "summary": "needle",
		})
		s.event("tool.result", map[string]any{
			"run_id": "r1", "handle": "h1", "tool": "grep", "outcome": "ok",
			"text": "a find\n",
		})
		s.event("usage", map[string]any{
			"run_id": "r1", "turn": 1, "request": 1,
			"input_tokens": 8000, "output_tokens": 500,
		})
		s.event("turn.ended", map[string]any{"run_id": "r1", "turn": 1})
		s.event("turn.started", map[string]any{"run_id": "r1", "turn": 2})
		if hold != nil {
			select {
			case <-hold:
			case <-time.After(10 * time.Second):
				return
			}
		}
		s.event("usage", map[string]any{
			"run_id": "r1", "turn": 2, "request": 2,
			"input_tokens": 12300, "output_tokens": 600,
		})
		s.event("run.settled", map[string]any{
			"run_id": "r1", "reason": "end_turn", "turns": 2,
			"detail": map[string]any{
				"usage": map[string]any{"input_tokens": 20300, "output_tokens": 1100},
			},
		})
	}
}

// changed returns the server's signal: the next record closes it. A
// wait takes the channel, then checks, then blocks: any record after
// the check closes the channel the wait holds, so no record is
// missed.
func (s *fakeServer) changed() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changedCh == nil {
		s.changedCh = make(chan struct{})
	}
	return s.changedCh
}

// signal closes the server's signal. The caller holds mu.
func (s *fakeServer) signal() {
	if s.changedCh != nil {
		close(s.changedCh)
		s.changedCh = nil
	}
}

// cancelCount is how many cancels the client has sent.
func (s *fakeServer) cancelCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cancels)
}

// waitCancel waits for want cancels. A run nobody cancels ends the wait
// quietly: the test that wanted it fails on its own assertions, and a
// late t.Error would land after the test. The wait blocks on the
// server's own signal, so the wait ends on the cancel's arrival
// instead of a poll.
func (s *fakeServer) waitCancel(want int) {
	deadline := time.After(10 * time.Second)
	for {
		changed := s.changed()
		if s.cancelCount() >= want {
			return
		}
		select {
		case <-changed:
		case <-deadline:
			return
		}
	}
}

// hangUp drops the client's end of the wire, the way a server that died
// would.
func (s *fakeServer) hangUp() {
	if closer, ok := s.w.(io.Closer); ok {
		closer.Close()
	}
}

// waitDecision blocks until the client answers the ask, or gives up
// after a wait no test could need: a goroutine that spins forever is a
// leaked one, and the test that wanted the decision fails on its own
// assertions anyway. The wait blocks on the server's own signal, so
// the wait ends on the decision's arrival instead of a poll.
func (s *fakeServer) waitDecision(askID json.RawMessage) {
	deadline := time.After(10 * time.Second)
	for {
		changed := s.changed()
		s.mu.Lock()
		n := len(s.decisions)
		s.mu.Unlock()
		if n > 0 {
			return
		}
		select {
		case <-changed:
		case <-deadline:
			s.t.Error("the ask waited 10s with no decision")
			return
		}
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
	s.signal()
}

// methodsSoFar returns the request methods the client has sent, in
// order.
func (s *fakeServer) methodsSoFar() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.methods...)
}
