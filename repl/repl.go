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
	"golang.org/x/term"

	termpkg "github.com/sdougbrown/poo/internal/term"

	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/reeflective/readline"

	"github.com/sdougbrown/poo/wire/protocol"
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
	// ResumeID opens the session this id names instead of starting a
	// new one. A resumed session keeps the working directory and the
	// model it recorded.
	ResumeID string
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
	// A terminal on both ends pins the input to the bottom. The
	// transcript scrolls in a region above it, so the prompt and the
	// draft hold still while output arrives. A pipe on either end keeps
	// the plain stream, where a redirect or a test reads the words.
	if termIsTerminal(in) && termIsTerminal(out) {
		if l := newLayout(out); l != nil {
			// The region is reserved once the session is open. A
			// startup failure or an interrupt during the handshake
			// must leave the terminal as it found it.
			defer l.leave()
			r.layout = l
		}
	}
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
	// none does. askMu guards it: the main loop writes it, and the
	// editor's key dispatch reads it through prompt().
	ask   *askRequest
	askMu sync.Mutex
	// eof says the input ended: the session ends once the run that
	// was streaming settles.
	eof bool
	// state renders the run the session is on, nil when idle.
	state *runState
	// frame ticks the preview a delta left dirty, while a run streams.
	// The main goroutine owns it; nil when idle.
	frame *time.Ticker
	// esc carries the run an Escape asked to stop.
	esc chan string
	// runID is the run the session is working on, empty when idle.
	// The cancel names it: the wire refuses a run/cancel that does
	// not say what to stop.
	runIDMu sync.Mutex
	runID   string
	// steers holds the lines this run accepted, by the input id the
	// wire named: the settle reports undelivered inputs by id, and
	// the transcript says what the person wrote. The map lives with
	// the run: endRun drops it with the rest of the run's state.
	steers map[string]string
	// lastMatches holds the models the last listing offered, in the
	// order it numbered them: a bare number picks out of it. It is the
	// only state a pick keeps, and a listing replaces it whole.
	lastMatches []string
}

// lineResult is one read from the input.
type lineResult struct {
	line string
	err  error
}

// streamFrame is how often the run loop paints the preview a delta left
// dirty: a long block coalesces its frames instead of rendering every
// token.
const streamFrame = 33 * time.Millisecond

// input reads the lines a person types. A terminal gets the readline
// editor with its vi mode; anything else gets plain lines, so a pipe
// or a test drives the repl too.
//
// Only read runs on the reader's goroutine. The main loop calls close,
// and prompt is called from the main loop and from the editor's key
// dispatch: it only publishes the text and wakes the editor, so the
// key goroutine may republish a changed prompt mid-read.
type input interface {
	// prompt shows the prompt, here and for the next read.
	prompt(p string)
	// read reads one line. An end of input is io.EOF.
	read() (string, error)
	// close releases the terminal.
	close()
}

// termIsTerminal reports whether the file behind f is a terminal.
//
// The question and its answer are internal/term's, so a command that asks it and the repl
// that asks it agree on every input.
func termIsTerminal(f any) bool {
	return termpkg.IsTerminal(f)
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
	defer m.disarmFrame()

	quit := make(chan struct{})
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	resizes := make(chan os.Signal, 1)
	if m.r.layout != nil {
		resizeSignals(resizes)
		defer signal.Stop(resizes)
	}
	// The handshake is done and Ctrl-C has a handler: the terminal can
	// give up its bottom rows now, and every way out restores them.
	if m.r.layout != nil {
		m.r.layout.enter()
	}
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
	defer m.flushPending()

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
			if code, done := m.handleEvent(evt, input); done {
				return code
			}
		case req, ok := <-m.conn.asks:
			if !ok {
				return m.gone()
			}
			if code, done := m.handleAsk(req, input); done {
				return code
			}
		case <-m.frameTick():
			m.r.paintPending()
		case <-resizes:
			// The terminal changed size: reserve the footer again at
			// the new geometry and redraw the prompt where it now
			// belongs. The response brackets the frames: a frame that
			// lands mid-response anchors only, since the region is
			// cleared for its rebuild and the preview cache is empty.
			if m.r.layout.resize() {
				m.r.rewrap(m.r.layout.cols())
				m.r.repaintPreview()
				m.r.layout.endResize()
				input.prompt(m.prompt())
			}
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
		// interrupt cleared what was typed. It erases its own line on a
		// submit, so the ^C the prompt used to draw belongs to the
		// transcript. An interrupt with nothing to clear is the person
		// leaving.
		m.r.write("^C\n")
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
		// it works is not the next one. A line that is not a command
		// is a steer: it rides the active run and reaches the model at
		// the run's next turn end. A command still waits — the run is
		// working, and the transcript says so, the way it always has.
		if name, _ := commandName(text); name != "" {
			m.r.notice("the run is working — Esc stops it; %q was not sent", text)
			return 0, false
		}
		return m.steer(text, input)
	default:
		return m.command(text, input)
	}
}

