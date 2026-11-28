package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/mcp/manifest"
	"github.com/sdougbrown/poo/pootest"
)

// TestMCPSelectionWithoutALauncherFailsTheBuild covers the binary's
// side of the launcher requirement: every entry point of this command
// passes the launcher it links beside its tools, so a profile that
// selects a server builds here — and the same build without the
// launcher, the way a consumer binary that forgot to link one is
// built, fails naming what it could not start.
func TestMCPSelectionWithoutALauncherFailsTheBuild(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(manifest, []byte(`{"mcpServers":{"flagserver":{"command":"true"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	document := "version: 1\n" +
		"endpoints:\n" +
		"  test:\n" +
		"    endpoint: http://127.0.0.1:1/v1\n" +
		"models:\n" +
		"  test-model:\n" +
		"    endpoint: test\n" +
		"    model_id: test-model\n" +
		"    api: chat_completions\n" +
		"    tools_mode: native\n" +
		"    ops: [chat]\n" +
		"    context_limit: 32768\n" +
		"roles:\n" +
		"  chat: test-model\n" +
		"mcp:\n" +
		"  paths: [\"" + manifest + "\"]\n" +
		"profiles:\n" +
		"  picky:\n" +
		"    base: code\n" +
		"    mcp_servers: [flagserver]\n" +
		"profile: picky\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}

	build := func() config.BuildOptions {
		return config.BuildOptions{
			Settings:       settings,
			WorkingDir:     dir,
			DefaultProfile: defaultProfile,
			Tools:          linkedTools(),
			Profiles:       linkedProfiles(),
			Executors:      executorRegistry(),
		}
	}

	// The command's own build passes the launcher it links, and a
	// selecting profile builds: the launcher is what makes the
	// selection runnable.
	opts := build()
	opts.MCPLauncher = linkedMCPLauncher()
	runtime, err := config.Build(opts)
	if err != nil {
		t.Fatalf("building with the linked launcher: %v", err)
	}
	if len(runtime.MCPServers) != 1 || runtime.MCPServers[0] != "flagserver" {
		t.Fatalf("the session selects %q, want flagserver", runtime.MCPServers)
	}

	// A build that links no launcher cannot run a selecting profile,
	// and the failure names the server and the launcher.
	_, err = config.Build(build())
	if err == nil {
		t.Fatal("the build without a launcher accepted a selecting profile")
	}
	for _, want := range []string{"flagserver", "launcher"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure is %q, want it to name %q", err, want)
		}
	}
}

// theMCPManifest is an mcp.json manifest with servers in it: the
// fixture the mcp command tests share, written where a scope names it.
func theMCPManifest(t *testing.T, path string, servers map[string]any) {
	t.Helper()
	body, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// theMCPSettings is a settings file whose mcp.paths name one directory,
// with the defaults off so the test sees exactly what it writes.
func theMCPSettings(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	body := `version: 1
mcp:
  paths: [` + dir + `]
  defaults: false
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// captureBoth runs a function and returns what it wrote to stdout and
// to stderr.
func captureBoth(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW
	doneOut := make(chan string, 1)
	doneErr := make(chan string, 1)
	go func() { out, _ := io.ReadAll(outR); doneOut <- string(out) }()
	go func() { out, _ := io.ReadAll(errR); doneErr <- string(out) }()
	fn()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, errOut := <-doneOut, <-doneErr
	outR.Close()
	errR.Close()
	return out, errOut
}

// loadManifests loads the .json manifests a directory holds, the way a
// session loads a manifest directory.
func loadManifests(t *testing.T, dir string) (*manifest.Set, error) {
	t.Helper()
	return manifest.Load(dir)
}

// theIsolatedHome points the user config directory at a fresh one: HOME
// for the platforms that read it, XDG_CONFIG_HOME for the ones that
// read that. The directory the command writes is the one the config
// package resolves, so a test asserts against the same seam the
// command writes through.
func theIsolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	pootest.IsolateUserConfigDirT(t, home)
	dir, err := config.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return mcpDir
}

// inDir runs the body with the working directory moved to dir, and puts
// it back when the body returns.
func inDir(t *testing.T, dir string, body func()) {
	t.Helper()
	home, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(home) }()
	body()
}

func TestTheMCPCommandListsTheServersTheSettingsName(t *testing.T) {
	dir := t.TempDir()
	theMCPManifest(t, filepath.Join(dir, "mcp.json"), map[string]any{
		"fixture": map[string]any{"command": "echo", "args": []string{"hi"}},
	})
	settings := theMCPSettings(t, dir)

	out := captureStdout(t, func() {
		if code := run([]string{"mcp", "list", "--settings", settings}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	if !strings.Contains(out, "fixture") {
		t.Errorf("the listing is %q, want it to name the server", out)
	}
	if !strings.Contains(out, "echo hi") {
		t.Errorf("the listing is %q, want it to name the command", out)
	}
	if !strings.Contains(out, "mcp.json") {
		t.Errorf("the listing is %q, want it to name the source file", out)
	}
}

func TestTheMCPListPrintsJSON(t *testing.T) {
	dir := t.TempDir()
	theMCPManifest(t, filepath.Join(dir, "mcp.json"), map[string]any{
		"fixture": map[string]any{"command": "echo", "args": []string{"hi"}},
	})
	settings := theMCPSettings(t, dir)

	out := captureStdout(t, func() {
		if code := run([]string{"mcp", "list", "--json", "--settings", settings}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	var listed []struct {
		Name    string            `json:"name"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Source  string            `json:"source"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("the listing is not JSON: %v\n%s", err, out)
	}
	if len(listed) != 1 || listed[0].Name != "fixture" || listed[0].Command != "echo" {
		t.Errorf("the listing is %+v, want one server named fixture", listed)
	}
}

func TestTheMCPListWarnsAboutARemoteServer(t *testing.T) {
	dir := t.TempDir()
	theMCPManifest(t, filepath.Join(dir, "mcp.json"), map[string]any{
		"remote": map[string]any{"type": "http", "url": "http://example.test/mcp"},
	})
	settings := theMCPSettings(t, dir)

	out, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "list", "--settings", settings}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	if strings.Contains(out, "remote") {
		t.Errorf("the listing is %q, want no server poo does not start", out)
	}
	if !strings.Contains(errOut, "remote") {
		t.Errorf("the diagnostics are %q, want a warning naming the skipped server", errOut)
	}
}

func TestTheMCPListHonorsTheProfileDefaultsWord(t *testing.T) {
	dir := t.TempDir()
	theMCPManifest(t, filepath.Join(dir, "mcp.json"), map[string]any{
		"fixture": map[string]any{"command": "echo"},
	})
	path := filepath.Join(dir, "settings.yaml")
	body := `version: 1
mcp:
  paths: [` + dir + `]
profiles:
  quiet:
    base: code
    mcp_defaults: false
profile: quiet
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"mcp", "list", "--settings", path, "--profile", "quiet"}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	// The settings name the directory, so the server still loads; the
	// profile's word turns the defaults off, and this settings file's
	// own path is all the session reads.
	if !strings.Contains(out, "fixture") {
		t.Errorf("the listing is %q, want the server the settings name", out)
	}

	// The default directory is where the profile's word bites: with it
	// off, a server only the defaults hold does not appear.
	globalDir := theIsolatedHome(t)
	theMCPManifest(t, filepath.Join(globalDir, "mcp.json"), map[string]any{
		"ambient": map[string]any{"command": "echo"},
	})
	out, _ = captureBoth(t, func() {
		if code := run([]string{"mcp", "list", "--settings", path, "--profile", "quiet"}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	if strings.Contains(out, "ambient") {
		t.Errorf("the listing is %q, want no server from the defaults the profile turns off", out)
	}
}

func TestTheMCPListReadsTheEnvironmentPaths(t *testing.T) {
	dir := t.TempDir()
	extra := t.TempDir()
	theMCPManifest(t, filepath.Join(extra, "extra.json"), map[string]any{
		"extra": map[string]any{"command": "echo"},
	})
	t.Setenv("POO_MCP_PATH", extra)
	settings := theMCPSettings(t, dir)

	out := captureStdout(t, func() {
		if code := run([]string{"mcp", "list", "--settings", settings}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	if !strings.Contains(out, "extra") {
		t.Errorf("the listing is %q, want the server the environment adds", out)
	}
}

func TestTheMCPAddMakesTheUserChooseAScope(t *testing.T) {
	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--", "echo", "hi"}); code != usageExitCode {
			t.Errorf("poo mcp add without a scope exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, "--global") || !strings.Contains(errOut, "--project") {
		t.Errorf("the diagnostics are %q, want them to name the two scopes", errOut)
	}
	_, errOut = captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--project", "--", "echo"}); code != usageExitCode {
			t.Errorf("poo mcp add with both scopes exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, "one scope") {
		t.Errorf("the diagnostics are %q, want them to refuse the double scope", errOut)
	}
}

func TestTheMCPAddWritesTheGlobalManifest(t *testing.T) {
	globalDir := theIsolatedHome(t)
	target := filepath.Join(globalDir, "mcp.json")

	out := captureStdout(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--env", "API_KEY=${API_KEY}", "--", "echo", "hi"}); code != 0 {
			t.Errorf("poo mcp add exited with %d", code)
		}
	})
	if !strings.Contains(out, filepath.Join(globalDir, "mcp.json")) {
		t.Errorf("the report is %q, want it to name the file written", out)
	}

	set, err := loadManifests(t, globalDir)
	if err != nil {
		t.Fatal(err)
	}
	server, ok := set.Lookup("fixture")
	if !ok {
		t.Fatalf("the manifest holds no fixture server: %+v", set.Names())
	}
	command, err := server.Command.Resolve(func(string) (string, bool) { return "", false }, "")
	if err != nil {
		t.Fatalf("resolving the command: %v", err)
	}
	if command != "echo" || len(server.Args) != 1 {
		t.Fatalf("the server is %+v, want echo with one argument", server)
	}
	argument, err := server.Args[0].Resolve(func(string) (string, bool) { return "", false }, "")
	if err != nil {
		t.Fatalf("resolving the argument: %v", err)
	}
	if argument != "hi" {
		t.Errorf("the argument is %q, want hi", argument)
	}
	// The template is stored as the words gave it: resolving it against
	// a host whose value is distinct from the reference names proves the
	// file holds the reference and not the host's answer.
	envValue, err := server.Env["API_KEY"].Resolve(func(string) (string, bool) { return "resolved", true }, "")
	if err != nil || envValue != "resolved" {
		t.Errorf("the environment entry is %q (error %v), want the host's value once resolved", envValue, err)
	}
	if !strings.Contains(readFile(t, target), "${API_KEY}") {
		t.Errorf("the manifest is %s, want the reference stored unresolved", readFile(t, target))
	}
}

func TestTheMCPAddRefusesANameTheSessionAlreadyDefines(t *testing.T) {
	dir := t.TempDir()
	theMCPManifest(t, filepath.Join(dir, "other.json"), map[string]any{
		"fixture": map[string]any{"command": "echo"},
	})
	settings := theMCPSettings(t, dir)
	globalDir := theIsolatedHome(t)

	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--settings", settings, "--", "echo", "hi"}); code != usageExitCode {
			t.Errorf("poo mcp add exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, "fixture") || !strings.Contains(errOut, "other.json") {
		t.Errorf("the diagnostics are %q, want them to name the server and its file", errOut)
	}
	if entries, err := os.ReadDir(globalDir); err != nil || len(entries) != 0 {
		t.Errorf("the refused add wrote to the scope: %v", entries)
	}
}

func TestTheMCPAddRefusesANameTheLoadRefuses(t *testing.T) {
	globalDir := theIsolatedHome(t)

	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "not a name", "--global", "--", "echo"}); code == 0 {
			t.Error("poo mcp add accepted a name the load refuses")
		}
	})
	if strings.Contains(errOut, "wrote") {
		t.Errorf("the diagnostics are %q, want no report of a write", errOut)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("the refused add left %s behind", filepath.Join(globalDir, "mcp.json"))
	}
}

func TestTheMCPAddRefusesAnEnvEntryThatIsNotAWord(t *testing.T) {
	globalDir := theIsolatedHome(t)

	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--env", "NOVALUE", "--", "echo"}); code != usageExitCode {
			t.Errorf("poo mcp add exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, "NOVALUE") {
		t.Errorf("the diagnostics are %q, want them to name the bad entry", errOut)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("the refused add left %s behind", filepath.Join(globalDir, "mcp.json"))
	}
}

func TestTheMCPAddWritesTheProjectManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("HOME", filepath.Join(dir, "home"))
	inDir(t, dir, func() {
		out := captureStdout(t, func() {
			if code := run([]string{"mcp", "add", "fixture", "--project", "--", "echo", "hi"}); code != 0 {
				t.Errorf("poo mcp add exited with %d", code)
			}
		})
		if !strings.Contains(out, ".poo") {
			t.Errorf("the report is %q, want it to name the project's file", out)
		}
		set, err := loadManifests(t, filepath.Join(dir, ".poo"))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := set.Lookup("fixture"); !ok {
			t.Errorf("the project manifest holds no fixture server: %+v", set.Names())
		}
	})
}

func TestTheMCPAddPreservesTheFileOtherKeysHold(t *testing.T) {
	globalDir := theIsolatedHome(t)
	target := filepath.Join(globalDir, "mcp.json")
	body := `{
  "otherHarnessKey": {"nested": 9007199254740993},
  "mcpServers": {"kept": {"command": "true"}},
  "poo": {"servers": {}}
}
`
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	captureStdout(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--", "echo"}); code != 0 {
			t.Errorf("poo mcp add exited with %d", code)
		}
	})
	edited, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"otherHarnessKey", "9007199254740993", `"kept"`, `"poo"`, "fixture"} {
		if !strings.Contains(string(edited), want) {
			t.Errorf("the edited file is %s, want it to keep %s", edited, want)
		}
	}

	captureStdout(t, func() {
		if code := run([]string{"mcp", "remove", "fixture", "--global"}); code != 0 {
			t.Errorf("poo mcp remove exited with %d", code)
		}
	})
	edited, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"otherHarnessKey", "9007199254740993", `"kept"`, `"poo"`} {
		if !strings.Contains(string(edited), want) {
			t.Errorf("the edited file is %s, want it to keep %s", edited, want)
		}
	}
	if strings.Contains(string(edited), "fixture") {
		t.Errorf("the edited file is %s, want the removed server gone", edited)
	}
}

func TestTheMCPRemoveRestoresTheFileWhenTheSessionRefusesTheResult(t *testing.T) {
	globalDir := theIsolatedHome(t)
	_ = globalDir
	dir := t.TempDir()
	// The removed server declares a resource feed in its own file, and
	// the load refuses a feed whose server the file no longer defines:
	// the edit has to put the file back.
	if err := os.MkdirAll(filepath.Join(dir, ".poo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".poo", "mcp.json"), []byte(`{
  "mcpServers": {"fixture": {"command": "echo"}},
  "poo": {"servers": {"fixture": {"feeds": {"inbox": {"resource": "inbox://lines"}}}}}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	theMCPSettings(t, filepath.Join(dir, ".poo"))
	before, err := os.ReadFile(filepath.Join(dir, ".poo", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}

	inDir(t, dir, func() {
		_, errOut := captureBoth(t, func() {
			if code := run([]string{"mcp", "remove", "fixture", "--project"}); code == 0 {
				t.Error("poo mcp remove accepted a result the session refuses")
			}
		})
		if !strings.Contains(errOut, "fixture") {
			t.Errorf("the diagnostics are %q, want them to name the server the feed outlives", errOut)
		}
	})
	after, err := os.ReadFile(filepath.Join(dir, ".poo", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("the failed removal changed the file: %s", after)
	}
}

func TestTheMCPRemoveEditsTheFileThatDefinesTheServer(t *testing.T) {
	dir := t.TempDir()
	manifests := filepath.Join(dir, ".poo")
	if err := os.MkdirAll(manifests, 0o755); err != nil {
		t.Fatal(err)
	}
	theMCPManifest(t, filepath.Join(manifests, "mcp.json"), map[string]any{
		"keeper": map[string]any{"command": "true"},
	})
	theMCPManifest(t, filepath.Join(manifests, "other.json"), map[string]any{
		"fixture": map[string]any{"command": "echo", "args": []string{"hi"}},
	})
	theMCPSettings(t, manifests)

	inDir(t, dir, func() {
		captureStdout(t, func() {
			if code := run([]string{"mcp", "remove", "fixture", "--project"}); code != 0 {
				t.Errorf("poo mcp remove exited with %d", code)
			}
		})
		set, err := loadManifests(t, manifests)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := set.Lookup("fixture"); ok {
			t.Error("the manifest still holds the removed server")
		}
		if _, ok := set.Lookup("keeper"); !ok {
			t.Error("the manifest lost the server the removal did not name")
		}
	})
}

func TestTheMCPRemoveNamesTheScopeWhenTheNameIsNowhere(t *testing.T) {
	globalDir := theIsolatedHome(t)

	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "remove", "fixture", "--global"}); code != usageExitCode {
			t.Errorf("poo mcp remove exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, globalDir) {
		t.Errorf("the diagnostics are %q, want them to name the scope searched", errOut)
	}
}

func TestTheMCPRemoveDoesNotCreateTheScopeDirectory(t *testing.T) {
	projectDir := t.TempDir()
	pootest.IsolateUserConfigDirT(t, t.TempDir())
	t.Setenv(config.EnvMCPPath, "")
	configDir, err := config.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(projectDir, "settings.yaml")
	if err := os.WriteFile(settings, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, scope := range []struct{ flag, dir string }{
		{"--global", filepath.Join(configDir, "mcp")},
		{"--project", filepath.Join(projectDir, ".poo")},
	} {
		t.Run(scope.flag, func(t *testing.T) {
			inDir(t, projectDir, func() {
				_, errOut := captureBoth(t, func() {
					if code := run([]string{"mcp", "remove", "fixture", scope.flag, "--settings", settings}); code != usageExitCode {
						t.Errorf("poo mcp remove exited with %d, want %d", code, usageExitCode)
					}
				})
				if !strings.Contains(errOut, "is in none of the manifests") {
					t.Errorf("the diagnostics are %q, want a missing server message", errOut)
				}
				if _, err := os.Stat(scope.dir); !os.IsNotExist(err) {
					t.Errorf("stat %s after remove: %v, want directory to remain absent", scope.dir, err)
				}
			})
		})
	}
}

func TestTheMCPListHonorsTheSettingsProfileWithoutAFlag(t *testing.T) {
	dir := t.TempDir()
	theMCPManifest(t, filepath.Join(dir, "mcp.json"), map[string]any{
		"fixture": map[string]any{"command": "echo"},
	})
	path := filepath.Join(dir, "settings.yaml")
	body := `version: 1
mcp:
  paths: [` + dir + `]
profiles:
  quiet:
    base: code
    mcp_defaults: false
profile: quiet
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	globalDir := theIsolatedHome(t)
	theMCPManifest(t, filepath.Join(globalDir, "mcp.json"), map[string]any{
		"ambient": map[string]any{"command": "echo"},
	})

	out, _ := captureBoth(t, func() {
		if code := run([]string{"mcp", "list", "--settings", path}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	if !strings.Contains(out, "fixture") {
		t.Errorf("the listing is %q, want the server the settings name", out)
	}
	if strings.Contains(out, "ambient") {
		t.Errorf("the listing is %q, want no server from the defaults the settings' profile turns off", out)
	}
}

func TestTheMCPListShowsTheTemplateTheFileSpells(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{
  "mcpServers": {"fixture": {"command": "echo", "args": ["${API_KEY}"], "env": {"KEY": "${KEY}"}}}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := theMCPSettings(t, dir)

	out := captureStdout(t, func() {
		if code := run([]string{"mcp", "list", "--json", "--settings", settings}); code != 0 {
			t.Errorf("poo mcp list exited with %d", code)
		}
	})
	var listed []struct {
		Args []string          `json:"args"`
		Env  map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("the listing is not JSON: %v\n%s", err, out)
	}
	if len(listed) != 1 || len(listed[0].Args) != 1 || listed[0].Args[0] != "${API_KEY}" {
		t.Errorf("the listing is %+v, want the argument the file spells", listed)
	}
	if listed[0].Env["KEY"] != "${KEY}" {
		t.Errorf("the environment entry is %q, want the reference the file holds", listed[0].Env["KEY"])
	}
}

func TestTheMCPAddStoresTheEnvTemplateUnresolved(t *testing.T) {
	globalDir := theIsolatedHome(t)
	target := filepath.Join(globalDir, "mcp.json")

	captureStdout(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--env", "API_KEY=${API_KEY}", "--", "echo"}); code != 0 {
			t.Errorf("poo mcp add exited with %d", code)
		}
	})
	body := readFile(t, target)
	if !strings.Contains(body, "${API_KEY}") {
		t.Errorf("the manifest is %s, want the template stored as the words gave it", body)
	}
}

func TestTheMCPRemoveRefusesAFileOutsideTheScope(t *testing.T) {
	configHome := theIsolatedHome(t)
	_ = configHome
	dir := t.TempDir()
	theMCPManifest(t, filepath.Join(dir, "settings-path.json"), map[string]any{
		"fixture": map[string]any{"command": "echo"},
	})
	if err := os.MkdirAll(filepath.Join(dir, ".poo"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `version: 1
mcp:
  paths: [` + dir + `]
  defaults: false
`
	if err := os.WriteFile(filepath.Join(dir, ".poo", "settings.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(dir, "settings-path.json"))

	settings := filepath.Join(dir, ".poo", "settings.yaml")
	inDir(t, dir, func() {
		_, errOut := captureBoth(t, func() {
			if code := run([]string{"mcp", "remove", "fixture", "--project", "--settings", settings}); code != usageExitCode {
				t.Errorf("poo mcp remove exited with %d, want %d", code, usageExitCode)
			}
		})
		if !strings.Contains(errOut, "settings-path.json") || !strings.Contains(errOut, "outside") {
			t.Errorf("the diagnostics are %q, want the refusal naming the outside file", errOut)
		}
	})
	if after := readFile(t, filepath.Join(dir, "settings-path.json")); after != before {
		t.Errorf("the refused removal changed %s", after)
	}
}

func TestTheMCPAddPrintsNoHintWhenTheProjectLoadsTheManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("HOME", filepath.Join(dir, "home"))
	if err := os.MkdirAll(filepath.Join(dir, ".poo"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `version: 1
mcp:
  paths: [.poo]
  defaults: false
`
	if err := os.WriteFile(filepath.Join(dir, ".poo", "settings.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	inDir(t, dir, func() {
		out := captureStdout(t, func() {
			if code := run([]string{"mcp", "add", "fixture", "--project", "--", "echo"}); code != 0 {
				t.Errorf("poo mcp add exited with %d", code)
			}
		})
		if strings.Contains(out, "do not load this manifest yet") {
			t.Errorf("the report is %q, want no hint when the settings load the manifest", out)
		}
	})
}

// readFile returns a file's content for the tests that assert on it.
func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestTheMCPAddRefusesANameASiblingInScopeHolds(t *testing.T) {
	globalDir := theIsolatedHome(t)
	theMCPManifest(t, filepath.Join(globalDir, "sibling.json"), map[string]any{
		"fixture": map[string]any{"command": "echo"},
	})
	// The settings turn the defaults off and name no paths, so the
	// session's resolution is empty: only the scope's own directory,
	// which the edit joins, can hold the name.
	settings := theMCPSettings(t, t.TempDir())

	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--settings", settings, "--", "echo", "hi"}); code != usageExitCode {
			t.Errorf("poo mcp add exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, "sibling.json") {
		t.Errorf("the diagnostics are %q, want them to name the sibling file", errOut)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("the refused add left %s behind", filepath.Join(globalDir, "mcp.json"))
	}
}

func TestTheMCPRemoveRefusesATargetThatDoesNotParse(t *testing.T) {
	globalDir := theIsolatedHome(t)
	target := filepath.Join(globalDir, "mcp.json")
	if err := os.WriteFile(target, []byte("{not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, target)

	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "remove", "fixture", "--global"}); code == 0 {
			t.Error("poo mcp remove accepted a manifest that does not parse")
		}
	})
	// The load a session runs refuses the unreadable file, and the
	// diagnostics carry that refusal: the edit never replaced content it
	// could not read.
	if !strings.Contains(errOut, "invalid character") || !strings.Contains(errOut, "mcp.json") {
		t.Errorf("the diagnostics are %q, want the load's refusal naming the file", errOut)
	}
	if after := readFile(t, target); after != before {
		t.Errorf("the refused remove changed the file: %s", after)
	}
}

func TestTheMCPProjectScopeWalksUpToTheProjectRoot(t *testing.T) {
	globalDir := theIsolatedHome(t)
	_ = globalDir
	// The project root is an ancestor: its .poo holds the settings, and
	// the working directory is a child of the root with no .poo of its
	// own, so the edit lands where the session's discovery reads.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".poo"), 0o755); err != nil {
		t.Fatal(err)
	}
	theMCPSettings(t, filepath.Join(root, ".poo"))
	child := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	inDir(t, child, func() {
		out := captureStdout(t, func() {
			if code := run([]string{"mcp", "add", "fixture", "--project", "--", "echo"}); code != 0 {
				t.Errorf("poo mcp add exited with %d", code)
			}
		})
		if !strings.Contains(out, filepath.Join(root, ".poo", "mcp.json")) {
			t.Errorf("the report is %q, want it to name the project root's file", out)
		}
		set, err := loadManifests(t, filepath.Join(root, ".poo"))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := set.Lookup("fixture"); !ok {
			t.Errorf("the project root's manifest holds no fixture server: %+v", set.Names())
		}
	})
}
