package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
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
