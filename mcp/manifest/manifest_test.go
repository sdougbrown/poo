package manifest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/mcp/manifest"
)

// writeManifest writes one mcp.json into a directory of its own and returns
// the directory.
func writeManifest(t *testing.T, body map[string]any) string {
	t.Helper()
	return writeManifestNamed(t, t.TempDir(), "mcp.json", body)
}

// writeManifestNamed writes one manifest under the directory and name given,
// and returns the file's path.
func writeManifestNamed(t *testing.T, dir, name string, body map[string]any) string {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// aServer is a stdio entry the manifest shape reads.
func aServer(command string) map[string]any {
	return map[string]any{"command": command}
}

// TestAClaudeCodeFileLoadsUnchanged covers the shared file: a manifest in the
// shape Claude Code reads, carrying poo's own top-level key and fields inside
// the entry that poo does not read, loads with its server and its resource
// feed.
func TestAClaudeCodeFileLoadsUnchanged(t *testing.T) {
	set, err := manifest.Load(filepath.Join("testdata", "claude"))
	if err != nil {
		t.Fatalf("loading the fixture: %v", err)
	}
	// The .yaml file in the same directory is another tool's manifest, and
	// the scan leaves it alone.
	if names := set.Names(); !slices.Equal(names, []string{"avenor"}) {
		t.Fatalf("the load holds the servers %q, want avenor alone", names)
	}
	if warnings := set.Warnings(); len(warnings) != 0 {
		t.Errorf("the load warned %q, want nothing", warnings)
	}
	server, ok := set.Lookup("avenor")
	if !ok {
		t.Fatal("the load does not hold the avenor server")
	}
	if got, want := resolve(t, server.Command, "AVENOR_BIN", "avenor-mcp", "session-7"), "avenor-mcp"; got != want {
		t.Errorf("the command resolved to %q, want %q", got, want)
	}
	if len(server.Args) != 4 {
		t.Fatalf("the server has %d arguments, want 4", len(server.Args))
	}
	if got, want := resolve(t, server.Args[3], "", "", "session-7"), "session-7"; got != want {
		t.Errorf("the session argument resolved to %q, want %q", got, want)
	}
	if got, want := resolve(t, server.Env["AVENOR_URL"], "", "", "session-7"), "http://127.0.0.1:8787"; got != want {
		t.Errorf("the environment default resolved to %q, want %q", got, want)
	}
	if got, want := resolve(t, server.Env["AVENOR_PRICE"], "", "", "session-7"), "$5"; got != want {
		t.Errorf("the literal dollar resolved to %q, want %q", got, want)
	}
	feeds := set.Feeds("avenor")
	if len(feeds) != 1 {
		t.Fatalf("the server declares %d resource feeds, want 1", len(feeds))
	}
	feed := feeds[0]
	if feed.Name != "inbox" || feed.Resource != "avenor://inbox" || feed.OnIdle != "wake" {
		t.Errorf("the resource feed is %+v, want the inbox at avenor://inbox waking the session", feed)
	}
	if feed.Filter == nil || !feed.Filter.Script() {
		t.Errorf("the resource feed's filter is %+v, want the script filter", feed.Filter)
	}
}

// TestAnUnknownTopLevelKeyIsIgnored covers the format contract: a top-level
// key the manifest does not name is a harness's own, and it is ignored the
// way a harness ignores poo's.
func TestAnUnknownTopLevelKeyIsIgnored(t *testing.T) {
	dir := writeManifest(t, map[string]any{
		"mcpServers": map[string]any{"avenor": aServer("true")},
		"harnessKey": map[string]any{"some": "harness value"},
	})
	set, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("loading the manifest with an unknown top-level key: %v", err)
	}
	if names := set.Names(); !slices.Equal(names, []string{"avenor"}) {
		t.Fatalf("the load holds the servers %q, want avenor alone", names)
	}
}

