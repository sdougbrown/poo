package main

import (
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/base"
	"github.com/sdougbrown/poo/profile/code"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/finish"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
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
		"read": func(config.ToolOptions) poo.Tool { return read.New() },
		"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
		"edit": func(config.ToolOptions) poo.Tool { return edit.New() },
		"call_cli": func(opts config.ToolOptions) poo.Tool {
			return cliregistry.NewCall(opts.Registry).WithSearch(opts.Search).WithExecutor(opts.Executor)
		},
		"find_tool": func(opts config.ToolOptions) poo.Tool {
			return cliregistry.NewFind(opts.Registry)
		},
	}
}
