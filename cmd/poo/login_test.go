package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
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

// TestChooseAnswersToEveryNameTheFlowCarries covers the name the operator types: the short
// name and the provider's id both name the same sign-in, and a name that is not one of
// them is refused.
func TestChooseAnswersToEveryNameTheFlowCarries(t *testing.T) {
	for _, name := range []string{"codex", "openai-codex"} {
		t.Run(name, func(t *testing.T) {
			o := loginOptions{provider: name}
			flow, ok := o.choose(strings.NewReader(""), io.Discard)
			if !ok {
				t.Fatalf("choose did not answer to %q", name)
			}
			if flow.Provider.ID != auth.CodexLogin.Provider.ID {
				t.Errorf("choose answered %q with %q, want the Codex sign-in", name, flow.Provider.ID)
			}
		})
	}
	t.Run("a name it does not know", func(t *testing.T) {
		o := loginOptions{provider: "nope"}
		if _, ok := o.choose(strings.NewReader(""), io.Discard); ok {
			t.Error("choose answered to a name it does not know")
		}
	})
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
		withStdin(t, "an-api-key\n")
		if code := run([]string{"login", "ollama-cloud", "--settings", settings}); code != 0 {
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

// TestAKeySignInRefusesTheBrowsersFlags covers the flags only a browser sign-in takes: a key flow
// opens no browser and waits for nothing, so a flag that says otherwise is refused instead of
// accepted and ignored.
func TestAKeySignInRefusesTheBrowsersFlags(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"no-browser", []string{"--no-browser"}, "--no-browser"},
		{"wait", []string{"--wait", "5"}, "--wait"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			settings := loginSettings(t, dir)
			withoutATerminal(t)
			withStdin(t, "an-api-key\n")

			code, stderr := runCLI(t, append([]string{"login", "ollama-cloud", "--settings", settings}, c.args...)...)
			if code != usageExitCode {
				t.Errorf("the exit code is %d, want %d", code, usageExitCode)
			}
			for _, want := range []string{c.want, "Ollama Cloud", "takes a key"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("the refusal is %q, want it to name %q", stderr, want)
				}
			}
			// The refusal comes before the sign-in, so nothing was read and nothing was written.
			if _, err := os.Stat(filepath.Join(dir, "auth.json")); err == nil {
				t.Error("the refused sign-in stored a credential")
			}
		})
	}
}

// TestABrowserSignInTakesTheBrowsersFlags covers the other side: the refusal is the key flow's
// alone, so a browser sign-in still takes both flags.
func TestABrowserSignInTakesTheBrowsersFlags(t *testing.T) {
	o := loginOptions{noBrowser: true, wait: 5, waitSet: true}
	if err := o.refuseBrowserFlags(auth.CodexLogin); err != nil {
		t.Errorf("the browser sign-in refused its own flags: %v", err)
	}
}

// TestTheStoredKeyReachesTheModel covers the whole of the key sign-in: the key `poo login`
// stores for Ollama Cloud is the key a request to the model its endpoint serves carries.
func TestTheStoredKeyReachesTheModel(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withoutATerminal(t)
	withStdin(t, "the-stored-key\n")
	if code := run([]string{"login", "ollama-cloud", "--force", "--settings", settings}); code != 0 {
		t.Fatalf("poo login exited with %d", code)
	}
	stored, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatalf("reading poo's credential file: %v", err)
	}
	if !strings.Contains(string(stored), "key: the-stored-key") {
		t.Fatalf("poo's file is\n%s\nwant it to hold the key the sign-in was given", stored)
	}

	runtime := loginRuntime(t, settings)
	if key := runtime.EndpointKey(ollamaModel(t, runtime)); key != "the-stored-key" {
		t.Errorf("the model resolves to the key %q, want the key the sign-in stored", key)
	}

	// The environment is the most explicit answer there is: a key set there is not silently
	// overridden by the stored copy.
	t.Setenv("OLLAMA_API_KEY", "the-env-key")
	if key := runtime.EndpointKey(ollamaModel(t, runtime)); key != "the-env-key" {
		t.Errorf("the model resolves to the key %q, want the environment's", key)
	}
}

