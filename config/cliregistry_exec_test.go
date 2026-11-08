package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// sandboxExecutors are the executors a binary that links the sandbox has.
func sandboxExecutors() *bash.Registry {
	registry := bash.NewRegistry()
	registry.Register("local", func(exec.Options) (exec.Executor, error) {
		return bash.NewLocal(), nil
	})
	registry.Register("bwrap", func(opts exec.Options) (exec.Executor, error) {
		return bwrap.New(bwrap.Options{
			Network: opts.Sandbox.Network,
			ROBinds: opts.Sandbox.ROBinds,
			RWBinds: opts.Sandbox.RWBinds,
			Masker:  opts.Masker,
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
cli_registry:
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
cli_registry:
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

const stdinMessage = "first line\nsecond line\n\n  indented line\nno trailing newline"

func stdinProbe(command []string, extra map[string]any) map[string]any {
	body := map[string]any{
		"description": "Read the message from stdin",
		"command":     command,
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "message", "style": "stdin", "flag": "--message-stdin"},
		},
	}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func stdinExecutorSettings(registryDir, executor string) string {
	return theModelSettings + fmt.Sprintf(`tools:
  bash:
    executor: %s
cli_registry:
  paths: [%q]
`, executor, registryDir)
}

func stdinRuntime(t *testing.T, executor string, body map[string]any) *config.Runtime {
	t.Helper()
	registryDir := registryFile(t, body)
	settings, err := config.Parse([]byte(stdinExecutorSettings(registryDir, executor)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	return buildIn(t, settings, config.Overrides{}, t.TempDir(), sandboxExecutors())
}

var (
	sandboxOnce sync.Once
	sandboxErr  error
)

func requireSandbox(t *testing.T) {
	t.Helper()
	sandboxOnce.Do(func() {
		_, sandboxErr = bwrap.New(bwrap.Options{})
	})
	if sandboxErr != nil {
		t.Skipf("skipping the bwrap executor: this host cannot create its sandbox: %v", sandboxErr)
	}
}

func forEachExecutor(t *testing.T, run func(t *testing.T, executor string)) {
	t.Helper()
	for _, executor := range []string{"local", "bwrap"} {
		t.Run(executor, func(t *testing.T) {
			if executor == "bwrap" {
				requireSandbox(t)
			}
			run(t, executor)
		})
	}
}

func messageArg(message string) *string { return &message }

func probeArgs(t *testing.T, message *string) string {
	t.Helper()
	args := map[string]any{}
	if message != nil {
		args["message"] = *message
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encoding the args: %v", err)
	}
	return string(encoded)
}

func preparedProbe(t *testing.T, runtime *config.Runtime, args string) poo.Prepared {
	t.Helper()
	tool, ok := runtime.Options.Tools[cliregistry.CallToolName]
	if !ok {
		t.Fatalf("the session offers %v", runtime.Options.ToolOrder)
	}
	prepared, err := tool.Prepare(json.RawMessage(`{"name":"probe","args":`+args+`}`), poo.PrepareEnv{
		WorkingDir:     runtime.Options.WorkingDir,
		ProtectedPaths: runtime.Options.ProtectedPaths,
	})
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}
	return prepared
}

func callProbe(t *testing.T, runtime *config.Runtime, args string) (poo.ToolResult, error) {
	t.Helper()
	return callProbeContext(context.Background(), t, runtime, args)
}

func callProbeContext(ctx context.Context, t *testing.T, runtime *config.Runtime, args string) (poo.ToolResult, error) {
	t.Helper()
	return runtime.Options.Tools[cliregistry.CallToolName].Call(ctx, preparedProbe(t, runtime, args), poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{
			WorkingDir:     runtime.Options.WorkingDir,
			ProtectedPaths: runtime.Options.ProtectedPaths,
		},
		Env:      runtime.Options.Env,
		Settings: runtime.Options.Settings,
	})
}

func probeObservation(t *testing.T, res poo.ToolResult) map[string]any {
	t.Helper()
	var obs map[string]any
	if err := json.Unmarshal(res.Structured, &obs); err != nil {
		t.Fatalf("decoding the observation %q: %v", res.Structured, err)
	}
	return obs
}

func probeOutput(t *testing.T, res poo.ToolResult) string {
	t.Helper()
	output, _ := probeObservation(t, res)[bash.EnvelopeOutput].(string)
	return output
}

func TestTheStdinBindingFeedsTheCLIExactly(t *testing.T) {
	messages := []struct {
		name    string
		message string
	}{
		{name: "no trailing newline", message: stdinMessage},
		{name: "trailing newline", message: stdinMessage + "\n"},
	}
	forEachExecutor(t, func(t *testing.T, executor string) {
		runtime := stdinRuntime(t, executor, stdinProbe([]string{"/bin/sh", "-c", "cat", "probe"}, nil))
		for _, tt := range messages {
			t.Run(tt.name, func(t *testing.T) {
				res, err := callProbe(t, runtime, probeArgs(t, messageArg(tt.message)))
				if err != nil {
					t.Fatalf("running the CLI: %v", err)
				}
				if res.IsError {
					t.Errorf("the CLI failed: %s", res.Text)
				}
				if got := probeOutput(t, res); got != tt.message {
					t.Errorf("the CLI read %q, want %q", got, tt.message)
				}
				if code := probeObservation(t, res)[bash.EnvelopeReturncode]; code != float64(0) {
					t.Errorf("the CLI exited with %v, want 0", code)
				}
			})
		}
	})
}

func TestTheStdinFlagIsTheOnlyArgvElement(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `printf 'argv[%s]\n' "$@"; cat`, "probe"}, nil)
		runtime := stdinRuntime(t, executor, body)
		res, err := callProbe(t, runtime, probeArgs(t, messageArg(stdinMessage)))
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		output := probeOutput(t, res)
		argv, stdin, ok := strings.Cut(output, "\n")
		if !ok {
			t.Fatalf("the CLI wrote %q, want its arguments and then its stdin", output)
		}
		if argv != "argv[--message-stdin]" {
			t.Errorf("the CLI's arguments are %q, want the flag alone", argv)
		}
		if stdin != stdinMessage {
			t.Errorf("the CLI read %q, want %q", stdin, stdinMessage)
		}
	})
}

