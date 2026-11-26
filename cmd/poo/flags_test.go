package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/session"
)

// The command's flags: the profile a run is built under, and the
// action a call no rule covers takes.

// TestTheProfileFlagPicksTheTools covers the flag: the run offers the
// tools the profile names, and the prompt is the profile's.
func TestTheProfileFlagPicksTheTools(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	code, stderr := runCLI(t,
		"run",
		"--profile", "search",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	tools, _ := endpoint.requests[0]["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		fn, _ := m["function"].(map[string]any)
		name, _ := fn["name"].(string)
		names[name] = true
	}
	for _, want := range []string{"read", "grep"} {
		if !names[want] {
			t.Errorf("the request does not offer the %q tool", want)
		}
	}
	for _, unwanted := range []string{"bash", "edit"} {
		if names[unwanted] {
			t.Errorf("the request offers the %q tool, and the search profile does not", unwanted)
		}
	}
}

// TestTheUnmatchedFlagChangesThePolicy covers the flag: the action for
// a call no rule covers is the one the command line names.
//
// The chain has no asker, so a policy that asks falls back, and the
// warning says so.
func TestTheUnmatchedFlagChangesThePolicy(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	code, stderr := runCLI(t,
		"run",
		"--permission-unmatched", "ask",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "falls back") {
		t.Errorf("stderr is %q, want it to warn about the fallback", stderr)
	}
}

// TestAnEmptyManifestPathFlagIsSkipped covers the manifest-path flags: an
// empty value, such as an unset variable expanded on the command line,
// adds no path, so it never resolves to the working directory.
func TestAnEmptyManifestPathFlagIsSkipped(t *testing.T) {
	inv, err := clicontract.Parse(pooApp(io.Discard), []string{
		"models",
		"--feeds-path", "",
		"--feeds-path", "feeds",
		"--cli-registry-path", "",
	})
	if err != nil {
		t.Fatalf("parsing the flags: %v", err)
	}
	var f sharedFlags
	f.bind(inv)
	overrides, err := f.overrides()
	if err != nil {
		t.Fatalf("reading the overrides: %v", err)
	}
	if got := overrides.FeedsPaths; len(got) != 1 || got[0] != "feeds" {
		t.Errorf("the feed paths are %q, want only %q", got, "feeds")
	}
	if got := overrides.CLIRegistryPaths; len(got) != 0 {
		t.Errorf("the registry paths are %q, want none", got)
	}
}

// TestTheSystemPromptFlagResolvesAgainstTheWorkingDirectory covers the
// flag: a relative file is beside the working directory it was typed
// in, and a leading ~ is the home directory.
func TestTheSystemPromptFlagResolvesAgainstTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("the base section"), 0o644); err != nil {
		t.Fatalf("writing the base file: %v", err)
	}
	t.Chdir(dir)

	inv, err := clicontract.Parse(pooApp(io.Discard), []string{
		"run",
		"--system-prompt", "prompt.md",
	})
	if err != nil {
		t.Fatalf("parsing the flags: %v", err)
	}
	var f sharedFlags
	f.bind(inv)
	overrides, err := f.overrides()
	if err != nil {
		t.Fatalf("reading the overrides: %v", err)
	}
	if got := overrides.SystemPrompt; got != filepath.Join(dir, "prompt.md") {
		t.Errorf("the system prompt is %q, want the file beside the working directory", got)
	}

	// The home directory is apart from the working directory, so a ~
	// that resolves to the working directory is not a pass.
	home := t.TempDir()
	t.Setenv("HOME", home)
	for value, want := range map[string]string{
		"~/flagged.md": filepath.Join(home, "flagged.md"),
		"~":            home,
	} {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"run",
			"--system-prompt", value,
		})
		if err != nil {
			t.Fatalf("parsing the flags: %v", err)
		}
		var f sharedFlags
		f.bind(inv)
		overrides, err := f.overrides()
		if err != nil {
			t.Fatalf("reading the overrides: %v", err)
		}
		if got := overrides.SystemPrompt; got != want {
			t.Errorf("the system prompt is %q, want %q", got, want)
		}
	}
}

