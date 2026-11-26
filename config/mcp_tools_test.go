package config_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/mcp"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/callmcp"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// testMCPServer is the test binary's own view of one started server: the
// config tests link the same wiring cmd/poo links, and the settings pass the
// launcher's handles to the tools opaque.
type testMCPServer struct {
	// group is the client the launcher started.
	group *mcp.Group
	// name is the server's declared name.
	name string
}

// testMCPServer is a server the tools over it read.
var _ callmcp.Server = (*testMCPServer)(nil)

// Name is the server's declared name.
func (s *testMCPServer) Name() string { return s.name }

// Tools returns the tools the server listed at startup.
func (s *testMCPServer) Tools() []mcp.Tool {
	tools, _ := s.group.Tools(s.name)
	return tools
}

// Offered returns the server's tools as the entries the model reads.
func (s *testMCPServer) Offered() []cli.Offered {
	return callmcp.Entries(s.name, s.Tools())
}

// CallTool calls one of them.
func (s *testMCPServer) CallTool(ctx context.Context, tool string, args json.RawMessage) (mcp.CallResult, error) {
	return s.group.CallTool(ctx, s.name+"/"+tool, args)
}

// mcpServers keeps the servers a session started, as the tools over them read
// them: the same conversion cmd/poo's own factory makes.
func mcpServers(running []config.MCPRunning) []callmcp.Server {
	var servers []callmcp.Server
	for _, handle := range running {
		session, ok := handle.(interface{ servers() []callmcp.Server })
		if !ok {
			continue
		}
		servers = append(servers, session.servers()...)
	}
	return servers
}

// offersTool reports whether a session's tool list holds a name.
func offersTool(names []string, name string) bool {
	for _, offered := range names {
		if offered == name {
			return true
		}
	}
	return false
}

// sessionPrompt renders the prompt a built session carries.
//
// The MCP catalog is the session's: it is rendered when the session attaches,
// so a session is what shows it.
func sessionPrompt(t *testing.T, runtime *config.Runtime, profile, workingDir string) string {
	t.Helper()
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord(profile, workingDir))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess.PromptState().Text
}

// mcpCatalogOf renders the MCP section an offered call tool carries, for the
// build's own instance of it.
func mcpCatalogOf(t *testing.T, runtime *config.Runtime) string {
	t.Helper()
	tool := runtime.Options.Tools[config.ToolCallMCP]
	if tool == nil {
		t.Fatal("the session offers no call_mcp tool")
	}
	contributor, ok := tool.(poo.PromptContributor)
	if !ok {
		t.Fatalf("the tool %T contributes no prompt section", tool)
	}
	return contributor.PromptSection(prompt.Env{})
}

// TestMCPSelectionOffersTheCallAndTheSearch covers the offering: a profile
// that selects a server is offered the tool that calls its tools, and a
// session with a decision model searches them as well.
func TestMCPSelectionOffersTheCallAndTheSearch(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	if !hasTool(runtime.Options.ToolOrder, config.ToolCallMCP) {
		t.Errorf("the session offers %v, and not call_mcp", runtime.Options.ToolOrder)
	}
	if !hasTool(runtime.Options.ToolOrder, cliregistry.FindToolName) {
		t.Errorf("the session offers %v, and not the search over the MCP tools", runtime.Options.ToolOrder)
	}
	// The profile names what it offers, so the session header carries it.
	var offered []string
	for _, tool := range runtime.Profile.Tools {
		offered = append(offered, tool.Name)
	}
	if !hasTool(offered, config.ToolCallMCP) {
		t.Errorf("the profile offers %q, and not call_mcp", offered)
	}
	// The tool describes itself without a session: a build starts no server.
	spec := runtime.Options.Tools[config.ToolCallMCP].Spec()
	if spec.Name != config.ToolCallMCP {
		t.Errorf("the tool calls itself %q, want %q", spec.Name, config.ToolCallMCP)
	}
	if spec.Class != poo.ClassSideEffecting {
		t.Errorf("the call is %q, want side-effecting", spec.Class)
	}
	if spec.PrimaryArg != callmcp.NameArgument {
		t.Errorf("the call's primary argument is %q, want the name a rule matches", spec.PrimaryArg)
	}
}