// TestAFileWithNoMCPServersFails covers the shape that is not a manifest:
// a file whose top level names no mcpServers is not one poo can use, and it
// fails the load naming the file.
func TestAFileWithNoMCPServersFails(t *testing.T) {
	path := writeManifest(t, map[string]any{"harnessKey": map[string]any{"some": "value"}})
	_, err := manifest.Load(path)
	if err == nil {
		t.Fatal("the load took a file with no mcpServers")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the failure does not name the file: %v", err)
	}
}

// resolve resolves a template over a host environment holding one variable.
func resolve(t *testing.T, template manifest.Template, name, value, sessionID string) string {
	t.Helper()
	host := func(asked string) (string, bool) {
		if name != "" && asked == name {
			return value, true
		}
		return "", false
	}
	resolved, err := template.Resolve(host, sessionID)
	if err != nil {
		t.Fatalf("resolving %+v: %v", template, err)
	}
	return resolved
}

// TestTheLoadWarnsOnTheTransportsPooDoesNotStart covers the skip: an entry
// naming http, sse, streamable-http, or a url is left out with a warning that
// names the server and the transport, and the stdio entry beside it loads.
func TestTheLoadWarnsOnTheTransportsPooDoesNotStart(t *testing.T) {
	dir := t.TempDir()
	writeManifestNamed(t, dir, "mcp.json", map[string]any{
		"mcpServers": map[string]any{
			"remote-sse":     map[string]any{"type": "sse", "url": "https://example.test/sse"},
			"remote-url":     map[string]any{"url": "https://example.test/mcp"},
			"remote-http":    map[string]any{"type": "http", "url": "https://example.test/mcp"},
			"remote-streams": map[string]any{"type": "streamable-http", "url": "https://example.test/mcp"},
			"local":          aServer("true"),
		},
	})
	set, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("loading the manifests: %v", err)
	}
	if names := set.Names(); !slices.Equal(names, []string{"local"}) {
		t.Fatalf("the load holds the servers %q, want local alone", names)
	}
	warnings := set.Warnings()
	want := []string{
		"server remote-http uses transport http, which poo does not start",
		"server remote-sse uses transport sse, which poo does not start",
		"server remote-streams uses transport streamable-http, which poo does not start",
		"server remote-url uses transport http, which poo does not start",
	}
	if !slices.Equal(warnings, want) {
		t.Errorf("the load warned\n%s\nwant\n%s", strings.Join(warnings, "\n"), strings.Join(want, "\n"))
	}
}

// TestTheWarningsFollowTheFiles covers the order: warnings are sorted by file
// and then by server name, whatever order the paths came in.
func TestTheWarningsFollowTheFiles(t *testing.T) {
	first := writeManifest(t, map[string]any{"mcpServers": map[string]any{
		"zeta": map[string]any{"type": "http", "url": "https://example.test/z"},
	}})
	second := writeManifest(t, map[string]any{"mcpServers": map[string]any{
		"alpha": map[string]any{"type": "http", "url": "https://example.test/a"},
	}})
	set, err := manifest.Load(second, first)
	if err != nil {
		t.Fatalf("loading the manifests: %v", err)
	}
	warnings := set.Warnings()
	if len(warnings) != 2 {
		t.Fatalf("the load warned %q, want two lines", warnings)
	}
	// The file that sorts first names zeta, and it comes first.
	if !strings.Contains(warnings[0], "zeta") {
		t.Errorf("the warnings are %q, want the first file's server first", warnings)
	}
}

