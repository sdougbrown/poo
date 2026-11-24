package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/repl"
	"github.com/sdougbrown/poo/session"
)

// replOptions are the flags of `poo repl`: the serve flags, plus the two
// that reopen a logged session instead of starting one.
type replOptions struct {
	serveOptions
	// cont opens the newest session in the working directory.
	cont bool
	// resume names the session to open.
	resume string
}

// bind reads the repl's flags out of a parsed invocation.
func (o *replOptions) bind(inv clicontract.Invocation) {
	o.serveOptions.bind(inv)
	o.cont = inv.Bools["continue"]
	o.resume = inv.Flags["resume"]
}

// handleRepl executes `poo repl`.
//
// The flags are the serve flags, and they pass through to the server
// the repl starts: the repl holds no configuration of its own, so one
// flag set says what both sides run. The parse and the validation run
// here too, so a bad flag is refused before the server starts.
//
// --continue and --resume open a session the store already holds. The
// id is resolved here, before the server starts, so a session that
// cannot be continued is a refusal rather than a repl that failed to
// open one; the server loads the session the id names.
func handleRepl(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o replOptions
	o.bind(inv)
	if !o.validate("poo repl", stderr) {
		return exit{usageExitCode}
	}
	// The conflict is a refusal about the line itself, so it comes
	// before the settings resolve: the front door refuses the same way,
	// and a settings error would hide the conflict.
	if o.cont && o.resume != "" {
		fmt.Fprintf(stderr, "poo repl: %v\n", errContinueResume)
		return exit{usageExitCode}
	}

	workingDir, err := workingDirectory(o.workingDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo repl: %v\n", err)
		return exit{usageExitCode}
	}

	// The repl resolves its settings once: the profile word and the
	// session it opens read the same document.
	settings, err := o.settings(workingDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo repl: %v\n", err)
		return exit{usageExitCode}
	}
	serverArgs := o.serverArgs(inv, settings)
	resumeID, err := o.resumeID(workingDir, settings)
	if err != nil {
		fmt.Fprintf(stderr, "poo repl: %v\n", err)
		// A store failure is operational: the line the person typed is
		// not at fault, and the same failure is operational on the
		// sessions commands. Every other refusal is a usage error.
		var storeErr *storeError
		if errors.As(err, &storeErr) {
			return exit{operationalExitCode}
		}
		return exit{usageExitCode}
	}
	if o.cont && resumeID == "" {
		fmt.Fprintf(stderr, "poo: no session to continue in %s\n", workingDir)
		return exit{usageExitCode}
	}
	return exit{repl.Run(repl.Options{
		ServerArgs:  serverArgs,
		WorkingDir:  workingDir,
		ResumeID:    resumeID,
		HistoryFile: historyPath(),
	})}
}

// errContinueResume is the refusal both doors share: each checks it
// before its settings resolve, so a line that names both flags is
// refused for the flags, whatever shape its settings are in.
var errContinueResume = errors.New("--continue and --resume are alternatives: give one")

// serverArgs returns the argv the repl hands its server: the serve
// subcommand, the flags the line parsed spelled a second time for the
// child, and the profile word the settings name.
func (o *replOptions) serverArgs(inv clicontract.Invocation, settings *config.Settings) []string {
	args := append([]string{repl.ServeSubcommand}, renderFlags(inv, serverFlagSpecs())...)
	return append(args, o.profileArg(inv, settings)...)
}

// profileArg returns the --profile words the repl spells for the serve
// child, and nothing when there are none.
//
// The settings name the profile the repl runs under, distinct from the
// sessions a consumer drives: the settings' own profile is the spawned
// children's default, and the conversation the operator opens is not
// that. The flag wins over the setting the way it wins everywhere
// else, so the word is spelled only when the command named none and
// the setting holds one — an absent key leaves the child to resolve
// the settings' profile, the way it did before this key existed.
//
// A reopened session is not that conversation either: it runs under
// the profile its log records, so the word is spelled only for a
// session the repl starts, and spelling it for a reopen would make
// the child build a runtime the reopened session never runs.
func (o *replOptions) profileArg(inv clicontract.Invocation, settings *config.Settings) []string {
	if inv.Supplied["profile"] {
		return nil
	}
	if o.cont || o.resume != "" {
		return nil
	}
	if settings.ReplProfile != nil && *settings.ReplProfile != "" {
		return []string{"--profile=" + *settings.ReplProfile}
	}
	return nil
}

// storeError marks a failure of the session store itself: the
// directory the settings point at exists, but opening or reading it
// failed. The line the repl parsed is not at fault, so the exit is
// operational rather than a refusal.
type storeError struct{ err error }

