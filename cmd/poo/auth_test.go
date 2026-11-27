package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
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
	if report.ClientID != "" {
		t.Errorf("the report names the client %q for a file that records none, want the field omitted", report.ClientID)
	}
	if strings.Contains(out, "client_id") {
		t.Errorf("the report carries client_id for a file that records none:\n%s", out)
	}
}

// TestTheAuthStatusReportsAStoredOpencodeKey covers the report of a file that
// holds no Codex token and no Ollama key: the OpenCode key is the credential
// the report names, live, because a key is sent as it is.
func TestTheAuthStatusReportsAStoredOpencodeKey(t *testing.T) {
	dir := t.TempDir()
	credential := filepath.Join(dir, "auth.json")
	body := `{"version": 1, "credentials": {"opencode": {"type": "api_key", "key": "oc_sk_test"}}}`
	if err := os.WriteFile(credential, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
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
	if report.Format != "poo" || report.Provider != "opencode" || report.State != "live" {
		t.Errorf("the report is %+v, want poo's file for the OpenCode provider, live", report)
	}
	if strings.Contains(out, "oc_sk_test") {
		t.Errorf("the report carries the key:\n%s", out)
	}
}

// TestTheAuthStatusNamesTheClientTheFileRecords covers the report of a
// credential that records the client it was granted to: the CLIENT line names
// it, and the JSON report carries client_id, so an operator can tell whose
// grant the file holds without opening it.
func TestTheAuthStatusNamesTheClientTheFileRecords(t *testing.T) {
	dir := t.TempDir()
	credential := filepath.Join(dir, "auth.json")
	body := `{"version": 1, "credentials": {"openai-codex": {"type": "oauth", "access": "an-access-token", "refresh": "r", "expires": 4102444800000, "accountId": "acct-1234", "clientId": "app_the-grant"}}}`
	if err := os.WriteFile(credential, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	settings := authSettings(t, dir, credential)

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settings}); code != 0 {
			t.Errorf("poo auth status exited with %d", code)
		}
	})
	for _, want := range []string{"CLIENT", "app_the-grant"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is %q, want it to name %q", out, want)
		}
	}

	out = captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settings, "--json"}); code != 0 {
			t.Errorf("poo auth status --json exited with %d", code)
		}
	})
	var report authStatus
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, out)
	}
	if report.ClientID != "app_the-grant" {
		t.Errorf("the report names the client %q, want app_the-grant", report.ClientID)
	}
}

// TestTheAuthStatusReportsAStoredKey covers the credential that has no tokens: the key poo's
// file holds for Ollama Cloud is reported as it is — live, with no expiry, and without the key.
func TestTheAuthStatusReportsAStoredKey(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withoutATerminal(t)
	withStdin(t, "the-stored-key\n")
	if code := run([]string{"login", "ollama-cloud", "--force", "--settings", settings}); code != 0 {
		t.Fatalf("poo login exited with %d", code)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settings}); code != 0 {
			t.Errorf("poo auth status exited with %d", code)
		}
	})
	for _, want := range []string{
		"FILE", filepath.Join(dir, "auth.json"),
		"PROVIDER", "ollama-cloud",
		"STATE", "live",
		"does not expire",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is %q, want it to name %q", out, want)
		}
	}
	if strings.Contains(out, "expired") {
		t.Errorf("the report gives a key an expiry: %q", out)
	}
	if strings.Contains(out, "the-stored-key") {
		t.Errorf("the report carries the key: %q", out)
	}

	out = captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settings, "--json"}); code != 0 {
			t.Errorf("poo auth status --json exited with %d", code)
		}
	})
	var report authStatus
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, out)
	}
	if report.Provider != "ollama-cloud" || report.State != "live" {
		t.Errorf("the report is %+v, want the Ollama Cloud key, live", report)
	}
	if report.Expires != "" {
		t.Errorf("the report gives a key the expiry %q, want none", report.Expires)
	}
}

