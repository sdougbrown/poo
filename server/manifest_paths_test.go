package server_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/server"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

func TestServersKeepTheirEnvironmentForLaterSessions(t *testing.T) {
	manifest := func(name string, registry bool) string {
		dir := t.TempDir()
		var body string
		if registry {
			body = fmt.Sprintf(`{"version":1,"tools":{"%s":{"description":"command","command":["true"],"inputSchema":{"type":"object","properties":{},"additionalProperties":false}}}}`, name)
		} else {
			body = fmt.Sprintf(`{"version":1,"feeds":{"%s":{"command":["true"]}}}`, name)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	feeds := make([]string, 1, 4)
	feeds[0] = manifest("basefeed", false)
	registries := make([]string, 1, 4)
	registries[0] = manifest("basetool", true)
	executors := bash.NewRegistry()
	executors.Register("local", func(exec.Options) (exec.Executor, error) { return bash.NewLocal(), nil })
	opts := server.Options{Settings: servedSettings(t, search.Name, `{"chat":"chat-model"}`), DefaultProfile: search.Name, WorkingDir: t.TempDir(), Overrides: config.Overrides{FeedsPaths: feeds, CLIRegistryPaths: registries}, Executors: executors, Profiles: []profile.Profile{search.Profile()}, Tools: map[string]config.ToolFactory{
		"read": func(config.ToolOptions) poo.Tool { return read.New() }, "grep": func(config.ToolOptions) poo.Tool { return grep.New() },
		"call_cli": func(o config.ToolOptions) poo.Tool {
			return cliregistry.NewCall(o.Registry).WithSearch(o.Search).WithExecutor(o.Executor)
		},
		"find_tool": func(o config.ToolOptions) poo.Tool { return cliregistry.NewFind(o.Registry) },
	}}
	servers := make([]*server.Server, 0, 2)
	seen := make([][]string, 2)
	for i, name := range []string{"first", "second"} {
		t.Setenv("POO_FEEDS_PATH", manifest(name+"feed", false))
		t.Setenv("POO_CLI_REGISTRY_PATH", manifest(name+"tool", true))
		index := i
		opts.SessionTools = func(r *config.Runtime) error {
			var names []string
			for _, f := range r.Feeds.Specs() {
				names = append(names, f.Name)
			}
			for _, e := range r.Registry.Entries() {
				names = append(names, e.Name)
			}
			seen[index] = append(seen[index], strings.Join(names, ","))
			return nil
		}
		s, err := server.New(opts)
		if err != nil {
			t.Fatal(err)
		}
		servers = append(servers, s)
	}
	for i, s := range servers {
		sess, info, err := s.Create(context.Background(), protocol.CreateParams{Mode: "ephemeral", Profile: search.Name}, wire.FactoryEnv{Client: silentClient{}})
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.Close(); err != nil {
			t.Fatal(err)
		}
		if err := info.Release(); err != nil {
			t.Fatal(err)
		}
		want := []string{"first", "second"}[i]
		if len(seen[i]) != 2 || !strings.Contains(seen[i][1], want+"feed") || !strings.Contains(seen[i][1], want+"tool") {
			t.Fatalf("server %d runtimes: %v", i, seen[i])
		}
	}
}
