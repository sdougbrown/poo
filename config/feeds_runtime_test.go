package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/bash"
)

// The runtime's feeds: the manifest a settings file names is a command
// the session runs, beside the session, for as long as the session is
// open.

// feedScript returns the absolute path of one fixture feed.
//
// The scripts are the feed package's own: the runtime runs the same
// commands the runner's tests run, so a change to the protocol is a
// change both packages see.
func feedScript(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "feed", "testdata", "bin", name))
	if err != nil {
		t.Fatalf("resolving the fixture feed %q: %v", name, err)
	}
	return path
}

// feedRunManifest writes a manifest with one long-lived feed and returns
// its path.
func feedRunManifest(t *testing.T, name, script, description string, env map[string]string) string {
	t.Helper()
	feed := map[string]any{
		"description": description,
		"command":     []string{feedScript(t, script)},
		"on_idle":     "wake",
	}
	if env != nil {
		feed["env"] = env
	}
	encoded, err := json.Marshal(map[string]any{"version": 1, "feeds": map[string]any{name: feed}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// waitUntil waits for a condition, so a test does not sleep for a guess.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// theModelRead reports whether the model's requests carry a message with
// this text and origin.
func theModelRead(tr *pootest.Transport, text string, origin poo.Origin) bool {
	for _, request := range tr.Requests() {
		for _, message := range request.Messages {
			if message.Origin != origin || len(message.Content) == 0 {
				continue
			}
			if message.Content[0].Text == text {
				return true
			}
		}
	}
	return false
}

// TestASessionRunsTheFeedItsSettingsName covers the runtime: the feed's
// process runs beside the session, its message reaches the model under
// the feed's origin, the session lists the feed with the jobs, and the
// prompt says the feed exists.
func TestASessionRunsTheFeedItsSettingsName(t *testing.T) {
	workDir := t.TempDir()
	manifest := feedRunManifest(t, "chat", "chat", "Messages from the fixture feed.", nil)
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)+"session:\n  mode: logged\n  dir: "+t.TempDir()+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()
	transport := pootest.NewTransport(pootest.Turn{Text: "the answer"}, pootest.Turn{Text: "the answer"})
	runtime.Options.Transport = transport

	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-11-04T12:00:00Z",
			WorkingDir: workDir,

			Profile: "code",
			Tools:   runtime.Profile.Tools, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()

	// The feed's first message wakes an idle run, and the model reads it
	// as the feed's input.
	waitUntil(t, "the feed's message in the model's view", func() bool {
		return theModelRead(transport, "[feed chat · from \"alice\": the first message]", poo.FeedOrigin("chat"))
	})

	// The feed is a job-shaped entry, under its origin and the feed tool.
	listed := false
	for _, job := range sess.Jobs() {
		if job.JobID != "feed:chat" {
			continue
		}
		listed = true
		if job.Tool != poo.FeedToolName {
			t.Errorf("the feed is listed as the tool %q, want %q", job.Tool, poo.FeedToolName)
		}
	}
	if !listed {
		t.Errorf("the session does not list the feed: %v", sess.Jobs())
	}

	// The prompt says the feed is there, and what it is for.
	if text := sess.PromptState().Text; !strings.Contains(text, "chat: Messages from the fixture feed.") {
		t.Errorf("the prompt does not list the feed: %q", text)
	}
}

// TestASessionRunsItsFeedInItsOwnDirectory covers the directory: a feed
// runs where its session works, not where the binary's build was made, so
// a session created in another directory reads that directory. The
// fixture prints the directory its process runs in as the message's text.
func TestASessionRunsItsFeedInItsOwnDirectory(t *testing.T) {
	sessionDir := t.TempDir()
	want, err := filepath.EvalSymlinks(sessionDir)
	if err != nil {
		t.Fatalf("resolving the session's directory: %v", err)
	}
	manifest := feedRunManifest(t, "here", "pwd", "The directory the feed runs in.", nil)
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)+"session:\n  mode: logged\n  dir: "+t.TempDir()+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	// The build's own directory is another one: the feed's directory is
	// the session's, and a runner that ran where the build put it would
	// print the build's.
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()
	transport := pootest.NewTransport(pootest.Turn{Text: "the answer"}, pootest.Turn{Text: "the answer"})
	runtime.Options.Transport = transport

	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-11-04T12:00:00Z",
			WorkingDir: sessionDir,

			Profile: "code",
			Tools:   runtime.Profile.Tools, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()

	waitUntil(t, "the feed's message in the model's view", func() bool {
		return theModelRead(transport, "[feed here: "+want+"]", poo.FeedOrigin("here"))
	})
}

// TestTheSettingsExecutorRunsTheFeedCommand covers the executor: a feed's
// command runs through the executor the settings name, the same one the
// session's other commands run through.
func TestTheSettingsExecutorRunsTheFeedCommand(t *testing.T) {
	manifest := feedRunManifest(t, "chat", "chat", "Messages from the fixture feed.", nil)
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)+"tools:\n  bash:\n    executor: counting\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	counting := &countingExecutor{Local: bash.NewLocal()}
	executors := theExecutors()
	executors.Register("counting", func(exec.Options) (exec.Executor, error) { return counting, nil })
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), executors)
	defer runtime.Store.Close()
	if len(runtime.Options.Feeds) != 1 {
		t.Fatalf("the build ran %v, want the one feed", runtime.Options.Feeds)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = runtime.Options.Feeds[0].Run(ctx, poo.FeedIO{
		Emit: func(string, string) error {
			cancel()
			return nil
		},
		Log: slog.New(slog.DiscardHandler),
		Dir: t.TempDir(),
	})
	if ctx.Err() == nil {
		t.Fatalf("the feed ended before it emitted: %v", err)
	}
	if got, want := counting.started(), [][]string{{feedScript(t, "chat")}}; !reflect.DeepEqual(got, want) {
		t.Errorf("the settings' executor started %q, want only the feed's command %q", got, want)
	}
}

