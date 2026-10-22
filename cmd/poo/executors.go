package main

import (
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
	registry.Register("local", func(bash.Options) (bash.Executor, error) {
		return bash.NewLocal(), nil
	})
	registry.Register("bwrap", func(opts bash.Options) (bash.Executor, error) {
		// The protected paths come from the settings, and the
		// sandbox masks the ones they name.
		return bwrap.New(bwrap.Options{Masker: opts.Masker})
	})
	return registry
}
