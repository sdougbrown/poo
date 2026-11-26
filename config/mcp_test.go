package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// theMCPSettings are the settings the mcp tests build from: a model, a chat
// role, and the mcp paths the case names.
func theMCPSettings(paths ...string) string {
	if len(paths) == 0 {
		return theModelSettings
	}
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, "\""+path+"\"")
	}
	return theModelSettings + "mcp:\n  paths: [" + strings.Join(quoted, ", ") + "]\n"
}

// mcpManifest writes an mcp.json holding the given entries, and the poo block
// the case names, and returns its path.
func mcpManifest(t *testing.T, servers map[string]any, poo map[string]any) string {
	t.Helper()
	body := map[string]any{"mcpServers": servers}
	if poo != nil {
		body["poo"] = poo
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// stdioServer is one entry poo starts.
func stdioServer() map[string]any {
	return map[string]any{"command": "true"}
}

// TestMCPDefaultsOnByDefault covers the default directory: the servers under
// poo's own mcp directory load when the settings say nothing, and a session
// still starts none of them until a profile selects one.
func TestMCPDefaultsOnByDefault(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	theDefaultMCPServer(t, pooDir)
	settings, err := config.Parse([]byte(theMCPSettings()), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if names := runtime.MCPServerNames(); !slices.Equal(names, []string{"avenor"}) {
		t.Fatalf("the build loaded the servers %q, want the default directory's avenor", names)
	}
	if len(runtime.MCPServers) != 0 {
		t.Errorf("the session selects the servers %q, and no profile names one", runtime.MCPServers)
	}

	// A profile that selects the server starts it, off the same default
	// directory.
	document := theMCPSettings() + "profiles:\n  avenor:\n    base: code\n    mcp_servers: [avenor]\nprofile: avenor\n"
	settings, err = config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime = build(t, settings, config.Overrides{})
	if !slices.Equal(runtime.MCPServers, []string{"avenor"}) {
		t.Errorf("the session selects the servers %q, want avenor", runtime.MCPServers)
	}
}

// TestMCPDefaultsProfileOverride covers the profile's word: a profile that
// turns the default mcp directory off runs without the servers it holds,
// whatever the settings say, and one that turns it back on loads them from a
// settings file that turned them off.
func TestMCPDefaultsProfileOverride(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	theDefaultMCPServer(t, pooDir)
	document := theMCPSettings() + "profiles:\n  slim:\n    base: code\n    mcp_defaults: false\nprofile: slim\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if names := runtime.MCPServerNames(); len(names) != 0 {
		t.Errorf("the profile turned the mcp defaults off, and the build loaded %q", names)
	}

	document = theMCPSettings() + "mcp:\n  defaults: false\nprofiles:\n  eager:\n    base: code\n    mcp_defaults: true\nprofile: eager\n"
	settings, err = config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime = build(t, settings, config.Overrides{})
	if names := runtime.MCPServerNames(); !slices.Equal(names, []string{"avenor"}) {
		t.Errorf("the profile turned the mcp defaults on, and the build loaded %q", names)
	}
}

// TestMCPSelectionInheritsAcrossChain covers the inheritance: a profile
// selects what its base chain selects and what it names itself, each server
// once, in the order of first appearance — and a sibling's selection is its
// own, whichever profile the session runs under.
func TestMCPSelectionInheritsAcrossChain(t *testing.T) {
	path := mcpManifest(t, map[string]any{
		"avenor": stdioServer(),
		"notes":  stdioServer(),
		"mail":   stdioServer(),
		"search": stdioServer(),
	}, nil)
	document := theMCPSettings(path) + `profiles:
  grandparent:
    base: base
    mcp_servers: [avenor, notes]
  first:
    base: grandparent
    mcp_servers: [notes, search]
  second:
    base: grandparent
    mcp_servers: [mail]
profile: first
`
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if want := []string{"avenor", "notes", "search"}; !slices.Equal(runtime.MCPServers, want) {
		t.Errorf("the first profile selects %q, want %q", runtime.MCPServers, want)
	}

	// The sibling inherits the grandparent's selection and nothing of its
	// own sibling's.
	runtime = build(t, settings, config.Overrides{Profile: "second"})
	if want := []string{"avenor", "notes", "mail"}; !slices.Equal(runtime.MCPServers, want) {
		t.Errorf("the second profile selects %q, want %q", runtime.MCPServers, want)
	}

	// The chain's own profile selects what it names, not what its children
	// added to it.
	runtime = build(t, settings, config.Overrides{Profile: "grandparent"})
	if want := []string{"avenor", "notes"}; !slices.Equal(runtime.MCPServers, want) {
		t.Errorf("the grandparent profile selects %q, want %q", runtime.MCPServers, want)
	}
}

// TestMCPSelectedMissingWarns covers the loss: a profile that selects a server
// no manifest declares runs, and the build says which server it could not
// find.
func TestMCPSelectedMissingWarns(t *testing.T) {
	path := mcpManifest(t, map[string]any{"avenor": stdioServer()}, nil)
	document := theMCPSettings(path) + "profiles:\n  greedy:\n    base: code\n    mcp_servers: [avenor, nowhere]\nprofile: greedy\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if !slices.Equal(runtime.MCPServers, []string{"avenor", "nowhere"}) {
		t.Errorf("the session selects the servers %q, want both as the profile names them", runtime.MCPServers)
	}
	if !strings.Contains(runtime.Warning, `"nowhere"`) {
		t.Errorf("the build warned %q, want the missing server named", runtime.Warning)
	}
	if strings.Contains(runtime.Warning, `"avenor"`) {
		t.Errorf("the build warned about the server it loaded: %q", runtime.Warning)
	}
}

// TestAProfileRefusesAnEmptySelectedServer covers the validation:
// a selected server name cannot be empty, so a profile that names one
// never reads as a server the settings could not find.
func TestAProfileRefusesAnEmptySelectedServer(t *testing.T) {
	document := theMCPSettings() + "profiles:\n  greedy:\n    base: code\n    mcp_servers: [\"\"]\nprofile: greedy\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	opts := config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		Executors:      theExecutors(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
	}
	_, err = config.Build(opts)
	if err == nil {
		t.Fatal("the build accepted an empty selected server name")
	}
	if !strings.Contains(err.Error(), "mcp_servers") {
		t.Errorf("the failure does not name the field: %v", err)
	}
}