// TestMCPSelectionWithoutTheCallToolFailsTheBuild covers the requirement: a
// selection starts processes whose tools only one call reaches, so a binary
// that links none cannot honour it, and the failure names what is missing.
func TestMCPSelectionWithoutTheCallToolFailsTheBuild(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	tools := theTools()
	delete(tools, config.ToolCallMCP)
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          tools,
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
		MCPLauncher:    theStubMCPLauncher(),
	})
	if err == nil {
		t.Fatal("the build accepted a selection with no tool to call the servers")
	}
	for _, want := range []string{"fixture", config.ToolCallMCP} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure is %q, want it to name %q", err, want)
		}
	}
}

// TestADisabledMCPCallOffersTheServersToNobody covers the operator's word: a
// session that turns call_mcp off lists no MCP tool, and its search returns
// none either — a hit the model cannot call is worse than no hit.
func TestADisabledMCPCallOffersTheServersToNobody(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+"tools:\n  disabled: ["+config.ToolCallMCP+"]\n"+theSessionDir(t)+
		theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	if hasTool(runtime.Options.ToolOrder, config.ToolCallMCP) {
		t.Errorf("the session offers a call the settings disable: %v", runtime.Options.ToolOrder)
	}
	// The search is bound with the call: a session with no way to call an MCP
	// tool has no MCP entry for a search to return.
	if hasTool(runtime.Options.ToolOrder, cliregistry.FindToolName) {
		t.Errorf("the session offers a search over servers it cannot call: %v", runtime.Options.ToolOrder)
	}
	if text := sessionPrompt(t, runtime, "picky", t.TempDir()); strings.Contains(text, "MCP tools:") {
		t.Errorf("the prompt lists MCP tools the session cannot call: %q", text)
	}
}

// TestADisabledMCPCallLeavesNoMCPEntryInTheSearch covers the search a session
// keeps for its catalog: the MCP servers it selected are reachable through no
// tool, so a search over the catalog returns the catalog's entries and nothing
// of theirs.
func TestADisabledMCPCallLeavesNoMCPEntryInTheSearch(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+theSessionDir(t)+"tools:\n  disabled: ["+config.ToolCallMCP+"]\n"+
		"cli_registry:\n  paths: [\""+thresholdRegistry(t, 40, 0)+"\"]\n"+
		theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		MCPLauncher: listedLauncher(map[string][]mcp.Tool{"fixture": {mcpTool("echo", "answers with what it is given", false)}}),
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()
	if !hasTool(runtime.Options.ToolOrder, cliregistry.FindToolName) {
		t.Fatalf("the session offers %v, and no search over its 40 entries", runtime.Options.ToolOrder)
	}
	runtime.Options.Decider = yesDecider{}
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{
			Name:      cliregistry.FindToolName,
			ID:        "c1",
			Arguments: json.RawMessage(`{"intent":"echo what I give it"}`),
		}}},
		pootest.Turn{Text: "done"},
	)
	runtime.Options.Transport = transport

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()
	if _, err := sess.Prompt(context.Background(), "find a tool"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	text := requestText(transport)
	// The search answered from the catalog, and the server's tool is not in
	// its reach.
	if !strings.Contains(text, "entry-") {
		t.Errorf("the search did not return the catalog's entries:\n%s", text)
	}
	if strings.Contains(text, "fixture/") {
		t.Errorf("the search returned a tool the session cannot call:\n%s", text)
	}
}

