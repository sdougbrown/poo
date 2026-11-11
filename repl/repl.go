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
	"sync/atomic"

	"github.com/chzyer/readline"
	"github.com/sdougbrown/poo/wire/protocol"
	"golang.org/x/term"
)

// promptMark is the mark the repl reads its lines behind.
const promptMark = "💩❯ "

// namedLevels are the reasoning levels poo knows, lowest first.
//
// A model that announces no levels of its own is climbed through the
// protocol's canonical levels, in this order: the server checks each
// one against the model's reasoning control, so a level it cannot take
// comes back as a refusal.
var namedLevels = protocol.ReasoningLevelNames

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
	running     atomic.Bool
	// catalog holds the models models/list reported, by name: the
	// listing and the statusline read it instead of calling again.
	catalog map[string]protocol.ModelInfo
	// runID is the run the session is working on, empty when idle.
	// The cancel names it: the wire refuses a run/cancel that does
	// not say what to stop.
	runID string
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

	quit := make(chan struct{})
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	go func() {
		for range interrupts {
			if m.running.Load() {
				m.cancelRun()
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
	for {
		input.prompt(m.status.prompt())
		line, err := readLine(input, quit)
		if err != nil {
			if errors.Is(err, errInterrupted) {
				// The idle Ctrl-C: a clean end, with the code a
				// signal termination carries.
				return 130
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, readline.ErrInterrupt) {
				m.r.failure(err.Error())
			}
			return 0
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		code, done := m.command(line, input)
		if done {
			return code
		}
	}
}

// errInterrupted is the end an idle Ctrl-C brings.
var errInterrupted = errors.New("interrupted")

// readLine reads one line, or ends the session when the interrupt
// channel closes: an idle Ctrl-C quits, whatever the line editor was
// doing. The reader goroutine that loses the race is left to the
// editor's own close, and its result is never read.
func readLine(input input, quit <-chan struct{}) (string, error) {
	type read struct {
		line string
		err  error
	}
	done := make(chan read, 1)
	go func() {
		line, err := input.read()
		done <- read{line, err}
	}()
	select {
	case r := <-done:
		return r.line, r.err
	case <-quit:
		input.close()
		return "", errInterrupted
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

	// The catalog names every model the session may switch to, and
	// the chat model's entry carries the window the statusline shows
	// the fill against and the reasoning levels /thinking cycles.
	var catalog protocol.ModelsResult
	if err := m.conn.call(protocol.MethodModelsList, protocol.ModelsParams{}, &catalog); err != nil {
		return err
	}
	m.catalog = map[string]protocol.ModelInfo{}
	for _, model := range catalog.Models {
		m.catalog[model.Name] = model
		if model.Name == session.Models.Chat {
			m.status.setWindow(model.ContextLimit)
		}
	}

	// A configure that changes nothing reads back what the session
	// runs at, the reasoning level among it, so the statusline names
	// the level a session opened with.
	var current protocol.ConfigureResult
	if err := m.conn.call(protocol.MethodSessionConfigure, protocol.ConfigureParams{SessionID: m.sessionID}, &current); err != nil {
		return err
	}
	m.status.level = current.Reasoning
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
	if termIsTerminal(m.in) {
		tab := &backtab{m: m}
		rl, err := readline.NewEx(m.lineConfig(tab))
		if err == nil {
			tab.rl = rl
			return &readlineInput{rl: rl}
		}
		// A readline that would not start falls through to plain
		// lines: the conversation works without the editor.
	}
	return &plainInput{scanner: bufio.NewScanner(m.in), write: m.r.write}
}

// lineConfig is the editor's configuration: a readline in vi mode, with
// Shift+Tab bound to the reasoning cycle.
func (m *machine) lineConfig(tab *backtab) *readline.Config {
	return &readline.Config{
		Prompt:      promptMark,
		HistoryFile: m.opts.HistoryFile,
		VimMode:     true,
		Stdout:      m.out,
		// The Shift+Tab sequence reaches the filter one rune at a time;
		// see backtab.
		FuncFilterInputRune: tab.filter,
	}
}

// backtab binds Shift+Tab to the reasoning cycle, the way pi cycles its
// thinking level.
//
// A terminal sends Shift+Tab as ESC, '[', 'Z', and readline's escape
// decoder reads that kind of sequence itself: a key it does not know is
// dropped, so the filter never sees it. The vim mode, which this repl
// reads in, forwards the ESC as a bare rune instead of looking ahead, so
// the three runes do reach the filter, one call each. This is the state
// that recognizes them.
//
// The ESC passes on, because it is the key that leaves the insert mode,
// and the '[' is held until the 'Z' decides. Holding it costs nothing: the
// ESC has just put readline in the normal mode, where a lone '[' only
// rings the bell. A run that completes is the cycle, and both of its
// runes are swallowed, so the vim handler never sees them.
//
// The cycle is a wire call, and it runs while the prompt is being read:
// the session has no run going then, so the server has nothing else to
// answer.
type backtab struct {
	// m is the machine whose reasoning level the cycle moves.
	m *machine
	// rl is the editor the statusline is set on, once it is built.
	rl *readline.Instance
	// stage is how much of the sequence has arrived: 1 after the ESC, 2
	// after the '['.
	stage int
}

// filter sees every rune the editor reads, before the vim handler does.
func (b *backtab) filter(r rune) (rune, bool) {
	switch {
	case r == readline.CharEsc:
		b.stage = 1
		return r, true
	case b.stage == 1 && r == readline.CharEscapeEx:
		b.stage = 2
		return r, false
	case b.stage == 2 && r == 'Z':
		b.stage = 0
		b.cycle()
		return r, false
	}
	b.stage = 0
	return r, true
}

// cycle moves the session to its next level and sets the prompt again, so
// the statusline shows the level the next line is read at.
//
// A refused level is a line of the transcript, and the prompt is taken back
// first because the editor has already painted it.
func (b *backtab) cycle() {
	if err := b.m.setThinking(b.m.nextThinkingLevel()); err != nil {
		fmt.Fprint(b.m.out, "\r\033[K")
		b.m.r.failure(err.Error())
	}
	if b.rl != nil {
		b.rl.SetPrompt(b.m.status.prompt())
	}
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
/model <name>    switch the chat model
/thinking        cycle the reasoning level (Shift+Tab)
/thinking <lvl>  set the reasoning level: off, minimal, low, medium, high,
                 xhigh or a token budget
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
		for _, name := range m.catalogNames() {
			shown := name
			if name == m.status.model {
				shown += " ←"
			}
			m.r.notice("%s · %s", shown, m.catalog[name].ModelID)
		}
		return 0, false
	case "/cancel":
		m.cancelRun()
		return 0, false
	case "/thinking":
		m.thinking("")
		return 0, false
	}
	if name, ok := strings.CutPrefix(line, "/model "); ok {
		m.switchModel(strings.TrimSpace(name))
		return 0, false
	}
	if level, ok := strings.CutPrefix(line, "/thinking "); ok {
		m.thinking(strings.TrimSpace(level))
		return 0, false
	}
	if err := m.runPrompt(line, input); err != nil {
		m.r.failure(err.Error())
		if isShutdown(err) {
			return 1, true
		}
	}
	return 0, false
}

// switchModel points the session's chat role at another model the
// catalog holds, and takes the new model's window for the statusline.
//
// The switch is the wire's own: session/configure changes the models
// between runs, and the answer names what the session talks to now.
// A name the catalog does not hold is the server's to refuse.
func (m *machine) switchModel(name string) {
	if name == "" {
		m.r.failure("which model? /model lists them")
		return
	}
	var res protocol.ConfigureResult
	err := m.conn.call(protocol.MethodSessionConfigure, protocol.ConfigureParams{
		SessionID: m.sessionID,
		Models:    &protocol.ModelRoles{Chat: name},
	}, &res)
	if err != nil {
		m.r.failure(err.Error())
		return
	}
	m.status.model = res.Models.Chat
	if info, ok := m.catalog[res.Models.Chat]; ok {
		m.status.setWindow(info.ContextLimit)
	}
	m.r.notice("model → %s", res.Models.Chat)
	// The level is the new model's to take, so the wire says where the
	// session runs now: a model that cannot take the level it had takes
	// the session to another one, and the repl says so.
	if res.Reasoning != m.status.level {
		m.r.notice("thinking → %s", levelName(res.Reasoning))
	}
	m.status.level = res.Reasoning
}

// thinking sets the session's reasoning level, or takes the next one when
// no level is named.
//
// The level is the wire's to check: the server refuses one the chat model
// does not take, and the refusal names the levels it does.
func (m *machine) thinking(level string) {
	if level == "" {
		level = m.nextThinkingLevel()
	}
	if err := m.setThinking(level); err != nil {
		m.r.failure(err.Error())
		return
	}
	m.r.notice("thinking → %s", levelName(m.status.level))
}

// nextThinkingLevel returns the level after the session's, wrapping at the
// end of the list.
//
// The order is the model's own, lowest first. A model that names no levels
// is climbed through the ones poo knows, which the server checks one by
// one; a level the session is not at starts the climb from the bottom.
func (m *machine) nextThinkingLevel() string {
	levels := namedLevels
	if info, ok := m.catalog[m.status.model]; ok && len(info.ReasoningLevels) > 0 {
		levels = info.ReasoningLevels
	}
	for i, level := range levels {
		if strings.EqualFold(level, m.status.level) {
			return levels[(i+1)%len(levels)]
		}
	}
	return levels[0]
}

// setThinking sets the session's reasoning level on the wire and takes the
// level the session says it runs at.
func (m *machine) setThinking(level string) error {
	var res protocol.ConfigureResult
	if err := m.conn.call(protocol.MethodSessionConfigure, protocol.ConfigureParams{
		SessionID: m.sessionID,
		Reasoning: level,
	}, &res); err != nil {
		return err
	}
	m.status.level = res.Reasoning
	return nil
}

// levelName names a reasoning level the way the transcript says it, where
// an empty level is a session that sends no reasoning setting.
func levelName(level string) string {
	if level == "" {
		return "none"
	}
	return level
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
	defer func() { m.runID = "" }()
	m.runID = res.RunID
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

// catalogNames returns the catalog's model names, in name order.
func (m *machine) catalogNames() []string {
	names := make([]string, 0, len(m.catalog))
	for name := range m.catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// cancelRun asks the active run to stop. A run that is not there is
// no failure: the ask raced with the settle.
func (m *machine) cancelRun() {
	if m.runID == "" {
		return
	}
	_ = m.conn.call(protocol.MethodRunCancel, protocol.CancelParams{
		SessionID: m.sessionID,
		RunID:     m.runID,
	}, nil)
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
		if raw, err := json.Marshal(evt.Payload); err == nil {
			var usage protocol.UsageEvent
			if json.Unmarshal(raw, &usage) == nil {
				m.status.setUsage(usage.InputTokens, 0)
			}
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
	if termIsTerminal(out) {
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