// steer sends a line to the run that is working: run/message, mode
// steer. The core queues it, and the run's next turn end delivers it
// to the model.
//
// The line's echo leads with a mark the prompt's does not carry, so
// the person can tell a steer from a prompt at a glance. A refusal
// keeps the session: the run that could not take the message goes on,
// and what was typed is said to have not gone.
func (m *machine) steer(text string, input input) (int, bool) {
	m.r.steerPrompt(text)
	var res protocol.MessageResult
	if err := m.conn.call(protocol.MethodRunMessage, protocol.MessageParams{
		SessionID: m.sessionID,
		Text:      text,
		Mode:      "steer",
	}, &res); err != nil {
		m.r.failure(err.Error())
		return 0, false
	}
	if m.steers == nil {
		m.steers = map[string]string{}
	}
	m.steers[res.InputID] = text
	return 0, false
}

// prompt is the prompt the next read sits behind: the ask's when a
// decision waits, the statusline's otherwise.
func (m *machine) prompt() string {
	m.askMu.Lock()
	ask := m.ask
	m.askMu.Unlock()
	if ask != nil {
		return askPrompt(ask.ask)
	}
	return m.status.prompt()
}

// askPrompt is the line a permission decision is typed behind.
func askPrompt(ask protocol.AskParams) string {
	return ansiYellow + "  allow " + sanitizeLine(ask.Tool) + "? [y/N] " + ansiReset
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
	if m.opts.ResumeID != "" {
		// The session is on disk. The server resolves the store the
		// way this process did, so the id alone opens it, and its own
		// header says where it works and what it talks to.
		err = m.conn.call(protocol.MethodSessionLoad, protocol.LoadParams{
			SessionID: m.opts.ResumeID,
		}, &session)
	} else {
		err = m.conn.call(protocol.MethodSessionCreate, protocol.CreateParams{
			WorkingDir: m.opts.WorkingDir,
			Profile:    profile,
		}, &session)
	}
	if err != nil {
		return err
	}
	m.sessionID = session.SessionID
	m.status.model = session.Models.Chat
	for _, warning := range session.Warnings {
		m.r.warning(warning)
	}
	if notice := recoveryNotice(session); notice != "" {
		m.r.warning(notice)
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
			m.status.setContextLimit(model.ContextLimit)
		}
	}

	// The session's state names the reasoning level it runs at, so
	// the statusline names the level a session opened with. The read
	// is the protocol's own read, not a configure that changes
	// nothing.
	var current protocol.GetResult
	if err := m.conn.call(protocol.MethodSessionGet, protocol.GetParams{SessionID: m.sessionID}, &current); err != nil {
		return err
	}
	m.status.level = current.Reasoning
	if m.opts.ResumeID != "" {
		m.replay()
	}
	return nil
}