// TestADisabledSearchLeavesTheMCPCatalogListed covers the pair: a session that
// turns find_tool off keeps call_mcp, and the catalog lists the tools instead
// of leaving the names to a search it does not offer.
func TestADisabledSearchLeavesTheMCPCatalogListed(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+"tools:\n  disabled: ["+cliregistry.FindToolName+"]\n"+
		theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	if hasTool(runtime.Options.ToolOrder, cliregistry.FindToolName) {
		t.Errorf("the session offers a search the settings disable: %v", runtime.Options.ToolOrder)
	}
	if !hasTool(runtime.Options.ToolOrder, config.ToolCallMCP) {
		t.Errorf("the session offers %v, and not call_mcp", runtime.Options.ToolOrder)
	}
}

// TestMCPSelectionWithoutADeciderSearchesNothing covers the gate: the search
// asks a decision model, so a session without one is offered the call alone
// and lists the tools its servers offer.
func TestMCPSelectionWithoutADeciderSearchesNothing(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	// withoutDeciderModel names a decider role the catalog cannot resolve, so
	// the settings ask for a search the session does not get.
	settings, err := config.Parse([]byte(withoutDeciderModel+theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	if hasTool(runtime.Options.ToolOrder, cliregistry.FindToolName) {
		t.Errorf("the session offers a search with no decision model: %v", runtime.Options.ToolOrder)
	}
	if !hasTool(runtime.Options.ToolOrder, config.ToolCallMCP) {
		t.Errorf("the session offers %v, and not call_mcp", runtime.Options.ToolOrder)
	}
}

// theSessionDir renders the settings block that puts the session's records in
// a directory of the test's own.
func theSessionDir(t *testing.T) string {
	t.Helper()
	return "session:\n  dir: " + t.TempDir() + "\n"
}

// theMCPPaths renders the settings block that loads one manifest.
func theMCPPaths(paths ...string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, "\""+path+"\"")
	}
	return "mcp:\n  paths: [" + strings.Join(quoted, ", ") + "]\n"
}

// theMCPProfile renders the settings block of a profile that selects servers.
func theMCPProfile(name, servers string) string {
	return "profiles:\n  " + name + ":\n    base: code\n    mcp_servers: [" + servers + "]\nprofile: " + name + "\n"
}

// listedServer is one server a test started without a process: it lists the
// tools a test names, and a call finds nothing listening.
type listedServer struct {
	// name is the server's declared name.
	name string
	// tools are the tools it lists.
	tools []mcp.Tool
}

// listedServer is a server the tools over it read.
var _ callmcp.Server = (*listedServer)(nil)

// Name is the server's declared name.
func (s *listedServer) Name() string { return s.name }

// Tools returns the tools the server lists.
func (s *listedServer) Tools() []mcp.Tool { return s.tools }

// Offered returns the server's tools as the entries the model reads.
func (s *listedServer) Offered() []cli.Offered {
	return callmcp.Entries(s.name, s.tools)
}

// CallTool finds no process to call.
func (s *listedServer) CallTool(context.Context, string, json.RawMessage) (mcp.CallResult, error) {
	return mcp.CallResult{}, mcp.ErrServerDown
}

// listedSession is the handle a test's launcher returns for one launch: the
// per-server views the tools over them read.
type listedSession struct {
	// views are the per-server views, one per declared server.
	views []callmcp.Server
}

// Close stops nothing: the listed servers hold no process.
func (h *listedSession) Close() error { return nil }

// Offered returns every entry the servers hold: the package's own derivation
// over the same per-server views the dispatch reads.
func (h *listedSession) Offered() []cli.Offered { return callmcp.Offered(h.views) }

// servers are the views the tools read.
func (h *listedSession) servers() []callmcp.Server { return h.views }

// listedLauncher returns a launcher whose servers list the tools the test
// names, so a test reads what a session offers without starting a process.
func listedLauncher(tools map[string][]mcp.Tool) config.MCPLauncher {
	return func(_ context.Context, values config.MCPLaunchSelection) (config.MCPRunning, error) {
		servers := make([]callmcp.Server, 0, len(values.Servers))
		for _, server := range values.Servers {
			servers = append(servers, &listedServer{name: server.Name, tools: tools[server.Name]})
		}
		return &listedSession{views: servers}, nil
	}
}