// TestAFeedsOnlyBuildBuildsAnExecutor covers the executor: a session that
// runs a feed builds one even when its profile offers no tool that runs a
// command, because the feed's own command runs through it. A build with
// no executor refuses the feed.
func TestAFeedsOnlyBuildBuildsAnExecutor(t *testing.T) {
	manifest := feedRunManifest(t, "chat", "chat", "Messages from the fixture feed.", nil)
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	// readOnlyTools links neither bash nor call_cli, so the feed is the
	// only thing in the build that runs a command.
	runtime, err := readOnlyBuild(t, settings, theExecutors())
	if err != nil {
		t.Fatalf("a read-only binary with a feed did not build: %v", err)
	}
	if len(runtime.Options.Feeds) != 1 {
		t.Errorf("the build ran %v, want the one feed", runtime.Options.Feeds)
	}

	_, err = readOnlyBuild(t, settings, nil)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a binary with a feed and no executor built anyway: %v", err)
	}
	if failure.Code != config.CodeUnsupported {
		t.Errorf("the failure is %+v, want unsupported", failure)
	}
}

// countingExecutor is the local executor, and it keeps the command of
// every process it starts.
type countingExecutor struct {
	*bash.Local
	mu   sync.Mutex
	argv [][]string
}

// Name is the executor's name, as the settings name it.
func (c *countingExecutor) Name() string { return "counting" }

// Start keeps the command and starts it locally.
func (c *countingExecutor) Start(ctx context.Context, command exec.Command, out io.Writer) (exec.Process, error) {
	c.mu.Lock()
	c.argv = append(c.argv, command.Argv)
	c.mu.Unlock()
	return c.Local.Start(ctx, command, out)
}

// started is the command of every process the executor started.
func (c *countingExecutor) started() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]string(nil), c.argv...)
}

// TestAFeedCannotReachAScrubbedVariable covers the environment: the
// manifest's reference reads the session's scrubbed environment, so a
// name the settings scrub is not there for a feed's command, and the
// attempt fails rather than starting with half an environment.
func TestAFeedCannotReachAScrubbedVariable(t *testing.T) {
	t.Setenv("POO_FIXTURE_SECRET", "the secret")
	manifest := feedRunManifest(t, "echoenv", "echoenv", "The fixture's environment.", map[string]string{
		"FIXTURE_ECHO": "env:POO_FIXTURE_SECRET",
	})
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)+"session:\n  dir: "+t.TempDir()+"\ntools:\n  env:\n    scrub: [POO_FIXTURE_SECRET]\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()
	if len(runtime.Options.Feeds) != 1 {
		t.Fatalf("the build ran %v, want the one feed", runtime.Options.Feeds)
	}

	emitted := 0
	err = runtime.Options.Feeds[0].Run(context.Background(), poo.FeedIO{
		Emit: func(string, string) error {
			emitted++
			return nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("the feed started with a variable the settings scrub")
	}
	if !strings.Contains(err.Error(), "POO_FIXTURE_SECRET") {
		t.Errorf("the attempt failed with %q, want it to name the variable", err)
	}
	if emitted != 0 {
		t.Errorf("the command ran anyway and emitted %d inputs", emitted)
	}
}

// TestAFeedOnlyProfileOffersTheJobTools covers the profile: the core
// offers jobs and cancel_job to a session that runs feeds even when no
// tool runs in the background, so the profile lists them too.
func TestAFeedOnlyProfileOffersTheJobTools(t *testing.T) {
	manifest := feedRunManifest(t, "chat", "chat", "Messages from the fixture feed.", nil)
	settings, err := config.Parse([]byte(theFeedsSettings(manifest)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{Profile: "base"})
	names := map[string]bool{}
	for _, tool := range runtime.Profile.Tools {
		names[tool.Name] = true
	}
	if !names["jobs"] || !names["cancel_job"] {
		t.Errorf("the profile offers %v, want jobs and cancel_job", runtime.Profile.Tools)
	}
}