func TestAnAbsentMessageOmitsTheFlagAndAnEmptyOneKeepsIt(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `for a in "$@"; do printf 'argv[%s]\n' "$a"; done; cat`, "probe"}, nil)
		runtime := stdinRuntime(t, executor, body)
		tests := []struct {
			name string
			args string
			want string
		}{
			{name: "absent", args: probeArgs(t, nil), want: ""},
			{name: "empty", args: probeArgs(t, messageArg("")), want: "argv[--message-stdin]\n"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				res, err := callProbe(t, runtime, tt.args)
				if err != nil {
					t.Fatalf("running the CLI: %v", err)
				}
				if res.IsError {
					t.Errorf("the CLI failed: %s", res.Text)
				}
				if got := probeOutput(t, res); got != tt.want {
					t.Errorf("the CLI wrote %q, want %q", got, tt.want)
				}
				if timedOut := probeObservation(t, res)[bash.EnvelopeTimedOut]; timedOut != false {
					t.Errorf("the CLI timed out waiting for input it should not have: %v", timedOut)
				}
			})
		}
	})
}

func TestTheCLIReadsTheMessageAndThenEOF(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `cat; printf '<eof>'`}, map[string]any{"timeout": "5s"})
		runtime := stdinRuntime(t, executor, body)
		res, err := callProbe(t, runtime, probeArgs(t, messageArg(stdinMessage)))
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		if got, want := probeOutput(t, res), stdinMessage+"<eof>"; got != want {
			t.Errorf("the CLI read %q, want %q", got, want)
		}
		if timedOut := probeObservation(t, res)[bash.EnvelopeTimedOut]; timedOut != false {
			t.Errorf("the CLI timed out waiting for EOF: %v", timedOut)
		}
	})
}