// TestTheStoredKeyOutranksTheSettingsLiteral covers the ladder's last two rungs: a literal the
// settings carry answers while no credential is stored, and the credential the operator signs in
// with outranks it once one is.
func TestTheStoredKeyOutranksTheSettingsLiteral(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	body := `version: 1
auth_file: ` + filepath.Join(dir, "auth.json") + `
endpoints:
  ollama:
    endpoint: https://ollama.com/v1
    api_key_env: OLLAMA_API_KEY
    api_key: the-literal
session:
  mode: ephemeral
`
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	runtime := loginRuntime(t, settings)
	if key := runtime.EndpointKey(ollamaModel(t, runtime)); key != "the-literal" {
		t.Fatalf("the model resolves to the key %q before a sign-in, want the settings' literal", key)
	}

	withoutATerminal(t)
	withStdin(t, "the-stored-key\n")
	if code := run([]string{"login", "ollama-cloud", "--force", "--settings", settings}); code != 0 {
		t.Fatalf("poo login exited with %d", code)
	}
	if key := runtime.EndpointKey(ollamaModel(t, runtime)); key != "the-stored-key" {
		t.Errorf("the model resolves to the key %q, want the key the sign-in stored", key)
	}
}

// loginRuntime builds the runtime a command builds from the settings file, so a test can ask
// what the credential poo stores resolves to.
func loginRuntime(t *testing.T, settingsPath string) *config.Runtime {
	t.Helper()
	settings, err := config.Load(settingsPath)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		NoChatModel:    true,
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	return runtime
}

// ollamaModel is the curated model the Ollama Cloud endpoint serves.
func ollamaModel(t *testing.T, runtime *config.Runtime) poo.ModelInfo {
	t.Helper()
	model, err := runtime.Catalog.Resolve("minimax-m3.1-flash")
	if err != nil {
		t.Fatalf("resolving the Ollama model: %v", err)
	}
	return model
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

	withStdin(t, "a-new-key\n")
	if code := run([]string{"login", "ollama-cloud", "--settings", settings}); code == 0 {
		t.Error("the sign-in exited 0 for a credential it was not asked to replace")
	}
	if stored, _ := os.ReadFile(target); string(stored) != body {
		t.Errorf("the credential was replaced without being asked:\n%s", stored)
	}
	withStdin(t, "a-new-key\n")
	if code := run([]string{"login", "ollama-cloud", "--force", "--settings", settings}); code != 0 {
		t.Errorf("the forced sign-in exited with %d", code)
	}
	if stored, _ := os.ReadFile(target); !strings.Contains(string(stored), "a-new-key") {
		t.Errorf("the forced sign-in did not replace the credential:\n%s", stored)
	}
}

