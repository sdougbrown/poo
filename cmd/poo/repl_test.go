package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// TestTheReplHandsItsServerTheFlagsItParsed covers the repl's handoff: the
// repl runs `poo serve` as a child, so it spells every flag it parsed a
// second time. The child has to read the same values back, and a flag the
// handoff forgets is a flag the session never sees.
func TestTheReplHandsItsServerTheFlagsItParsed(t *testing.T) {
	app := pooApp(io.Discard)
	repl, err := clicontract.Parse(app, []string{
		"repl",
		"--settings", "settings.yaml",
		"--ephemeral",
		"--feeds-path", "one.yaml",
		"--feeds-path", "two.yaml",
		"--profile", "code",
		"--endpoint", "http://localhost:4000/v1",
		"--model-id", "test-model",
		"--context-limit", "8192",
		"--working-dir", "/tmp",
		"--executor", "local",
	})
	if err != nil {
		t.Fatalf("parsing the repl's flags: %v", err)
	}

	argv := append([]string{"serve"}, renderFlags(repl, serverFlagSpecs())...)
	server, err := clicontract.Parse(app, argv)
	if err != nil {
		t.Fatalf("the server refused the flags the repl spelled (%v): %v", argv, err)
	}

	for name := range repl.Supplied {
		if !server.Supplied[name] {
			t.Errorf("the handoff dropped --%s: %v", name, argv)
		}
	}
	for name, value := range repl.Flags {
		if server.Flags[name] != value {
			t.Errorf("--%s is %q for the server, want %q", name, server.Flags[name], value)
		}
	}
	for name, value := range repl.Bools {
		if server.Bools[name] != value {
			t.Errorf("--%s is %t for the server, want %t", name, server.Bools[name], value)
		}
	}
	for name, value := range repl.Ints {
		if server.Ints[name] != value {
			t.Errorf("--%s is %d for the server, want %d", name, server.Ints[name], value)
		}
	}
	for name, values := range repl.Repeated {
		if !slices.Equal(server.Repeated[name], values) {
			t.Errorf("--%s is %q for the server, want %q", name, server.Repeated[name], values)
		}
	}
}

// TestTheContinuePickerTakesTheNewestUnlockedSessionOfTheDirectory covers
// who --continue picks: the session with the latest activity whose working
// directory is the one the repl runs in, and not one another process
// holds.
func TestTheContinuePickerTakesTheNewestUnlockedSessionOfTheDirectory(t *testing.T) {
	dir := t.TempDir()
	elsewhere := t.TempDir()
	// A directory reached by another name is the same directory: a
	// session records the path it resolved, and the repl runs in the
	// path the person typed.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("linking the directory: %v", err)
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	listing := func(id, workingDir string, minute int, locked bool) session.Listing {
		return session.Listing{
			ID:           id,
			WorkingDir:   workingDir,
			LastActivity: at.Add(time.Duration(minute) * time.Minute),
			Locked:       locked,
		}
	}

	cases := []struct {
		name     string
		listings []session.Listing
		want     string
	}{
		{
			name: "the newest session of the directory wins",
			listings: []session.Listing{
				listing("older", dir, 1, false),
				listing("newer", dir, 5, false),
			},
			want: "newer",
		},
		{
			name: "a newer session elsewhere does not count",
			listings: []session.Listing{
				listing("here", dir, 1, false),
				listing("elsewhere", elsewhere, 9, false),
			},
			want: "here",
		},
		{
			name: "a locked session is not continued",
			listings: []session.Listing{
				listing("free", dir, 1, false),
				listing("locked", dir, 9, true),
			},
			want: "free",
		},
		{
			name: "another spelling of the directory is the same directory",
			listings: []session.Listing{
				listing("recorded", link, 3, false),
			},
			want: "recorded",
		},
		{
			name: "nothing in the directory to continue",
			listings: []session.Listing{
				listing("elsewhere", elsewhere, 9, false),
			},
			want: "",
		},
		{
			name: "everything in the directory is locked",
			listings: []session.Listing{
				listing("locked", dir, 9, true),
			},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := pickContinue(c.listings, dir)
			if got.ID != c.want {
				t.Errorf("the picker chose %q, want %q", got.ID, c.want)
			}
			if ok != (c.want != "") {
				t.Errorf("the picker reported %t for %q", ok, got.ID)
			}
		})
	}
}

