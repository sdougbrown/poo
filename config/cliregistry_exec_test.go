package config_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// sandboxExecutors are the executors a binary that links the sandbox has.
func sandboxExecutors() *bash.Registry {
	registry := bash.NewRegistry()
	registry.Register("local", func(bash.Options) (bash.Executor, error) {
		return bash.NewLocal(), nil
	})
	registry.Register("bwrap", func(opts bash.Options) (bash.Executor, error) {
		return bwrap.New(bwrap.Options{
			DisableNetwork: opts.Sandbox.DisableNetwork,
			ROBinds:        opts.Sandbox.ROBinds,
			RWBinds:        opts.Sandbox.RWBinds,
			Masker:         opts.Masker,
		})
	})
	return registry
}

// registryFile writes a registry with one CLI and returns the directory
// that holds it.
func registryFile(t *testing.T, body map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	tools := map[string]any{"probe": body}
	encoded, err := json.Marshal(map[string]any{"version": 1, "tools": tools})
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), encoded, 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	return dir
}

// callCLI runs the session's call_cli the way the core does.
func callCLI(t *testing.T, runtime *config.Runtime) poo.ToolResult {
	t.Helper()
	tool, ok := runtime.Options.Tools[cliregistry.CallToolName]
	if !ok {
		t.Fatalf("the session offers %v", runtime.Options.ToolOrder)
	}
	prepared, err := tool.Prepare(json.RawMessage(`{"name":"probe","args":{}}`), poo.PrepareEnv{
		WorkingDir:     runtime.Options.WorkingDir,
		ProtectedPaths: runtime.Options.ProtectedPaths,
	})
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{
			WorkingDir:     runtime.Options.WorkingDir,
			ProtectedPaths: runtime.Options.ProtectedPaths,
		},
		Env:      runtime.Options.Env,
		Settings: runtime.Options.Settings,
	})
	if err != nil {
		t.Fatalf("running the CLI: %v", err)
	}
	return res
}

// TestTheSandboxProtectsALiteralFile covers the protection the settings
// name as one file: the session's own matcher expands to it, and the
// sandboxed CLI cannot read it. Protecting the directory it sits in is
// not enough to cover the file.
func TestTheSandboxProtectsALiteralFile(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "protected-file")
	if err := os.WriteFile(protected, []byte("synthetic-protected-file-content"), 0o600); err != nil {
		t.Fatalf("writing the protected file: %v", err)
	}
	registry := registryFile(t, map[string]any{
		"description": "Read the protected file",
		"command":     []string{"/bin/cat", protected},
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	})
	document := theModelSettings + fmt.Sprintf(`tools:
  bash:
    executor: bwrap
  protected_paths: [%q]
cliregistry:
  paths: [%q]
`, protected, registry)
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildIn(t, settings, config.Overrides{}, dir, sandboxExecutors())

	res := callCLI(t, runtime)
	if strings.Contains(res.Text, "synthetic-protected-file-content") {
		t.Errorf("the sandboxed CLI read the protected file: %s", res.Text)
	}
}

// TestTheSettingsExecutorRunsTheCLI covers the wiring: the executor the
// settings name is the one call_cli runs through, with the sandbox's
// options and the protected paths the settings name.
func TestTheSettingsExecutorRunsTheCLI(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "protected")
	if err := os.Mkdir(protected, 0o700); err != nil {
		t.Fatalf("creating the protected directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(protected, "content"), []byte("protected content"), 0o600); err != nil {
		t.Fatalf("writing the protected file: %v", err)
	}
	registry := registryFile(t, map[string]any{
		"description": "Print the directory and read a protected file",
		"command":     []string{"/bin/sh", "-c", `pwd; cat "$1"`, "probe", filepath.Join(protected, "content")},
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	})
	document := theModelSettings + fmt.Sprintf(`tools:
  bash:
    executor: bwrap
  protected_paths: [%q]
cliregistry:
  paths: [%q]
`, protected, registry)
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildIn(t, settings, config.Overrides{}, dir, sandboxExecutors())

	res := callCLI(t, runtime)
	if !strings.Contains(res.Text, dir) {
		t.Errorf("the CLI did not run in the session's directory: %s", res.Text)
	}
	if strings.Contains(res.Text, "protected content") {
		t.Errorf("the CLI read a protected path: %s", res.Text)
	}
}
