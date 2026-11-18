package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// TestTheUIFollowsTheSettings covers the ui a bare invocation
// launches: the settings' name wins, and the binary's default covers
// everything else.
func TestTheUIFollowsTheSettings(t *testing.T) {
	settings := &config.Settings{Version: 1}
	name, err := pickUI(settings, defaultUI, frontDoors)
	if err != nil {
		t.Fatalf("the default ui was refused: %v", err)
	}
	if name != "repl" {
		t.Errorf("the default ui is %q, want repl", name)
	}

	settings.UI = "repl"
	if name, _ := pickUI(settings, defaultUI, frontDoors); name != "repl" {
		t.Errorf("the settings' ui is %q, want repl", name)
	}
}

// TestAnUnknownUIIsRefused covers a settings file that names an interface
// the binary does not link: the refusal lists what it offers.
func TestAnUnknownUIIsRefused(t *testing.T) {
	settings := &config.Settings{Version: 1, UI: "tui"}
	_, err := pickUI(settings, defaultUI, frontDoors)
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

	// The refusal names the missing environment file: a run that
	// fell through to the project's settings would refuse with the
	// door's name instead.
	missing := filepath.Join(project, "missing.json")
	t.Setenv("POO_SETTINGS", missing)
	t.Chdir(project)
	code, stderr := runCLI(t)
	if code != 2 {
		t.Errorf("a missing environment file exited %d, want 2", code)
	}
	if !strings.Contains(stderr, "missing.json") {
		t.Errorf("the refusal does not name the missing environment file: %q", stderr)
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
	if code := run([]string{"repl", "--no-such-flag"}); code != 2 {
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

// TestAFrontDoorIsADeclaredCommand covers the doors: each carries its own
// command, so the declaration and the launch read one entry.
func TestAFrontDoorIsADeclaredCommand(t *testing.T) {
	app := pooApp(io.Discard)
	for door := range frontDoors {
		if app.Find(door) == nil {
			t.Errorf("the front door %q is not a declared command", door)
		}
	}
	if _, ok := frontDoors[defaultUI]; !ok {
		t.Errorf("the default ui %q is not one of the front doors", defaultUI)
	}
}

// TestABareInvocationHandsTheSettingsToTheInterface covers the argv a bare
// invocation dispatches: the settings file the command resolved reaches the
// interface, so the session it opens reads what it read.
func TestABareInvocationHandsTheSettingsToTheInterface(t *testing.T) {
	cases := []struct {
		name string
		path string
		want []string
	}{
		{name: "the settings were found", path: "/project/.poo/settings.yaml", want: []string{"repl", "--settings", "/project/.poo/settings.yaml"}},
		{name: "no settings were found", path: "", want: []string{"repl"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := frontDoorLine("repl", c.path, nil)
			if !slices.Equal(got, c.want) {
				t.Errorf("the argv is %q, want %q", got, c.want)
			}
		})
	}
}

// TestTheFrontDoorReadsTheFlagsOnItsLine covers where the alias's settings
// come from: the line's --settings and --working-dir are the invocation's,
// so `poo -c` reads the file they name and discovers from the directory
// they name, not from the one this process runs in.
func TestTheFrontDoorReadsTheFlagsOnItsLine(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(project, ".poo"), 0o755); err != nil {
		t.Fatalf("writing the project directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, ".poo", "settings.json"),
		[]byte(`{"version":1,"ui":"tui"}`), 0o644); err != nil {
		t.Fatalf("writing the project's settings: %v", err)
	}
	pinned := filepath.Join(base, "pinned.json")
	if err := os.WriteFile(pinned, []byte(`{"version":1,"no_such_key":true}`), 0o644); err != nil {
		t.Fatalf("writing the pinned settings: %v", err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("POO_SETTINGS", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "no-config"))

	// The project's settings name an interface this binary does not
	// offer: the refusal says so, and no interface runs.
	code, stderr := runCLI(t, "-c", "--working-dir", project)
	if code != 2 {
		t.Errorf("poo -c --working-dir exited %d, want 2", code)
	}
	if !strings.Contains(stderr, "tui") {
		t.Errorf("the refusal does not name the project's ui: %q", stderr)
	}

	// A settings file the line names is the one read, and a key it does
	// not have is the refusal: a run that fell back to discovery would
	// launch the interface instead.
	code, stderr = runCLI(t, "-c", "--settings", pinned)
	if code != 2 {
		t.Errorf("poo -c --settings exited %d, want 2", code)
	}
	if !strings.Contains(stderr, "no_such_key") {
		t.Errorf("the refusal does not name the pinned file's key: %q", stderr)
	}
}

// TestTheContinueAliasIsTheFrontDoor covers `poo -c`, `poo --continue`
// and `poo --resume <id>`: they launch the front door with the rest of the
// line, so a short invocation is the repl with the flags that reopen a
// session. A leading flag that is not one of them keeps the behaviour it
// has.
//
// The line is also where the front door's own settings come from, so the
// test reads them the way the launch does: the working directory and the
// settings file as the line spells them, and the argv the interface is
// started with.
func TestTheContinueAliasIsTheFrontDoor(t *testing.T) {
	cases := []struct {
		name string
		args []string
		door bool
		// settings and dir are what the alias's words name for the
		// settings file and the working directory.
		settings string
		dir      string
		// argv is the line the interface is started with, beside the
		// settings file the command resolved.
		argv []string
	}{
		{
			name: "the shorthand",
			args: []string{"-c"},
			argv: []string{"repl", "--settings", "/project/settings.yaml", "-c"},
			door: true,
		},
		{
			name: "the long flag with the repl's flags after it",
			args: []string{"--continue", "--working-dir", "/work"},
			dir:  "/work",
			argv: []string{"repl", "--settings", "/project/settings.yaml", "--continue", "--working-dir", "/work"},
			door: true,
		},
		{
			name:     "resume names the session and keeps its own settings",
			args:     []string{"--resume", "s1", "--settings", "/mine/settings.json"},
			settings: "/mine/settings.json",
			argv:     []string{"repl", "--resume", "s1", "--settings", "/mine/settings.json"},
			door:     true,
		},
		{
			name:     "the settings spelled with an equals sign",
			args:     []string{"-c", "--settings=/mine/settings.json"},
			settings: "/mine/settings.json",
			argv:     []string{"repl", "-c", "--settings=/mine/settings.json"},
			door:     true,
		},
		{
			name: "a word after the terminator is not a flag",
			args: []string{"--continue", "--", "--settings", "/mine/settings.json"},
			argv: []string{"repl", "--settings", "/project/settings.yaml", "--continue", "--", "--settings", "/mine/settings.json"},
			door: true,
		},
		{
			name: "the print alias is not the front door",
			args: []string{"--print", "hello"},
			door: false,
		},
		{
			name: "a flag no alias names keeps its behaviour",
			args: []string{"--ephemeral"},
			door: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			extra, door := frontDoorAlias(c.args)
			if door != c.door {
				t.Fatalf("frontDoorAlias(%q) is the front door: %t, want %t", c.args, door, c.door)
			}
			if !door {
				return
			}
			settings, dir := aliasLine(extra)
			if settings != c.settings || dir != c.dir {
				t.Errorf("aliasLine(%q) is the settings %q in %q, want %q in %q",
					extra, settings, dir, c.settings, c.dir)
			}
			if got := frontDoorLine("repl", "/project/settings.yaml", extra); !slices.Equal(got, c.argv) {
				t.Errorf("the argv is %q, want %q", got, c.argv)
			}
		})
	}
}
