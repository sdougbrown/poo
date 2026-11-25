package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// TestDarwinConfigFallbackCredentialNativeDefault covers the native credential path.
func TestDarwinConfigFallbackCredentialNativeDefault(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only credential discovery")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsPath := writeSettings(t, t.TempDir(), "version: 1\nsession:\n  mode: ephemeral\n")
	settings, err := Load(settingsPath)
	if err != nil {
		t.Fatalf("loading settings: %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "poo", AuthFileName)
	if got := settings.CredentialFile(); got != want {
		t.Errorf("CredentialFile() = %q, want %q", got, want)
	}
	if got, err := DefaultAuthFile(); err != nil || got != want {
		t.Errorf("DefaultAuthFile() = %q, %v, want %q", got, err, want)
	}
}

// TestDarwinConfigFallbackLegacyOnlyArbitrarySettings ignores auth beside arbitrary settings.
func TestDarwinConfigFallbackLegacyOnlyArbitrarySettings(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only credential discovery")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	fallback := filepath.Join(home, ".config", "poo", AuthFileName)
	if err := os.MkdirAll(filepath.Dir(fallback), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settingsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(settingsDir, AuthFileName), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settingsPath := writeSettings(t, settingsDir, "version: 1\nsession:\n  mode: ephemeral\n")
	settings, err := Load(settingsPath)
	if err != nil {
		t.Fatalf("loading settings: %v", err)
	}
	if got := settings.CredentialFile(); got != fallback {
		t.Errorf("CredentialFile() = %q, want %q", got, fallback)
	}
	if got, err := DefaultAuthFile(); err != nil || got != fallback {
		t.Errorf("DefaultAuthFile() = %q, %v, want %q", got, err, fallback)
	}
}

// TestDarwinConfigFallbackCredentialNativeWins keeps native credentials ahead of fallback.
func TestDarwinConfigFallbackCredentialNativeWins(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only credential discovery")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	native := filepath.Join(home, "Library", "Application Support", "poo", AuthFileName)
	fallback := filepath.Join(home, ".config", "poo", AuthFileName)
	for _, path := range []string{native, fallback} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := Load(writeSettings(t, t.TempDir(), "version: 1\nsession:\n  mode: ephemeral\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.CredentialFile(); got != native {
		t.Errorf("CredentialFile() = %q, want %q", got, native)
	}
	if got, err := DefaultAuthFile(); err != nil || got != native {
		t.Errorf("DefaultAuthFile() = %q, %v, want %q", got, err, native)
	}
}

// TestDarwinConfigFallbackIgnoresCredentialPipe keeps the default write target
// when the alternate path would block a reader instead of providing a file.
func TestDarwinConfigFallbackIgnoresCredentialPipe(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only credential discovery")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	fallback := filepath.Join(home, ".config", "poo", AuthFileName)
	if err := os.MkdirAll(filepath.Dir(fallback), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("mkfifo", fallback).CombinedOutput(); err != nil {
		t.Fatalf("creating the credential pipe: %v: %s", err, out)
	}
	want := filepath.Join(home, "Library", "Application Support", "poo", AuthFileName)
	if got, err := DefaultAuthFile(); err != nil || got != want {
		t.Errorf("DefaultAuthFile() = %q, %v, want %q", got, err, want)
	}
}

// TestDarwinConfigFallbackCredentialRelativeHome rejects cwd-relative candidates.
func TestDarwinConfigFallbackCredentialRelativeHome(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only credential discovery")
	}
	t.Chdir(t.TempDir())
	t.Setenv("HOME", "relative-home")
	if err := os.MkdirAll(filepath.Join("relative-home", ".config", "poo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("relative-home", ".config", "poo", AuthFileName), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DefaultAuthFile()
	if err == nil || got != "" {
		t.Errorf("DefaultAuthFile() = %q, %v; want empty path and error", got, err)
	}
}

// TestTheCredentialFileBesideTheModelsFileIsAnsweredBeforeItExists covers the file
// that is not there yet. A settings load resolves the path once, and a server keeps
// that answer: if it were pinned to poo's default because pi's file happened to be
// missing at load, a credential pi writes afterwards would never be read.
func TestTheCredentialFileBesideTheModelsFileIsAnsweredBeforeItExists(t *testing.T) {
	dir := tempUserConfigDir(t)
	models := filepath.Join(dir, ModelsFileName)
	if err := os.WriteFile(models, []byte(`{"providers": {"home": {"baseUrl": "http://127.0.0.1:1/v1", "models": [{"id": "qwen"}]}}}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	beside := filepath.Join(dir, AuthFileName)
	if _, err := os.Stat(beside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the test's credential file already exists: %v", err)
	}
	path := writeSettings(t, dir, `version: 1
session:
  mode: ephemeral
`)
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if got := settings.CredentialFile(); got != beside {
		t.Errorf("the credential file is %q, want the one beside the models file at %q", got, beside)
	}
	// The file arrives afterwards, and the answer already pointed at it.
	if err := os.WriteFile(beside, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	if got := settings.CredentialFile(); got != beside {
		t.Errorf("the credential file is %q after the file arrived, want %q", got, beside)
	}
}

// TestTheCredentialFilePooWritesIsNamedForItsEncoding covers the name poo's own file
// goes by. The file is written as YAML, so the name says YAML: a file named .json that
// holds YAML misleads anything that reads it by its extension.
func TestTheCredentialFilePooWritesIsNamedForItsEncoding(t *testing.T) {
	dir := tempUserConfigDir(t)
	path := writeSettings(t, t.TempDir(), "version: 1\nsession:\n  mode: ephemeral\n")
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	want := filepath.Join(dir, "poo", "auth.yaml")
	if got := settings.CredentialFile(); got != want {
		t.Errorf("the credential file is %q, want %q", got, want)
	}
}

// TestTheLegacyCredentialFileIsReadWhereItIs covers the file poo wrote before the name
// described its encoding: an operator who already signed in there is not migrated out
// of their credential, and poo keeps reading and writing the file that is there.
func TestTheLegacyCredentialFileIsReadWhereItIs(t *testing.T) {
	dir := tempUserConfigDir(t)
	poo := filepath.Join(dir, "poo")
	if err := os.MkdirAll(poo, 0o700); err != nil {
		t.Fatalf("creating the credential directory: %v", err)
	}
	legacy := filepath.Join(poo, LegacyAuthFileName)
	if err := os.WriteFile(legacy, []byte("version: 1\ncredentials:\n"), 0o600); err != nil {
		t.Fatalf("writing the legacy credential file: %v", err)
	}
	path := writeSettings(t, t.TempDir(), "version: 1\nsession:\n  mode: ephemeral\n")
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if got := settings.CredentialFile(); got != legacy {
		t.Errorf("the credential file is %q, want the file that is there at %q", got, legacy)
	}

	// The new name wins where an operator has moved the file to it.
	named := filepath.Join(poo, AuthFileName)
	if err := os.Rename(legacy, named); err != nil {
		t.Fatalf("moving the credential file: %v", err)
	}
	settings, err = Load(path)
	if err != nil {
		t.Fatalf("loading the settings again: %v", err)
	}
	if got := settings.CredentialFile(); got != named {
		t.Errorf("the credential file is %q, want the one it was moved to at %q", got, named)
	}
}

// TestTheCredentialFileBesidePisModelsFileIsPisOwn covers the pi setup poo reads
// without naming a file. pi writes its credentials to auth.json beside its models.json,
// and poo answers that path before pi's first login, so a credential pi writes later is
// the one poo reads.
func TestTheCredentialFileBesidePisModelsFileIsPisOwn(t *testing.T) {
	dir := tempUserConfigDir(t)
	if err := os.WriteFile(filepath.Join(dir, PiModelsFileName),
		[]byte(`{"providers": {"home": {"baseUrl": "http://127.0.0.1:1/v1", "models": [{"id": "qwen"}]}}}`), 0o644); err != nil {
		t.Fatalf("writing pi's models file: %v", err)
	}
	path := writeSettings(t, dir, "version: 1\nsession:\n  mode: ephemeral\n")
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	want := filepath.Join(dir, "auth.json")
	if got := settings.CredentialFile(); got != want {
		t.Fatalf("the credential file is %q, want pi's own at %q", got, want)
	}
	// pi signs in afterwards, and the answer already pointed at pi's file.
	if err := os.WriteFile(want, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("writing pi's credential: %v", err)
	}
	if got := settings.CredentialFile(); got != want {
		t.Errorf("the credential file is %q after pi wrote, want %q", got, want)
	}
}
