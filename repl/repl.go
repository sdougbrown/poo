// Package repl is poo's line-oriented front door: a wire client that
// runs a session against the poo binary's own serve command and
// renders the conversation in the terminal.
//
// The repl is deliberately small. It holds no run loop of its own and
// no session state beyond what the wire tells it: the server runs the
// session, streams the events, and asks about permissions; the repl
// renders what arrives and sends what is typed. A richer interface
// replaces it without taking anything from it.
package repl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chzyer/readline"
	"github.com/sdougbrown/poo/wire/protocol"
	"golang.org/x/term"
)

// promptMark is the mark the repl reads its lines behind.
const promptMark = "💩❯ "

// Options are the repl's settings.
type Options struct {
	// ServerArgs are the argv the repl starts the server with. The
	// first word is the subcommand, so a settings file reaches the
	// session through the same flags every command takes.
	ServerArgs []string
	// WorkingDir is the directory the session's tools work in. It is
	// also the folder the statusline names.
	WorkingDir string
	// Profile is the profile the session runs under. An empty name
	// takes the server's default.
	Profile string
	// HistoryFile holds the input history between sessions. An empty
	// name keeps no history.
	HistoryFile string
	// In and Out default to the process's own. Everything the person
	// reads goes to Out; failures render there too, like the rest of
	// the transcript.
	In  io.Reader
	Out io.Writer
	// StartServer starts the poo server. It returns the server's
	// input, its output, and the wait for its exit. The default runs
	// this binary with the ServerArgs; tests put their own server
	// here.
	StartServer func(args []string) (stdin io.WriteCloser, stdout io.ReadCloser, wait func() error, err error)
}

// Run starts the repl and runs it until the session ends.
//
// The exit code is the process's: zero after a clean quit, one when
// the server would not start or died.
func Run(opts Options) int {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}

	r := newRenderer(out)
	m := &machine{
		opts:   opts,
		out:    out,
		in:     in,
		r:      r,
		status: newStatus(opts.WorkingDir),
	}
	return m.run()
}

// machine is one repl session: the connection, the session on it, the
// renderer and the status.
type machine struct {
	opts   Options
	out    io.Writer
	in     io.Reader
	r      *renderer
	status *status

	conn        *conn
	sessionID   string
	wait        func() error
	serverStdin io.WriteCloser
	// catalog holds the models models/list reported, by name: the
	// listing and the statusline read it instead of calling again.
	catalog map[string]protocol.ModelInfo

	// gate asks the reader for the next line, and lines carries what
	// it read. One goroutine owns the input for the whole session:
	// the editor refuses a second reader, and the Escape that stops a
	// run has to arrive while the run streams.
	gate  chan struct{}
	lines chan lineResult
	// reading says a read is in flight. Only the main loop sets it.
	reading bool
	// ask is the permission ask that waits for an answer, nil when
	// none does.
	ask *askRequest
	// eof says the input ended: the session ends once the run that
	// was streaming settles.
	eof bool
	// state and streams render the run the session is on, nil when
	// nothing streams.
	state   *runState
	streams *streamView
	// esc carries the run an Escape asked to stop.
	esc chan string
	// escGen counts the keys the filter delivered: an escape is a key
	// of its own only when no key follows it inside escWindow.
	escGen atomic.Int64
	// escState is where the escape the filter is holding has got to.
	// The filter is the only writer, on the reader's goroutine.
	escState escState
	// runID is the run the session is working on, empty when idle.
	// The cancel names it: the wire refuses a run/cancel that does
	// not say what to stop.
	runIDMu sync.Mutex
	runID   string
}

// lineResult is one read from the input.
type lineResult struct {
	line string
	err  error
}

// escWindow is how long an escape byte waits for the rest of its
// sequence before the repl reads it as Escape pressed on its own. A
// terminal sends a whole arrow key in one write, so the wait only has
// to cover the reader's own scheduling.
const escWindow = 40 * time.Millisecond

// input reads the lines a person types. A terminal gets the readline
// editor with its vi mode; anything else gets plain lines, so a pipe
// or a test drives the repl too.
//
// Only read runs on the reader's goroutine; prompt and close are the
// main loop's.
type input interface {
	// prompt shows the prompt, here and for the next read.
	prompt(p string)
	// read reads one line. An end of input is io.EOF.
	read() (string, error)
	// close releases the terminal.
	close()
}