// mcpTool is one tool a listed server offers.
func mcpTool(name, description string, readOnly bool) mcp.Tool {
	return mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		Annotations: mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: !readOnly},
	}
}

// manyMCPTools returns n tools a listed server offers, by name.
func manyMCPTools(n int) []mcp.Tool {
	tools := make([]mcp.Tool, 0, n)
	for i := 0; i < n; i++ {
		tools = append(tools, mcpTool(fmt.Sprintf("tool-%03d", i), fmt.Sprintf("does the job %d", i), false))
	}
	return tools
}

// TestMCPCatalogListsAShortOffering covers the section: a session whose
// servers offer few tools lists each one with its arguments and the server's
// own hint, because the prompt is the only place the names are when the
// offering is not searched.
func TestMCPCatalogListsAShortOffering(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(theMCPSettings(path)+theSessionDir(t)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		MCPLauncher: listedLauncher(map[string][]mcp.Tool{"fixture": {mcpTool("echo", "answers with what it is given", false), mcpTool("count", "reads the counter", true)}}),
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()

	text := sessionPrompt(t, runtime, "picky", t.TempDir())
	for _, want := range []string{
		"fixture/count (args: text*) (read-only): reads the counter",
		"fixture/echo (args: text*) (destructive): answers with what it is given",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "call_mcp") {
		t.Errorf("the prompt does not say what calls an MCP tool: %s", text)
	}
}

// TestALongMCPOfferingIsSearched covers the other half: past the threshold the
// catalog leaves the list to the search, and a session without the search
// lists the tools whatever their number.
func TestALongMCPOfferingIsSearched(t *testing.T) {
	tools := manyMCPTools(40)
	for _, tc := range []struct {
		name     string
		settings string
		searched bool
	}{
		{name: "with a decider", settings: withDecider, searched: true},
		{name: "without a decider", settings: withoutDeciderModel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
			settings, err := config.Parse([]byte(tc.settings+theMCPPaths(path)+theSessionDir(t)+theMCPProfile("picky", "fixture")), config.FormatYAML)
			if err != nil {
				t.Fatalf("decoding the settings: %v", err)
			}
			runtime := buildWith(t, config.BuildOptions{
				Settings:    settings,
				MCPLauncher: listedLauncher(map[string][]mcp.Tool{"fixture": tools}),
				WorkingDir:  t.TempDir(),
			})
			defer runtime.Store.Close()

			text := sessionPrompt(t, runtime, "picky", t.TempDir())
			if tc.searched {
				if !strings.Contains(text, "too many to list") || !strings.Contains(text, cliregistry.FindToolName) {
					t.Errorf("the prompt does not leave the offering to the search:\n%s", text)
				}
				if strings.Contains(text, "fixture/tool-000") {
					t.Errorf("the prompt lists an offering it should search:\n%s", text)
				}
				return
			}
			if strings.Contains(text, "too many to list") {
				t.Errorf("the prompt names a search the session does not offer:\n%s", text)
			}
			if !strings.Contains(text, "fixture/tool-000 (args: text*) (destructive)") {
				t.Errorf("the prompt does not list the offering it cannot search:\n%s", text)
			}
		})
	}
}