// TestTheAuthStatusReportsACorruptEntryInsteadOfAKeyBesideIt covers a file holding two entries:
// a Codex entry that does not read, and a key that does. The entry poo cannot use is the failure
// the report is for — a key found past it would report a file that works, and drop the one thing
// the operator has to fix.
func TestTheAuthStatusReportsACorruptEntryInsteadOfAKeyBesideIt(t *testing.T) {
	dir := t.TempDir()
	credential := filepath.Join(dir, "auth.json")
	body := `{
  "version": 1,
  "credentials": {
    "openai-codex": {"type": "oauth", "accountId": "acct-1234"},
    "ollama-cloud": {"type": "api_key", "key": "a-key-beside-it"}
  }
}`
	if err := os.WriteFile(credential, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	settings := authSettings(t, dir, credential)

	code, stderr := runCLI(t, "auth", "status", "--settings", settings)
	if code == 0 {
		t.Error("poo auth status exited 0 for a file whose Codex entry does not read")
	}
	for _, want := range []string{"holds an entry for openai-codex", "access token"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the failure is %q, want it to name %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "a-key-beside-it") {
		t.Errorf("the failure carries the key: %q", stderr)
	}
}

// TestTheAuthStatusReportsACorruptKeyInsteadOfASignIn covers a file whose only entry is the key:
// the Codex read finds no entry, and the key read finds one that carries no key. The key is the
// credential the file holds, so the failure the operator can act on is the key's; naming the
// Codex entry would send them to a sign-in that is not the problem.
func TestTheAuthStatusReportsACorruptKeyInsteadOfASignIn(t *testing.T) {
	dir := t.TempDir()
	credential := filepath.Join(dir, "auth.json")
	body := `{
  "version": 1,
  "credentials": {
    "ollama-cloud": {"type": "api_key"}
  }
}`
	if err := os.WriteFile(credential, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	settings := authSettings(t, dir, credential)

	code, stderr := runCLI(t, "auth", "status", "--settings", settings)
	if code == 0 {
		t.Error("poo auth status exited 0 for a key entry that carries no key")
	}
	if !strings.Contains(stderr, "an api key credential needs a key") {
		t.Errorf("the failure is %q, want it to say the key entry carries no key", stderr)
	}
	if strings.Contains(stderr, "holds no openai-codex entry") {
		t.Errorf("the failure names a Codex entry the report was not asked about: %q", stderr)
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
	if err := os.WriteFile(target, []byte(`{
  "version": 1,
  "credentials": {
    "openai-codex": {"type": "oauth", "access": "already-there", "refresh": "r", "expires": 4102444800000, "accountId": "acct-already"}
  }
}`), 0o600); err != nil {
		t.Fatalf("writing poo's credential file: %v", err)
	}
	settings := authSettings(t, dir, target)

	stdin := withoutATerminal(t)
	if code := run([]string{"auth", "import", "pi", "--settings", settings, "--from", pi}); code == 0 {
		t.Error("the import exited 0 for a credential it was not asked to replace")
	}
	_ = stdin
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
	for _, want := range []string{
		"clientId: " + auth.DefaultClientID,
		"tokenUrl: " + auth.DefaultTokenURL,
	} {
		if !strings.Contains(string(stored), want) {
			t.Errorf("poo's file is\n%s\nwant it to carry the stamped default %q", stored, want)
		}
	}
}

// TestTheImportKeepsTheClientTheSourceRecords covers a source that names its own grant: a
// credential file that records the client it was granted to keeps that record through the
// import, so poo refreshes the grant as the granting client, not poo's default.
func TestTheImportKeepsTheClientTheSourceRecords(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "elsewhere.json")
	body := `{"openai-codex": {"type": "oauth", "access": "a-granted-token", "refresh": "r", "expires": 4102444800000, "accountId": "acct-granted", "clientId": "app_the-grant", "tokenUrl": "https://grant.example/token"}}`
	if err := os.WriteFile(source, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the source credential: %v", err)
	}
	target := filepath.Join(dir, "auth.json")
	settings := authSettings(t, dir, target)

	captureStdout(t, func() {
		if code := run([]string{"auth", "import", "pi", "--settings", settings, "--from", source}); code != 0 {
			t.Errorf("poo auth import pi exited with %d", code)
		}
	})
	stored, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading poo's credential file: %v", err)
	}
	for _, want := range []string{"clientId: app_the-grant", "tokenUrl: https://grant.example/token"} {
		if !strings.Contains(string(stored), want) {
			t.Errorf("poo's file is\n%s\nwant it to carry %q", stored, want)
		}
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
			if got := confirmed(bufio.NewScanner(strings.NewReader(c.input))); got != c.want {
				t.Errorf("the answer %q reads as %t, want %t", c.input, got, c.want)
			}
		})
	}
}

// withoutATerminal points os.Stdin at a pipe for the rest of the test.
//
// A terminal on stdin makes a command that asks before replacing wait for an answer, so a
// test that leaves the operator's stdin in place blocks in a terminal and passes in a pipe.
// The pipe is not a terminal, so the branch these tests describe is the one that runs
// everywhere.
func withoutATerminal(t *testing.T) *os.File {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	old := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = old
		_ = reader.Close()
		_ = writer.Close()
	})
	return reader
}

// TestTheAuthStatusNamesTheReadOnlyMode covers the state a shared file's
// report gives: an expired credential under auth_readonly is not "refresh
// needed" — poo would refuse the trade — and the state names the tool that
// owns the refresh instead.
func TestTheAuthStatusNamesTheReadOnlyMode(t *testing.T) {
	dir := t.TempDir()
	// The token stopped working an hour ago.
	credential := codexCredential(t, dir, time.Now().Add(-time.Hour).UnixMilli())
	settingsPath := authSettings(t, dir, credential)
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading the settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, append(settings, []byte("auth_readonly: true\n")...), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settingsPath}); code != 0 {
			t.Errorf("poo auth status exited with %d", code)
		}
	})
	if !strings.Contains(out, "expired (read-only: refresh with the tool that owns the file)") {
		t.Errorf("the report is %q, want the state that names the owning tool", out)
	}
	if strings.Contains(out, "refresh needed") {
		t.Errorf("the report is %q, want no state a read-only file cannot act on", out)
	}
}

// TestTheAuthStatusLeavesAnUnreadableExpiryEmpty covers the access token that
// names no exp: the report carries no expiry at all, not the epoch a zero
// millisecond count renders as.
func TestTheAuthStatusLeavesAnUnreadableExpiryEmpty(t *testing.T) {
	dir := t.TempDir()
	credential := filepath.Join(dir, "auth.json")
	// The access token is not a JWT, so its expiry cannot be read.
	body := fmt.Sprintf(`{"openai-codex":{"type":"oauth","access":"not-a-jwt","refresh":"a-refresh","expires":%d,"accountId":"acct-test"}}`, time.Now().Add(time.Hour).UnixMilli())
	if err := os.WriteFile(credential, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	settings := authSettings(t, dir, "auth.json")

	out := captureStdout(t, func() {
		if code := run([]string{"auth", "status", "--settings", settings}); code != 0 {
			t.Errorf("poo auth status exited with %d", code)
		}
	})
	if strings.Contains(out, "1970") {
		t.Errorf("the report is %q, want no expiry rendered from a zero time", out)
	}
	if !strings.Contains(out, "EXPIRES") {
		t.Errorf("the report is %q, want the expiry line", out)
	}
}