// termIsTerminal reports whether the file behind f is a terminal.
func termIsTerminal(f any) bool {
	fd, ok := f.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(int(fd.Fd()))
}

// run starts the server, opens the session and loops until the person
// quits or the server goes away.
func (m *machine) run() int {
	// The shutdown runs even when the start failed part way: a
	// handshake that failed with the server still alive leaves the
	// session and the pipes for it to close.
	defer m.shutdown()
	if err := m.start(); err != nil {
		m.r.failure(err.Error())
		return 1
	}

	input := m.newInput()
	defer input.close()
	m.gate = make(chan struct{})
	m.lines = make(chan lineResult)
	m.esc = make(chan string, 1)

	quit := make(chan struct{})
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	go func() {
		for range interrupts {
			if runID := m.activeRun(); runID != "" {
				m.cancelRun(runID)
			} else {
				// An idle Ctrl-C ends the session, the way every
				// interactive tool quits: the exit code is the
				// one a signal termination carries.
				close(quit)
				return
			}
		}
	}()

	m.greet()
	stop := make(chan struct{})
	defer close(stop)
	go m.readLines(input, stop)

	for {
		// The read stays live while a run streams: Escape and Ctrl-C
		// are keys, and a key needs a reader.
		var open chan struct{}
		if !m.reading && !m.eof {
			input.prompt(m.prompt())
			open = m.gate
		}
		select {
		case open <- struct{}{}:
			m.reading = true
		case evt, ok := <-m.conn.events:
			if !ok {
				return m.gone()
			}
			if m.state == nil {
				// Nothing of the session's own streams: a job's or a
				// feed's event is not the conversation.
				continue
			}
			if m.event(evt, m.state, m.streams) {
				if code, done := m.endRun(input); done {
					return code
				}
			}
		case req, ok := <-m.conn.asks:
			if !ok {
				return m.gone()
			}
			m.openAsk(req, input)
		case runID := <-m.esc:
			m.cancelRun(runID)
		case res := <-m.lines:
			m.reading = false
			if code, done := m.line(res, input); done {
				return code
			}
		case <-quit:
			return 130
		}
	}
}

// readLines owns the input: one read at a time, behind the prompt the
// main loop set. It ends when the session does.
func (m *machine) readLines(input input, stop <-chan struct{}) {
	for {
		select {
		case <-m.gate:
			line, err := input.read()
			select {
			case m.lines <- lineResult{line: line, err: err}:
			case <-stop:
				return
			}
			if err != nil && !errors.Is(err, readline.ErrInterrupt) {
				return
			}
		case <-stop:
			return
		}
	}
}

// line handles one read: an interrupt, the end of the input, the
// answer to an ask, a line submitted while a run works, or a command.
func (m *machine) line(res lineResult, input input) (int, bool) {
	switch {
	case errors.Is(res.err, readline.ErrInterrupt):
		// The editor has already cleared the line it held, so the
		// interrupt cleared what was typed. An interrupt with nothing
		// to clear is the person leaving.
		if res.line == "" {
			return 130, true
		}
		return 0, false
	case res.err != nil:
		if !errors.Is(res.err, io.EOF) {
			m.r.failure(res.err.Error())
			return 0, true
		}
		// An input that ended is the session's end, but a run that
		// streams still has events worth reading: the end waits for
		// the settle.
		m.eof = true
		if m.ask != nil {
			m.answer("", input)
		}
		if m.activeRun() != "" {
			return 0, false
		}
		return 0, true
	}
	text := strings.TrimSpace(res.line)
	switch {
	case m.ask != nil:
		m.answer(text, input)
		return 0, false
	case text == "":
		return 0, false
	case m.activeRun() != "":
		// A run takes one prompt at a time, so a line submitted while
		// it works is not the next one — but it is not dropped in
		// silence either: the transcript says what was typed and which
		// key stops the turn.
		m.r.notice("the run is working — Esc stops it; %q was not sent", text)
		return 0, false
	default:
		return m.command(text, input)
	}
}

// prompt is the prompt the next read sits behind: the ask's when a
// decision waits, the statusline's otherwise.
func (m *machine) prompt() string {
	if m.ask != nil {
		return askPrompt(m.ask.ask)
	}
	return m.status.prompt()
}

