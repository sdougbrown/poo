package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/codemode/quickjs"
	codemodestarlark "github.com/sdougbrown/poo/codemode/starlark"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/mcp"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/base"
	"github.com/sdougbrown/poo/profile/code"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/callmcp"
	"github.com/sdougbrown/poo/tool/cliregistry"
	"github.com/sdougbrown/poo/tool/codemode"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/fetch"
	"github.com/sdougbrown/poo/tool/finish"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/history"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/tool/write"
)

// addFinishTool gives the session the typed-result tool.
//
// The settings name the schema the result is validated against, and
// the tool is the one the binary links. A session with a schema and no
// tool would never offer the model a way to finish.
//
// The tool joins the profile's tools, so the client is told about it
// like every other tool the session offers.
func addFinishTool(runtime *config.Runtime) error {
	if runtime.FinishSchema == nil {
		return nil
	}
	tool, err := finish.New(runtime.FinishSchema)
	if err != nil {
		return err
	}
	if runtime.Options.Tools == nil {
		runtime.Options.Tools = map[string]poo.Tool{}
	}
	runtime.Options.Tools[poo.FinishToolName] = tool
	runtime.Options.ToolOrder = append(runtime.Options.ToolOrder, poo.FinishToolName)
	runtime.Options.FinishSchema = runtime.FinishSchema
	runtime.Profile.Tools = append(runtime.Profile.Tools, poo.ToolInfo{
		Name:  poo.FinishToolName,
		Class: poo.ClassReadOnly,
	})
	return nil
}

// defaultProfile is the profile a session runs under when neither the
// command line nor the settings name one.
//
// The code profile is the one a coding session runs under: bash is the
// primary tool, and the file tools are the conveniences around it.
const defaultProfile = code.Name

// linkedProfiles returns the profiles the command links.
//
// The command offers the three profiles the plan names, and no others:
// a profile the command does not link is a profile the command cannot
// run.
func linkedProfiles() []profile.Profile {
	return []profile.Profile{
		base.Profile(),
		code.Profile(),
		search.Profile(),
	}
}

// linkedTools returns the tools the command links.
//
// The tools are built from the settings' options, so the executor and
// the child environment are the ones the settings name. A tool that
// needs nothing from the settings ignores them.
func linkedTools() map[string]config.ToolFactory {
	return map[string]config.ToolFactory{
		"bash": func(opts config.ToolOptions) poo.Tool {
			return bash.New(bash.WithExecutor(opts.Executor), bash.WithScrub(opts.Scrub))
		},
		"read":    func(opts config.ToolOptions) poo.Tool { return read.New(read.WithInputs(opts.Inputs...)) },
		"grep":    func(config.ToolOptions) poo.Tool { return grep.New() },
		"history": func(config.ToolOptions) poo.Tool { return history.New() },
		"edit":    func(opts config.ToolOptions) poo.Tool { return edit.New() },
		"write":   func(opts config.ToolOptions) poo.Tool { return write.New() },
		"codemode": func(opts config.ToolOptions) poo.Tool {
			// An unset engine is the settings' default one, the same
			// value the settings check judges limits by. Starlark is the
			// engine a settings file has to name.
			engine := opts.CodemodeEngine
			if engine == "" {
				engine = config.DefaultCodemodeEngine
			}
			if engine == "starlark" {
				return codemode.New(codemodestarlark.New())
			}
			return codemode.New(quickjs.New())
		},
		"fetch": func(config.ToolOptions) poo.Tool {
			return fetch.New()
		},
		"call_cli": func(opts config.ToolOptions) poo.Tool {
			return cliregistry.NewCall(opts.Registry).WithSearch(opts.Search).WithSessionTools(opts.ToolNames).WithExecutor(opts.Executor)
		},
		"find_tool": func(opts config.ToolOptions) poo.Tool {
			// The search narrows the session's snapshot: the catalog's entries,
			// and the MCP tools behind call_mcp. Those are already the entries
			// the session can reach, so the search offers nothing the model
			// cannot call.
			return cliregistry.NewFind(opts.Offered)
		},
		"call_mcp": func(opts config.ToolOptions) poo.Tool {
			// The call lists the MCP entries of the same snapshot, and its
			// refusal points at the search only when the session offers one.
			return callmcp.New(mcpServers(opts.MCP), opts.Offered).
				WithInputs(opts.Inputs...).
				WithSearch(offersTool(opts.ToolNames, cliregistry.FindToolName))
		},
	}
}

