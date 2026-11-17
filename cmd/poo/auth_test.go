package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codexCredential is a poo-format credential file holding one Codex entry whose
// access token expires at the given millisecond.
func codexCredential(t *testing.T, dir string, expires int64) string {
	t.Helper()
	path := filepath.Join(dir, "auth.json")
	body := `{
  "version": 1,
  "credentials": {
    "openai-codex": {
      "type": "oauth",
      "access": "an-access-token",
      "refresh": "a-refresh-token",
      "expires": ` + itoa64(expires) + `,
      "accountId": "acct-1234"
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	return path
}

// itoa64 renders a millisecond count.
func itoa64(v int64) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// authSettings is a settings file naming a credential file of the test's own.
func authSettings(t *testing.T, dir, authFile string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	body := `version: 1
auth_file: ` + authFile + `
session:
  mode: ephemeral
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestTheAuthStatusReportsTheCredential covers the report: the file, the shape,
// the provider, the account and the state, with no token value in it.
func TestTheAuthStatusReportsTheCredential(t *testing.T) {
	dir := t.TempDir()
	credential := codexCredential(t, dir, 4102444800000)
	settings := authSettings(t, dir, credential)

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settings}); code != 0 {
			t.Errorf("poo auth status exited with %d", code)
		}
	})
	for _, want := range []string{
		"FILE", credential,
		"FORMAT", "poo",
		"PROVIDER", "openai-codex",
		"ACCOUNT", "acct-1234",
		"STATE", "live",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is %q, want it to name %q", out, want)
		}
	}
	for _, secret := range []string{"an-access-token", "a-refresh-token"} {
		if strings.Contains(out, secret) {
			t.Errorf("the report carries the credential: %q", out)
		}
	}
}

// TestTheAuthStatusAnswersInJSON covers the flag: the same report, in the names
// a script reads.
func TestTheAuthStatusAnswersInJSON(t *testing.T) {
	dir := t.TempDir()
	credential := codexCredential(t, dir, 4102444800000)
	settings := authSettings(t, dir, credential)

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settings, "--json"}); code != 0 {
			t.Errorf("poo auth status --json exited with %d", code)
		}
	})
	var report authStatus
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, out)
	}
	if report.File != credential {
		t.Errorf("the report names the file %q, want %q", report.File, credential)
	}
	if report.Format != "poo" || report.Provider != "openai-codex" || report.State != "live" {
		t.Errorf("the report is %+v, want poo's file for the Codex provider, live", report)
	}
	if report.AccountID != "acct-1234" {
		t.Errorf("the report names the account %q, want acct-1234", report.AccountID)
	}
}

// TestTheAuthStatusOfAMissingCredentialIsAFailure covers the report with nothing
// to report on: a command that names no credential is a failure, so a script
// can tell.
func TestTheAuthStatusOfAMissingCredentialIsAFailure(t *testing.T) {
	dir := t.TempDir()
	settings := authSettings(t, dir, filepath.Join(dir, "missing.json"))

	if code := run([]string{"auth", "status", "--settings", settings}); code == 0 {
		t.Errorf("poo auth status exited 0 for a credential that is not there")
	}
}