// askPrompt is the line a permission decision is typed behind.
func askPrompt(ask protocol.AskParams) string {
	return ansiYellow + "  allow " + ask.Tool + "? [y/N] " + ansiReset
}

// gone is the exit code a lost server carries.
func (m *machine) gone() int {
	if m.conn != nil && m.conn.err != nil {
		m.r.failure(m.conn.err.Error())
	} else {
		m.r.failure(errServerClosed.Error())
	}
	return 1
}

// escState is where the escape the filter is holding has got to. The
// escape byte begins every arrow, Home, Delete and function key, so a
// run cannot act on it until it knows what followed.
type escState int

const (
	// escIdle holds no escape.
	escIdle escState = iota
	// escOpen holds an escape that has not said what it is yet.
	escOpen
	// escSequence holds the escape of a key that has more bytes.
	escSequence
)

// filterRune reads the escape byte before the editor does, and only
// while a run works: there Escape stops the run, and nothing else the
// run needs. Idle, every key is the editor's.
func (m *machine) filterRune(r rune) (rune, bool) {
	runID := m.activeRun()
	if runID == "" {
		m.escState = escIdle
		return r, true
	}
	switch {
	case r == readline.CharEsc:
		// The escape is a key of its own unless the rest of an arrow,
		// Home or function key follows it, and the window tells which.
		m.escState = escOpen
		seen := m.escGen.Add(1)
		time.AfterFunc(escWindow, func() {
			if m.escGen.Load() != seen {
				return
			}
			select {
			case m.esc <- runID:
			default:
			}
		})
		return r, false
	case m.escState == escIdle:
		return r, true
	}
	// Something followed the escape after all: it was a prefix.
	m.escGen.Add(1)
	if m.escState == escOpen {
		if r == '[' || r == 'O' {
			m.escState = escSequence
			return r, false
		}
		m.escState = escIdle
		return r, true
	}
	// The rest of the key goes too, so an arrow leaves no text behind.
	if r >= 0x20 && r <= 0x7e {
		if r >= 0x40 {
			m.escState = escIdle
		}
		return r, false
	}
	m.escState = escIdle
	return r, true
}

// start starts the server, does the handshake, opens the session and
// reads the model's window for the statusline.
func (m *machine) start() error {
	stdin, stdout, wait, err := m.server()
	if err != nil {
		return err
	}
	m.conn = startConn(stdout, stdin)
	m.wait = wait
	m.serverStdin = stdin

	var info protocol.InitializeResult
	err = m.conn.call(protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersions: []int{protocol.Version},
		Client: protocol.Client{
			Name:    "poo-repl",
			Version: "0",
			Capabilities: protocol.ClientCapabilities{
				PermissionAnswers: true,
			},
		},
	}, &info)
	if err != nil {
		return err
	}

	profile := m.opts.Profile
	if profile == "" {
		profile = info.DefaultProfile
	}
	var session protocol.SessionInfo
	err = m.conn.call(protocol.MethodSessionCreate, protocol.CreateParams{
		WorkingDir: m.opts.WorkingDir,
		Profile:    profile,
	}, &session)
	if err != nil {
		return err
	}
	m.sessionID = session.SessionID
	m.status.model = session.Models.Chat
	for _, warning := range session.Warnings {
		m.r.warning(warning)
	}

	// The catalog names every model the session may switch to, and
	// the chat model's entry carries the window the statusline shows
	// the fill against.
	var catalog protocol.ModelsResult
	if err := m.conn.call(protocol.MethodModelsList, protocol.ModelsParams{}, &catalog); err != nil {
		return err
	}
	m.catalog = map[string]protocol.ModelInfo{}
	for _, model := range catalog.Models {
		m.catalog[model.Name] = model
		if model.Name == session.Models.Chat {
			m.status.setContextLimit(model.ContextLimit)
		}
	}
	return nil
}

// server starts the poo server.
//
// The default runs this binary with the options' argv, so a repl and
// its server are the same build. The server's own errors reach the
// terminal as they happen, because they are the operator's to read.
func (m *machine) server() (io.WriteCloser, io.ReadCloser, func() error, error) {
	if m.opts.StartServer != nil {
		return m.opts.StartServer(m.opts.ServerArgs)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("repl: finding the server: %w", err)
	}
	args := m.opts.ServerArgs
	if len(args) == 0 {
		args = []string{"serve"}
	}
	cmd := exec.Command(exe, args...)
	ownProcessGroup(cmd)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("repl: starting %s: %w", exe, err)
	}
	return stdin, stdout, cmd.Wait, nil
}

