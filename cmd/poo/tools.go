package main

import (
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/base"
	"github.com/sdougbrown/poo/profile/code"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

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
	}
}
