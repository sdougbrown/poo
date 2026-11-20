package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// noAmbientKeys empties the environment keys a provider's credential can
// be read from, so the catalog a test's flow sees is the one the test's
// own fixtures wake.
func noAmbientKeys(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OLLAMA_API_KEY", "OPENCODE_API_KEY", "TYPESAFE_API_KEY"} {
		t.Setenv(key, "")
	}
}

// TestTheWelcomeFlowWritesTheSettings covers the front door a bare
// invocation takes when nothing is configured: it signs the operator in,
// asks for a chat model and a starting profile, and writes the settings
// file that records the answers. The file is the one discovery finds on
// the next run, and it loads.
func TestTheWelcomeFlowWritesTheSettings(t *testing.T) {
	isolatedConfigHome(t)
	noAmbientKeys(t)
	t.Setenv("POO_SETTINGS", "")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	withStdin(t, "2\nollama-key\n1\n\n")
	var stdout, stderr bytes.Buffer

	path, ok := runWelcome(&stdout, &stderr)
	if !ok {
		t.Fatalf("the welcome flow did not finish; stderr: %s", stderr.String())
	}
	dir, err := config.UserConfigDir()
	if err != nil {
		t.Fatalf("the user config directory is not resolvable here: %v", err)
	}
	if path != filepath.Join(dir, config.SettingsFileName) {
		t.Errorf("the flow wrote %q, want the settings file in the user config directory", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the settings the flow wrote: %v", err)
	}
	var decoded struct {
		Profile string            `json:"profile"`
		Roles   map[string]string `json:"roles"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("the settings the flow wrote do not read: %v", err)
	}
	if decoded.Roles["chat"] != "claude-sonnet-4-5" {
		t.Errorf("the chat role is %q, want the model the operator picked", decoded.Roles["chat"])
	}
	if decoded.Profile != "code" {
		t.Errorf("the profile is %q, want the default the empty answer took", decoded.Profile)
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("the flow's output is %q, want it to say where the settings are", stderr.String())
	}

	// The file is the one discovery finds, and it loads: the session the
	// front door opens next reads what the flow wrote.
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("the settings the flow wrote do not load: %v", err)
	}
	if loaded.Roles["chat"] != "claude-sonnet-4-5" || loaded.Profile != "code" {
		t.Errorf("the loaded settings are roles %v profile %q, want what the flow wrote", loaded.Roles, loaded.Profile)
	}
}

// TestAWelcomeRecoversFromASignInWithNoChatModel covers the flow's dead
// end and its way out: a sign-in whose provider serves no chat turns
// writes no settings file, the flow asks for another sign-in, and the
// one after it finishes the setup the first could not.
func TestAWelcomeRecoversFromASignInWithNoChatModel(t *testing.T) {
	isolatedConfigHome(t)
	noAmbientKeys(t)
	t.Setenv("POO_SETTINGS", "")
	withStdin(t, "4\ntypesafe-key\ny\n3\nopencode-key\n1\n\n")
	var stdout, stderr bytes.Buffer

	path, ok := runWelcome(&stdout, &stderr)
	if !ok {
		t.Fatalf("the flow did not finish after the re-sign-in; stderr: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "no model in the catalog serves chat") {
		t.Errorf("the flow's output is %q, want the no-chat-model refusal on the way", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Sign in with another provider?") {
		t.Errorf("the flow's output is %q, want the offer of another sign-in", stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the settings the flow wrote: %v", err)
	}
	var decoded struct {
		Roles map[string]string `json:"roles"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("the settings the flow wrote do not read: %v", err)
	}
	if decoded.Roles["chat"] != "opencode-anthropic/claude-haiku-5-5" {
		t.Errorf("the chat role is %q, want the model the second sign-in can run", decoded.Roles["chat"])
	}
}

// TestAWelcomeWithNoChatModelStopsWhenDeclined covers the other way out:
// an operator who declines the re-sign-in leaves with the credential
// they wrote and no settings file.
func TestAWelcomeWithNoChatModelStopsWhenDeclined(t *testing.T) {
	isolatedConfigHome(t)
	noAmbientKeys(t)
	t.Setenv("POO_SETTINGS", "")
	withStdin(t, "4\ntypesafe-key\nn\n")
	var stdout, stderr bytes.Buffer

	if _, ok := runWelcome(&stdout, &stderr); ok {
		t.Fatalf("the flow finished after the re-sign-in was declined")
	}
	if strings.Contains(stderr.String(), "Which one? [1-4] Which one?") {
		t.Errorf("the flow's output is %q, and it asked for a sign-in twice", stderr.String())
	}
}

// TestABareFrontDoorWithoutSettingsAsksForATerminal covers the front
// door's guard: a run with no settings and no terminal to ask on is
// refused with the commands that do the same work on their own, and no
// repl starts to fail a moment later.
func TestABareFrontDoorWithoutSettingsAsksForATerminal(t *testing.T) {
	isolatedConfigHome(t)
	t.Setenv("POO_SETTINGS", "")
	withStdin(t, "")

	code, stderr := runCLI(t)
	if code == 0 {
		t.Errorf("a bare invocation with nothing configured exited 0")
	}
	if !strings.Contains(stderr, "poo login") {
		t.Errorf("the refusal is %q, want it to name the command that signs in", stderr)
	}
	if strings.Contains(stderr, "Welcome to poo.") {
		t.Errorf("the refusal is %q, and the welcome flow ran without a terminal to ask on", stderr)
	}
}

// TestAFrontDoorWithSettingsSkipsTheWelcome covers the other side of the
// guard: a settings file that discovery finds launches the interface the
// settings name, and the welcome flow does not run. The refusal is the
// settings load's own, so a file that does not read is told apart from
// one that was never found.
func TestAFrontDoorWithSettingsSkipsTheWelcome(t *testing.T) {
	isolatedConfigHome(t)
	project := t.TempDir()
	settings := filepath.Join(project, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"version":1,"no_such_key":true}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Setenv("POO_SETTINGS", settings)
	withStdin(t, "")

	code, stderr := runCLI(t)
	if code == 0 {
		t.Errorf("a bare invocation over a broken settings file exited 0")
	}
	if strings.Contains(stderr, "Welcome to poo.") {
		t.Errorf("the welcome flow ran over a settings file discovery found")
	}
	if !strings.Contains(stderr, "no_such_key") {
		t.Errorf("the refusal is %q, want the load's own diagnostic", stderr)
	}
}