func TestAStdinBoundCLIKeepsItsFailureObservation(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `cat; printf 'the CLI failed' >&2; exit 3`}, nil)
		runtime := stdinRuntime(t, executor, body)
		res, err := callProbe(t, runtime, probeArgs(t, messageArg(stdinMessage)))
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		if !res.IsError {
			t.Errorf("a failed CLI was not reported as an error: %s", res.Text)
		}
		obs := probeObservation(t, res)
		if got, want := obs[bash.EnvelopeReturncode], float64(3); got != want {
			t.Errorf("the CLI exited with %v, want %v", got, want)
		}
		if got, want := obs[bash.EnvelopeOutput], stdinMessage; got != want {
			t.Errorf("the CLI read %q, want %q", got, want)
		}
		if got, want := obs[bash.EnvelopeStderr], "the CLI failed"; got != want {
			t.Errorf("the CLI's stderr is %q, want %q", got, want)
		}
	})
}

func TestFailedCLIReportsItsExitWhenStdinIsIncomplete(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `printf 'failed' >&2; exit 3`}, nil)
		runtime := stdinRuntime(t, executor, body)
		res, err := callProbe(t, runtime, probeArgs(t, messageArg(strings.Repeat("x", 1<<20))))
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		obs := probeObservation(t, res)
		if !res.IsError || obs[bash.EnvelopeReturncode] != float64(3) || obs[bash.EnvelopeStderr] != "failed" {
			t.Fatalf("expected the CLI failure: %v", obs)
		}
	})
}

func TestConfiguredSuccessDoesNotHideIncompleteStdin(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `exit 3`}, map[string]any{
			"output": map[string]any{"mode": "raw", "success_exit_codes": []int{3}},
		})
		runtime := stdinRuntime(t, executor, body)
		_, err := callProbe(t, runtime, probeArgs(t, messageArg(strings.Repeat("secret", 1<<18))))
		if !errors.Is(err, exec.ErrStdinIncomplete) {
			t.Fatalf("expected incomplete stdin error, got %v", err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("error contains stdin")
		}
	})
}

func TestACancelledRunStopsAStdinBoundCLI(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `sleep 30`}, map[string]any{"timeout": "10s"})
		runtime := stdinRuntime(t, executor, body)
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(300*time.Millisecond, cancel)
		defer timer.Stop()
		defer cancel()

		start := time.Now()
		res, err := callProbeContext(ctx, t, runtime, probeArgs(t, messageArg(strings.Repeat("x", 1<<20))))
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 8*time.Second {
			t.Errorf("the cancelled call took %v to return", elapsed)
		}
		if !res.IsError {
			t.Errorf("a cancelled call was not reported as an error: %s", res.Text)
		}
		obs := probeObservation(t, res)
		if code := obs[bash.EnvelopeReturncode]; code != nil {
			t.Errorf("a stopped CLI has exit status %v, want null", code)
		}
		if timedOut := obs[bash.EnvelopeTimedOut]; timedOut != false {
			t.Errorf("a cancelled call was reported as a timeout: %v", timedOut)
		}
	})
}

func TestAManifestTimeoutStopsAStdinBoundCLI(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, executor string) {
		body := stdinProbe([]string{"/bin/sh", "-c", `sleep 30`}, map[string]any{"timeout": "300ms"})
		runtime := stdinRuntime(t, executor, body)
		res, err := callProbe(t, runtime, probeArgs(t, messageArg(strings.Repeat("x", 1<<20))))
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		obs := probeObservation(t, res)
		if !res.IsError || obs[bash.EnvelopeTimedOut] != true || obs[bash.EnvelopeReturncode] != nil {
			t.Fatalf("expected a timeout with no return code: %v", obs)
		}
	})
}