// recoveryNotice says what a load recovered, in one line: the torn tail
// it cut and the runs it marked interrupted. A load that recovered
// nothing has no notice.
func recoveryNotice(info protocol.SessionInfo) string {
	var recovered []string
	if info.RecoveredTail {
		recovered = append(recovered, "a torn tail was cut")
	}
	if runs := len(info.InterruptedRuns); runs > 0 {
		recovered = append(recovered, fmt.Sprintf("%d interrupted run(s) marked", runs))
	}
	if len(recovered) == 0 {
		return ""
	}
	return "the session was recovered: " + strings.Join(recovered, ", ")
}

// The scrollback a resumed session shows: the size of the page the repl
// asks for, and the most items it renders of them. The two are the
// server's history limits, the page a session/history call answers
// with and the largest one it may ask for.
const (
	historyPage  = protocol.DefaultHistoryLimit
	historyItems = protocol.MaxHistoryLimit
)

// replay renders the scrollback of the session the repl resumed, oldest
// first, so the person reads the conversation the session already had.
//
// The history stays the server's: nothing here goes back to the model.
// The pages are read backwards from the newest item, and a session with
// more items than the cap says that older history is not shown instead
// of paging through all of it.
func (m *machine) replay() {
	var items []protocol.HistoryItem
	truncated := false
	for {
		params := protocol.HistoryParams{SessionID: m.sessionID, Limit: historyPage}
		if len(items) > 0 {
			params.BeforeSeq = items[0].Seq
		}
		var page protocol.HistoryResult
		if err := m.conn.call(protocol.MethodSessionHistory, params, &page); err != nil {
			m.r.warning("the session's history could not be read: " + err.Error())
			return
		}
		if len(page.Items) == 0 {
			break
		}
		items = append(page.Items, items...)
		if !page.HasMore {
			break
		}
		if len(items) >= historyItems {
			truncated = true
			break
		}
	}
	if len(items) > historyItems {
		items = items[len(items)-historyItems:]
		truncated = true
	}
	if truncated {
		m.r.notice("older history is not shown")
	}
	for _, item := range items {
		m.replayItem(item)
	}
}

// replayItem renders one item of a resumed session's history.
//
// An item the transcript has no place for — a run's boundary, a
// permission decision, a job's — is skipped: the transcript shows the
// conversation, and a compaction is the one structural change worth a
// line.
//
// The text is the session's, not this terminal's to obey: the replay
// strips its escape sequences before rendering it, the way the live stream
// strips what arrives, so a recorded sequence is not painted again.
// Stripping escapes is all this does; whatever else the text carries is
// the renderer's, on the replay as on the live path.
func (m *machine) replayItem(item protocol.HistoryItem) {
	switch item.Type {
	case "message":
		switch item.Role {
		case "user":
			m.replayUser(item)
		case "assistant":
			m.r.reply(ansi.Strip(item.Text))
		}
	case "tool_call":
		m.r.toolCall(item.Tool, item.Summary)
	case "tool_result":
		m.r.toolResult(item.Tool, item.Outcome, item.Text, 0)
	case "compaction":
		m.r.notice("the view was compacted: %s", sanitizeText(item.Text))
	}
}

// replayUser renders a user-role message the way the live transcript
// drew it.
//
// A line the person typed is the prompt the session ran; a line that
// rode a run — a steer, an interrupt or a follow-up — is the steer's
// echo, which is how the live path draws the one of them it sends. Any
// other origin is a line nobody typed — a job's result, a feed's
// message, a hook's step, the session's own notice — and the live
// transcript draws none of them, so the replay draws none either. An
// item with no origin at all is a record older than the origin or a
// binary that does not report it, and it reads as the prompt it always
// did.
func (m *machine) replayUser(item protocol.HistoryItem) {
	switch item.Origin {
	case "", "user":
		m.r.userPrompt(sanitizeText(item.Text))
	case "steer", "interrupt", "follow_up":
		m.r.steerPrompt(sanitizeText(item.Text))
	}
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
	// A layout receives the server's diagnostics through a writer that
	// holds partial lines; the plain path keeps the process's stderr.
	var diagnostics *layoutStderr
	if m.r != nil && m.r.layout != nil {
		diagnostics = &layoutStderr{l: m.r.layout}
		cmd.Stderr = diagnostics
	} else {
		cmd.Stderr = os.Stderr
	}
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
	wait := cmd.Wait
	if diagnostics != nil {
		procWait := wait
		wait = func() error {
			// The copy goroutine is done once Wait returns, so the line
			// it held is final here, newline or not.
			err := procWait()
			diagnostics.flush()
			return err
		}
	}
	return stdin, stdout, wait, nil
}