// piAuthSettings is a settings file that names pi's credential file, so the settings resolve
// the credential to a file that is not poo's.
func piAuthSettings(t *testing.T, dir, pi string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	body := `version: 1
pi_auth_file: ` + pi + `
session:
  mode: ephemeral
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestALoginWritesPoosOwnFileWhenTheSettingsPointAtPis covers the file the sign-in writes:
// a settings that resolves the credential to pi's file still writes poo's own, because the
// credential is poo's to keep.
func TestALoginWritesPoosOwnFileWhenTheSettingsPointAtPis(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	dir := t.TempDir()
	pi := filepath.Join(dir, "pi-auth.json")
	if err := os.WriteFile(pi, []byte(`{"openai-codex": {}}`), 0o600); err != nil {
		t.Fatalf("writing pi's file: %v", err)
	}
	settings := piAuthSettings(t, dir, pi)
	withoutATerminal(t)
	withStdin(t, "testkey\n")

	if code := run([]string{"login", "ollama-cloud", "--force", "--settings", settings}); code != 0 {
		t.Fatalf("poo login exited with %d", code)
	}
	want, err := config.DefaultAuthFile()
	if err != nil {
		t.Fatalf("resolving poo's credential file: %v", err)
	}
	stored, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("reading poo's credential file: %v", err)
	}
	if !strings.Contains(string(stored), "key: testkey") {
		t.Errorf("poo's file is\n%s\nwant it to hold the key", stored)
	}
	if stored, _ := os.ReadFile(pi); strings.Contains(string(stored), "testkey") {
		t.Errorf("the credential landed in the file the settings point at:\n%s", stored)
	}
}

// TestALoginSaysWhereTheSettingsStillPoint covers the hint: the credential is in poo's file,
// and the settings still resolve it to the other file, so the report names the setting that
// points poo at the copy.
func TestALoginSaysWhereTheSettingsStillPoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	dir := t.TempDir()
	pi := filepath.Join(dir, "pi-auth.json")
	if err := os.WriteFile(pi, []byte(`{"openai-codex": {}}`), 0o600); err != nil {
		t.Fatalf("writing pi's file: %v", err)
	}
	settings := piAuthSettings(t, dir, pi)
	withoutATerminal(t)
	withStdin(t, "testkey\n")

	out := captureStdout(t, func() {
		if code := run([]string{"login", "ollama-cloud", "--force", "--settings", settings}); code != 0 {
			t.Fatalf("poo login exited with %d", code)
		}
	})
	poo, err := config.DefaultAuthFile()
	if err != nil {
		t.Fatalf("poo's own file: %v", err)
	}
	for _, want := range []string{
		"The settings still resolve the credential to " + pi,
		"auth_file: " + poo,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is %q, want it to name %q", out, want)
		}
	}
}

// TestTheCallbackComesBackOnTheFlowsRedirect covers the wait: a request to the path the flow's
// redirect names ends it, and the callback it carries is what the exchange is given.
func TestTheCallbackComesBackOnTheFlowsRedirect(t *testing.T) {
	attempt := codexAttemptForTest(t)
	ready := make(chan struct{})
	done := make(chan auth.Callback, 1)
	failed := make(chan error, 1)
	go func() {
		callback, err := waitForCallback(context.Background(), attempt, 5e9, func() { close(ready) }, io.Discard)
		if err != nil {
			failed <- err
			return
		}
		done <- callback
	}()
	// The listener is up by the time the wait says it is.
	<-ready
	response := askCallback(t, attempt.Flow.RedirectURI, "",
		fmt.Sprintf("?code=the-code&state=%s", url.QueryEscape(attempt.State)))
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the callback page is answered %d, want %d", response.StatusCode, http.StatusOK)
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

// codexAccessToken is an access token that names an account, in the shape the token endpoint
// answers with.
func codexAccessToken(t *testing.T, account string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account},
	})
	if err != nil {
		t.Fatalf("encoding the token's claims: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
}

// TestTheCodexBrowserSignInStoresTheCredential covers the whole browser sign-in: the command
// prints the URL, the callback comes back on the flow's loopback, the code it carries is traded
// at the flow's token endpoint, and the credential it answers with is written to the file.
func TestTheCodexBrowserSignInStoresTheCredential(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	withoutATerminal(t)

	// The code is traded at an endpoint of the test's: the Codex one is OpenAI's.
	access := codexAccessToken(t, "the-account")
	traded := make(chan url.Values, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case traded <- r.PostForm:
		default:
		}
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"the-refresh-token","expires_in":3600}`, access)
	}))
	defer endpoint.Close()

	// The flow is the sign-in's, on endpoints of the test's: the Codex client's redirect is on
	// port 1455, which a test cannot count on.
	old := auth.CodexLogin
	defer func() { auth.CodexLogin = old }()
	flow := codexFlowForTest(t)
	flow.TokenURL = endpoint.URL
	auth.CodexLogin = flow

	stderr := streamTo(t, &os.Stderr)
	stdout := streamTo(t, &os.Stdout)
	exited := make(chan int, 1)
	go func() {
		exited <- run([]string{"login", "codex", "--settings", settings, "--no-browser", "--force"})
	}()

	// The callback has to come from another goroutine: the command is waiting for it.
	authorization := signInURL(t, stderr)
	response := askCallback(t, flow.RedirectURI, "",
		fmt.Sprintf("?code=the-code&state=%s", url.QueryEscape(authorization.Query().Get("state"))))
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("reading the callback page: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the callback page is %d: %s", response.StatusCode, body)
	}

	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("poo login exited %d; its stderr is\n%s", code, stderr())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("poo login did not finish; its stderr is\n%s", stderr())
	}
	if printed := stderr(); !strings.Contains(printed, "Open this URL to sign in") {
		t.Errorf("the report is %q, want it to name the sign-in", printed)
	}
	if printed := stdout(); !strings.Contains(printed, "Signed in to openai-codex") {
		t.Errorf("the report is %q, want it to say which provider signed in", printed)
	}
	select {
	case form := <-traded:
		for name, want := range map[string]string{
			"grant_type": "authorization_code",
			"code":       "the-code",
			// The client the trade names is the one the URL was built for.
			"client_id": authorization.Query().Get("client_id"),
			// The redirect the trade names is the one the code came back on.
			"redirect_uri": flow.RedirectURI,
		} {
			if got := form.Get(name); got != want {
				t.Errorf("the trade sent %s=%q, want %q", name, got, want)
			}
		}
	default:
		t.Error("the code the callback carried was never traded")
	}
	credentials := auth.Credentials{Path: filepath.Join(dir, "auth.json")}
	entry, err := credentials.Read(auth.Codex)
	if err != nil {
		t.Fatalf("reading the credential back: %v", err)
	}
	if entry.Access != access || entry.Refresh != "the-refresh-token" {
		t.Errorf("the credential is %+v, want the token the endpoint answered", entry)
	}
	if entry.AccountID != "the-account" {
		t.Errorf("the credential names the account %q, want the one its token claims", entry.AccountID)
	}
}