// TestTheSystemPromptFlagNamesItselfWhenTheHomeIsMissing covers the
// failure: with no home directory a ~ cannot be resolved, and the
// error names the flag and the path it was given.
func TestTheSystemPromptFlagNamesItselfWhenTheHomeIsMissing(t *testing.T) {
	t.Setenv("HOME", "")
	inv, err := clicontract.Parse(pooApp(io.Discard), []string{
		"run",
		"--system-prompt", "~/prompt.md",
	})
	if err != nil {
		t.Fatalf("parsing the flags: %v", err)
	}
	var f sharedFlags
	f.bind(inv)
	_, err = f.overrides()
	if err == nil {
		t.Fatal("the overrides resolved, want the error")
	}
	if !strings.Contains(err.Error(), "--system-prompt") {
		t.Errorf("the error is %q, want it to name --system-prompt", err)
	}
	if !strings.Contains(err.Error(), "~/prompt.md") {
		t.Errorf("the error is %q, want it to name the path", err)
	}
	// The error is the raw home resolution, so it carries no code;
	// if it did, the code is one of the settings' codes.
	if code, ok := config.CodeOf(err); ok {
		switch code {
		case config.CodeDecode, config.CodeUnknownField, config.CodeUnavailable, config.CodeInvalid, config.CodeUnsupported:
		default:
			t.Errorf("the error carries the code %q, want one of the settings' codes", code)
		}
	}
}

// TestThePermissionFallbackFlagIsApplied covers the one-shot's
// --permission-fallback flag: a side-effecting call no rule covers is
// decided by the fallback, and the flag overrides the settings' or the
// default fallback for the run.
func TestThePermissionFallbackFlagIsApplied(t *testing.T) {
	// Control: the same configuration named in the settings file
	// reaches the policy.
	controlDir := t.TempDir()
	settingsPath := filepath.Join(controlDir, "settings.yaml")
	settings := "version: 1\npermission:\n  unmatched: ask\n  fallback: allow\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	controlEndpoint := newFakeEndpointSequence(t, []string{bashCall(), doneAnswer()})
	controlCode, controlStderr := runCLI(t,
		"run",
		"--settings", settingsPath,
		"--endpoint", controlEndpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", controlDir,
		"--log", filepath.Join(controlDir, "run.jsonl"),
		"run the command",
	)
	if controlCode != 0 {
		t.Fatalf("the control run exited with %d: %s", controlCode, controlStderr)
	}
	if outcome := toolOutcome(t, controlDir); outcome != "ok" {
		t.Fatalf("the control outcome = %q, want ok", outcome)
	}

	// The flag: the same run with --permission-fallback allow.
	dir := t.TempDir()
	endpoint := newFakeEndpointSequence(t, []string{bashCall(), doneAnswer()})
	code, stderr := runCLI(t,
		"run",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--permission-unmatched", "ask",
		"--permission-fallback", "allow",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"run the command",
	)
	if code != 0 {
		t.Fatalf("the run exited with %d: %s", code, stderr)
	}
	if outcome := toolOutcome(t, dir); outcome != "ok" {
		t.Errorf("the tool's outcome = %q, want ok: the flag allows the call", outcome)
	}
	if !strings.Contains(stderr, `falls back to "allow"`) {
		t.Errorf("stderr is %q, want the warning to name the allow fallback", stderr)
	}
}