// layoutStderr writes through a layout, so a writer outside the run
// loop lands above the footer without touching the renderer's state. A
// pipe's write is not a row: a line split across chunks is held until
// its newline lands, so one diagnostic reads as one row.
type layoutStderr struct {
	l *layout
	// held is the partial line the pipe has not finished.
	held string
}

func (w *layoutStderr) Write(p []byte) (int, error) {
	w.held += string(p)
	for {
		i := strings.IndexByte(w.held, '\n')
		if i < 0 {
			break
		}
		w.l.write(w.held[:i+1])
		w.held = w.held[i+1:]
	}
	return len(p), nil
}

// flush writes the line the pipe ended without: a server's last
// diagnostic may carry no newline. The waiter calls it, after the copy
// goroutine is done, so no write races the hold.
func (w *layoutStderr) flush() {
	if w.held == "" {
		return
	}
	w.l.write(w.held)
	w.held = ""
}

// newInput builds the line reader for the session. The editor is
// hardwired to the process's own stdio, so it only runs when the repl's
// input is exactly that terminal and the output is a terminal too —
// frames would land in a redirect otherwise; anything else reads plain
// lines.
func (m *machine) newInput() input {
	if f, ok := m.in.(interface{ Fd() uintptr }); ok &&
		term.IsTerminal(int(f.Fd())) && f.Fd() == os.Stdin.Fd() &&
		termIsTerminal(m.out) {
		if e := newEditor(m); e != nil {
			return e
		}
		if m.r.layout != nil {
			// The pinned layout belongs to the editor. Without it the
			// transcript goes back to the plain stream, where a
			// terminal without an editor reads from.
			m.r.layout.leave()
			m.r.layout = nil
		}
	}
	return &plainInput{scanner: bufio.NewScanner(m.in), write: m.r.write}
}

// greet says the session is open.
func (m *machine) greet() {
	m.r.notice("poo repl — /help for the commands, /quit to leave")
}

// commandName returns the command a line names and its argument, or an
// empty name when the line is a prompt. A command is one of the names
// the repl knows, whole, or one of the names that takes an argument and
// a space after it.
//
// One reader decides what a command is: the dispatcher below and the
// line typed while a run works agree, so a line that would be a prompt
// on an idle session steers a working one.
func commandName(line string) (name, arg string) {
	if strings.HasPrefix(line, "/model ") {
		return "/model", strings.TrimSpace(strings.TrimPrefix(line, "/model "))
	}
	if strings.HasPrefix(line, "/thinking ") {
		return "/thinking", strings.TrimSpace(strings.TrimPrefix(line, "/thinking "))
	}
	switch line {
	case "/quit", "/q", "/exit", "/help", "/h", "?", "/less", "/model", "/models", "/cancel", "/thinking":
		return line, ""
	}
	return "", ""
}

