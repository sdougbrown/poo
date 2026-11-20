package repl

// editorRaceHooks are the seams the read/close handshake is tested at.
//
// A hook is called at a named boundary, and it blocks the caller until
// it returns. A test can therefore hold a read between the
// handshake's claim and the editor, a read between the editor and the
// handshake's report, or a close between its abort and its wait, and
// pin the order the two make their moves in.
type editorRaceHooks struct {
	// BeforeReadline is called on the reader's goroutine after a read
	// claims the handshake and before it enters the editor.
	BeforeReadline func()
	// AfterReadline is called on the reader's goroutine after a read
	// leaves the editor and before the handshake reports it.
	AfterReadline func()
	// BeforeWait is called on a close's goroutine just before it parks
	// in the handshake's wait. The handshake's mutex is held: a hook
	// must not block on the handshake.
	BeforeWait func()
}

// SetEditorRaceHooks installs the editor's race hooks and returns a
// function that removes them.
//
// A session without them never calls anything, so the editor's own
// path does not change.
func (i *editorInput) SetEditorRaceHooks(hooks *editorRaceHooks) func() {
	i.readMu.Lock()
	prev := i.raceHooks
	i.raceHooks = hooks
	i.readMu.Unlock()
	return func() {
		i.readMu.Lock()
		i.raceHooks = prev
		i.readMu.Unlock()
	}
}
