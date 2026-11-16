package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/provider/auth"
)

// loginSettings is a settings file naming a credential file of the test's own.
func loginSettings(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte(`version: 1
auth_file: `+filepath.Join(dir, "auth.json")+`
session:
  mode: ephemeral
`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestALoginWithNoProviderNamesTheOnesItOffers covers the bare command with no terminal to ask
// on: it says what to type instead of guessing.
func TestALoginWithNoProviderNamesTheOnesItOffers(t *testing.T) {
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withoutATerminal(t)

	code, stderr := runCLI(t, "login", "--settings", settings)
	if code != usageExitCode {
		t.Errorf("the exit code is %d, want %d", code, usageExitCode)
	}
	for _, want := range []string{"codex", "ollama-cloud"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal is %q, want it to name %q", stderr, want)
		}
	}
}

// TestALoginWithAnUnknownProviderIsRefused covers the name that is not one of the sign-ins.
func TestALoginWithAnUnknownProviderIsRefused(t *testing.T) {
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withoutATerminal(t)

	code, stderr := runCLI(t, "login", "gemini", "--settings", settings)
	if code != usageExitCode {
		t.Errorf("the exit code is %d, want %d", code, usageExitCode)
	}
	if !strings.Contains(stderr, "gemini") {
		t.Errorf("the refusal is %q, want it to name what was asked for", stderr)
	}
}

// TestAKeyLoginStoresAnAPIKey covers the sign-in that needs nothing but a key: the credential is
// written as an api key entry, in poo's own file, and the command says who it signed in as.
func TestAKeyLoginStoresAnAPIKey(t *testing.T) {
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withoutATerminal(t)

	out := captureStdout(t, func() {
		if code := run([]string{"login", "ollama-cloud", "--key", "an-api-key", "--settings", settings}); code != 0 {
			t.Errorf("poo login exited with %d", code)
		}
	})
	if !strings.Contains(out, "ollama-cloud") {
		t.Errorf("the report is %q, want it to name the provider", out)
	}
	if strings.Contains(out, "an-api-key") {
		t.Errorf("the report carries the key: %q", out)
	}
	stored, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatalf("reading poo's credential file: %v", err)
	}
	for _, want := range []string{"type: api_key", "key: an-api-key"} {
		if !strings.Contains(string(stored), want) {
			t.Errorf("the file is\n%s\nwant it to hold %q", stored, want)
		}
	}
	// And it reads back as the credential it is.
	credentials := auth.Credentials{Path: filepath.Join(dir, "auth.json")}
	entry, err := credentials.Read(ollamaCloud.Provider)
	if err != nil {
		t.Fatalf("reading the credential back: %v", err)
	}
	if entry.Kind != auth.KindAPIKey || entry.Key != "an-api-key" {
		t.Errorf("the credential reads back as %+v", entry)
	}
}

// TestAKeyLoginTakesTheKeyFromStdin covers the key that is not on the command line, so it need
// not enter a shell's history.
func TestAKeyLoginTakesTheKeyFromStdin(t *testing.T) {
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withStdin(t, "the-key-from-stdin\n")

	if code := run([]string{"login", "ollama-cloud", "--settings", settings}); code != 0 {
		t.Errorf("poo login exited with %d", code)
	}
	credentials := auth.Credentials{Path: filepath.Join(dir, "auth.json")}
	if entry, err := credentials.Read(ollamaCloud.Provider); err != nil || entry.Key != "the-key-from-stdin" {
		t.Errorf("the credential is %+v, %v", entry, err)
	}
}

// TestAKeyLoginWithNoKeyIsAFailure covers the sign-in that has nothing to sign in with.
func TestAKeyLoginWithNoKeyIsAFailure(t *testing.T) {
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withStdin(t, "")

	code, stderr := runCLI(t, "login", "ollama-cloud", "--settings", settings)
	if code != operationalExitCode {
		t.Errorf("the exit code is %d, want %d", code, operationalExitCode)
	}
	if !strings.Contains(stderr, "no key") {
		t.Errorf("the failure is %q, want it to say no key was given", stderr)
	}
}

// TestAKeyLoginDoesNotReplaceAnExistingCredential covers the replace rule at the command that
// signs in: an entry that is there stays until the operator asks.
func TestAKeyLoginDoesNotReplaceAnExistingCredential(t *testing.T) {
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withoutATerminal(t)
	target := filepath.Join(dir, "auth.json")
	body := "version: 1\ncredentials:\n  ollama-cloud:\n    type: api_key\n    key: already-there\n"
	if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}

	if code := run([]string{"login", "ollama-cloud", "--key", "a-new-key", "--settings", settings}); code == 0 {
		t.Error("the sign-in exited 0 for a credential it was not asked to replace")
	}
	if stored, _ := os.ReadFile(target); string(stored) != body {
		t.Errorf("the credential was replaced without being asked:\n%s", stored)
	}
	if code := run([]string{"login", "ollama-cloud", "--key", "a-new-key", "--force", "--settings", settings}); code != 0 {
		t.Errorf("the forced sign-in exited with %d", code)
	}
	if stored, _ := os.ReadFile(target); !strings.Contains(string(stored), "a-new-key") {
		t.Errorf("the forced sign-in did not replace the credential:\n%s", stored)
	}
}

// TestTheCallbackComesBackOnTheFlowsRedirect covers the wait: a request to the path the flow's
// redirect names ends it, and the callback it carries is what the exchange is given.
func TestTheCallbackComesBackOnTheFlowsRedirect(t *testing.T) {
	attempt, err := codexAttemptForTest()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	done := make(chan auth.Callback, 1)
	failed := make(chan error, 1)
	go func() {
		callback, err := waitForCallback(context.Background(), attempt, 5e9, io.Discard)
		if err != nil {
			failed <- err
			return
		}
		done <- callback
	}()
	// The listener is up by the time the connection succeeds.
	var response error
	for i := 0; i < 100; i++ {
		response = getCallback(attempt)
		if response == nil {
			break
		}
	}
	if response != nil {
		t.Fatalf("the callback could not be delivered: %v", response)
	}
	select {
	case callback := <-done:
		if callback.Code != "the-code" || callback.State != attempt.State {
			t.Errorf("the callback is %+v", callback)
		}
	case err := <-failed:
		t.Fatalf("the wait failed: %v", err)
	}
}