// TestTheMCPRoutingMatrixOffersWhatTheSessionCanReach covers the four shapes a
// session reaches its tools through: the catalog alone, the catalog beside the
// MCP servers, and the catalog's entries offered directly with the servers
// beside them.
//
// The catalog's own rule is the one it always was — a short catalog is offered
// entry by entry, a long one behind the call or the search — and adding MCP
// servers changes none of it.
func TestTheMCPRoutingMatrixOffersWhatTheSessionCanReach(t *testing.T) {
	small := registry(t, "deploy", "logs")
	large := thresholdRegistry(t, 40, 0)
	for _, tc := range []struct {
		name     string
		settings string
		registry string
		servers  string
		want     []string
		unwanted []string
	}{
		{
			name:     "MCP only",
			settings: withoutDeciderModel,
			servers:  "fixture",
			want:     []string{config.ToolCallMCP},
			unwanted: []string{cliregistry.CallToolName},
		},
		{
			name:     "MCP only, with a decider",
			settings: withDecider,
			servers:  "fixture",
			want:     []string{config.ToolCallMCP, cliregistry.FindToolName},
		},
		{
			name:     "a short catalog is elevated beside the servers",
			settings: withoutDeciderModel,
			registry: small,
			servers:  "fixture",
			want:     []string{"deploy", "logs", config.ToolCallMCP},
			unwanted: []string{cliregistry.CallToolName},
		},
		{
			name:     "a long catalog is called beside the servers",
			settings: withoutDeciderModel,
			registry: large,
			servers:  "fixture",
			want:     []string{cliregistry.CallToolName, config.ToolCallMCP},
			unwanted: []string{cliregistry.FindToolName, "entry-000"},
		},
		{
			// The elevated entries are tools of their own, so the search the
			// servers brought narrows only what call_mcp reaches.
			name:     "an elevated catalog leaves the search to the servers",
			settings: withDecider,
			registry: small,
			servers:  "fixture",
			want:     []string{"deploy", "logs", cliregistry.FindToolName, config.ToolCallMCP},
			unwanted: []string{cliregistry.CallToolName},
		},
		{
			name:     "a long catalog is searched beside the servers",
			settings: withDecider,
			registry: large,
			servers:  "fixture",
			want:     []string{cliregistry.CallToolName, cliregistry.FindToolName, config.ToolCallMCP},
			unwanted: []string{"entry-000"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
			document := tc.settings + theSessionDir(t) + theMCPPaths(path)
			if tc.registry != "" {
				document += "cli_registry:\n  paths: [\"" + tc.registry + "\"]\n"
			}
			settings, err := config.Parse([]byte(document+theMCPProfile("picky", tc.servers)), config.FormatYAML)
			if err != nil {
				t.Fatalf("decoding the settings: %v", err)
			}
			runtime := buildWith(t, config.BuildOptions{
				Settings:    settings,
				MCPLauncher: listedLauncher(map[string][]mcp.Tool{"fixture": {mcpTool("echo", "answers with what it is given", false)}}),
				WorkingDir:  t.TempDir(),
			})
			defer runtime.Store.Close()

			for _, name := range tc.want {
				if !hasTool(runtime.Options.ToolOrder, name) {
					t.Errorf("the session offers %v, and not %q", runtime.Options.ToolOrder, name)
				}
			}
			for _, name := range tc.unwanted {
				if hasTool(runtime.Options.ToolOrder, name) {
					t.Errorf("the session offers %q beside the servers: %v", name, runtime.Options.ToolOrder)
				}
			}
			// One search is offered once, however many offerings it narrows.
			searches := 0
			for _, name := range runtime.Options.ToolOrder {
				if name == cliregistry.FindToolName {
					searches++
				}
			}
			if searches > 1 {
				t.Errorf("the session offers %d searches: %v", searches, runtime.Options.ToolOrder)
			}
		})
	}
}

