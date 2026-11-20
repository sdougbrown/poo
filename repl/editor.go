package repl

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"

	"github.com/reeflective/readline"
	"github.com/reeflective/readline/inputrc"
	"golang.org/x/term"
)

// editorInput reads lines through the reeflective readline editor.
//
// The editor is hardwired to the process's own stdio: it reads keys
// from os.Stdin and renders to os.Stdout, and newInput only builds it
// when the repl's input is exactly that. Any other terminal falls back
// to the plain reader.
type editorInput struct {
	m  *machine
	rl *readline.Shell

	// restore puts the terminal back the way newEditor found it. The
	// editor enters and leaves raw mode around every read, but a read
	// still blocked at session end leaves raw mode behind: the close
	// restores the state captured at construction.
	restore func() error

	// mu guards the prompt and the draft. The session's loop sets the
	// prompt and the editor's goroutine mirrors it into the frame
	// snapshot the frame renders; the editor's goroutine reports the
	// draft.
	mu          sync.Mutex
	promptText  string
	framePrompt string
	line        []rune

	// readMu guards the read/close handshake. A session that ends
	// while the editor holds a read must not leave its goroutine in
	// the terminal. Run returns behind close, and whatever tears the
	// terminal down after — a test closing its pty — races a reader
	// still inside. readHeld reports the one read in flight; the
	// session's gate is what keeps it to one. closed reports that
	// close has shut the read gate. close aborts the held read and
	// waits until it has left; a read that arrives after close is the
	// interrupt, without entering the terminal at all. raceHooks holds
	// the seams the handshake is tested at, installed by
	// SetEditorRaceHooks; readMu guards the pointer.
	readMu    sync.Mutex
	readCond  *sync.Cond
	readHeld  bool
	closed    bool
	raceHooks *editorRaceHooks
}

// newEditor builds the line editor for the session.
func newEditor(m *machine) *editorInput {
	i := &editorInput{m: m, promptText: promptMark, framePrompt: promptMark}
	i.readCond = sync.NewCond(&i.readMu)

	sh := readline.NewShell()
	i.rl = sh

	// The session reads the prompt behind the ask's line or the mark;
	// prompt() swaps it live. The statusline is not part of it: the
	// layout paints the footer's bottom row itself.
	sh.Prompt.Primary(func() string {
		i.mu.Lock()
		defer i.mu.Unlock()
		return i.framePrompt
	})
	sh.Prompt.Secondary(func() string {
		return ansiDim + "  ⋯ " + ansiReset
	})

	// Vim mode, the way the editor before this one read: insert mode at
	// the prompt, Escape back to the command keymap.
	sh.Keymap.SetMain("vi-insert")

	// The editor probes the cursor position with an ESC[6n query and
	// derives its geometry from the answer. The query and its answer
	// ride the same stream the keys do, and the answer's row lags the
	// frame that asked, so the pinned footer sizes the input area
	// itself and the probe stays off.
	sh.Config.Set("cursor-position-probe", false)

	if m.opts.HistoryFile != "" {
		// AddFromFile registers the source even when the file is new:
		// the first accepted line creates it, and an unreadable one
		// reads as an empty history, not a failure.
		sh.History.AddFromFile("poo", m.opts.HistoryFile)
	}

	// Escape stops a run, and idle it is the vim command keymap: the
	// editor itself dispatches bare Escape through the bind named
	// vi-movement-mode, so the session's own command takes that name
	// and does both jobs. Without a run working, the builtin's switch
	// to the command keymap runs unchanged.
	escape := func() {
		if runID := m.activeRun(); runID != "" {
			select {
			case m.esc <- runID:
			default:
			}
			return
		}
		sh.ViCommandMode()
	}

	// Ctrl-J is a line break: a newline into the buffer, on any keymap
	// that edits text. Enter keeps accept-line, so a draft of several
	// lines submits whole.
	newline := func() {
		sh.Cursor().InsertAt(inputrc.Newline)
	}

	cycle := func() {
		// The Shift+Tab reasoning cycle, the way pi cycles its thinking
		// level: a wire call, so it runs only while the session reads.
		// A run in flight takes the key, the way the filter gate the
		// plain reader used to.
		if m.activeRun() != "" {
			return
		}
		level, ok, notice := m.nextThinkingLevel()
		if !ok {
			m.r.notice("%s", notice)
		} else if err := m.setThinking(level); err != nil {
			m.r.failure(err.Error())
		} else {
			i.prompt(m.prompt())
		}
	}

	sh.Keymap.Register(map[string]func(){
		"vi-movement-mode": escape,
		"poo-newline":      newline,
		"poo-cycle":        cycle,
	})

	// Escape: an exact bind is what makes the keymap engine dispatch
	// bare Escape at all — a key that also starts longer sequences is
	// otherwise held as a prefix and dropped. The arrow keys and the
	// Shift+Tab cycle still match, longer first.
	for _, keymapName := range []string{"emacs", "vi-insert", "vi-command"} {
		sh.Config.Bind(keymapName, inputrc.Unescape(`\e`), "vi-movement-mode", false)
		sh.Config.Bind(keymapName, inputrc.Unescape(`\e[Z`), "poo-cycle", false)
	}

	// Ctrl-J: a line break wherever Enter used to share the binding.
	for _, keymapName := range []string{"emacs", "vi-insert"} {
		sh.Config.Bind(keymapName, inputrc.Unescape(`\C-j`), "poo-newline", false)
	}

	// The editor reads the process's stdin; raw mode is on from
	// construction, so keys typed before the first read wait in the
	// terminal as typed instead of cooked. The state captured here is
	// the cooked terminal a close puts back.
	if f, ok := m.in.(interface{ Fd() uintptr }); ok {
		fd := int(f.Fd())
		if state, err := term.GetState(fd); err == nil {
			i.restore = func() error { return term.Restore(fd, state) }
		}
		if _, err := term.MakeRaw(fd); err != nil {
			// A terminal that will not go raw leaves the reading to
			// the plain reader: the conversation works without the
			// editor's key handling.
			return nil
		}
		// The editor's frames render through the layout's own writer, so
		// a frame and the transcript never split a row, and the layout
		// is resized for the draft before every frame renders. Set only
		// once the editor is certain to run: the render output is
		// process-wide, and close is the only thing that clears it.
		if l := m.r.layout; l != nil {
			readline.SetRenderOutput(l.raw())
			sh.SetRefreshHook(i.refresh)
		}
		// The streamed rows and the editor's prompt wrap at the same
		// width, so the renderer reads it from the same place, live.
		m.r.liveWidth = func() int {
			if w, _, err := term.GetSize(fd); err == nil {
				return w
			}
			return -1
		}
	}

	return i
}

