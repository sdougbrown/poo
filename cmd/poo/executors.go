package main

import (
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
)

// executorRegistry returns the executors the command links.
//
// The command links the local executor and the sandboxed one, so a
// session may ask for either. A name the registry does not hold is a
// configuration error: the command never runs a command through an
// executor it did not choose.
func executorRegistry() *bash.Registry {
	registry := bash.NewRegistry()
	registry.Register("local", func(exec.Options) (exec.Executor, error) {
		return bash.NewLocal(), nil
	})
	registry.Register("bwrap", func(opts exec.Options) (exec.Executor, error) {
		// The settings name the sandbox's own options, and the
		// protected paths come from the settings too, so the sandbox
		// masks the ones they name.
		return bwrap.New(bwrap.Options{
			DisableNetwork: opts.Sandbox.DisableNetwork,
			ROBinds:        opts.Sandbox.ROBinds,
			RWBinds:        opts.Sandbox.RWBinds,
			Masker:         opts.Masker,
		})
	})
	return registry
}