// TestANamedFileThatIsNotJSONFails covers the operator's own word: a path that
// names a file directly is a path to an mcp.json, so a file in another format
// fails the load instead of being skipped.
func TestANamedFileThatIsNotJSONFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.yaml")
	if err := os.WriteFile(path, []byte("mcpServers: {}\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	_, err := manifest.Load(path)
	if err == nil {
		t.Fatal("the load read a named file that is not JSON")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the failure does not name the path: %v", err)
	}
}

// TestAServerTwoFilesDefineFails covers the duplicate: a server name means one
// entry, so two files that define it fail the load, naming both.
func TestAServerTwoFilesDefineFails(t *testing.T) {
	first := writeManifest(t, map[string]any{"mcpServers": map[string]any{"avenor": aServer("true")}})
	second := writeManifest(t, map[string]any{"mcpServers": map[string]any{"avenor": aServer("false")}})
	_, err := manifest.Load(first, second)
	if err == nil {
		t.Fatal("the load took two entries for one server name")
	}
	if !strings.Contains(err.Error(), "avenor") || !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Errorf("the failure does not name the server and both files: %v", err)
	}
}

// TestMalformedReferencesFailTheLoad covers the values a launch could never
// resolve: they fail where the manifest loads, naming the server.
func TestMalformedReferencesFailTheLoad(t *testing.T) {
	tests := []struct {
		name   string
		server map[string]any
		want   string
	}{
		{
			name:   "unknown session reference",
			server: map[string]any{"command": "serve", "args": []string{"--session", "${session:runid}"}},
			want:   "session:runid",
		},
		{
			name:   "unterminated reference",
			server: map[string]any{"command": "${AVENOR_BIN"},
			want:   "unterminated",
		},
		{
			name:   "no variable name",
			server: map[string]any{"command": "serve", "env": map[string]any{"URL": "${}"}},
			want:   "names no variable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manifest.Load(writeManifest(t, map[string]any{"mcpServers": map[string]any{"avenor": tt.server}}))
			if err == nil {
				t.Fatal("the load took a value it could never resolve")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "avenor") {
				t.Errorf("the failure does not name the server and the reason: %v", err)
			}
		})
	}
}

// TestAnEntryWithoutWhatPooStartsFails covers the two shapes an entry cannot
// take: no command to run, and a type poo does not know.
func TestAnEntryWithoutWhatPooStartsFails(t *testing.T) {
	tests := []struct {
		name   string
		server map[string]any
		want   string
	}{
		{name: "no command", server: map[string]any{"args": []string{"--serve"}}, want: "has no command"},
		{name: "unknown type", server: map[string]any{"type": "websocket", "url": "wss://example.test"}, want: "websocket"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manifest.Load(writeManifest(t, map[string]any{"mcpServers": map[string]any{"avenor": tt.server}}))
			if err == nil {
				t.Fatal("the load took an entry poo cannot start")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the failure does not say what is wrong: %v", err)
			}
		})
	}
}

// theFeedServer is a manifest holding one stdio server, whose poo block
// declares the given resource feeds.
func theFeedServer(feeds map[string]any) map[string]any {
	return map[string]any{
		"mcpServers": map[string]any{"avenor": aServer("serve")},
		"poo":        map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": feeds}}},
	}
}

// TestThePooBlockIsPooOnly covers the declarations the block may not make: a
// server the file does not define, one whose transport poo does not start, and
// a field poo does not know.
func TestThePooBlockIsPooOnly(t *testing.T) {
	t.Run("unknown server", func(t *testing.T) {
		body := map[string]any{
			"mcpServers": map[string]any{"avenor": aServer("true")},
			"poo":        map[string]any{"servers": map[string]any{"elsewhere": map[string]any{"feeds": map[string]any{"inbox": map[string]any{"resource": "avenor://inbox"}}}}},
		}
		_, err := manifest.Load(writeManifest(t, body))
		if err == nil || !strings.Contains(err.Error(), "elsewhere") {
			t.Errorf("the load took feeds for a server it does not define: %v", err)
		}
	})
	t.Run("skipped server", func(t *testing.T) {
		body := map[string]any{
			"mcpServers": map[string]any{"avenor": map[string]any{"type": "sse", "url": "https://example.test/sse"}},
			"poo":        map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": map[string]any{"inbox": map[string]any{"resource": "avenor://inbox"}}}}},
		}
		_, err := manifest.Load(writeManifest(t, body))
		if err == nil || !strings.Contains(err.Error(), "sse") {
			t.Errorf("the load took feeds for a server it does not start: %v", err)
		}
	})
	t.Run("unknown field", func(t *testing.T) {
		body := map[string]any{
			"mcpServers": map[string]any{"avenor": aServer("true")},
			"poo":        map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": map[string]any{"inbox": map[string]any{"resource": "avenor://inbox", "on_idle": "wake", "poll": "1s"}}}}},
		}
		_, err := manifest.Load(writeManifest(t, body))
		if err == nil || !strings.Contains(err.Error(), "poll") {
			t.Errorf("the load took a field poo does not know in its own block: %v", err)
		}
	})
}

