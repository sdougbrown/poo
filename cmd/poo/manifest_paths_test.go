package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLoadsPathsInSettingsFlagEnvironmentOrder(t *testing.T) {
	for _, kind := range []struct{ field, flag, env string }{
		{"feeds", "--feeds-path", "POO_FEEDS_PATH"},
		{"cli_registry", "--cli-registry-path", "POO_CLI_REGISTRY_PATH"},
	} {
		t.Run(kind.field, func(t *testing.T) {
			dir := t.TempDir()
			settingsPath := filepath.Join(dir, "settings.json")
			paths := []string{filepath.Join(dir, "settings-missing"), filepath.Join(dir, "flag-one-missing"), filepath.Join(dir, "flag-two-missing"), filepath.Join(dir, "env-missing")}
			t.Setenv(kind.env, paths[3])
			for step := range paths {
				settings := fmt.Sprintf(`{"version":1,"%s":{"paths":[]}}`, kind.field)
				if step == 0 {
					settings = fmt.Sprintf(`{"version":1,"%s":{"paths":[%q]}}`, kind.field, paths[0])
				}
				if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
					t.Fatal(err)
				}
				args := []string{"run", "--settings", settingsPath, "--endpoint", "http://localhost:1/v1", "--model-id", "test", "--context-limit", "32768", "--working-dir", dir}
				if step <= 1 {
					args = append(args, kind.flag, paths[1])
				}
				if step <= 2 {
					args = append(args, kind.flag, paths[2])
				}
				args = append(args, "hello")
				code, stderr := runCLI(t, args...)
				if code == 0 || !strings.Contains(stderr, paths[step]) {
					t.Fatalf("step %d: exit %d, %s", step, code, stderr)
				}
			}
		})
	}
}

func TestRunLoadsManifestPathsFromFlagsAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	feed := func(name string) string {
		p := filepath.Join(dir, name+".json")
		if err := os.WriteFile(p, []byte(fmt.Sprintf(`{"version":1,"feeds":{"%s":{"command":["true"]}}}`, name)), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	registry := func(name string) string {
		p := filepath.Join(dir, name+".json")
		body := fmt.Sprintf(`{"version":1,"tools":{"%s":{"description":"%s command","command":["true"],"inputSchema":{"type":"object","properties":{},"additionalProperties":false}}}}`, name, name)
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	feedFlag, feedEnv := feed("flagfeed"), feed("envfeed")
	regFlag, regEnv := registry("flagtool"), registry("envtool")
	t.Setenv("POO_FEEDS_PATH", feedEnv)
	t.Setenv("POO_CLI_REGISTRY_PATH", regEnv)
	endpoint := newFakeEndpoint(t, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	code, stderr := runCLI(t, "run", "--feeds-path", feedFlag, "--cli-registry-path", regFlag, "--endpoint", endpoint.URL+"/v1", "--model-id", "test", "--context-limit", "32768", "--working-dir", dir, "--log", filepath.Join(dir, "run.log"), "hello")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	request := fmt.Sprint(endpoint.requests[0])
	for _, name := range []string{"flagtool", "envtool", "flagfeed", "envfeed"} {
		if !strings.Contains(request, name) {
			t.Errorf("request missing %s: %s", name, request)
		}
	}
}