// TestTheWaitGivesUpWhenTheSignInDoesNotComeBack covers the bound on a sign-in: a wait nothing
// answers returns an error that says so instead of blocking.
func TestTheWaitGivesUpWhenTheSignInDoesNotComeBack(t *testing.T) {
	withoutATerminal(t)
	attempt := codexAttemptForTest(t)
	started := time.Now()
	_, err := waitForCallback(context.Background(), attempt, 50*time.Millisecond, nil, io.Discard)
	if err == nil {
		t.Fatal("the wait came back with no error and no callback")
	}
	if !strings.Contains(err.Error(), "did not come back in time") {
		t.Errorf("the wait is %v, want it to say the sign-in did not come back in time", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the wait took %s, want it to give up on the bound it was given", elapsed)
	}
}

// TestTheCallbackPageReportsTheAnswer covers the page the browser lands on. It is the wait's own
// handler that answers, so the requests go to its listener.
func TestTheCallbackPageReportsTheAnswer(t *testing.T) {
	withoutATerminal(t)
	attempt := codexAttemptForTest(t)
	ready := make(chan struct{})
	done := make(chan auth.Callback, 1)
	failed := make(chan error, 1)
	go func() {
		callback, err := waitForCallback(context.Background(), attempt, 5*time.Second, func() { close(ready) }, io.Discard)
		if err != nil {
			failed <- err
			return
		}
		done <- callback
	}()
	<-ready

	// A path the flow's redirect does not name is not the sign-in coming back.
	response := askCallback(t, attempt.Flow.RedirectURI, "/not-the-callback", "")
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("a request to another path is answered %d, want %d", response.StatusCode, http.StatusNotFound)
	}

	// The endpoint's refusal is reported on the page, on the path the flow names.
	response = askCallback(t, attempt.Flow.RedirectURI, "",
		fmt.Sprintf("?error=access_denied&state=%s", url.QueryEscape(attempt.State)))
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("reading the callback page: %v", err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("a refused sign-in is answered %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(string(page), "refused") {
		t.Errorf("the page is %q, want it to say the sign-in was refused", page)
	}
	select {
	case callback := <-done:
		if callback.Error != "access_denied" {
			t.Errorf("the callback is %+v, want the refusal the page reported", callback)
		}
	case err := <-failed:
		t.Fatalf("the wait failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not come back after the refusal")
	}
}

// TestThePickerTakesTheNumberOfASignIn covers the list a bare `poo login` prints: a number
// names its sign-in, in the order the list shows them.
func TestThePickerTakesTheNumberOfASignIn(t *testing.T) {
	for _, c := range []struct {
		input string
		want  string
	}{
		{"1\n", auth.CodexLogin.Provider.ID},
		{"2\n", ollamaCloud.Provider.ID},
	} {
		t.Run(c.want, func(t *testing.T) {
			var o loginOptions
			var stderr strings.Builder
			flow, ok := o.choose(strings.NewReader(c.input), &stderr)
			if !ok {
				t.Fatalf("the picker refused %q: %s", c.input, stderr.String())
			}
			if flow.Provider.ID != c.want {
				t.Errorf("the picker answered %q with %q, want %q", c.input, flow.Provider.ID, c.want)
			}
		})
	}
}

// TestThePickerRefusesWhatIsNotASignIn covers the answers the list does not take: a number
// outside it, a word, and no answer at all.
func TestThePickerRefusesWhatIsNotASignIn(t *testing.T) {
	for _, c := range []struct {
		input string
		want  string
	}{
		{"3\n", "that is not one of the sign-ins"},
		{"0\n", "that is not one of the sign-ins"},
		{"abc\n", "that is not one of the sign-ins"},
		{"", "nothing was chosen"},
	} {
		t.Run(c.input, func(t *testing.T) {
			var o loginOptions
			var stderr strings.Builder
			if flow, ok := o.choose(strings.NewReader(c.input), &stderr); ok {
				t.Fatalf("the picker answered %q with %q", c.input, flow.Provider.ID)
			}
			if !strings.Contains(stderr.String(), c.want) {
				t.Errorf("the refusal is %q, want it to say %q", stderr.String(), c.want)
			}
		})
	}
}
