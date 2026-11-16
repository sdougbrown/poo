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

// piCredential is a pi auth.json holding one Codex credential.
func piCredential(t *testing.T, dir, access string, expires int64) string {
	t.Helper()
	path := filepath.Join(dir, "pi-auth.json")
	body := `{"openai-codex": {"type": "oauth", "access": "` + access + `", "refresh": "a-refresh-token", "expires": ` + itoa64(expires) + `, "accountId": "acct-from-pi"}, "anthropic": {"type": "api_key", "key": "$ANTHROPIC_API_KEY"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing pi's credential file: %v", err)
	}
	return path
}

// TestTheAuthImportCopiesPisCredentialIntoPoosFile covers the import: the entry
// pi's file holds is written into poo's own file, in poo's shape, and the message
// says what the copy costs.
func TestTheAuthImportCopiesPisCredentialIntoPoosFile(t *testing.T) {
	dir := t.TempDir()
	pi := piCredential(t, dir, "an-access-token-from-pi", 4102444800000)
	target := filepath.Join(dir, "auth.json")
	settings := authSettings(t, dir, target)

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "import", "pi", "--settings", settings, "--from", pi}); code != 0 {
			t.Errorf("poo auth import pi exited with %d", code)
		}
	})
	for _, want := range []string{"acct-from-pi", pi, target, "pi's copy is stale"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is %q, want it to name %q", out, want)
		}
	}
	stored, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading poo's credential file: %v", err)
	}
	for _, want := range []string{"version: 1", "openai-codex:", "an-access-token-from-pi", "acct-from-pi"} {
		if !strings.Contains(string(stored), want) {
			t.Errorf("poo's file is\n%s\nwant it to hold %q", stored, want)
		}
	}
	if strings.Contains(string(stored), "ANTHROPIC_API_KEY") {
		t.Errorf("the import copied pi's other providers:\n%s", stored)
	}
	// pi's file is not touched: it is another tool's.
	before, _ := os.ReadFile(pi)
	if !strings.Contains(string(before), "ANTHROPIC_API_KEY") {
		t.Error("pi's file was rewritten")
	}
}

// TestTheAuthImportRefusesToReplaceACredential covers the replace rule: a file that
// already holds a credential keeps it unless the operator asks.
func TestTheAuthImportRefusesToReplaceACredential(t *testing.T) {
	dir := t.TempDir()
	pi := piCredential(t, dir, "an-access-token-from-pi", 4102444800000)
	target := filepath.Join(dir, "auth.json")
	codexCredential(t, dir, 4102444800000)
	if err := os.WriteFile(target, []byte(`{
  "version": 1,
  "credentials": {
    "openai-codex": {"type": "oauth", "access": "already-there", "refresh": "r", "expires": 4102444800000, "accountId": "acct-already"}
  }
}`), 0o600); err != nil {
		t.Fatalf("writing poo's credential file: %v", err)
	}
	settings := authSettings(t, dir, target)

	if code := run([]string{"auth", "import", "pi", "--settings", settings, "--from", pi}); code == 0 {
		t.Error("the import exited 0 for a credential it was not asked to replace")
	}
	stored, _ := os.ReadFile(target)
	if !strings.Contains(string(stored), "already-there") {
		t.Errorf("the credential was replaced without being asked:\n%s", stored)
	}
	if code := run([]string{"auth", "import", "pi", "--settings", settings, "--from", pi, "--force"}); code != 0 {
		t.Error("the forced import failed")
	}
	stored, _ = os.ReadFile(target)
	if !strings.Contains(string(stored), "an-access-token-from-pi") {
		t.Errorf("the forced import did not replace the credential:\n%s", stored)
	}
}

// TestTheAuthImportRefusesTheOneFile covers a copy that would write a file onto
// itself: there is nothing to import.
func TestTheAuthImportRefusesTheOneFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.json")
	settings := authSettings(t, dir, target)
	if err := os.WriteFile(target, []byte(`{"openai-codex": {"type": "oauth", "access": "a", "refresh": "r", "expires": 4102444800000, "accountId": "acct"}}`), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	code, _ := runCLI(t, "auth", "import", "pi", "--settings", settings, "--from", target)
	if code != usageExitCode {
		t.Errorf("the exit code is %d, want %d for a copy onto the same file", code, usageExitCode)
	}
}

// TestTheAuthImportSaysWherePoosReadPoints covers the mismatch an operator can land
// in: the copy is in poo's file, and the settings still resolve the credential to the
// file it was copied from. The command names the setting that fixes it.
func TestTheAuthImportSaysWherePoosReadPoints(t *testing.T) {
	dir := t.TempDir()
	pi := piCredential(t, dir, "an-access-token-from-pi", 4102444800000)
	// The settings name pi's file, so that is what poo resolves and reads.
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte(`version: 1
pi_auth_file: `+pi+`
session:
  mode: ephemeral
`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	t.Setenv("APPDATA", configDir)

	code, _, out := runCLIWithStdout(t, "auth", "import", "pi", "--settings", settings)
	if code != 0 {
		t.Fatalf("poo auth import pi exited with %d:\n%s", code, out)
	}
	if !strings.Contains(out, "auth_file:") {
		t.Errorf("the report is %q, want it to name the setting that points poo at the copy", out)
	}
	if !strings.Contains(out, pi) {
		t.Errorf("the report is %q, want it to name the file poo still reads", out)
	}
}

// TestTheAuthImportFindsPisOwnCredential covers the source nothing names: pi keeps its
// credential in its own directory, and a bare `poo auth import pi` must find it there. A
// wrong path or an inverted check would tell a signed-in operator there is nothing to
// copy.
func TestTheAuthImportFindsPisOwnCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	piDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0o700); err != nil {
		t.Fatalf("creating pi's directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piDir, "auth.json"), []byte(`{"openai-codex": {"type": "oauth", "access": "an-access-token-from-pi", "refresh": "r", "expires": 4102444800000, "accountId": "acct-from-pi"}}`), 0o600); err != nil {
		t.Fatalf("writing pi's credential: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.json")
	settings := authSettings(t, dir, target)

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "import", "pi", "--settings", settings}); code != 0 {
			t.Errorf("poo auth import pi exited with %d", code)
		}
	})
	if !strings.Contains(out, "acct-from-pi") {
		t.Errorf("the report is %q, want the credential pi holds", out)
	}
	stored, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading poo's credential file: %v", err)
	}
	if !strings.Contains(string(stored), "an-access-token-from-pi") {
		t.Errorf("poo's file is\n%s\nwant pi's credential", stored)
	}
}

// TestTheAuthImportSaysWhenPisFileIsNotThere covers the refusal: a home with no pi
// credential is nothing to copy, and the command says so instead of writing an empty file.
func TestTheAuthImportSaysWhenPisFileIsNotThere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.json")
	settings := authSettings(t, dir, target)

	code, stderr := runCLI(t, "auth", "import", "pi", "--settings", settings)
	if code != usageExitCode {
		t.Errorf("the exit code is %d, want %d", code, usageExitCode)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("the failure is %q, want it to say pi's file was not found", stderr)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("poo's credential file was written with nothing to copy")
	}
}

// TestTheAuthImportRefusesANamedFileThatIsNotThere covers the promise an explicit path
// makes: a --from that names a missing file fails, rather than falling through to pi's own
// directory and importing a file the operator did not name.
//
// A pi_auth_file that is missing is refused earlier, when the settings load: an explicit
// path is a promise there too.
func TestTheAuthImportRefusesANamedFileThatIsNotThere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	piDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0o700); err != nil {
		t.Fatalf("creating pi's directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piDir, "auth.json"), []byte(`{"openai-codex": {"type": "oauth", "access": "another-file", "refresh": "r", "expires": 4102444800000, "accountId": "acct-other"}}`), 0o600); err != nil {
		t.Fatalf("writing pi's credential: %v", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.json")
	settings := authSettings(t, dir, target)
	code, stderr := runCLI(t, "auth", "import", "pi", "--settings", settings, "--from", filepath.Join(dir, "typo.json"))
	if code != usageExitCode {
		t.Errorf("the exit code is %d, want %d", code, usageExitCode)
	}
	if !strings.Contains(stderr, "not there") {
		t.Errorf("the failure is %q, want it to say the named file is not there", stderr)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("poo's credential file was written from a file the settings did not name")
	}
}

// TestTheConfirmationReadsAnAnswer covers the prompt's parsing. An inverted answer would
// replace a credential the operator declined to replace, which is the one thing the
// ErrExists rule exists to prevent.
func TestTheConfirmationReadsAnAnswer(t *testing.T) {
	for _, c := range []struct {
		name  string
		input string
		want  bool
	}{
		{name: "yes", input: "y\n", want: true},
		{name: "the whole word", input: "YES\n", want: true},
		{name: "no", input: "n\n"},
		{name: "nothing at all", input: "\n"},
		{name: "no input", input: ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := confirmed(strings.NewReader(c.input)); got != c.want {
				t.Errorf("the answer %q reads as %t, want %t", c.input, got, c.want)
			}
		})
	}
}