// newInput builds the line reader for the session.
func (m *machine) newInput() input {
	if termIsTerminal(m.in) {
		editor := &readlineInput{}
		cfg := &readline.Config{
			Prompt:      promptMark,
			HistoryFile: m.opts.HistoryFile,
			VimMode:     true,
			Stdout:      m.out,
			// The filter is where Escape is read: it has to stop a
			// run before the editor takes the key.
			FuncFilterInputRune: m.filterRune,
		}
		if rc, ok := m.in.(io.ReadCloser); ok {
			cfg.Stdin = rc
		}
		if f, ok := m.in.(interface{ Fd() uintptr }); ok {
			fd := int(f.Fd())
			cfg.FuncMakeRaw, cfg.FuncExitRaw = rawMode(fd)
			// The editor decides whether to draw from the terminal it
			// holds, which is the input's, not the process's stdin.
			cfg.FuncIsTerminal = func() bool {
				return term.IsTerminal(fd) && termIsTerminal(m.out)
			}
		}
		rl, err := readline.NewEx(cfg)
		if err == nil {
			editor.rl = rl
			// The transcript goes through the editor's own writer, so
			// the prompt is redrawn under every line it prints.
			m.r.sink = rl.Stdout()
			return editor
		}
		// A readline that would not start falls through to plain
		// lines: the conversation works without the editor.
	}
	return &plainInput{scanner: bufio.NewScanner(m.in), write: m.r.write}
}

// rawMode puts the terminal behind fd into raw mode and back. The
// editor assumes its terminal is the process's own stdin, which is only
// true when the repl's input is.
func rawMode(fd int) (enter, exit func() error) {
	var state *term.State
	enter = func() error {
		var err error
		state, err = term.MakeRaw(fd)
		return err
	}
	exit = func() error {
		if state == nil {
			return nil
		}
		return term.Restore(fd, state)
	}
	return enter, exit
}

// greet says the session is open.
func (m *machine) greet() {
	m.r.notice("poo repl — /help for the commands, /quit to leave")
}

// command runs one input line. The bool says the repl is done, and
// the code is the exit code it takes.
func (m *machine) command(line string, input input) (int, bool) {
	switch line {
	case "/quit", "/q", "/exit":
		return 0, true
	case "/help", "/h", "?":
		m.r.notice(`/quit            leave the repl
/less            read the transcript again in less
/model           name the session's models
Esc              stop the running turn
/help            this list
anything else    a prompt for the session`)
		return 0, false
	case "/less":
		if err := m.r.page(m.in); err != nil {
			m.r.failure(err.Error())
		}
		return 0, false
	case "/model", "/models":
		for _, name := range m.catalogNames() {
			shown := name
			if name == m.status.model {
				shown += " ←"
			}
			m.r.notice("%s · %s", shown, m.catalog[name].ModelID)
		}
		return 0, false
	case "/cancel":
		m.cancelRun(m.activeRun())
		return 0, false
	default:
		if err := m.startRun(line); err != nil {
			m.r.failure(err.Error())
			if isShutdown(err) {
				return 1, true
			}
		}
		return 0, false
	}
}

// startRun sends one prompt and reports the run it started. The events
// come to the main loop, which renders them until the settle.
func (m *machine) startRun(text string) error {
	m.r.userPrompt(text)
	var res protocol.PromptResult
	if err := m.conn.call(protocol.MethodRunPrompt, protocol.PromptParams{
		SessionID: m.sessionID,
		Text:      text,
	}, &res); err != nil {
		return err
	}
	m.runIDMu.Lock()
	m.runID = res.RunID
	m.runIDMu.Unlock()
	m.state = &runState{}
	m.streams = newStreamView(m.r.sink, m.r.term)
	return nil
}