// TestASearchDoesNotReturnAnElevatedEntry covers the view: a catalog short
// enough to show is offered entry by entry, so it contributes nothing to the
// search. A hit for one of its entries would tell the model to call call_cli,
// a tool the session does not offer; what is left to search is what call_mcp
// reaches.
func TestASearchDoesNotReturnAnElevatedEntry(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+theSessionDir(t)+
		"cli_registry:\n  paths: [\""+registry(t, "deploy", "logs")+"\"]\n"+
		theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		MCPLauncher: listedLauncher(map[string][]mcp.Tool{"fixture": {mcpTool("echo", "answers with what it is given", true)}}),
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()
	for _, name := range []string{"deploy", "logs", config.ToolCallMCP, cliregistry.FindToolName} {
		if !hasTool(runtime.Options.ToolOrder, name) {
			t.Fatalf("the session offers %v, and not %q", runtime.Options.ToolOrder, name)
		}
	}
	if hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Fatalf("the session offers %v, and the entries are already its own", runtime.Options.ToolOrder)
	}
	runtime.Options.Decider = yesDecider{}
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{
			Name:      cliregistry.FindToolName,
			ID:        "c1",
			Arguments: json.RawMessage(`{"intent":"show me where I am running"}`),
		}}},
		pootest.Turn{Text: "done"},
	)
	runtime.Options.Transport = transport

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()
	if _, err := sess.Prompt(context.Background(), "show me where I am running"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The answer is what the search returned; the prompt's guidance names both
	// calls whatever the session offers, so the hit list is what the test reads.
	text := requestText(transport)
	answer := text[strings.Index(text, "best first:"):]
	for _, want := range []string{"fixture/echo", config.ToolCallMCP} {
		if !strings.Contains(answer, want) {
			t.Errorf("the search's answer does not carry %q:\n%s", want, answer)
		}
	}
	// The elevated entries are tools of their own, and no call of the catalog's
	// is offered, so a hit for one would point the model at nothing.
	for _, unwanted := range []string{"deploy", "logs", cliregistry.CallToolName} {
		if strings.Contains(answer, unwanted) {
			t.Errorf("the search's answer names %q, which the session does not call:\n%s", unwanted, answer)
		}
	}
}

// TestACatalogBehindTheCallIsListedBesideTheSearchedServers covers the prompt's
// two catalogs: a search the MCP offering brought says nothing about the
// catalog's own, which is short enough to list. A catalog that vanished behind
// a search would cost a decision-model round trip for a list the model could
// read, and its entries would be nowhere at all.
func TestACatalogBehindTheCallIsListedBesideTheSearchedServers(t *testing.T) {
	// Twenty entries are short enough to list, and the elevation rule declines
	// them because one entry's name is a tool the session already offers.
	names := make([]string, 0, 20)
	names = append(names, "bash")
	for i := 0; i < 19; i++ {
		names = append(names, fmt.Sprintf("entry-%03d", i))
	}
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+theSessionDir(t)+
		"cli_registry:\n  paths: [\""+registry(t, names...)+"\"]\n"+
		theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		MCPLauncher: listedLauncher(map[string][]mcp.Tool{"fixture": {mcpTool("echo", "answers with what it is given", true)}}),
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()
	if !hasTool(runtime.Options.ToolOrder, cliregistry.FindToolName) {
		t.Fatalf("the session offers %v, and the case is about the search it carries", runtime.Options.ToolOrder)
	}

	text := sessionPrompt(t, runtime, "picky", t.TempDir())
	if !strings.Contains(text, "These are the only commands call_cli runs") || !strings.Contains(text, "entry-000") {
		t.Errorf("the prompt does not list the catalog the session can call:\n%s", text)
	}
	if strings.Contains(text, "too many to list") {
		t.Errorf("the prompt leaves a twenty-entry catalog to the search the servers brought:\n%s", text)
	}
}