// command runs one input line. The bool says the repl is done, and
// the code is the exit code it takes.
func (m *machine) command(line string, input input) (int, bool) {
	name, arg := commandName(line)
	switch name {
	case "/quit", "/q", "/exit":
		return 0, true
	case "/help", "/h", "?":
		m.r.notice(`/quit            leave the repl
/less            read the transcript again in less
/model           list the session's models, numbered
/model <text>    switch to the model the text matches: one match switches,
                 several list ranked and numbered
/model <number>  switch to a number of the last list
/thinking        cycle the reasoning level (Shift+Tab)
/thinking <lvl>  set the reasoning level: off, minimal, low, medium, high,
                 xhigh or a token budget
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
		if arg == "" {
			m.listModels(m.catalogNames(), "/model <text> filters this list, /model <number> picks from it")
			return 0, false
		}
		m.switchModel(arg)
		return 0, false
	case "/cancel":
		m.cancelRun(m.activeRun())
		return 0, false
	case "/thinking":
		m.thinking(arg)
		return 0, false
	}
	if err := m.startRun(line); err != nil {
		m.r.failure(err.Error())
		if isShutdown(err) {
			return 1, true
		}
	}
	return 0, false
}

// switchModel moves the session's chat role to the model a person named.
//
// The name is what they remember, not what the catalog says: an exact
// name switches as it always has, and anything else is a fragment that
// matches the catalog. One match is the switch, several are a list to
// pick from, and a bare number takes a number off the last list.
func (m *machine) switchModel(fragment string) {
	if fragment == "" {
		m.r.failure("which model? /model lists them")
		return
	}
	// A bare number picks from the last listing. A catalog that holds a
	// model by that number keeps it reachable by name.
	if n, ok := pickIndex(fragment); ok {
		if n >= 1 && n <= len(m.lastMatches) {
			m.configureModel(m.lastMatches[n-1])
			return
		}
		if _, named := m.catalog[fragment]; !named {
			m.failureNoPick(n)
			return
		}
	}
	if _, ok := m.catalog[fragment]; ok {
		m.configureModel(fragment)
		return
	}
	matches := rankModels(m.catalogNames(), fragment, m.status.model)
	switch len(matches) {
	case 0:
		m.lastMatches = nil
		m.r.failure(fmt.Sprintf("no model matches %q — /model lists them", fragment))
	case 1:
		m.configureModel(matches[0])
	default:
		m.listModels(matches, "/model <number> picks one of these")
	}
}

// pickIndex reads a listing's mark: all its digits, or no number at all.
func pickIndex(s string) (int, bool) {
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// failureNoPick says why a bare number picked nothing.
func (m *machine) failureNoPick(n int) {
	if len(m.lastMatches) == 0 {
		m.r.failure(fmt.Sprintf("there is no /model list to pick %d from — /model <text> searches the catalog", n))
		return
	}
	m.r.failure(fmt.Sprintf("the last /model list has %d model(s) — /model <number> takes 1-%d",
		len(m.lastMatches), len(m.lastMatches)))
}

// listModels writes models as a numbered list, marks the one the session
// runs at, says how to pick out of it, and keeps it for the number.
func (m *machine) listModels(names []string, hint string) {
	m.lastMatches = names
	for i, name := range names {
		shown := name
		if name == m.status.model {
			shown += " ←"
		}
		m.r.notice("%d. %s · %s", i+1, shown, m.catalog[name].ModelID)
	}
	if hint != "" {
		m.r.notice("%s", hint)
	}
}

// configureModel points the session's chat role at a model the catalog
// holds, and takes the new model's window for the statusline.
//
// The switch is the wire's own: session/configure changes the models
// between runs, and the answer names what the session talks to now.
// A name the catalog does not hold is the server's to refuse.
func (m *machine) configureModel(name string) {
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
		m.status.setContextLimit(info.ContextLimit)
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
		next, ok, notice := m.nextThinkingLevel()
		if !ok {
			m.r.notice("%s", notice)
			return
		}
		level = next
	}
	if err := m.setThinking(level); err != nil {
		m.r.failure(err.Error())
		return
	}
	m.r.notice("thinking → %s", levelName(m.status.level))
}

// nextThinkingLevel returns the level after the session's, wrapping at the
// end of the list, and whether the model has a list at all.
//
// The order is the model's own, lowest first. A model that names no levels
// is climbed through the ones poo knows when its reasoning takes named
// levels; a model controlled by a token budget, or by nothing, has no list
// to climb, and the second result is false with a notice saying what to
// type, rather than a level the server refuses on every press.
func (m *machine) nextThinkingLevel() (level string, ok bool, notice string) {
	levels := namedLevels
	if info, ok := m.catalog[m.status.model]; ok {
		switch {
		case len(info.ReasoningLevels) > 0:
			levels = info.ReasoningLevels
		case info.ReasoningMode == protocol.ReasoningModeBudget:
			return "", false, "the model takes a token budget: /thinking <tokens>"
		case info.ReasoningMode == protocol.ReasoningModeNone:
			return "", false, "the model does not control its reasoning"
		}
	}
	for i, level := range levels {
		if strings.EqualFold(level, m.status.level) {
			return levels[(i+1)%len(levels)], true, ""
		}
	}
	return levels[0], true, ""
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

// armFrame starts the ticker that paints the preview a delta left
// dirty, at the stream's frame rate.
func (m *machine) armFrame() {
	m.frame = time.NewTicker(streamFrame)
}

// disarmFrame stops the frame ticker. The run loop reads its channel
// through frameTick, which is nil while no run streams, so an idle
// prompt wakes for nothing.
func (m *machine) disarmFrame() {
	if m.frame != nil {
		m.frame.Stop()
		m.frame = nil
	}
}

// frameTick is the channel the frame ticker ticks, nil while idle.
func (m *machine) frameTick() <-chan time.Time {
	if m.frame == nil {
		return nil
	}
	return m.frame.C
}

// startRun sends one prompt and reports the run it started. The events
// come to the main loop, which renders them until the settle.
func (m *machine) startRun(text string) error {
	m.r.userPrompt(text)
	// The hint stands from the echo until the first delta lands: a run
	// whose model is slow to speak still says it is working.
	m.r.hint()
	var res protocol.PromptResult
	if err := m.conn.call(protocol.MethodRunPrompt, protocol.PromptParams{
		SessionID: m.sessionID,
		Text:      text,
	}, &res); err != nil {
		m.r.takeHint()
		return err
	}
	m.runIDMu.Lock()
	m.runID = res.RunID
	m.runIDMu.Unlock()
	m.state = &runState{}
	m.steers = nil
	m.armFrame()
	return nil
}

// endRun closes the settled run and restores the prompt.
func (m *machine) endRun(input input) (int, bool) {
	m.disarmFrame()
	m.runIDMu.Lock()
	m.runID = ""
	m.runIDMu.Unlock()
	m.state = nil
	m.steers = nil
	if m.ask != nil {
		m.askMu.Lock()
		m.ask = nil
		m.askMu.Unlock()
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
func (m *machine) event(evt protocol.Event, runID string, state *runState) bool {
	switch evt.Type {
	case "message.delta":
		id, _ := evt.Payload["message_id"].(string)
		kind, _ := evt.Payload["kind"].(string)
		text, _ := evt.Payload["text"].(string)
		// The renderer owns the streamed block and its boundaries: a
		// message that leaves, or a write that interrupts, ends the
		// block there.
		m.r.streamDelta(id, kind, text)
	case "tool.call":
		// The call decorates the transcript below the message it
		// interrupted; the call's own write settles that message first.
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
			// The run's own requests are the ones the statusline
			// reports: a background job's request rides the same
			// connection and counts against its own fill.
			if json.Unmarshal(raw, &usage) == nil && usage.RunID == runID {
				m.status.setTokens(usage.InputTokens)
			}
		}
	case "turn.ended":
		state.turns++
	case "run.settled":
		if id, _ := evt.Payload["run_id"].(string); id != "" && id == runID {
			reason, _ := evt.Payload["reason"].(string)
			detail := settleDetail(evt.Payload)
			// The settle's usage is the run's total, the sum of every
			// request's count, and the settle line is where the run's own
			// accounting belongs. The statusline is not: the usage events
			// already put the last request's fill there, and feeding the
			// sum in would show a context fuller than the window.
			//
			// A run whose endpoint reported no usage emits no usage
			// event either, so the statusline keeps the last fill it
			// measured: that is the closest estimate of the window the
			// next request asks for, and the settle's total is not.
			input, output := 0, 0
			if detail.Usage != nil {
				input, output = detail.Usage.InputTokens, detail.Usage.OutputTokens
			}
			m.r.settle(reason, state.turns, input, output)
			m.reportUndelivered(detail.UndeliveredInput)
			return true
		}
	}
	return false
}

// reportUndelivered says what a settle dropped: the inputs it accepted
// but never delivered, the ones that were queued when the run stopped
// taking turns. The notice names what the person wrote, when the
// acceptance's answer named the input.
func (m *machine) reportUndelivered(dropped []protocol.UndeliveredInput) {
	for _, u := range dropped {
		said := u.InputID
		if text, ok := m.steers[u.InputID]; ok {
			said = fmt.Sprintf("%q", text)
		}
		m.r.notice("the run settled before %s was delivered; send it again", said)
	}
}

// settleDetail reads a settle event's detail through the protocol's
// own shape: the payload is re-encoded and decoded, so a field the
// contract renames is a compile error here, not a settle line and a
// delivery notice that silently read nothing.
//
// A detail that will not decode is the zero detail: the settle line
// renders the reason alone, and no notice claims an input was dropped.
func settleDetail(payload map[string]any) protocol.SettleDetail {
	var detail protocol.SettleDetail
	raw, err := json.Marshal(payload["detail"])
	if err != nil {
		return detail
	}
	json.Unmarshal(raw, &detail)
	return detail
}

// flushPending renders what the deltas had buffered when the session
// ends, so a run cut off mid-message still leaves its reply: the loop's
// ways out — a quit, a lost server, a failed read — all pass here.
func (m *machine) flushPending() {
	if m.state != nil {
		m.r.finish()
	}
}

// handleEvent renders one event of the session's run, and reports when
// the run it belongs to ends the session. The main loop and the ask's
// drain both step through events here, so a change to dispatch or to
// endRun's contract lands in one place.
func (m *machine) handleEvent(evt protocol.Event, input input) (int, bool) {
	if m.state == nil {
		// Nothing of the session's own streams: a job's or a feed's
		// event is not the conversation.
		return 0, false
	}
	if m.event(evt, m.activeRun(), m.state) {
		return m.endRun(input)
	}
	return 0, false
}

// handleAsk takes one permission ask. The reader hands events and
// asks to separate channels, so the main loop's select takes them in
// whatever order it finds ready: an ask can come up while the deltas
// that reached the connection before it are still queued. Those drain
// here first — the reply so far renders whole, above the question,
// never split by it, and a settle among them ends the run the usual
// way.
func (m *machine) handleAsk(req askRequest, input input) (int, bool) {
	for {
		select {
		case evt, ok := <-m.conn.events:
			if !ok {
				return m.gone(), true
			}
			if code, done := m.handleEvent(evt, input); done {
				return code, true
			}
		default:
			m.openAsk(req, input)
			return 0, false
		}
	}
}

// openAsk shows a permission ask. The prompt it shows is the line the
// answer is typed behind, so the y or n reaches the session the same
// way every other line does.
func (m *machine) openAsk(req askRequest, input input) {
	m.askMu.Lock()
	m.ask = &req
	m.askMu.Unlock()
	m.r.write(ansiYellow + "⚙ permission · " + sanitizeLine(req.ask.Tool) + ansiReset + "\n")
	if summary := sanitizeText(req.ask.Summary); summary != "" {
		m.r.write(ansiDim + summary + ansiReset + "\n")
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
	m.askMu.Lock()
	m.ask = nil
	m.askMu.Unlock()
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

// runState is the run's accounting: the turns it took. The messages
// themselves stream through the renderer as the deltas arrive, so
// there is nothing here to render at the end.
type runState struct {
	// turns is how many turns the run took: the wire's settle event
	// does not carry the count, the turn events do.
	turns int
}

// The errors the run loop reports to the caller.
var (
	errServerClosed = errors.New("the server closed the connection")
)

// isShutdown says whether a failure means the server is gone.
func isShutdown(err error) bool {
	return errors.Is(err, errServerClosed)
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