func bashCall() string {
	return "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo allowed\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

func doneAnswer() string {
	return "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

func toolOutcome(t *testing.T, dir string) string {
	t.Helper()
	outcome := ""
	for _, rec := range readLog(t, filepath.Join(dir, "run.jsonl")) {
		if rec["record"] == "tool.result" {
			outcome, _ = rec["outcome"].(string)
		}
	}
	return outcome
}

// TestThePruneOlderThanFlagDeletesTheOldSessions covers the CLI
// mapping: --older-than marks a session by its age, and --dry-run
// deletes nothing.
func TestThePruneOlderThanFlagDeletesTheOldSessions(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://localhost:4000/v1")
	sessionDir := filepath.Join(dir, "sessions")
	old := time.Now().Add(-48 * time.Hour)
	store, err := session.NewStore(sessionDir, session.WithClock(func() time.Time { return old }))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID: "old-session",
			Mode:      "logged",
			CreatedAt: old.UTC().Format(time.RFC3339Nano),

			Profile: "code", Model: poo.SessionModel{ModelID: "test-model"}},
	}
	if _, err := store.Create(context.Background(), created); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--older-than", "1", "--dry-run"}); code != 0 {
			t.Errorf("the dry run exited with %d", code)
		}
	})
	if !strings.Contains(out, "would delete old-session") {
		t.Errorf("the dry run says %q, want it to name old-session", out)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "old-session.jsonl")); err != nil {
		t.Errorf("the dry run deleted the session: %v", err)
	}

	out = captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--older-than", "1"}); code != 0 {
			t.Errorf("the prune exited with %d", code)
		}
	})
	if !strings.Contains(out, "deleted old-session") {
		t.Errorf("the prune says %q, want it to name old-session", out)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "old-session.jsonl")); !os.IsNotExist(err) {
		t.Errorf("the old session is still there: %v", err)
	}
}

// TestTheRunFlagsReachTheOptions covers the binding: every flag the run
// declares lands in the field that reads it. A lookup that names the wrong
// flag reads an empty value without failing, so the whole struct is
// compared at once.
func TestTheRunFlagsReachTheOptions(t *testing.T) {
	inv, err := clicontract.Parse(pooApp(io.Discard), []string{
		"run",
		"--settings", "s.yaml",
		"--system-prompt", "system.md",
		"--append-system-prompt", "extra",
		"--permission-unmatched", "ask",
		"--session-dir", "sessions",
		"--user-prompt-file", "user.md",
		"--ephemeral",
		"--profile", "search",
		"--feeds-path", "feeds.yaml",
		"--feeds-path", "more.yaml",
		"--cli-registry-path", "registry.yaml",
		"--endpoint", "http://localhost:4000/v1",
		"--model-id", "test-model",
		"--context-limit", "8192",
		"--api-key-env", "POO_KEY",
		"--tools-mode", "text",
		"--permission-fallback", "deny",
		"--reasoning-effort", "low",
		"--max-turns", "3",
		"--max-output", "1024",
		"--truncation-retries", "2",
		"--log", "run.jsonl",
		"--working-dir", "/tmp",
		"--executor", "bwrap",
		"--project-prompt",
		"--timeout", "5m",
		"--no-wait-jobs",
		"--",
		"a prompt",
	})
	if err != nil {
		t.Fatalf("parsing the run's flags: %v", err)
	}
	var o runOptions
	o.bind(inv)

	want := runOptions{
		sharedFlags: sharedFlags{
			settingsFile:        "s.yaml",
			systemPrompt:        "system.md",
			appendSystemPrompt:  "extra",
			permissionUnmatched: "ask",
			sessionDir:          "sessions",
			userPromptFile:      "user.md",
			ephemeral:           true,
			profile:             "search",
			feedsPaths:          []string{"feeds.yaml", "more.yaml"},
			cliRegistryPaths:    []string{"registry.yaml"},
		},
		endpointFlags: endpointFlags{
			endpoint:     "http://localhost:4000/v1",
			modelID:      "test-model",
			contextLimit: 8192,
			apiKeyEnv:    "POO_KEY",
			toolsMode:    "text",
		},
		permissionFallback:     "deny",
		reasoningEffort:        "low",
		maxTurns:               3,
		maxOutput:              1024,
		truncationRetries:      2,
		truncationRetriesGiven: true,
		logPath:                "run.jsonl",
		workingDir:             "/tmp",
		executor:               "bwrap",
		projectPrompt:          true,
		timeout:                5 * time.Minute,
		noWaitJobs:             true,
	}
	if !reflect.DeepEqual(o, want) {
		t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
	}
	if len(inv.Args) != 1 || inv.Args[0] != "a prompt" {
		t.Errorf("the prompt is %q, want one word", inv.Args)
	}
}