// TestEachSessionCarriesItsOwnMCPCatalog covers the split between what the
// header records and what a session reads: the tool offering is the build's and
// every session of it offers the same tools, while the catalog behind the call
// is the one the session's own servers listed.
func TestEachSessionCarriesItsOwnMCPCatalog(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+theMCPPaths(path)+theSessionDir(t)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	// Each session starts a server that lists one tool of its own, so a catalog
	// that belonged to the build would show one session the other's tool.
	launcher := func(_ context.Context, values config.MCPLaunchSelection) (config.MCPRunning, error) {
		servers := make([]callmcp.Server, 0, len(values.Servers))
		for _, server := range values.Servers {
			name := values.SessionID + "-tool"
			servers = append(servers, &listedServer{name: server.Name, tools: []mcp.Tool{mcpTool(name, "does the "+name+" job", true)}})
		}
		return &listedSession{views: servers}, nil
	}
	runtime := buildWith(t, config.BuildOptions{Settings: settings, MCPLauncher: launcher, WorkingDir: t.TempDir()})
	defer runtime.Store.Close()

	// The header records the offering, and the build's own instance lists no
	// tool: a build starts no server.
	if !hasTool(runtime.Options.ToolOrder, config.ToolCallMCP) {
		t.Fatalf("the session offers %v, and not call_mcp", runtime.Options.ToolOrder)
	}
	if section := mcpCatalogOf(t, runtime); strings.Contains(section, "MCP (Model Context Protocol) tools:") {
		t.Errorf("the build's own call carries a catalog:\n%s", section)
	}

	for _, id := range []string{"s1", "s2"} {
		created := theCreatedRecord("picky", t.TempDir())
		created.SessionID = id
		sess, err := runtime.CreateSession(context.Background(), runtime.Store, created)
		if err != nil {
			t.Fatalf("creating the session %s: %v", id, err)
		}
		text := sess.PromptState().Text
		if err := sess.Close(); err != nil {
			t.Fatalf("closing the session %s: %v", id, err)
		}
		if !strings.Contains(text, "fixture/"+id+"-tool") {
			t.Errorf("the session %s does not list the tool its own server offered:\n%s", id, text)
		}
		other := "s1"
		if id == "s1" {
			other = "s2"
		}
		if strings.Contains(text, "fixture/"+other+"-tool") {
			t.Errorf("the session %s lists the tool another session's server offered:\n%s", id, text)
		}
	}
}

// yesDecider answers every question the same way, so a search returns every
// entry it was asked about.
type yesDecider struct{}

// Decide answers yes to every question.
func (yesDecider) Decide(_ context.Context, _ any, _ string, questions []decide.Question) ([]decide.Answer, error) {
	answers := make([]decide.Answer, 0, len(questions))
	for _, question := range questions {
		answers = append(answers, decide.Answer{ID: question.ID, Probabilities: map[string]float64{"yes": 0.9, "no": 0.1}})
	}
	return answers, nil
}

// TestASessionFindsAndCallsAnMCPTool covers the two tools together: the model
// searches the offering, the hit names the call that runs the tool, and the
// call reaches the server the session started.
func TestASessionFindsAndCallsAnMCPTool(t *testing.T) {
	binary := fixtureServer(t)
	path := mcpManifest(t, map[string]any{"fixture": map[string]any{"command": binary}}, nil)
	settings, err := config.Parse([]byte(withDecider+theSessionDir(t)+theMCPPaths(path)+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	var group *mcp.Group
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		MCPLauncher: clientLauncher(&group),
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()
	// The search answers through the session's decider, and a test names the
	// answer rather than running a decision endpoint.
	runtime.Options.Decider = yesDecider{}
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{
			Name:      cliregistry.FindToolName,
			ID:        "c1",
			Arguments: json.RawMessage(`{"intent":"echo the words I give it"}`),
		}}},
		pootest.Turn{Calls: []pootest.Call{{
			Name:      config.ToolCallMCP,
			ID:        "c2",
			Arguments: json.RawMessage(`{"name":"fixture/echo","args":{"text":"hello"}}`),
		}}},
		pootest.Turn{Text: "done"},
	)
	runtime.Options.Transport = transport

	workDir := t.TempDir()
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", workDir))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()
	if _, err := sess.Prompt(context.Background(), "echo something"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	text := requestText(transport)
	// The hit said which tool calls an MCP tool, and by which name.
	for _, want := range []string{"fixture/echo", config.ToolCallMCP, callmcp.NameArgument} {
		if !strings.Contains(text, want) {
			t.Errorf("the search's answer does not carry %q:\n%s", want, text)
		}
	}
	// The call reached the server: the fixture answers with what it was
	// given and the world it runs in.
	for _, want := range []string{`"text":"hello"`, workDir} {
		if !strings.Contains(text, want) {
			t.Errorf("the call's answer does not carry %q:\n%s", want, text)
		}
	}
}