// TestAMCPPathThatDoesNotLoadNamesTheField covers the failure: an
// mcp.paths entry the loader cannot read fails the build, and the failure
// names the settings field the operator wrote.
func TestAMCPPathThatDoesNotLoadNamesTheField(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "mcp.json")
	settings, err := config.Parse([]byte(theMCPSettings(missing)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err == nil {
		t.Fatal("the build accepted an mcp path that is not there")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a config error", err)
	}
	if failure.Code != config.CodeInvalid {
		t.Errorf("the failure is %+v, want invalid", failure)
	}
	if failure.Field != "mcp.paths" {
		t.Errorf("the failure names %q, want mcp.paths", failure.Field)
	}
	if !strings.Contains(err.Error(), "no such file") {
		t.Errorf("the failure %q does not keep the cause", err)
	}
}

// theDefaultMCPFeed writes one server with a resource feed into the
// default mcp directory and returns the directory.
func theDefaultMCPFeed(t *testing.T, pooDir string) string {
	t.Helper()
	dir := filepath.Join(pooDir, "mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("making the default mcp directory: %v", err)
	}
	body := `{"mcpServers": {"avenor": {"command": "true"}}, "poo": {"servers": {"avenor": {"feeds": {"inbox": {"resource": "avenor://inbox"}}}}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return dir
}

// TestASelectedResourceFeedShapesTheProfileTools covers the tool side of
// the counting: a selected server's resource feed is a feed the session
// runs, so a profile that offers no background tool of its own still offers
// the job tools — but the feed is one the session starts beside itself, not
// one the model subscribes to, so the subscribe tool stays out. The
// optional-feed side of the contract is feeds_optional_test.go's.
func TestASelectedResourceFeedShapesTheProfileTools(t *testing.T) {
	poo := map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": map[string]any{
		"inbox": map[string]any{"resource": "avenor://inbox"},
	}}}}
	path := mcpManifest(t, map[string]any{"avenor": stdioServer()}, poo)
	document := theMCPSettings(path) + "profiles:\n  watcher:\n    base: base\n    mcp_servers: [avenor]\nprofile: watcher\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	offered := map[string]bool{}
	for _, tool := range runtime.Profile.Tools {
		offered[tool.Name] = true
	}
	for _, name := range []string{"jobs", "cancel_job"} {
		if !offered[name] {
			t.Errorf("the profile does not offer the %q tool, and the selected server declares a feed the session runs", name)
		}
	}
	if offered["subscribe_feed"] {
		t.Errorf("the profile offers the subscribe tool, and the resource feed is one the session starts beside itself, not one the model subscribes to")
	}
}

// TestAListingCountsASelectedServersResourceFeed covers the listing side of
// the counting: a profile whose only feed is a resource feed a selected
// server declares is advertised with the job tools, the way a session under
// it runs — and a profile that selects nothing, or turns the mcp defaults
// off, is advertised without them.
func TestAListingCountsASelectedServersResourceFeed(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	theDefaultMCPFeed(t, pooDir)
	document := theMCPSettings() + `profiles:
  watcher:
    base: base
    mcp_servers: [avenor]
  plain:
    base: base
  quiet:
    base: base
    mcp_defaults: false
    mcp_servers: [avenor]
profile: watcher
`
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	offered := map[string]map[string]bool{}
	for _, listed := range runtime.Profiles {
		offered[listed.Name] = map[string]bool{}
		for _, tool := range listed.Tools {
			offered[listed.Name][tool.Name] = true
		}
	}
	for _, name := range []string{"jobs", "cancel_job"} {
		if !offered["watcher"][name] {
			t.Errorf("the listing does not offer the %q tool for the watcher profile, and its selected server declares a feed the session runs", name)
		}
	}
	if offered["watcher"]["subscribe_feed"] {
		t.Errorf("the listing offers the subscribe tool for the watcher profile, and the resource feed is one the session starts beside itself")
	}
	if offered["plain"]["jobs"] {
		t.Errorf("the listing offers the job tools for a profile that runs no feed and selects no server")
	}
	if offered["quiet"]["jobs"] {
		t.Errorf("the listing offers the job tools for a profile whose mcp defaults are off: it runs no feed")
	}
	// The session and its listing entry agree for the profile the session
	// runs.
	session := map[string]bool{}
	for _, tool := range runtime.Profile.Tools {
		session[tool.Name] = true
	}
	for _, name := range []string{"jobs", "cancel_job"} {
		if !session[name] {
			t.Errorf("the session does not offer the %q tool, and the selected server declares a feed the session runs", name)
		}
	}
}

// TestASelectedResourceFeedNamesItsSourceWithoutAnExecutor covers the
// diagnostic: a binary that links no executor fails a session whose only
// feed is a selected server's resource feed, and the failure names the mcp
// manifest — not the feeds the settings never named.
func TestASelectedResourceFeedNamesItsSourceWithoutAnExecutor(t *testing.T) {
	poo := map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": map[string]any{
		"inbox": map[string]any{"resource": "avenor://inbox"},
	}}}}
	path := mcpManifest(t, map[string]any{"avenor": stdioServer()}, poo)
	document := theMCPSettings(path) + "profiles:\n  watcher:\n    base: base\n    mcp_servers: [avenor]\nprofile: watcher\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
	})
	if err == nil {
		t.Fatal("the binary links no executor, and the build took the session anyway")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a config error", err)
	}
	if failure.Code != config.CodeUnsupported || failure.Field != "tools.bash.executor" {
		t.Errorf("the failure is %+v, want unsupported on tools.bash.executor", failure)
	}
	if !strings.Contains(failure.Message, "a selected mcp server declares a feed") {
		t.Errorf("the message %q does not name the mcp manifest as the source", failure.Message)
	}
	if strings.Contains(failure.Message, "the settings name a feed") {
		t.Errorf("the message %q blames the feeds the settings never named", failure.Message)
	}
}

// TestMCPFeedNameCollision covers the shared namespace: a resource feed a
// selected server declares may not carry the name of a feed the settings load,
// and the failure names the name and the server.
func TestMCPFeedNameCollision(t *testing.T) {
	feeds := feedManifestNamed(t, "inbox")
	poo := map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": map[string]any{
		"inbox": map[string]any{"resource": "avenor://inbox"},
	}}}}
	path := mcpManifest(t, map[string]any{"avenor": stdioServer()}, poo)
	document := theMCPSettings(path) + "feeds:\n  paths: [\"" + feeds + "\"]\nprofiles:\n  inbox:\n    base: code\n    mcp_servers: [avenor]\nprofile: inbox\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings: settings, Overrides: config.Overrides{}, WorkingDir: t.TempDir(),
		DefaultProfile: "code", Tools: theTools(), Profiles: theProfiles(), Executors: theExecutors(),
	})
	if err == nil {
		t.Fatal("the build took two feeds for one name")
	}
	for _, want := range []string{"inbox", "avenor", "mcp.paths"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure is %v, want it to name %q", err, want)
		}
	}
}

// TestAProfilesDeclaredFeedCountsASelectedServersResourceFeed covers the other
// side of the namespace: a profile that declares a feed by name is satisfied by
// a resource feed a selected server declares, and the build does not warn about
// a feed it runs.
func TestAProfilesDeclaredFeedCountsASelectedServersResourceFeed(t *testing.T) {
	poo := map[string]any{"servers": map[string]any{"avenor": map[string]any{"feeds": map[string]any{
		"inbox": map[string]any{"resource": "avenor://inbox"},
	}}}}
	path := mcpManifest(t, map[string]any{"avenor": stdioServer()}, poo)
	document := theMCPSettings(path) + "profiles:\n  inbox:\n    base: code\n    mcp_servers: [avenor]\n    feeds: [inbox]\nprofile: inbox\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if strings.Contains(runtime.Warning, `"inbox"`) {
		t.Errorf("the build warned %q, and the selected server declares the inbox feed", runtime.Warning)
	}
}
