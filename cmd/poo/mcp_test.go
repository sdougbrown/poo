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
)

// theMCPManifest is an mcp.json manifest with two servers in it: the
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

// theIsolatedHome points the user config directory at a fresh one: HOME
// for the platforms that read it, XDG_CONFIG_HOME for the ones that
// read that. The directory the command writes is the one the config
// package resolves, so a test asserts against the same seam the
// command writes through.
func theIsolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
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

// loadManifests loads the .json manifests a directory holds, the way a
// session loads a manifest directory.
func loadManifests(t *testing.T, dir string) (*manifest.Set, error) {
	t.Helper()
	return manifest.Load(dir)
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

func TestTheMCPAddMakesTheUserChooseAScope(t *testing.T) {
	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--", "echo", "hi"}); code != usageExitCode {
			t.Errorf("poo mcp add without a scope exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, "--global") || !strings.Contains(errOut, "--project") {
		t.Errorf("the diagnostics are %q, want them to name the two scopes", errOut)
	}
}

func TestTheMCPAddWritesTheGlobalManifest(t *testing.T) {
	globalDir := theIsolatedHome(t)

	out := captureStdout(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--", "echo", "hi"}); code != 0 {
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
}

func TestTheMCPAddRefusesANameTheScopeAlreadyDefines(t *testing.T) {
	globalDir := theIsolatedHome(t)
	theMCPManifest(t, filepath.Join(globalDir, "other.json"), map[string]any{
		"fixture": map[string]any{"command": "echo"},
	})

	_, errOut := captureBoth(t, func() {
		if code := run([]string{"mcp", "add", "fixture", "--global", "--", "echo", "hi"}); code != usageExitCode {
			t.Errorf("poo mcp add exited with %d, want %d", code, usageExitCode)
		}
	})
	if !strings.Contains(errOut, "fixture") || !strings.Contains(errOut, "other.json") {
		t.Errorf("the diagnostics are %q, want them to name the server and its file", errOut)
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

func TestTheMCPAddWritesTheProjectManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
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

func TestTheMCPRemoveDeletesOneEntryAndKeepsTheOthers(t *testing.T) {
	globalDir := theIsolatedHome(t)
	theMCPManifest(t, filepath.Join(globalDir, "mcp.json"), map[string]any{
		"fixture": map[string]any{"command": "echo", "args": []string{"hi"}},
		"keeper":  map[string]any{"command": "true"},
	})

	captureStdout(t, func() {
		if code := run([]string{"mcp", "remove", "fixture", "--global"}); code != 0 {
			t.Errorf("poo mcp remove exited with %d", code)
		}
	})
	set, err := loadManifests(t, globalDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set.Lookup("fixture"); ok {
		t.Error("the manifest still holds the removed server")
	}
	if _, ok := set.Lookup("keeper"); !ok {
		t.Error("the manifest lost the server the removal did not name")
	}
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