// TestEveryOtherCommandBindsItsFlags covers the binding the run's test
// cannot: a flag added to serve, prompt, models or sessions has its own
// lookup, and a lookup that names the wrong flag reads an empty value
// without failing. Each command's whole struct is compared at once, the
// way the run's is.
func TestEveryOtherCommandBindsItsFlags(t *testing.T) {
	t.Run("serve", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"serve",
			"--settings", "s.yaml",
			"--system-prompt", "system.md",
			"--append-system-prompt", "extra",
			"--permission-unmatched", "allow",
			"--session-dir", "sessions",
			"--user-prompt-file", "user.md",
			"--ephemeral",
			"--profile", "base",
			"--feeds-path", "feeds.yaml",
			"--cli-registry-path", "registry.yaml",
			"--endpoint", "http://localhost:4000/v1",
			"--model-id", "test-model",
			"--context-limit", "4096",
			"--api-key-env", "POO_KEY",
			"--tools-mode", "auto",
			"--working-dir", "/tmp",
			"--executor", "local",
		})
		if err != nil {
			t.Fatalf("parsing the serve's flags: %v", err)
		}
		var o serveOptions
		o.bind(inv)
		want := serveOptions{
			sharedFlags: sharedFlags{
				settingsFile:        "s.yaml",
				systemPrompt:        "system.md",
				appendSystemPrompt:  "extra",
				permissionUnmatched: "allow",
				sessionDir:          "sessions",
				userPromptFile:      "user.md",
				ephemeral:           true,
				profile:             "base",
				feedsPaths:          []string{"feeds.yaml"},
				cliRegistryPaths:    []string{"registry.yaml"},
			},
			endpointFlags: endpointFlags{
				endpoint:     "http://localhost:4000/v1",
				modelID:      "test-model",
				contextLimit: 4096,
				apiKeyEnv:    "POO_KEY",
				toolsMode:    "auto",
			},
			workingDir: "/tmp",
			executor:   "local",
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("daemon", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"daemon",
			"--settings", "s.yaml",
			"--system-prompt", "system.md",
			"--append-system-prompt", "extra",
			"--permission-unmatched", "allow",
			"--session-dir", "sessions",
			"--user-prompt-file", "user.md",
			"--ephemeral",
			"--profile", "base",
			"--feeds-path", "feeds.yaml",
			"--cli-registry-path", "registry.yaml",
			"--endpoint", "http://localhost:4000/v1",
			"--model-id", "test-model",
			"--context-limit", "4096",
			"--api-key-env", "POO_KEY",
			"--tools-mode", "auto",
			"--session", "my-session",
			"--auto-resume",
			"--working-dir", "/tmp",
		})
		if err != nil {
			t.Fatalf("parsing the daemon's flags: %v", err)
		}
		var o daemonOptions
		o.bind(inv)
		want := daemonOptions{
			sharedFlags: sharedFlags{
				settingsFile:        "s.yaml",
				systemPrompt:        "system.md",
				appendSystemPrompt:  "extra",
				permissionUnmatched: "allow",
				sessionDir:          "sessions",
				userPromptFile:      "user.md",
				ephemeral:           true,
				profile:             "base",
				feedsPaths:          []string{"feeds.yaml"},
				cliRegistryPaths:    []string{"registry.yaml"},
			},
			endpointFlags: endpointFlags{
				endpoint:     "http://localhost:4000/v1",
				modelID:      "test-model",
				contextLimit: 4096,
				apiKeyEnv:    "POO_KEY",
				toolsMode:    "auto",
			},
			session:         "my-session",
			autoResume:      true,
			autoResumeGiven: true,
			workingDir:      "/tmp",
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("prompt", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"prompt",
			"--settings", "s.yaml",
			"--user-prompt-file", "user.md",
			"--profile", "search",
			"--project-prompt",
			"--pretty",
		})
		if err != nil {
			t.Fatalf("parsing the prompt's flags: %v", err)
		}
		var o promptOptions
		o.bind(inv)
		want := promptOptions{
			sharedFlags: sharedFlags{
				settingsFile:   "s.yaml",
				userPromptFile: "user.md",
				profile:        "search",
			},
			projectPrompt: true,
			pretty:        true,
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("models", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"models",
			"--json",
			"--explain", "gemma4",
			"--session-dir", "sessions",
		})
		if err != nil {
			t.Fatalf("parsing the models' flags: %v", err)
		}
		var o modelsOptions
		o.bind(inv)
		want := modelsOptions{
			sharedFlags: sharedFlags{sessionDir: "sessions"},
			json:        true,
			explain:     "gemma4",
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("login", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"login", "codex",
			"--force",
			"--no-browser",
			"--wait", "60",
			"--settings", "s.yaml",
		})
		if err != nil {
			t.Fatalf("parsing the login's flags: %v", err)
		}
		var o loginOptions
		o.bind(inv)
		want := loginOptions{
			sharedFlags: sharedFlags{settingsFile: "s.yaml"},
			force:       true,
			noBrowser:   true,
			wait:        60,
			waitSet:     true,
			provider:    "codex",
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("auth import pi", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"auth", "import", "pi",
			"--force",
			"--from", "pi-auth.json",
			"--settings", "s.yaml",
		})
		if err != nil {
			t.Fatalf("parsing the import's flags: %v", err)
		}
		var o authImportOptions
		o.bind(inv)
		want := authImportOptions{
			sharedFlags: sharedFlags{settingsFile: "s.yaml"},
			force:       true,
			from:        "pi-auth.json",
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("auth status", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"auth", "status",
			"--json",
			"--settings", "s.yaml",
		})
		if err != nil {
			t.Fatalf("parsing the status' flags: %v", err)
		}
		var o authOptions
		o.bind(inv)
		want := authOptions{
			sharedFlags: sharedFlags{settingsFile: "s.yaml"},
			json:        true,
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("sessions prune", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"sessions", "prune",
			"--older-than", "30",
			"--keep", "5",
			"--dry-run",
			"--session-dir", "sessions",
		})
		if err != nil {
			t.Fatalf("parsing the prune's flags: %v", err)
		}
		var o sessionsOptions
		o.bind(inv)
		want := sessionsOptions{
			sharedFlags: sharedFlags{sessionDir: "sessions"},
			olderThan:   30,
			keep:        5,
			dryRun:      true,
		}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the bound options are\n%+v\nwant\n%+v", o, want)
		}
	})

	t.Run("sessions list", func(t *testing.T) {
		inv, err := clicontract.Parse(pooApp(io.Discard), []string{
			"sessions", "list",
			"--json",
		})
		if err != nil {
			t.Fatalf("parsing the list's flags: %v", err)
		}
		var o sessionsOptions
		o.bind(inv)
		if !o.json {
			t.Error("the list did not bind --json")
		}
		if o.olderThan != 0 || o.keep != 0 || o.dryRun {
			t.Errorf("the list bound the prune's flags: %+v", o)
		}
	})
}