// TestContinueReadsTheStore covers --continue against a store on disk: the
// sessions of two directories are listed, the newest of the repl's
// directory is held by another process, and the pick is the next one.
func TestContinueReadsTheStore(t *testing.T) {
	base := t.TempDir()
	sessionDir := filepath.Join(base, "sessions")
	settingsFile := theSettings(t, base, "http://localhost:4000/v1")
	here := filepath.Join(base, "here")
	there := filepath.Join(base, "there")
	for _, dir := range []string{here, there} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}

	now := time.Now()
	recordSession(t, sessionDir, "here-older", here, now.Add(-3*time.Hour))
	recordSession(t, sessionDir, "here-newest", here, now.Add(-1*time.Hour))
	recordSession(t, sessionDir, "there-recent", there, now.Add(-time.Minute))
	held, err := session.Acquire(session.LockPath(sessionDir, "here-newest"))
	if err != nil {
		t.Fatalf("taking the lock of the newest session: %v", err)
	}
	defer held.Release()

	o := replOptions{}
	o.settingsFile = settingsFile
	o.sessionDir = sessionDir
	o.cont = true
	id, err := o.resumeID(here)
	if err != nil {
		t.Fatalf("continuing: %v", err)
	}
	if id != "here-older" {
		t.Errorf("--continue opened %q, want here-older", id)
	}

	// A directory none of them worked in has nothing to continue, and
	// the repl is told so rather than opening a new session.
	empty := filepath.Join(base, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("creating %s: %v", empty, err)
	}
	id, err = o.resumeID(empty)
	if err != nil {
		t.Fatalf("continuing in an empty directory: %v", err)
	}
	if id != "" {
		t.Errorf("--continue in an empty directory opened %q, want no session", id)
	}
}

// recordSession writes one logged session into the store, in the working
// directory and at the time it names.
func recordSession(t *testing.T, dir, id, workingDir string, at time.Time) {
	t.Helper()
	store, err := session.NewStore(dir, session.WithClock(func() time.Time { return at }))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{
			Format:     poo.FormatVersion,
			SessionID:  id,
			Mode:       "logged",
			CreatedAt:  at.UTC().Format(time.RFC3339Nano),
			WorkingDir: workingDir,
			Profile:    "code",
			Model:      poo.SessionModel{ModelID: "test-model"},
		},
	}
	if _, err := store.Create(context.Background(), created); err != nil {
		t.Fatalf("creating the session %s: %v", id, err)
	}
}

// TestTheReplRefusesTwoWaysToReopenAtOnce covers the flags that conflict:
// --continue with --resume, and either one with a session that writes
// nothing, are refusals before a server starts.
func TestTheReplRefusesTwoWaysToReopenAtOnce(t *testing.T) {
	base := t.TempDir()
	settingsFile := theSettings(t, base, "http://localhost:4000/v1")
	sessionDir := filepath.Join(base, "sessions")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "continue and resume",
			args: []string{"repl", "--continue", "--resume", "s1"},
			want: "alternatives",
		},
		{
			name: "continue and the ephemeral flag",
			args: []string{"repl", "--continue", "--ephemeral"},
			want: "without one",
		},
		{
			name: "resume and the ephemeral flag",
			args: []string{"repl", "--resume", "s1", "--ephemeral"},
			want: "without one",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append(append([]string{}, c.args...),
				"--settings", settingsFile, "--session-dir", sessionDir)
			code, stderr, stdout := runCLIWithStdout(t, args...)
			if code != 2 {
				t.Errorf("poo %s exited %d, want 2", strings.Join(args, " "), code)
			}
			// The repl greets the terminal as soon as it runs, and it
			// runs only after the server is up: a silent stdout is the
			// refusal that came first.
			if stdout != "" {
				t.Errorf("poo %s wrote %q, so a repl started", strings.Join(args, " "), stdout)
			}
			if !strings.Contains(stderr, c.want) {
				t.Errorf("the refusal is %q, want it to name the conflict (%q)", stderr, c.want)
			}
		})
	}
}