// endRun closes the run a settle ended: an ask it left open goes with
// it, and the editor gets its prompt back.
func (m *machine) endRun(input input) (int, bool) {
	m.runIDMu.Lock()
	m.runID = ""
	m.runIDMu.Unlock()
	m.state, m.streams = nil, nil
	if m.ask != nil {
		// An ask for a run that settled is a question about nothing.
		m.ask = nil
	}
	input.prompt(m.prompt())
	if m.eof {
		return 0, true
	}
	return 0, false
}

// catalogNames returns the catalog's model names, in name order.
func (m *machine) catalogNames() []string {
	names := make([]string, 0, len(m.catalog))
	for name := range m.catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// activeRun is the run the session is on, empty when idle.
func (m *machine) activeRun() string {
	m.runIDMu.Lock()
	defer m.runIDMu.Unlock()
	return m.runID
}

// cancelRun asks the named run to stop. A run that is gone is no
// failure: the cancel raced with the settle, and the name keeps it from
// stopping the run that replaced it.
func (m *machine) cancelRun(runID string) {
	if runID == "" || m.activeRun() != runID {
		return
	}
	_ = m.conn.call(protocol.MethodRunCancel, protocol.CancelParams{
		SessionID: m.sessionID,
		RunID:     runID,
	}, nil)
}

// event renders one event. The bool says the run settled.
func (m *machine) event(evt protocol.Event, state *runState, streams *streamView) bool {
	switch evt.Type {
	case "message.delta":
		id, _ := evt.Payload["message_id"].(string)
		kind, _ := evt.Payload["kind"].(string)
		text, _ := evt.Payload["text"].(string)
		state.delta(id, kind, text)
		streams.tick()
	case "tool.call":
		streams.stop()
		state.flush(m.r)
		tool, _ := evt.Payload["tool"].(string)
		summary, _ := evt.Payload["summary"].(string)
		m.r.toolCall(tool, summary)
	case "tool.result":
		tool, _ := evt.Payload["tool"].(string)
		outcome, _ := evt.Payload["outcome"].(string)
		text, _ := evt.Payload["text"].(string)
		elided := 0
		if n, ok := evt.Payload["elided_chars"].(float64); ok {
			elided = int(n)
		}
		m.r.toolResult(tool, outcome, text, elided)
	case "usage":
		if raw, err := json.Marshal(evt.Payload); err == nil {
			var usage protocol.UsageEvent
			if json.Unmarshal(raw, &usage) == nil {
				m.status.setTokens(usage.InputTokens)
			}
		}
	case "turn.ended":
		state.turns++
	case "run.settled":
		if id, _ := evt.Payload["run_id"].(string); id != "" && id == m.activeRun() {
			streams.stop()
			state.flush(m.r)
			reason, _ := evt.Payload["reason"].(string)
			input, output := settledUsage(evt.Payload)
			m.r.settle(reason, state.turns, input, output)
			m.status.setTokens(input)
			return true
		}
	}
	return false
}

// settledUsage reads the token accounting out of a settle event's
// detail: the payload is re-encoded and read through the protocol's own
// shape, so a field the contract renames is a compile error here, not a
// statusline that silently reads zero.
func settledUsage(payload map[string]any) (input, output int) {
	raw, err := json.Marshal(payload["detail"])
	if err != nil {
		return 0, 0
	}
	var detail protocol.SettleDetail
	if json.Unmarshal(raw, &detail) != nil || detail.Usage == nil {
		return 0, 0
	}
	return detail.Usage.InputTokens, detail.Usage.OutputTokens
}

// openAsk shows a permission ask. The prompt it shows is the line the
// answer is typed behind, so the y or n reaches the session the same
// way every other line does.
func (m *machine) openAsk(req askRequest, input input) {
	m.ask = &req
	m.r.write(ansiYellow + "⚙ permission · " + req.ask.Tool + ansiReset + "\n")
	if req.ask.Summary != "" {
		m.r.write(ansiDim + req.ask.Summary + ansiReset + "\n")
	}
	if m.eof {
		// No input is left to answer with, and the refused call is
		// the safe side.
		m.answer("", input)
		return
	}
	input.prompt(m.prompt())
}

// answer sends the decision for the open ask.
//
// An unknown answer denies the call: the safe side of a mistake is the
// refused one.
func (m *machine) answer(text string, input input) {
	req := m.ask
	m.ask = nil
	decision := protocol.AskResult{Decision: protocol.DecisionDeny}
	switch strings.ToLower(text) {
	case "y", "yes":
		decision.Decision = protocol.DecisionAllow
	}
	m.r.write(ansiDim + "→ " + decision.Decision + ansiReset + "\n")
	if err := m.conn.answer(*req, decision); err != nil {
		m.r.failure(err.Error())
	}
	input.prompt(m.prompt())
}

// shutdown closes the session, closes the server's input so its read
// loop ends, and waits for the process to leave.
func (m *machine) shutdown() {
	if m.conn != nil && m.sessionID != "" {
		_ = m.conn.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: m.sessionID}, nil)
	}
	if m.serverStdin != nil {
		m.serverStdin.Close()
	}
	if m.wait != nil {
		_ = m.wait()
	}
}

