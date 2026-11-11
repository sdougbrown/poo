package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// TestTheDoorFollowsTheSettings covers the door a bare invocation
// launches: the settings' name wins, and the binary's default covers
// everything else.
func TestTheDoorFollowsTheSettings(t *testing.T) {
	settings := &config.Settings{Version: 1}
	name, err := pickDoor(settings, defaultDoor, doors)
	if err != nil {
		t.Fatalf("the default door was refused: %v", err)
	}
	if name != "repl" {
		t.Errorf("the default door is %q, want repl", name)
	}

	settings.Door = "repl"
	if name, _ := pickDoor(settings, defaultDoor, doors); name != "repl" {
		t.Errorf("the settings' door is %q, want repl", name)
	}
}

// TestAnUnknownDoorIsRefused covers a settings file that names a door
// the binary does not link: the refusal lists what it offers.
func TestAnUnknownDoorIsRefused(t *testing.T) {
	settings := &config.Settings{Version: 1, Door: "tui"}
	_, err := pickDoor(settings, defaultDoor, doors)
	if err == nil {
		t.Fatal("an unknown door was picked")
	}
	if !strings.Contains(err.Error(), "repl") {
		t.Errorf("the refusal does not name what the binary offers: %v", err)
	}
}

// TestTheDoorReadsTheSettingsTheCommandResolves covers the bare
// invocation's settings resolution: the project's file decides the
// door, an environment file that is not there is a refusal, and a
// settings file that fails to load is one too.
func TestTheDoorReadsTheSettingsTheCommandResolves(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".poo"), 0o755); err != nil {
		t.Fatalf("writing the project directory: %v", err)
	}
	settings := filepath.Join(project, ".poo", "settings.json")
	if err := os.WriteFile(settings, []byte(`{"version":1,"door":"tui"}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Chdir(project)
	t.Setenv("POO_SETTINGS", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(project, "no-config"))

	if code := runDoor(); code != 2 {
		t.Errorf("an unknown door exited %d, want 2", code)
	}

	missing := filepath.Join(project, "missing.json")
	t.Setenv("POO_SETTINGS", missing)
	t.Chdir(project)
	if code := runDoor(); code != 2 {
		t.Errorf("a missing environment file exited %d, want 2", code)
	}

	broken := filepath.Join(project, "broken.json")
	if err := os.WriteFile(broken, []byte(`{"version":1,"no_such_key":true}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Setenv("POO_SETTINGS", broken)
	if code := runDoor(); code != 2 {
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