// TestTheWaitFlagKnowsWhetherItWasTyped covers the flag's presence, not its value: a key
// sign-in refuses --wait because it was asked for, so the default the operator never typed is
// not a request.
func TestTheWaitFlagKnowsWhetherItWasTyped(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want bool
	}{
		{"typed", []string{"login", "codex", "--wait", "60"}, true},
		{"not typed", []string{"login", "codex"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			inv, err := clicontract.Parse(pooApp(io.Discard), c.args)
			if err != nil {
				t.Fatalf("parsing the login's flags: %v", err)
			}
			var o loginOptions
			o.bind(inv)
			if o.waitSet != c.want {
				t.Errorf("waitSet is %t for %v, want %t", o.waitSet, c.args, c.want)
			}
		})
	}
}

// TestEveryCommandWithItsOwnFlagsHasABindingTest covers the blind spot the
// bind tests share: they are a list, and a command added without a subtest
// would slip through. The commands are walked from the declaration, so a
// new one with its own flags fails here until it is bound somewhere.
func TestEveryCommandWithItsOwnFlagsHasABindingTest(t *testing.T) {
	// covered names the test that binds each command's own flags. A
	// command whose handler reads an invocation directly names the test
	// that drives that flag instead.
	covered := map[string]string{
		"run":            "TestTheRunFlagsReachTheOptions",
		"serve":          "TestEveryOtherCommandBindsItsFlags/serve",
		"daemon":         "TestEveryOtherCommandBindsItsFlags/daemon",
		"prompt":         "TestEveryOtherCommandBindsItsFlags/prompt",
		"models":         "TestEveryOtherCommandBindsItsFlags/models",
		"auth status":    "TestEveryOtherCommandBindsItsFlags/auth_status",
		"auth refresh":   "TestAuthRefresh",
		"auth import pi": "TestEveryOtherCommandBindsItsFlags/auth_import_pi",
		"login":          "TestEveryOtherCommandBindsItsFlags/login",
		"sessions list":  "TestEveryOtherCommandBindsItsFlags/sessions_list",
		"sessions prune": "TestEveryOtherCommandBindsItsFlags/sessions_prune",
		"version":        "TestTheVersionCommandAnswersInBothForms",
		// The repl binds serveOptions, so the serve subtest covers its
		// binding; this test covers the second spelling it hands the child.
		"repl": "TestTheReplHandsItsServerTheFlagsItParsed",
	}
	app := pooApp(io.Discard)
	var walk func(prefix string, commands []*clicontract.Command)
	walk = func(prefix string, commands []*clicontract.Command) {
		for _, command := range commands {
			path := command.Name
			if prefix != "" {
				path = prefix + " " + command.Name
			}
			own := len(command.Flags) + len(command.PersistentFlags)
			if own > 0 {
				if _, ok := covered[path]; !ok {
					t.Errorf("the command %q declares %d flag(s) and no test binds them: add a subtest and a line here", path, own)
				}
			}
			walk(path, command.Subcommands)
		}
	}
	// The front doors are in the tree too: the app takes their commands
	// from the declaration, so walking it reaches them.
	walk("", app.Commands)
}

