package main

import (
	"context"
	"os"

	"github.com/sdougbrown/poo"
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
			return cliregistry.NewFind(opts.Registry)
		},
	}
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
		return group, nil
	}
}
