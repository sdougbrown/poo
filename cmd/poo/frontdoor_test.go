package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// TestTheFrontDoorFollowsTheSettings covers the door a bare invocation
// launches: the settings' name wins, and the binary's default covers
// everything else.
func TestTheFrontDoorFollowsTheSettings(t *testing.T) {
	settings := &config.Settings{Version: 1}
	name, err := pickFrontDoor(settings, defaultFrontDoor, frontDoors)
	if err != nil {
		t.Fatalf("the default door was refused: %v", err)
	}
	if name != "repl" {
		t.Errorf("the default door is %q, want repl", name)
	}

	settings.FrontDoor = "repl"
	if name, _ := pickFrontDoor(settings, defaultFrontDoor, frontDoors); name != "repl" {
		t.Errorf("the settings' door is %q, want repl", name)
	}
}

// TestAnUnknownFrontDoorIsRefused covers a settings file that names a
// door the binary does not link: the refusal lists what it offers.
func TestAnUnknownFrontDoorIsRefused(t *testing.T) {
	settings := &config.Settings{Version: 1, FrontDoor: "tui"}
	_, err := pickFrontDoor(settings, defaultFrontDoor, frontDoors)
	if err == nil {
		t.Fatal("an unknown door was picked")
	}
	if !strings.Contains(err.Error(), "repl") {
		t.Errorf("the refusal does not name what the binary offers: %v", err)
	}
}

// TestTheFrontDoorReadsTheSettingsFile covers the bare invocation's
// settings: the file POO_SETTINGS names decides the door, and a file
// that fails to load is a refusal.
func TestTheFrontDoorReadsTheSettingsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"front_door":"tui"}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Setenv("POO_SETTINGS", path)

	if code := runFrontDoor(); code != 2 {
		t.Errorf("an unknown door exited %d, want 2", code)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"version":1,"no_such_key":true}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Setenv("POO_SETTINGS", bad)
	if code := runFrontDoor(); code != 2 {
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