// TestTheTruncationRetryFlagIsValidatedAndReachesTheLimit covers the retry's
// flag. A negative count is a usage error. A count the operator gave lands in
// the settings' limit, where 0 is the setting's own value for no retries.
func TestTheTruncationRetryFlagIsValidatedAndReachesTheLimit(t *testing.T) {
	apply := func(args ...string) (*config.Settings, error) {
		inv, err := clicontract.Parse(pooApp(io.Discard), append([]string{"run"}, args...))
		if err != nil {
			return nil, err
		}
		var o runOptions
		o.bind(inv)
		if err := o.validate(inv.Args); err != nil {
			return nil, err
		}
		settings := &config.Settings{Version: 1}
		if err := o.apply(settings); err != nil {
			return nil, err
		}
		return settings, nil
	}

	_, err := apply("--truncation-retries=-1", "--", "a prompt")
	if err == nil || !strings.Contains(err.Error(), "--truncation-retries must not be negative") {
		t.Fatalf("a negative retry count is refused with %v, want the flag's own reason", err)
	}

	// Zero is the setting's own value for "no retries", so the flag can turn
	// off the default.
	settings, err := apply("--truncation-retries=0", "--", "a prompt")
	if err != nil {
		t.Fatalf("giving the retry count: %v", err)
	}
	if settings.Limits.TruncationRetries == nil || *settings.Limits.TruncationRetries != 0 {
		t.Errorf("the settings hold %v, want the 0 the flag gave", settings.Limits.TruncationRetries)
	}

	// A run that gave no flag leaves the limit unset, so the build's default
	// applies.
	settings, err = apply("--", "a prompt")
	if err != nil {
		t.Fatalf("omitting the retry count: %v", err)
	}
	if settings.Limits.TruncationRetries != nil {
		t.Errorf("the settings hold %v, want the limit left unset", settings.Limits.TruncationRetries)
	}
}