// refresh mirrors the draft the editor holds, and sizes the footer for
// it, before the editor's frame renders. A draft that wraps grows the
// footer before the editor needs the room, and the line's end shrinks
// it back.
func (i *editorInput) refresh() {
	line := []rune(*i.rl.Line())
	i.mu.Lock()
	i.line = append(i.line[:0], line...)
	// The frame renders one prompt for the whole frame: snapshot the
	// pending text here, where the footer is sized for it, so the
	// sizing and the primary printing read the same text.
	i.framePrompt = i.promptText
	promptText := i.framePrompt
	i.mu.Unlock()
	l := i.m.r.layout
	if l == nil {
		return
	}
	l.prepare(i.rowsFor(promptText, line))
}

// rowsFor is the rows the footer reserves for a prompt and a draft.
// It covers the rows they fill and one spare: the editor erases the rows
// below its line on every repaint, and the spare is what that erase
// spends instead of the line's own last row.
func (i *editorInput) rowsFor(prompt string, draft []rune) int {
	return promptRows(prompt, draft, i.m.r.layout.cols()) + 1
}

// promptRows is the rows a prompt and a draft fill in the footer, the
// editor's own line count: every explicit input line starts at the
// prompt's indent, and an exact width multiple still puts the cursor on
// a row of its own, the way the editor's own span math counts.
func promptRows(prompt string, draft []rune, width int) int {
	if width <= 0 {
		return 1
	}
	indent := ansi.StringWidth(prompt)
	rows := 0
	for _, line := range strings.Split(string(draft), "\n") {
		cells := indent + ansi.StringWidth(line)
		rows += cells/width + 1
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// prompt shows the prompt here and for the next read. It only publishes
// the text and wakes the editor: the refresh hook owns the terminal
// work — sizing, clearing, anchoring — on the editor's own goroutine,
// before the frame renders.
func (i *editorInput) prompt(p string) {
	i.mu.Lock()
	i.promptText = p
	i.mu.Unlock()
	i.rl.Keys.RequestRefresh()
}

// read reads one line, newlines inside a draft included. An end of
// input is io.EOF; an interrupt is readline.ErrInterrupt. A close is
// an interrupt for a read that starts after it; a read the close
// aborts in flight returns the editor's own end, io.EOF. A read that
// claimed the handshake before the close and enters the editor behind
// the abort ends the same way: the abort latches in the editor's key
// engine, so a read that starts behind it reads the end instead of
// parking in the terminal.
func (i *editorInput) read() (string, error) {
	i.readMu.Lock()
	if i.closed {
		i.readMu.Unlock()
		return "", readline.ErrInterrupt
	}
	i.readHeld = true
	hooks := i.raceHooks
	i.readMu.Unlock()
	if hooks != nil && hooks.BeforeReadline != nil {
		hooks.BeforeReadline()
	}
	line, err := i.rl.Readline()
	if hooks != nil && hooks.AfterReadline != nil {
		hooks.AfterReadline()
	}
	i.readMu.Lock()
	i.readHeld = false
	i.readCond.Broadcast()
	i.readMu.Unlock()
	return strings.TrimSuffix(line, "\n"), err
}

// close puts the terminal back. A read still blocked at session end
// holds raw mode; the state captured at construction is what a cooked
// terminal was. The blocked read is aborted first: without it, a
// session that ended while the editor held the read leaves the
// reader parked on the terminal, where it reads the next session's
// keystrokes. The close is not done until that reader has left: Run
// returns behind it, so a reader still in the terminal races
// whatever tears the terminal down after the session. The wait has
// no deadline — a reader that will not leave holds the exit with
// it. The abort does not stop a read that has not entered yet from
// entering, so the close also shuts the read gate, and a read that
// starts after it is the interrupt. A second close finds the gate
// shut and the reader gone, and puts the same captured terminal
// back.
func (i *editorInput) close() {
	readline.SetRenderOutput(nil)
	i.readMu.Lock()
	i.closed = true
	i.rl.Keys.Abort()
	for i.readHeld {
		if h := i.raceHooks; h != nil && h.BeforeWait != nil {
			h.BeforeWait()
		}
		i.readCond.Wait()
	}
	i.readMu.Unlock()
	if i.restore != nil {
		_ = i.restore()
	}
}
