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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"

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
	// In, Out and Err default to the process's own.
	In  io.Reader
	Out io.Writer
	Err io.Writer
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
	errw := opts.Err
	if errw == nil {
		errw = os.Stderr
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}

	r := newRenderer(out)
	m := &machine{
		opts:   opts,
		out:    out,
		errw:   errw,
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
	errw   io.Writer
	in     io.Reader
	r      *renderer
	status *status

	conn        *conn
	sessionID   string
	wait        func() error
	serverStdin io.WriteCloser
	running     atomic.Bool
}

// input reads the lines a person types. A terminal gets the readline
// editor with its vi mode; anything else gets plain lines, so a pipe
// or a test drives the repl too.
type input interface {
	// prompt sets the prompt the next line reads behind.
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
	if err := m.start(); err != nil {
		m.r.failure(err.Error())
		return 1
	}
	defer m.shutdown()

	input := m.newInput()
	defer input.close()

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	go func() {
		for range interrupts {
			if m.running.Load() {
				m.cancelRun()
			}
		}
	}()

	m.greet()
	for {
		input.prompt(m.status.prompt())
		line, err := input.read()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, readline.ErrInterrupt) {
				m.r.failure(err.Error())
			}
			return 0
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		code, quit := m.command(line, input)
		if quit {
			return code
		}
	}
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

	// The window comes from the catalog: the model's entry names the
	// context limit the statusline shows the fill against.
	var catalog struct {
		Models []protocol.ModelInfo `json:"models"`
	}
	if err := m.conn.call(protocol.MethodModelsList, struct{}{}, &catalog); err == nil {
		for _, model := range catalog.Models {
			if model.Name == session.Models.Chat {
				m.status.setUsage(-1, model.ContextLimit)
			}
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
	if _, ok := m.in.(interface{ Fd() uintptr }); ok && termIsTerminal(m.in) {
		rl, err := readline.NewEx(&readline.Config{
			Prompt:      promptMark,
			HistoryFile: m.opts.HistoryFile,
			VimMode:     true,
			Stdout:      m.out,
		})
		if err == nil {
			return &readlineInput{rl: rl}
		}
		// A readline that would not start falls through to plain
		// lines: the conversation works without the editor.
	}
	return &plainInput{scanner: bufio.NewScanner(m.in), write: m.r.write}
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
/cancel          stop the running turn
/help            this list
anything else    a prompt for the session`)
		return 0, false
	case "/less":
		if err := m.r.page(); err != nil {
			m.r.failure(err.Error())
		}
		return 0, false
	case "/model", "/models":
		var catalog struct {
			Models []protocol.ModelInfo `json:"models"`
		}
		if err := m.conn.call(protocol.MethodModelsList, struct{}{}, &catalog); err != nil {
			m.r.failure(err.Error())
			return 0, false
		}
		for _, model := range catalog.Models {
			name := model.Name
			if name == m.status.model {
				name += " ←"
			}
			m.r.notice("%s · %s", name, model.ModelID)
		}
		return 0, false
	case "/cancel":
		m.cancelRun()
		return 0, false
	default:
		if err := m.runPrompt(line, input); err != nil {
			if isShutdown(err) {
				m.r.failure(err.Error())
				return 1, true
			}
			m.r.failure(err.Error())
		}
		return 0, false
	}
}

// runPrompt sends one prompt and renders the run's events until the
// run settles.
func (m *machine) runPrompt(text string, input input) error {
	m.r.userPrompt(text)
	var res protocol.PromptResult
	if err := m.conn.call(protocol.MethodRunPrompt, protocol.PromptParams{
		SessionID: m.sessionID,
		Text:      text,
	}, &res); err != nil {
		return err
	}

	m.running.Store(true)
	defer m.running.Store(false)
	streams := newStreamView(m.out)
	state := &runState{}
	for {
		select {
		case evt, ok := <-m.conn.events:
			if !ok {
				return errServerClosed
			}
			if done := m.event(evt, res.RunID, state, streams); done {
				return nil
			}
		case req, ok := <-m.conn.asks:
			if !ok {
				return errServerClosed
			}
			if err := m.answerAsk(req, input); err != nil {
				return err
			}
		}
	}
}

// cancelRun asks the active run to stop. A run that is not there is
// no failure: the ask raced with the settle.
func (m *machine) cancelRun() {
	_ = m.conn.call(protocol.MethodRunCancel, protocol.CancelParams{SessionID: m.sessionID}, nil)
}

// event renders one event. The bool says the run settled.
func (m *machine) event(evt protocol.Event, runID string, state *runState, streams *streamView) bool {
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
		if n, ok := evt.Payload["input_tokens"].(float64); ok {
			m.status.setUsage(int(n), 0)
		}
	case "turn.ended":
		state.turns++
	case "run.settled":
		if id, _ := evt.Payload["run_id"].(string); id == runID {
			streams.stop()
			state.flush(m.r)
			reason, _ := evt.Payload["reason"].(string)
			input, output := settledUsage(evt.Payload)
			m.r.settle(reason, state.turns, input, output)
			m.status.setUsage(input, 0)
			return true
		}
	}
	return false
}

// settledUsage reads the token accounting out of a settle event's
// detail.
func settledUsage(payload map[string]any) (input, output int) {
	detail, ok := payload["detail"].(map[string]any)
	if !ok {
		return 0, 0
	}
	usage, ok := detail["usage"].(map[string]any)
	if !ok {
		return 0, 0
	}
	if n, ok := usage["input_tokens"].(float64); ok {
		input = int(n)
	}
	if n, ok := usage["output_tokens"].(float64); ok {
		output = int(n)
	}
	return input, output
}

// answerAsk renders a permission ask and reads the decision.
//
// The run waits while the ask is open, so nothing else streams past
// the question. An end of input, an interrupt or an unknown answer
// denies: the safe side of a mistake is the refused call.
func (m *machine) answerAsk(req askRequest, input input) error {
	ask := req.ask
	m.r.write(ansiYellow + "⚙ permission · " + ask.Tool + ansiReset + "\n")
	if ask.Summary != "" {
		m.r.write(ansiDim + firstLine(ask.Summary) + ansiReset + "\n")
	}
	input.prompt(ansiYellow + "  allow " + ask.Tool + "? [y/N] " + ansiReset)
	line, _ := input.read()
	decision := protocol.AskResult{Decision: protocol.DecisionDeny}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		decision.Decision = protocol.DecisionAllow
	}
	m.r.write(ansiDim + "→ " + decision.Decision + ansiReset + "\n")
	return m.conn.answer(req, decision)
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

// newStreamView builds the hint for out.
func newStreamView(out io.Writer) *streamView {
	if f, ok := out.(interface{ Fd() uintptr }); ok && termIsTerminal(f) {
		return &streamView{out: out, live: true}
	}
	return &streamView{out: out}
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
}

func (i *readlineInput) read() (string, error) {
	line, err := i.rl.Readline()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}

func (i *readlineInput) close() {
	i.rl.Close()
}

// plainInput reads lines without an editor.
//
// The prompts it reads behind go through the renderer, so they land
// in the transcript the pager shows again.
type plainInput struct {
	scanner *bufio.Scanner
	write   func(string)
	shown   string
}

func (i *plainInput) prompt(p string) {
	i.shown = p
}

func (i *plainInput) read() (string, error) {
	if i.shown != "" {
		i.write(i.shown)
		i.shown = ""
	}
	if !i.scanner.Scan() {
		return "", io.EOF
	}
	return i.scanner.Text(), nil
}

func (i *plainInput) close() {}
