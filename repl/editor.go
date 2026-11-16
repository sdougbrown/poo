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

	// mu guards the prompt and the draft. The editor's goroutine reports
	// the draft, and the session's loop sets the prompt.
	mu         sync.Mutex
	promptText string
	line       []rune
	pos        int
}

// newEditor builds the line editor for the session.
func newEditor(m *machine) *editorInput {
	i := &editorInput{m: m, promptText: promptMark}

	sh := readline.NewShell()
	i.rl = sh

	// The session reads the prompt behind the statusline, the ask's
	// line, or a steer's mark; prompt() swaps it live.
	sh.Prompt.Primary(func() string {
		i.mu.Lock()
		defer i.mu.Unlock()
		return i.promptText
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
		if hist, err := readline.NewHistoryFromFile(m.opts.HistoryFile); err == nil {
			sh.History.Add("poo", hist)
		}
	}

	// Escape stops a run, and idle it is the vim command keymap: the
	// editor itself dispatches bare Escape through the bind named
	// vi-movement-mode, so the session's own command takes that name
	// and does both jobs. Without a run working, it hands the editor
	// back its command keymap the way the builtin did.
	escape := func() {
		if runID := m.activeRun(); runID != "" {
			select {
			case m.esc <- runID:
			default:
			}
			return
		}
		sh.Selection().Reset()
		sh.Iterations.Reset()
		sh.Buffers.Reset()
		sh.Hint.Reset()
		if sh.Keymap.Main() == "vi-insert" && !sh.Cursor().AtBeginningOfLine() {
			sh.Cursor().Dec()
		}
		sh.Keymap.SetLocal("")
		sh.Keymap.SetMain("vi-command")
	}

	// Ctrl-J is a line break: a newline into the buffer, on any keymap
	// that edits text. Enter keeps accept-line, so a draft of several
	// lines submits whole.
	newline := func() {
		sh.Cursor().InsertAt(inputrc.Newline)
	}

	cycle := func() {
		// The Shift+Tab reasoning cycle, the way pi cycles its thinking
		// level; the cycle is a wire call, and it runs while the prompt
		// is being read: the session has no run going then, so the
		// server has nothing else to answer.
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

	// The editor's frames render through the layout's own writer, so a
	// frame and the transcript never split a row, and the layout is
	// resized for the draft before every frame renders.
	if l := m.r.layout; l != nil {
		readline.SetRenderOutput(l.raw())
		sh.SetRefreshHook(i.refresh)
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
	pos := i.rl.Cursor().Pos()
	i.mu.Lock()
	i.line = append(i.line[:0], line...)
	i.pos = pos
	promptText := i.promptText
	i.mu.Unlock()
	l := i.m.r.layout
	if l == nil {
		return
	}
	before := l.footerRows()
	if !l.ensure(i.rowsFor(promptText, line)) {
		return
	}
	// A paint that needed more rows than the old footer had ran past
	// the footer and left rows in the region. A history recall paints
	// its whole line at once, so it is the usual case. Those rows belong
	// to no stream; clear them so the draft is not beside a copy of
	// itself.
	if painted := promptRows(promptText, line, l.cols()); painted > before {
		l.clearBelowLast()
	}
	// The footer moved, so the editor's line ends at a new row. Put the
	// cursor there, and let the editor erase and draw the line from the
	// new prompt row.
	// The cursor is left where the editor's own frame put it: the
	// layout's writes save and restore it, and the editor's repaint
	// math moves it relative to itself. Placing it here would fight
	// that arithmetic.
}

// rowsFor is the rows the footer reserves for a prompt and a draft.
// It covers the rows they fill and one spare: the editor erases the rows
// below its line on every repaint, and the spare is what that erase
// spends instead of the line's own last row.
func (i *editorInput) rowsFor(prompt string, draft []rune) int {
	return promptRows(prompt, draft, i.m.r.layout.cols()) + 1
}

// promptRows is the rows a prompt and the draft before the cursor fill
// in the footer, the editor's own line count.
func promptRows(prompt string, draft []rune, width int) int {
	if width <= 0 {
		return 1
	}
	cells := ansi.StringWidth(prompt) + ansi.StringWidth(string(draft))
	rows := cells / width
	if cells%width != 0 {
		rows++
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// prompt shows the prompt here and for the next read. A read in flight
// repaints on the request; otherwise the next read opens behind it.
func (i *editorInput) prompt(p string) {
	i.mu.Lock()
	i.promptText = p
	draft := append([]rune(nil), i.line...)
	i.mu.Unlock()
	if l := i.m.r.layout; l != nil {
		l.ensure(i.rowsFor(p, draft))
		// The layout may have moved the terminal's rows under the
		// editor (a resize, a footer that grew): anchor the cursor on
		// the row the draft's own cursor reads on, and let the editor
		// repaint from there.
		pos := i.pos
		if pos > len(draft) {
			pos = len(draft)
		}
		l.place(promptRows(p, draft[:pos], l.cols()) - 1)
	}
	i.rl.Keys.RequestRefresh()
}

// read reads one line, newlines inside a draft included. An end of
// input is io.EOF; an interrupt is readline.ErrInterrupt.
func (i *editorInput) read() (string, error) {
	line, err := i.rl.Readline()
	return strings.TrimSuffix(line, "\n"), err
}

// close puts the terminal back. A read still blocked at session end
// holds raw mode; the state captured at construction is what a cooked
// terminal was.
func (i *editorInput) close() {
	readline.SetRenderOutput(nil)
	if i.restore != nil {
		_ = i.restore()
	}
}
