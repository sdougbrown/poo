package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSettings writes a settings file into dir and returns its path.
func writeSettings(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestAuthFileNamesPooOwnCredentialFile covers the setting poo writes a
// credential to: a relative path answers to the settings file's own directory,
// and a file that is not there yet is not a failure, because it is the file a
// login creates.
func TestAuthFileNamesPooOwnCredentialFile(t *testing.T) {
	dir := t.TempDir()
	path := writeSettings(t, dir, `version: 1
auth_file: creds/auth.json
session:
  mode: ephemeral
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	want := filepath.Join(dir, "creds", "auth.json")
	if got := settings.CredentialFile(); got != want {
		t.Errorf("the credential file is %q, want the one the setting named at %q", got, want)
	}
}

// TestNamingBothCredentialFilesIsRefused covers the two settings that answer the
// same question. poo reads one credential at a time, so a settings file that
// names a file for each is refused rather than guessed at.
func TestNamingBothCredentialFilesIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := writeSettings(t, dir, `version: 1
auth_file: poo.json
pi_auth_file: pi.json
session:
  mode: ephemeral
`)
	_, err := Load(path)
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("the settings are %v, want a settings failure", err)
	}
	if failure.Field != "auth_file" {
		t.Errorf("the failure names %q, want auth_file", failure.Field)
	}
	for _, named := range []string{"auth_file", "pi_auth_file"} {
		if !strings.Contains(failure.Error(), named) {
			t.Errorf("the failure is %q, want it to name %s", failure, named)
		}
	}
}

// TestTheCredentialFileBesideTheModelsFileIsRead covers the arrangement pi's
// files already have: an auth.json beside the models file is the credential poo
// has always read there, so a pi setup keeps working without naming a file.
func TestTheCredentialFileBesideTheModelsFileIsRead(t *testing.T) {
	dir := tempUserConfigDir(t)
	models := filepath.Join(dir, ModelsFileName)
	if err := os.WriteFile(models, []byte(`{"providers": {"home": {"baseUrl": "http://127.0.0.1:1/v1", "models": [{"id": "qwen"}]}}}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, AuthFileName), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if got, want := settings.CredentialFile(), filepath.Join(dir, AuthFileName); got != want {
		t.Errorf("the credential file is %q, want %q", got, want)
	}
}

// TestTheDefaultCredentialFileIsPooOwn covers the file poo falls back to: its
// own, in the user's config directory, which is where a credential poo owns
// lives. Nothing beside the settings names one here.
func TestTheDefaultCredentialFileIsPooOwn(t *testing.T) {
	dir := tempUserConfigDir(t)
	path := writeSettings(t, t.TempDir(), `version: 1
session:
  mode: ephemeral
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if got, want := settings.CredentialFile(), filepath.Join(dir, "poo", AuthFileName); got != want {
		t.Errorf("the credential file is %q, want poo's own at %q", got, want)
	}
}