func (e *storeError) Error() string { return e.err.Error() }

func (e *storeError) Unwrap() error { return e.err }

// resumeID returns the session the repl opens: the id --resume names, or
// the one --continue picks out of the store.
//
// No flag is the empty id, which opens a new session. So is --continue
// with nothing to continue: the caller refuses that case, rather than
// start a session nobody asked for.
//
// Either flag opens a session already on disk, so the session it opens is
// one the store holds: an id it does not hold, or a session that works in
// another directory, is refused here, before a server starts.
//
// --resume reads the session's own log, so a sibling that will not open is
// not this session's failure. --continue reads the store's listing: which
// session is the newest is only in the store's answer.
func (o *replOptions) resumeID(workingDir string, settings *config.Settings) (string, error) {
	if o.cont && o.resume != "" {
		return "", errContinueResume
	}
	if !o.cont && o.resume == "" {
		return "", nil
	}
	overrides, err := o.overrides()
	if err != nil {
		return "", err
	}
	if config.SessionMode(settings, overrides) == "ephemeral" {
		return "", errors.New("--continue and --resume open a logged session, and this command runs without one: drop --ephemeral, or the settings' session.mode")
	}
	dir, err := config.SessionDirectory(settings, overrides)
	if err != nil {
		return "", err
	}
	if o.resume != "" {
		return namedSession(dir, o.resume, workingDir)
	}
	// The store's own failure is operational, not a refusal: the
	// directory the settings point at is not the line's fault, and the
	// same failure is operational on the sessions commands.
	store, err := session.NewStore(dir)
	if err != nil {
		return "", &storeError{err}
	}
	defer store.Close()
	listings, err := store.List(context.Background())
	if err != nil {
		return "", &storeError{err}
	}
	listing, ok := pickContinue(listings, workingDir)
	if !ok {
		return "", nil
	}
	return listing.ID, nil
}

// namedSession returns the session --resume names, once the store holds it
// and it works where this repl does.
//
// The session's own log is what it reads, not the store's listing: the
// listing reads every session in the directory, and one of them being
// unreadable is not this session's failure.
//
// The session's tools run in the directory it recorded, while the
// statusline, the settings' rules and the decider read the one the repl
// runs in, so a session from another directory is refused rather than run
// half in each. storeDir is where the id was looked for, which is what a
// person needs to hear when it is not there.
//
// A session another process holds is not refused here: the load reports
// the lock, and a refusal there says the same thing with the session's
// own answer.
func namedSession(storeDir, id, workingDir string) (string, error) {
	meta, err := session.MetadataOf(storeDir, id)
	switch {
	case errors.Is(err, session.ErrSessionNotFound):
		return "", fmt.Errorf("no session %s in %s", id, storeDir)
	case errors.Is(err, session.ErrInvalidSessionID):
		return "", fmt.Errorf("%s is not a session id: an id is a plain name", id)
	case err != nil:
		return "", err
	}
	if !sameDir(meta.WorkingDir, workingDir) {
		return "", fmt.Errorf("session %s works in %s: resume it from there, or pass --working-dir %s",
			id, meta.WorkingDir, meta.WorkingDir)
	}
	return id, nil
}

// pickContinue returns the session --continue opens: the one whose last
// activity is latest among the sessions that work in dir and that no
// process holds.
func pickContinue(listings []session.Listing, dir string) (session.Listing, bool) {
	var newest session.Listing
	for _, listing := range listings {
		if listing.Locked || !sameDir(listing.WorkingDir, dir) {
			continue
		}
		if newest.ID == "" || listing.LastActivity.After(newest.LastActivity) {
			newest = listing
		}
	}
	return newest, newest.ID != ""
}

// sameDir reports whether two paths name the same directory.
//
// A session records the directory it worked in as it was resolved then,
// and a symbolic link may spell that directory another way: both sides
// are made absolute and resolved before they are compared. A path that
// will not resolve is compared as it is absolute, and an empty one names
// no directory at all.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return canonicalDir(a) == canonicalDir(b)
}

// canonicalDir resolves one side of a directory comparison.
func canonicalDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

// historyPath is where the repl keeps its input history between
// sessions. A cache directory the system does not name means no
// history, not a failure.
func historyPath() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	history := filepath.Join(cache, "poo", "repl-history-v2")
	// readline's history save does not create directories: the parent
	// is here, once, or the save fails quietly every session.
	if err := os.MkdirAll(filepath.Dir(history), 0o755); err != nil {
		return ""
	}
	return history
}
