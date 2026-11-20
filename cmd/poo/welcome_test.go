package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/auth"
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

// storeCredential writes a credential into poo's own file, the state a
// `poo login` leaves behind.
func storeCredential(t *testing.T, provider auth.Provider, credential auth.Credential) {
	t.Helper()
	path, err := config.DefaultAuthFile()
	if err != nil {
		t.Fatalf("the default credential file is not resolvable here: %v", err)
	}
	credentials := auth.Credentials{Path: path}
	if _, err := credentials.Set(context.Background(), provider, credential, true); err != nil {
		t.Fatalf("writing the credential fixture: %v", err)
	}
}

// TestAWelcomeWithACredentialSkipsTheSignIn covers the shortcut the
// existing credential takes: an operator who ran `poo login` before their
// first run is not asked to sign in again. The flow proceeds from the
// credential that is there.
func TestAWelcomeWithACredentialSkipsTheSignIn(t *testing.T) {
	isolatedConfigHome(t)
	noAmbientKeys(t)
	t.Setenv("POO_SETTINGS", "")
	storeCredential(t, auth.Opencode, auth.Credential{Kind: auth.KindAPIKey, Key: "oc-key"})
	withStdin(t, "1\n\n")
	var stdout, stderr bytes.Buffer

	path, ok := runWelcome(&stdout, &stderr)
	if !ok {
		t.Fatalf("the flow did not finish; stderr: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "Sign in with:") {
		t.Errorf("the flow's output is %q, and it asked for a sign-in the credential file already holds", stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the settings the flow wrote: %v", err)
	}
	if !strings.Contains(string(data), "opencode-anthropic/claude-haiku-5-5") {
		t.Errorf("the settings are %q, want the chat model the credential runs", string(data))
	}
}

// TestThePickerOffersOnlyModelsTheCredentialRuns covers the picker's two
// credential rungs: an api-key credential wakes the endpoints that read
// its provider's variable, and the Codex endpoint is offered only when
// the file holds the Codex credential of its own.
func TestThePickerOffersOnlyModelsTheCredentialRuns(t *testing.T) {
	isolatedConfigHome(t)
	noAmbientKeys(t)
	t.Setenv("POO_SETTINGS", "")

	// An OpenCode credential wakes the OpenCode endpoints, and nothing
	// else: not the Codex endpoint the file's existence alone would wake.
	storeCredential(t, auth.Opencode, auth.Credential{Kind: auth.KindAPIKey, Key: "oc-key"})
	withStdin(t, "1\n\n")
	var stderr bytes.Buffer
	if _, ok := runWelcome(io.Discard, &stderr); !ok {
		t.Fatalf("the flow did not finish; stderr: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "openai-codex/") {
		t.Errorf("the picker offered a Codex model the credential cannot run: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "claude-sonnet-4-5") {
		t.Errorf("the picker offered a hosted model no key wakes: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "opencode/glm-5.3") {
		t.Errorf("the picker's output is %q, want the models the credential runs", stderr.String())
	}

	// A Codex credential brings the Codex models back, and naming one
	// writes it as the chat role.
	storeCredential(t, auth.Codex, auth.Credential{Access: "access", Refresh: "refresh"})
	withStdin(t, "1\n\n")
	stderr.Reset()
	path, ok := runWelcome(io.Discard, &stderr)
	if !ok {
		t.Fatalf("the flow did not finish with a Codex credential; stderr: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "openai-codex/gpt-5.4-mini") {
		t.Errorf("the picker's output is %q, want the Codex models the credential runs", stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the settings the flow wrote: %v", err)
	}
	if !strings.Contains(string(data), "openai-codex/gpt-5.4-mini") {
		t.Errorf("the settings are %q, want the Codex model the operator picked", string(data))
	}
}

// TestAWelcomeDoesNotClobberExistingSettings covers the create-not-clobber
// rule: a settings file that is already there is refused and left as the
// operator wrote it, not traded for the flow's answers.
func TestAWelcomeDoesNotClobberExistingSettings(t *testing.T) {
	isolatedConfigHome(t)
	noAmbientKeys(t)
	t.Setenv("POO_SETTINGS", "")
	storeCredential(t, auth.Opencode, auth.Credential{Kind: auth.KindAPIKey, Key: "oc-key"})
	dir, err := config.UserConfigDir()
	if err != nil {
		t.Fatalf("the user config directory is not resolvable here: %v", err)
	}
	path := filepath.Join(dir, config.SettingsFileName)
	operators := "{\n  \"version\": 1,\n  \"profile\": \"search\"\n}\n"
	if err := os.WriteFile(path, []byte(operators), 0o644); err != nil {
		t.Fatalf("writing the operator's settings: %v", err)
	}
	withStdin(t, "1\n\n")
	var stdout, stderr bytes.Buffer

	if _, ok := runWelcome(&stdout, &stderr); ok {
		t.Fatalf("the flow finished over a settings file that was already there")
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("the flow's output is %q, want it to name the file that is already there", stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the settings: %v", err)
	}
	if string(data) != operators {
		t.Errorf("the settings are now %q, want the operator's own, byte for byte", string(data))
	}
}