// TestAnUnknownFieldInThePooBlockStillFails covers the regression guard: the
// top level tolerates an unknown key, but the `poo` block is poo's alone, so
// a field poo does not know in it still fails the load.
func TestAnUnknownFieldInThePooBlockStillFails(t *testing.T) {
	body := map[string]any{
		"mcpServers": map[string]any{"avenor": aServer("true")},
		"harnessKey": map[string]any{"some": "value"},
		"poo":        map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": map[string]any{"inbox": map[string]any{"resource": "avenor://inbox", "poll": "1s"}}}}},
	}
	_, err := manifest.Load(writeManifest(t, body))
	if err == nil || !strings.Contains(err.Error(), "poll") {
		t.Errorf("the load took a field poo does not know in its own block: %v", err)
	}
}

// TestTwoFeedsMayNotShareAName covers the name: a resource feed is a feed, and
// a feed's name means one feed however many servers and files declare one.
func TestTwoFeedsMayNotShareAName(t *testing.T) {
	t.Run("one file", func(t *testing.T) {
		body := map[string]any{
			"mcpServers": map[string]any{"avenor": aServer("true"), "notes": aServer("true")},
			"poo": map[string]any{"servers": map[string]any{
				"avenor": map[string]any{"feeds": map[string]any{"inbox": map[string]any{"resource": "avenor://inbox"}}},
				"notes":  map[string]any{"feeds": map[string]any{"inbox": map[string]any{"resource": "notes://inbox"}}},
			}},
		}
		_, err := manifest.Load(writeManifest(t, body))
		if err == nil {
			t.Fatal("the load took two feeds for one name")
		}
		if !strings.Contains(err.Error(), "inbox") || !strings.Contains(err.Error(), "avenor") || !strings.Contains(err.Error(), "notes") {
			t.Errorf("the failure does not name the feed and both servers: %v", err)
		}
	})
	t.Run("two files", func(t *testing.T) {
		var dirs []string
		for _, server := range []string{"avenor", "notes"} {
			dir := writeManifest(t, map[string]any{
				"mcpServers": map[string]any{server: aServer("true")},
				"poo": map[string]any{"servers": map[string]any{server: map[string]any{"feeds": map[string]any{
					"inbox": map[string]any{"resource": server + "://inbox"},
				}}}},
			})
			dirs = append(dirs, dir)
		}
		_, err := manifest.Load(dirs...)
		if err == nil {
			t.Fatal("the load took two feeds for one name across two files")
		}
		if !strings.Contains(err.Error(), "inbox") || !strings.Contains(err.Error(), "avenor") || !strings.Contains(err.Error(), "notes") {
			t.Errorf("the failure does not name the feed and both servers: %v", err)
		}
	})
}

// TestOneResourceHasOneReader covers the pair: two feeds on one server's
// resource would drain the same read, so the load refuses them.
func TestOneResourceHasOneReader(t *testing.T) {
	feeds := map[string]any{
		"inbox":  map[string]any{"resource": "avenor://inbox"},
		"digest": map[string]any{"resource": "avenor://inbox"},
	}
	_, err := manifest.Load(writeManifest(t, theFeedServer(feeds)))
	if err == nil {
		t.Fatal("the load took two feeds on one resource")
	}
	if !strings.Contains(err.Error(), "avenor://inbox") || !strings.Contains(err.Error(), "one reader") {
		t.Errorf("the failure does not name the resource and why: %v", err)
	}
}