// TestAPrefixRuleAllowsOneMCPServer covers the permission class: the call is
// side-effecting with the qualified name as its primary argument, so a rule
// on the server's prefix allows its tools and reaches nothing else.
func TestAPrefixRuleAllowsOneMCPServer(t *testing.T) {
	binary := fixtureServer(t)
	path := mcpManifest(t, map[string]any{"fixture": map[string]any{"command": binary}}, nil)
	settings, err := config.Parse([]byte(theModelSettings+theSessionDir(t)+theMCPPaths(path)+`permission:
  unmatched: deny
  rules:
    - tool: `+config.ToolCallMCP+`
      command_prefix: fixture/
      action: allow
`+theMCPProfile("picky", "fixture")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	var group *mcp.Group
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		MCPLauncher: clientLauncher(&group),
		WorkingDir:  t.TempDir(),
	})
	defer runtime.Store.Close()
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{
			{Name: config.ToolCallMCP, ID: "c1", Arguments: json.RawMessage(`{"name":"fixture/echo","args":{"text":"allowed"}}`)},
			{Name: config.ToolCallMCP, ID: "c2", Arguments: json.RawMessage(`{"name":"elsewhere/echo","args":{}}`)},
		}},
		pootest.Turn{Text: "done"},
	)
	runtime.Options.Transport = transport

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, theCreatedRecord("picky", t.TempDir()))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()
	if _, err := sess.Prompt(context.Background(), "call the fixture"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	text := requestText(transport)
	if !strings.Contains(text, `"text":"allowed"`) {
		t.Errorf("the allowed call did not reach the server:\n%s", text)
	}
	// The prefix is the whole rule: a name it does not match falls to the
	// unmatched action, so the second call never left the session.
	if !strings.Contains(text, "the permission policy denied this call") {
		t.Errorf("the call outside the prefix was not refused:\n%s", text)
	}
	answers := permissionAnswers(t, sess)
	if len(answers) != 0 {
		t.Errorf("the allowed call asked %d times, want none", len(answers))
	}
}

// TestTheListingOffersTheMCPCallASessionDoes covers the two paths that decide what
// a profile offers: the profile a person reads and the session a profile runs
// name the same tools, so a server a profile selects reaches the model on both.
func TestTheListingOffersTheMCPCallASessionDoes(t *testing.T) {
	path := mcpManifest(t, map[string]any{"fixture": stdioServer()}, nil)
	settings, err := config.Parse([]byte(withDecider+theSessionDir(t)+theMCPPaths(path)+
		"profiles:\n  picky:\n    base: code\n    mcp_servers: [fixture]\n  plain:\n    base: code\nprofile: picky\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	offered := map[string][]string{}
	for _, listed := range runtime.Profiles {
		for _, tool := range listed.Tools {
			offered[listed.Name] = append(offered[listed.Name], tool.Name)
		}
	}
	if !hasTool(offered["picky"], config.ToolCallMCP) {
		t.Errorf("the listing offers the selecting profile %q, and not %q", offered["picky"], config.ToolCallMCP)
	}
	if !hasTool(offered["picky"], cliregistry.FindToolName) {
		t.Errorf("the listing offers the selecting profile %q, and not the search", offered["picky"])
	}
	if hasTool(offered["plain"], config.ToolCallMCP) {
		t.Errorf("the listing offers a profile that selects no server %q", offered["plain"])
	}
	// The running session's own offering is the listing's.
	if got, want := len(runtime.Profile.Tools), len(offered["picky"]); got != want {
		t.Errorf("the session offers %d tools, and the listing %d", got, want)
	}
}
