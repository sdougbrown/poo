# readline — a patched copy of reeflective/readline v1.3.0

The repl's editor reads this module through a `replace` directive in
`go.mod`. Upstream is hardwired to the process's own stdio: it reads
keys from `os.Stdin`, renders frames to `os.Stdout`, and offers no way
for a host that draws around the editor to take part. The pinned footer
needs exactly that, so the copy carries three small patches:

- `readline.go`, `internal/term/output.go` — `SetRenderOutput(w)` routes
  rendered frames through the layout's own writer, so a frame and the
  transcript never split a row.
- `readline.go`, `shell.go` — `Shell.SetRefreshHook(f)` runs a function
  before every UI refresh: the repl sizes the footer for the draft
  before the frame renders.
- `vim.go` — `Shell.ViCommandMode()` exports the builtin keymap switch, so
  a host whose own Escape command delegates the idle case to it.
- `internal/display/refresh.go` — with the cursor-position probe off,
  the editor does not know its start row, and the erase it makes below
  the input line spent the rows the footer owns. When the probe is off,
  the erase is skipped; the footer reserves a spare row of its own for
  it.

The patch is meant for upstream: the two exported hooks and the probe
fix are general, and this copy goes away once a release carries them.
Keep the diff against v1.3.0 exactly the four changes above, plus
`inputrc/bind.go`'s three-line gofmt repair — upstream's own file is not
gofmt-clean, and the CI's Format step reads the whole module.