// TestTheResourceFeedsAreValidated covers the declarations themselves: a feed
// name outside a feed's shape, a feed with no resource, an idle policy poo does
// not know, and a filter of neither kind all fail the load.
func TestTheResourceFeedsAreValidated(t *testing.T) {
	tests := []struct {
		name string
		feed map[string]any
		want string
	}{
		{name: "bad name", feed: map[string]any{"Inbox": map[string]any{"resource": "avenor://inbox"}}, want: "not a valid feed name"},
		{name: "no resource", feed: map[string]any{"inbox": map[string]any{"description": "nothing"}}, want: "names no resource"},
		{name: "bad on_idle", feed: map[string]any{"inbox": map[string]any{"resource": "avenor://inbox", "on_idle": "sleep"}}, want: "sleep"},
		{name: "empty filter", feed: map[string]any{"inbox": map[string]any{"resource": "avenor://inbox", "filter": map[string]any{}}}, want: "filter.command or a filter.question"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manifest.Load(writeManifest(t, theFeedServer(tt.feed)))
			if err == nil {
				t.Fatal("the load took a resource feed it cannot honor")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the failure does not say what is wrong: %v", err)
			}
		})
	}
}

// TestAQuestionFilterLoads covers the other kind: a resource feed screens its
// records with the session's decision model as well as with a command.
func TestAQuestionFilterLoads(t *testing.T) {
	feeds := map[string]any{
		"inbox": map[string]any{"resource": "avenor://inbox", "filter": map[string]any{"question": "Does this message need the session?", "min_no_probability": 0.8}},
	}
	set, err := manifest.Load(writeManifest(t, theFeedServer(feeds)))
	if err != nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	decls := set.Feeds("avenor")
	if len(decls) != 1 || decls[0].Filter == nil || decls[0].Filter.Script() {
		t.Fatalf("the load holds the resource feeds %+v, want one question filter", decls)
	}
	if decls[0].Filter.MinNoProbability != 0.8 {
		t.Errorf("the filter's threshold is %v, want 0.8", decls[0].Filter.MinNoProbability)
	}
}

// TestTheSetHandsOutCopies covers the set's immutability: a reader that
// changes what Lookup or Feeds handed it leaves the set as the load left it.
func TestTheSetHandsOutCopies(t *testing.T) {
	set, err := manifest.Load(filepath.Join("testdata", "claude"))
	if err != nil {
		t.Fatalf("loading the fixture: %v", err)
	}
	server, ok := set.Lookup("avenor")
	if !ok {
		t.Fatal("the load does not hold the avenor server")
	}
	server.Env["AVENOR_URL"] = mustTemplate(t, "clobbered")
	server.Args[0] = mustTemplate(t, "clobbered")
	feeds := set.Feeds("avenor")
	feeds[0].Resource = "clobbered://inbox"
	feeds[0].Filter.Command[0] = "clobbered"

	again, _ := set.Lookup("avenor")
	if got, want := resolve(t, again.Env["AVENOR_URL"], "", "", "session-7"), "http://127.0.0.1:8787"; got != want {
		t.Errorf("the environment value is %q, want the manifest's %q", got, want)
	}
	if got := resolve(t, again.Args[0], "", "", "session-7"); got != "mcp" {
		t.Errorf("the first argument is %q, want the manifest's %q", got, "mcp")
	}
	againFeeds := set.Feeds("avenor")
	if againFeeds[0].Resource != "avenor://inbox" || againFeeds[0].Filter.Command[0] != "avenor" {
		t.Errorf("the resource feed is %+v, want the manifest's own", againFeeds[0])
	}
}

// mustTemplate parses a template the test wrote.
func mustTemplate(t *testing.T, source string) manifest.Template {
	t.Helper()
	template, err := manifest.ParseTemplate(source)
	if err != nil {
		t.Fatalf("parsing %q: %v", source, err)
	}
	return template
}