// TestTheReplRefusesToContinueWithNothingToContinue covers --continue in a
// store that holds no session of this directory: the refusal names the
// directory it looked in, and no session is opened in its place.
func TestTheReplRefusesToContinueWithNothingToContinue(t *testing.T) {
	base := t.TempDir()
	settingsFile := theSettings(t, base, "http://localhost:4000/v1")
	sessionDir := filepath.Join(base, "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", sessionDir, err)
	}
	// The refusal names the directory it found nothing to continue in.
	empty := t.TempDir()
	t.Chdir(empty)

	code, stderr, stdout := runCLIWithStdout(t,
		"repl", "--continue", "--settings", settingsFile, "--session-dir", sessionDir)
	if code != 2 {
		t.Errorf("poo repl --continue exited %d, want 2", code)
	}
	if !strings.Contains(stderr, empty) {
		t.Errorf("the refusal does not name the directory it looked in %s: %q", empty, stderr)
	}
	if stdout != "" {
		t.Errorf("poo repl --continue wrote %q, so a repl started", stdout)
	}
}

// TestResumeNeedsTheSessionInItsOwnDirectory covers --resume across
// directories: the session has to be in the store, and it has to work
// where this repl does, because the tools run in the session's directory
// while the settings and the statusline read the repl's.
func TestResumeNeedsTheSessionInItsOwnDirectory(t *testing.T) {
	base := t.TempDir()
	sessionDir := filepath.Join(base, "sessions")
	settingsFile := theSettings(t, base, "http://localhost:4000/v1")
	here := filepath.Join(base, "here")
	there := filepath.Join(base, "there")
	for _, dir := range []string{here, there} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	now := time.Now()
	recordSession(t, sessionDir, "here-session", here, now.Add(-time.Hour))
	recordSession(t, sessionDir, "there-session", there, now.Add(-time.Hour))

	o := replOptions{}
	o.settingsFile = settingsFile
	o.sessionDir = sessionDir

	// The session of this directory opens, and the one of another
	// directory is refused with the directory it works in.
	o.resume = "here-session"
	id, err := o.resumeID(here)
	if err != nil {
		t.Fatalf("resuming here-session in its own directory: %v", err)
	}
	if id != "here-session" {
		t.Errorf("--resume opened %q, want here-session", id)
	}

	o.resume = "there-session"
	if _, err := o.resumeID(here); err == nil {
		t.Error("a session from another directory was opened")
	} else {
		for _, want := range []string{"there", "--working-dir"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal is %q, want it to name %q", err, want)
			}
		}
	}
	// The directory it recorded is where it opens, without another flag.
	if id, err := o.resumeID(there); err != nil || id != "there-session" {
		t.Errorf("resuming there-session in its own directory is (%q, %v)", id, err)
	}

	// A session the store does not hold is refused with the store it was
	// looked for in.
	o.resume = "no-such-session"
	if _, err := o.resumeID(here); err == nil {
		t.Error("a session the store does not hold was opened")
	} else if !strings.Contains(err.Error(), "no-such-session") || !strings.Contains(err.Error(), sessionDir) {
		t.Errorf("the refusal is %q, want it to name the session and the store %s", err, sessionDir)
	}

	// The lock is the server's to report: a session another process
	// holds is the id the repl hands the load.
	held, err := session.Acquire(session.LockPath(sessionDir, "here-session"))
	if err != nil {
		t.Fatalf("taking the lock of here-session: %v", err)
	}
	defer held.Release()
	o.resume = "here-session"
	if id, err := o.resumeID(here); err != nil || id != "here-session" {
		t.Errorf("resuming a locked session is (%q, %v), want the id and no refusal", id, err)
	}
}

// TestTheReplRefusesAResumedSessionFromAnotherDirectory covers the same
// refusal at the front door: the exit code says the line was refused, the
// message names the directory the session works in, and no repl starts.
func TestTheReplRefusesAResumedSessionFromAnotherDirectory(t *testing.T) {
	base := t.TempDir()
	sessionDir := filepath.Join(base, "sessions")
	settingsFile := theSettings(t, base, "http://localhost:4000/v1")
	there := filepath.Join(base, "there")
	if err := os.MkdirAll(there, 0o755); err != nil {
		t.Fatalf("creating %s: %v", there, err)
	}
	recordSession(t, sessionDir, "there-session", there, time.Now())
	t.Chdir(t.TempDir())

	code, stderr, stdout := runCLIWithStdout(t,
		"repl", "--resume", "there-session", "--settings", settingsFile, "--session-dir", sessionDir)
	if code != 2 {
		t.Errorf("poo repl --resume exited %d, want 2", code)
	}
	if !strings.Contains(stderr, there) {
		t.Errorf("the refusal does not name the session's directory: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("poo repl --resume wrote %q, so a repl started", stdout)
	}
}