// runState holds the streamed messages a run has open, and the turns
// the run took.
type runState struct {
	// messages are the replies, in arrival order.
	messages []*messageBuffer
	// byID finds a message the deltas name.
	byID map[string]*messageBuffer
	// turns is how many turns the run took: the wire's settle event
	// does not carry the count, the turn events do.
	turns int
}

// messageBuffer is one reply the deltas are still writing.
type messageBuffer struct {
	id       string
	text     strings.Builder
	thinking strings.Builder
}

// delta adds one piece of a streamed message.
func (s *runState) delta(id, kind, text string) {
	if s.byID == nil {
		s.byID = map[string]*messageBuffer{}
	}
	m := s.byID[id]
	if m == nil {
		m = &messageBuffer{id: id}
		s.byID[id] = m
		s.messages = append(s.messages, m)
	}
	switch kind {
	case "thinking":
		m.thinking.WriteString(text)
	default:
		m.text.WriteString(text)
	}
}

// flush renders the messages the deltas finished with, oldest first,
// and empties the buffers.
func (s *runState) flush(r *renderer) {
	for _, m := range s.messages {
		r.thinking(m.thinking.String())
		r.message(m.text.String())
	}
	s.messages = nil
	s.byID = nil
}

// streamView is the one-line hint that the model is writing. A
// terminal gets a dim line that the first render takes back; a pipe
// gets nothing.
type streamView struct {
	out   io.Writer
	live  bool
	shown bool
}

// newStreamView builds the hint for out. live says out is a terminal.
func newStreamView(out io.Writer, live bool) *streamView {
	return &streamView{out: out, live: live}
}

// tick shows the hint, once per run.
func (s *streamView) tick() {
	if s.live && !s.shown {
		fmt.Fprint(s.out, ansiDim+"… writing"+ansiReset)
		s.shown = true
	}
}

// stop takes the hint back.
func (s *streamView) stop() {
	if s.shown {
		fmt.Fprint(s.out, "\r\033[K")
		s.shown = false
	}
}

// The errors the run loop reports to the caller.
var (
	errServerClosed = errors.New("the server closed the connection")
)

// isShutdown says whether a failure means the server is gone.
func isShutdown(err error) bool {
	return errors.Is(err, errServerClosed)
}

// readlineInput reads lines through the readline editor.
type readlineInput struct {
	rl *readline.Instance
}

func (i *readlineInput) prompt(p string) {
	i.rl.SetPrompt(p)
	i.rl.Refresh()
}

func (i *readlineInput) read() (string, error) {
	line, err := i.rl.Readline()
	return strings.TrimSuffix(line, "\n"), err
}

func (i *readlineInput) close() {
	// The editor's own Close waits on a read it cannot interrupt: its
	// stdin wrapper closes the fill pipe and leaves the read below it
	// parked. The terminal goes back and the history is saved; the
	// editor's goroutines end with the process.
	_ = i.rl.Terminal.ExitRawMode()
	i.rl.Operation.Close()
}

// plainInput reads lines without an editor.
//
// The prompts go through the renderer, so they land in the transcript
// the pager shows again.
type plainInput struct {
	scanner *bufio.Scanner
	write   func(string)
	shown   string
}

func (i *plainInput) prompt(p string) {
	if p == i.shown || p == "" {
		return
	}
	i.shown = p
	i.write(p)
}

func (i *plainInput) read() (string, error) {
	if !i.scanner.Scan() {
		return "", io.EOF
	}
	return i.scanner.Text(), nil
}

func (i *plainInput) close() {}
