package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// TestTheUIFollowsTheSettings covers the ui a bare invocation
// launches: the settings' name wins, and the binary's default covers
// everything else.
func TestTheUIFollowsTheSettings(t *testing.T) {
	settings := &config.Settings{Version: 1}
	name, err := pickUI(settings, defaultUI, uis)
	if err != nil {
		t.Fatalf("the default ui was refused: %v", err)
	}
	if name != "repl" {
		t.Errorf("the default ui is %q, want repl", name)
	}

	settings.UI = "repl"
	if name, _ := pickUI(settings, defaultUI, uis); name != "repl" {
		t.Errorf("the settings' ui is %q, want repl", name)
	}
}

// TestAnUnknownUIIsRefused covers a settings file that names an interface
// the binary does not link: the refusal lists what it offers.
func TestAnUnknownUIIsRefused(t *testing.T) {
	settings := &config.Settings{Version: 1, UI: "tui"}
	_, err := pickUI(settings, defaultUI, uis)
	if err == nil {
		t.Fatal("an unknown ui was picked")
	}
	if !strings.Contains(err.Error(), "repl") {
		t.Errorf("the refusal does not name what the binary offers: %v", err)
	}
}

// TestTheUIReadsTheSettingsTheCommandResolves covers the bare
// invocation's settings resolution: the project's file decides the
// interface, an environment file that is not there is a refusal, and a
// settings file that fails to load is one too.
func TestTheUIReadsTheSettingsTheCommandResolves(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".poo"), 0o755); err != nil {
		t.Fatalf("writing the project directory: %v", err)
	}
	settings := filepath.Join(project, ".poo", "settings.json")
	if err := os.WriteFile(settings, []byte(`{"version":1,"ui":"tui"}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Chdir(project)
	t.Setenv("POO_SETTINGS", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(project, "no-config"))

	if code := runUI(); code != 2 {
		t.Errorf("an unknown ui exited %d, want 2", code)
	}

	missing := filepath.Join(project, "missing.json")
	t.Setenv("POO_SETTINGS", missing)
	t.Chdir(project)
	if code := runUI(); code != 2 {
		t.Errorf("a missing environment file exited %d, want 2", code)
	}

	broken := filepath.Join(project, "broken.json")
	if err := os.WriteFile(broken, []byte(`{"version":1,"no_such_key":true}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Setenv("POO_SETTINGS", broken)
	if code := runUI(); code != 2 {
		t.Errorf("a bad settings file exited %d, want 2", code)
	}
}

// TestTheReplRefusesItsFlagsBeforeTheServerStarts covers the repl's
// own flag handling: a flag it does not know is a refusal, and no
// server starts for it.
func TestTheReplRefusesItsFlagsBeforeTheServerStarts(t *testing.T) {
	if code := runRepl([]string{"--no-such-flag"}); code != 2 {
		t.Errorf("an unknown flag exited %d, want 2", code)
	}
}

// TestACommandReadsTheProjectSettings covers the discovery wiring every
// non-door command shares: a project settings file a command's directory
// holds is the settings the command runs under, without --settings or
// the environment naming it.
func TestACommandReadsTheProjectSettings(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".poo"), 0o755); err != nil {
		t.Fatalf("writing the project directory: %v", err)
	}
	// A key the settings do not have is the observable: the command
	// that loaded the discovered file refuses it, and a command that
	// never found it runs on the defaults.
	if err := os.WriteFile(filepath.Join(project, ".poo", "settings.json"),
		[]byte(`{"version":1,"no_such_key":true}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Chdir(project)
	t.Setenv("POO_SETTINGS", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(project, "no-config"))

	code, stderr := runCLI(t, "models")
	if code != 2 {
		t.Errorf("the command exited %d, want 2 for the discovered settings' refusal", code)
	}
	if !strings.Contains(stderr, "no_such_key") {
		t.Errorf("the refusal does not name the discovered file's key: %q", stderr)
	}
}

// TestAStandaloneArgumentIsRefused covers the mistyped invocation: a
// word where a flag belongs is a refusal, not a session on the
// defaults with the word ignored.
func TestAStandaloneArgumentIsRefused(t *testing.T) {
	code, stderr := runCLI(t, "serve", "garbage", "--ephemeral")
	if code != 2 {
		t.Errorf("poo serve exited %d, want 2", code)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Errorf("the refusal does not name the argument: %q", stderr)
	}

	code, stderr = runCLI(t, "repl", "garbage")
	if code != 2 {
		t.Errorf("poo repl exited %d, want 2", code)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Errorf("the refusal does not name the argument: %q", stderr)
	}
}