// mcpSession is the launcher's handle for one session's launch: the group
// that owns the session's server processes, and the names the selection
// declared.
type mcpSession struct {
	// group is the client the launcher started, which owns the processes.
	group *mcp.Group
	// names are the servers the selection declared, in selection order.
	names []string
}

// servers are the per-server views the tools over them read.
func (h *mcpSession) servers() []callmcp.Server {
	servers := make([]callmcp.Server, 0, len(h.names))
	for _, name := range h.names {
		servers = append(servers, &mcpServer{group: h.group, name: name})
	}
	return servers
}

// Offered returns every entry the servers hold: the package's own derivation
// over the same per-server views the dispatch reads, so a handle is never
// listed without being dispatchable.
func (h *mcpSession) Offered() []cli.Offered {
	return callmcp.Offered(h.servers())
}

// Close stops the servers and waits for their processes to end.
func (h *mcpSession) Close() error { return h.group.Close() }

// mcpServer is one MCP server the launcher started, as the tools over it see
// it: the server's own name, the tools it listed, and a call that qualifies the
// tool with that name.
type mcpServer struct {
	// group is the client the launcher started, which owns the process.
	group *mcp.Group
	// name is the server's declared name.
	name string
}

// mcpServer implements the servers the tools over them read.
var _ callmcp.Server = (*mcpServer)(nil)

// Name is the server's declared name.
func (s *mcpServer) Name() string { return s.name }

// Tools returns the tools the server listed at startup.
func (s *mcpServer) Tools() []mcp.Tool {
	tools, _ := s.group.Tools(s.name)
	return tools
}

// CallTool calls one of them, qualifying the name the way the model writes it.
func (s *mcpServer) CallTool(ctx context.Context, tool string, args json.RawMessage) (mcp.CallResult, error) {
	return s.group.CallTool(ctx, s.name+"/"+tool, args)
}

// mcpServers keeps the servers a session started, as the tools over them read
// them.
//
// The settings hold the handles as their launcher returned them, and the tools
// over them are this binary's, so this is where the two meet: a handle that the
// tools cannot read is left out, and the launcher's own return type is pinned
// to what they read.
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

// linkedMCPLauncher returns the MCP launcher the command links.
//
// The launcher is the binary's one seam to the MCP client: the
// settings and the profiles hold the declarations and the selection,
// and this is what turns a selected server into a process a session
// owns. It introduces itself with the binary's own version, and it
// resolves a manifest's ${NAME} against the host environment, so an
// explicit reference may reintroduce a variable the session scrubbed —
// the operator's choice, and the one the launcher is told the scrub
// rule for.
func linkedMCPLauncher() config.MCPLauncher {
	version := buildVersion().String()
	hostEnv := exec.Lookup(os.Environ())
	return func(ctx context.Context, launch config.MCPLaunchSelection) (config.MCPRunning, error) {
		group, err := mcp.Launch(ctx, mcp.LaunchOptions{
			SessionID:     launch.SessionID,
			Dir:           launch.WorkingDir,
			HostEnv:       hostEnv,
			Scrub:         launch.Scrub,
			Servers:       launch.Servers,
			ClientVersion: version,
		})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(launch.Servers))
		for _, server := range launch.Servers {
			names = append(names, server.Name)
		}
		return &mcpSession{group: group, names: names}, nil
	}
}
